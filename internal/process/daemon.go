// Package process handles OS-level process spawning, signal delivery, and stdin/stdout piping for daemons.
package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // intentional: pprof only exposed when EOS_PPROF_ADDR is set
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/monitor"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/ownership"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

type daemon struct {
	listener     net.Listener
	ctx          context.Context
	logger       *slog.Logger
	db           *database.DB
	mgr          *manager.LocalManager
	otelProvider *otelx.Provider
	otelHandles  *otelx.Handles
	stop         context.CancelFunc
	sigChan      chan os.Signal
	pidFile      string
	socketPath   string
}

// otelShutdownTimeout bounds how long daemon shutdown waits for the OTel SDK
// to flush pending spans/metrics to the collector. Mirrors sinkShutdownTimeout
// in internal/manager/sink_process.go, the same grace the OTLP logs sink uses
// to flush before the daemon's own shutdown deadline.
const otelShutdownTimeout = 3 * time.Second

// StandaloneDaemonStartOptions bundles the plain-value settings that shape
// how StartStandaloneDaemon boots: where it logs, how verbosely, the base
// data directory, and whether it's supervised by systemd. Grouped separately
// from the *config.* parameters, which stay as their own arguments since
// they're already-assembled config structs rather than loose scalars.
type StandaloneDaemonStartOptions struct {
	BaseDir             string
	LogToFileAndConsole bool
	Verbose             bool
	UnderSystemd        bool
}

func StartStandaloneDaemon(ctx context.Context, opts StandaloneDaemonStartOptions, standaloneDaemonConfig *config.StandaloneDaemonConfig, healthConfig *config.HealthConfig, shutdownConfig config.ShutdownConfig, telemetryConfig config.TelemetryConfig) error {
	d, err := newStandaloneDaemon(ctx, opts.LogToFileAndConsole, opts.Verbose, opts.BaseDir, standaloneDaemonConfig, shutdownConfig, telemetryConfig)
	if err != nil {
		return err
	}
	defer d.shutdown(ctx)

	if addr := os.Getenv("EOS_PPROF_ADDR"); addr != "" {
		go func() { _ = http.ListenAndServe(addr, nil) }() //nolint:gosec // addr is operator-controlled via env var
	}

	reconcileCtx, reconcileSpan := d.otelHandles.Tracer.Start(ctx, "eos.daemon.reconcile_orphans")
	reconcileOrphans(reconcileCtx, d.db, d.logger)
	reconcileSpan.End()

	// Bring the health monitor up before recovering persisted services: the
	// monitor is what advances a service to Running, the readiness signal a
	// dependent's boot gate waits on. Recover after it, or a dependency could
	// never be observed ready and every dependent would stall to max_wait.
	d.serve(healthConfig, shutdownConfig)

	if opts.UnderSystemd {
		if err := d.recover(); err != nil {
			return err
		}
	}

	d.logger.Info("daemon started successfully")

	d.wait()
	return nil
}

func bootPersistedServices(ctx context.Context, mgr *manager.LocalManager, logger *slog.Logger) error {
	allRegisteredServices, err := mgr.GetAllServiceCatalogEntries(ctx)
	if err != nil {
		errorMessage := fmt.Errorf("getting all service catalog entries: %w", err)
		logger.Info(errorMessage.Error())
		return errorMessage
	}

	// Boot each service in its own goroutine so a dependent blocked on its
	// depends_on can't hold up an independent service — or the very dependency
	// it's waiting for — from starting. This keeps boot order-independent: any
	// catalog order converges, and a cycle or unmet dependency fails loud after
	// its own max_wait instead of wedging the whole boot on a fixed sequence.
	var wg sync.WaitGroup
	for _, service := range allRegisteredServices {
		wg.Add(1)
		go func(entry types.ServiceCatalogEntry) {
			defer wg.Done()
			bootService(ctx, mgr, logger, entry)
		}(service)
	}
	wg.Wait()
	return nil
}

// bootService gates one persisted service on its declared dependencies, then
// starts it. Dependency-wait and start failures are logged and swallowed: one
// service failing to come up must not abort the daemon or the other services'
// boot, matching the pre-ordering "log and continue" behavior.
func bootService(ctx context.Context, mgr *manager.LocalManager, logger *slog.Logger, entry types.ServiceCatalogEntry) {
	logger.Debug("booting persisted service", "service", entry.Name)

	cfg, err := manager.LoadServiceConfig(filepath.Join(entry.DirectoryPath, entry.ConfigFileName))
	if err != nil {
		logger.Info(fmt.Errorf("boot: loading service config for %s: %w", entry.Name, err).Error())
		return
	}

	if len(cfg.DependsOn) > 0 {
		maxWait, waitErr := manager.ParseMaxWait(cfg.MaxWait)
		if waitErr != nil {
			logger.Info(fmt.Errorf("boot: %s: %w", entry.Name, waitErr).Error())
			return
		}
		depErr := manager.RecordDependencyWait(ctx, mgr, mgr, entry.Name, cfg.DependsOn, maxWait)
		if depErr != nil {
			logger.Info(fmt.Errorf("boot: %w", depErr).Error())
			return
		}
	}

	if _, startErr := mgr.StartService(ctx, entry.Name); startErr != nil {
		logger.Info(fmt.Errorf("starting service: %w", startErr).Error())
	}
}

// reconcileOrphans runs once at daemon startup and checks every known PGID
// for every service against the real OS process table, regardless of what
// the DB's last-known state for that row says. A row recorded Stopped/Failed
// can still point at a live process (e.g. a SIGCHLD race lost the real exit
// event), and a row recorded Running/Starting can point at a process that's
// actually dead (e.g. after an out-of-band kill or crash) — both cases are
// corrected here instead of trusting the single most-recent row's state.
//
// It also clears every recorded dependency_waits row: any wait still on disk
// belonged to a goroutine (see manager.RecordDependencyWait) from the
// previous daemon process, which no longer exists now that this one is just
// starting — so every such row is guaranteed stale, rather than waiting out
// manager.DependencyWaitStaleAfter for GetDependencyWaitStatus's own
// self-heal to catch up.
func reconcileOrphans(ctx context.Context, db *database.DB, logger *slog.Logger) {
	if clearErr := db.ClearAllDependencyWaits(ctx); clearErr != nil {
		logger.Error("reconcile orphans: clearing stale dependency waits", "error", clearErr)
	}

	entries, err := db.GetAllServiceCatalogEntries(ctx)
	if err != nil {
		logger.Error("reconcile orphans: listing catalog", "error", err)
		return
	}

	for _, entry := range entries {
		history, err := db.GetProcessHistoryEntriesByServiceName(ctx, entry.Name)
		if err != nil {
			logger.Error("reconcile orphans: fetching history", "service", entry.Name, "error", err)
			continue
		}

		for i := range history {
			hist := &history[i]
			if hist.PGID <= 0 {
				continue
			}

			// Only kill a PGID we can positively confirm is still the same
			// process eos recorded. IsAlive alone is unsafe: the kernel
			// recycles PGID numbers, so a live PGID may now belong to an
			// unrelated process. IsAliveMatching also compares the recorded
			// start time, ruling out that collision. A non-positive
			// StartedAtTicks means we never captured a verifiable start time,
			// so treat it as not-ours and never kill on an unverifiable match.
			if hist.StartedAtTicks > 0 && procutil.IsAliveMatching(hist.PGID, hist.StartedAtTicks) {
				if killErr := syscall.Kill(-hist.PGID, syscall.SIGKILL); killErr != nil {
					logger.Info("reconcile orphans: kill PGID", "service", entry.Name, "pgid", hist.PGID, "error", killErr)
				}
				reconcileMarkStopped(ctx, db, logger, entry.Name, hist.PGID)
				continue
			}

			// Reached here either because the PGID is gone, or because it's
			// alive but its start time no longer matches — a recycled PGID
			// belonging to some other process. In both cases our recorded
			// process is dead, so reconcile a still-live-looking row to
			// Stopped without sending any signal to a PGID that isn't ours.
			switch hist.State {
			case types.ProcessStateRunning, types.ProcessStateStarting, types.ProcessStateUnknown:
				reconcileMarkStopped(ctx, db, logger, entry.Name, hist.PGID)
			case types.ProcessStateStopped, types.ProcessStateFailed:
				// already terminal and our process confirmed dead — no-op
			}
		}
	}
}

func reconcileMarkStopped(ctx context.Context, db *database.DB, logger *slog.Logger, serviceName string, pgid int) {
	now := time.Now()
	if updateErr := db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateStopped),
		StoppedAt: &now,
	}); updateErr != nil {
		logger.Error("reconcile orphans: updating state", "service", serviceName, "error", updateErr)
	} else {
		logger.Info("reconcile orphans: orphan stopped", "pgid", pgid, "service", serviceName)
	}
}

// shutdown takes the pre-signal-handling ctx (StartStandaloneDaemon's own
// parameter, not d.ctx) so the telemetry flush gets a fresh deadline: d.ctx
// is already Done() by the time shutdown runs, since it's the context
// signal.NotifyContext canceled on SIGTERM/SIGINT.
//
// d.stop() cancels d.ctx, which is also the context every service process was
// launched under (exec.CommandContext(d.ctx, ...) in buildLaunchCommand). That
// same cancellation is what triggers each process's graceful stop: the
// LocalManager built for this daemon is configured with a non-zero
// shutdownGracePeriod (see WithShutdownGracePeriod), so every launched cmd.Cmd
// has cmd.Cancel set to SIGTERM the process group and cmd.WaitDelay set to the
// grace period — Go's exec runtime enforces signal-then-wait-then-kill on
// context cancellation independent of this function, with no DB/manager call
// needed here (issue #93: d.mgr's own ctx is this same d.ctx, so any manager
// call made after d.stop() would itself see a canceled context and fail).
//
// d.mgr.WaitServices() then blocks until that has actually happened: canceling
// a context only closes a channel, it does not itself run anything, so without
// waiting here this function could return — and the whole process exit before
// the goroutine watching d.ctx even gets scheduled to invoke cmd.Cancel,
// silently skipping the SIGTERM-then-wait sequence entirely.
func (d *daemon) shutdown(ctx context.Context) {
	d.stop()
	d.mgr.WaitServices()
	if err := d.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		d.logger.Error("closing listener", "error", err)
	}
	if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
		d.logger.Error("removing pid file", "error", err)
	}
	if err := os.Remove(d.socketPath); err != nil && !os.IsNotExist(err) {
		d.logger.Error("removing socket", "error", err)
	}
	if err := d.db.CloseDBConnection(); err != nil {
		d.logger.Error("failed to close database", "error", err)
	}
	otelCtx, otelCancel := context.WithTimeout(ctx, otelShutdownTimeout)
	defer otelCancel()
	if err := d.otelProvider.Shutdown(otelCtx); err != nil {
		d.logger.Error("flushing telemetry on shutdown", "error", err)
	}
}

func (d *daemon) recover() error {
	return bootPersistedServices(d.ctx, d.mgr, d.logger)
}

func (d *daemon) serve(healthConfig *config.HealthConfig, shutdownConfig config.ShutdownConfig) {
	//nolint:gosec // G115: os.Getuid() is never negative on the POSIX platforms eos targets (linux, darwin)
	go handleIncomingCommands(d.ctx, d.listener, d.mgr, d.logger, uint32(os.Getuid()))

	healthMonitor := monitor.NewHealthMonitor(d.mgr, d.db, d.logger, healthConfig, shutdownConfig, d.otelHandles)
	go healthMonitor.Start(d.ctx)
}

// reclaimStalePIDFile clears a leftover PID file whose daemon has since died.
// It errors if a live daemon still owns the file.
func reclaimStalePIDFile(pidFile string, logger *slog.Logger) error {
	if _, pidFileStatErr := os.Stat(pidFile); pidFileStatErr == nil {
		data, _ := os.ReadFile(pidFile) // #nosec G304 -- path sanitized in config.NewDaemonConfig
		oldPid, _ := strconv.Atoi(string(data))

		if process, findProcessErr := os.FindProcess(oldPid); findProcessErr == nil {
			if process.Signal(syscall.Signal(0)) == nil {
				errorMessage := fmt.Errorf("daemon already running with PID %d", oldPid)
				logger.Info(errorMessage.Error())
				return errorMessage
			}
		}
		if pidRemoveErr := os.Remove(pidFile); pidRemoveErr != nil {
			errorMessage := fmt.Errorf("unable to remove the pid file, got: %w", pidRemoveErr)
			logger.Error(errorMessage.Error())
			return errorMessage
		}
	}
	return nil
}

// maxUnixSocketPathLen is the size of the kernel's sockaddr_un.sun_path field on
// this platform (104 bytes on macOS/Darwin, 108 on Linux). A bound socket path
// must fit within this field including a trailing NUL, so the usable path length
// is one byte less.
const maxUnixSocketPathLen = len(syscall.RawSockaddrUnix{}.Path)

// validateSocketPathLength returns an actionable error when socketPath is too
// long to bind as a Unix domain socket on this platform. bind(2) rejects an
// over-long path with EINVAL, which otherwise surfaces only as a cryptic
// "bind: invalid argument" with no hint that the path length is the cause.
func validateSocketPathLength(socketPath string) error {
	maxLen := maxUnixSocketPathLen - 1 // leave room for the trailing NUL
	if len(socketPath) > maxLen {
		return fmt.Errorf(
			"socket path %q is %d bytes, exceeding the %d-byte maximum for a Unix domain socket on %s; "+
				"set EOS_BASE_DIR to a shorter directory (the socket path is EOS_BASE_DIR + %q)",
			socketPath, len(socketPath), maxLen, runtime.GOOS,
			string(os.PathSeparator)+config.DaemonSocketPath)
	}
	return nil
}

// removeExistingSocket clears a leftover unix socket file so Listen can rebind.
func removeExistingSocket(socketPath string, logger *slog.Logger) error {
	if _, socketPathStatErr := os.Stat(socketPath); socketPathStatErr == nil {
		if socketPathRemoveErr := os.Remove(socketPath); socketPathRemoveErr != nil {
			errorMessage := fmt.Errorf("unable to remove the socket, got: %w", socketPathRemoveErr)
			logger.Error(errorMessage.Error())
			return errorMessage
		}
	}
	return nil
}

func newStandaloneDaemon(ctx context.Context, logToFileAndConsole bool, verbose bool, baseDir string, standaloneDaemonConfig *config.StandaloneDaemonConfig, shutdownConfig config.ShutdownConfig, telemetryConfig config.TelemetryConfig) (*daemon, error) {
	startedAt := time.Now()

	logger, err := manager.NewDaemonLogger(baseDir, logToFileAndConsole, verbose, standaloneDaemonConfig.Log.LogDir, standaloneDaemonConfig.Log.LogFileName, standaloneDaemonConfig.Log.LogMaxFiles, config.DaemonLogFileSizeLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to setup daemon logger: %w", err)
	}

	logger.Info("daemon logger started")
	pidFile := standaloneDaemonConfig.PIDFile
	socketPath := standaloneDaemonConfig.SocketPath

	// Validate the socket path length before writing the pidfile or binding the
	// socket, so an over-long EOS_BASE_DIR yields a clear error instead of a
	// cryptic "bind: invalid argument" and leaves no stale pidfile behind.
	if socketPathErr := validateSocketPathLength(socketPath); socketPathErr != nil {
		logger.Info(socketPathErr.Error())
		return nil, socketPathErr
	}

	if reclaimErr := reclaimStalePIDFile(pidFile, logger); reclaimErr != nil {
		return nil, reclaimErr
	}

	myPID := os.Getpid()
	err = os.WriteFile(pidFile, fmt.Appendf(nil, "%d", myPID), 0600)
	if err != nil {
		errorMessage := fmt.Errorf("failed to write to pid file: %w", err)
		logger.Info(errorMessage.Error())
		return nil, errorMessage
	}
	// Under sudo the daemon (re)starts as root even though the base dir was
	// already chowned to the invoking user; align the freshly written PID file
	// to that owner so an unprivileged `eos status` isn't locked out afterward.
	// See issue #91.
	if alignErr := ownership.Align(baseDir, pidFile); alignErr != nil {
		errorMessage := fmt.Errorf("failed to align pid file ownership: %w", alignErr)
		logger.Info(errorMessage.Error())
		return nil, errorMessage
	}
	logger.Debug("PID written", "path", pidFile, "pid", myPID)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGCHLD)

	if removeErr := removeExistingSocket(socketPath, logger); removeErr != nil {
		return nil, removeErr
	}

	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		errorMessage := fmt.Errorf("failed to create socket: %w", err)
		logger.Info(errorMessage.Error())
		return nil, errorMessage
	}
	// bind(2) creates the socket file with a mode derived from umask, which
	// can leave it group- or world-accessible. The peer uid check in
	// handleConnection is the real gate, but pinning the mode to owner-only
	// keeps ls -l honest about who can even attempt to connect.
	if chmodErr := os.Chmod(socketPath, 0600); chmodErr != nil {
		errorMessage := fmt.Errorf("failed to set socket permissions: %w", chmodErr)
		logger.Info(errorMessage.Error())
		return nil, errorMessage
	}
	logger.Debug("socket listening", "path", socketPath)

	db, err := database.NewDB(ctx, baseDir)
	if err != nil {
		errorMessage := fmt.Errorf("failed to connect to database: %w", err)
		logger.Info(errorMessage.Error())
		return nil, errorMessage
	}
	logger.Debug("database connected")

	tel, err := setupDaemonTelemetry(ctx, telemetryConfig, shutdownConfig, db, baseDir, logger, startedAt)
	if err != nil {
		return nil, err
	}

	return &daemon{
		logger:       logger,
		db:           db,
		mgr:          tel.mgr,
		otelProvider: tel.provider,
		otelHandles:  tel.handles,
		listener:     listener,
		ctx:          ctx,
		stop:         stop,
		sigChan:      sigChan,
		pidFile:      pidFile,
		socketPath:   socketPath,
	}, nil
}

// daemonTelemetry bundles the pieces setupDaemonTelemetry assembles: the
// OTel provider (real or no-op), the instrument handles built from it, and
// the LocalManager wired to record through them.
type daemonTelemetry struct {
	provider *otelx.Provider
	handles  *otelx.Handles
	mgr      *manager.LocalManager
}

// setupDaemonTelemetry builds the daemon's OTel provider, its instrument
// handles, the LocalManager wired to them, and registers the daemon-level
// observable gauges (uptime, services registered/running).
//
// Telemetry export is an add-on to process supervision, not a prerequisite
// for it: a misconfigured collector shouldn't stop the daemon from managing
// services, so a construction failure on the real provider falls back to the
// disabled (no-op) one — cfg.Enable false, which otelx.NewProvider never
// errors on — rather than failing daemon startup.
func setupDaemonTelemetry(ctx context.Context, telemetryConfig config.TelemetryConfig, shutdownConfig config.ShutdownConfig, db *database.DB, baseDir string, logger *slog.Logger, startedAt time.Time) (daemonTelemetry, error) {
	otelx.SetErrorHandler(logger)

	otelProvider, err := otelx.NewProvider(ctx, otelx.Config{
		Enable:   telemetryConfig.Enable,
		Endpoint: telemetryConfig.Endpoint,
		Insecure: telemetryConfig.Insecure,
	}, "eos", buildinfo.Version)
	if err != nil {
		logger.Error("telemetry setup failed, continuing without it", "error", err)
		otelProvider, err = otelx.NewProvider(ctx, otelx.Config{}, "eos", buildinfo.Version)
		if err != nil {
			return daemonTelemetry{}, fmt.Errorf("failed to set up fallback telemetry provider: %w", err)
		}
	}

	otelHandles, err := otelx.NewHandles(otelProvider.TracerProvider, otelProvider.MeterProvider)
	if err != nil {
		return daemonTelemetry{}, fmt.Errorf("failed to set up telemetry instruments: %w", err)
	}

	mgr := manager.NewLocalManager(db, baseDir, ctx, logger, manager.WithTelemetry(otelHandles), manager.WithShutdownGracePeriod(shutdownConfig.GracePeriod))

	if regErr := otelx.RegisterDaemonGauges(otelProvider.MeterProvider, startedAt,
		func(gaugeCtx context.Context) int { return len(catalogEntriesOrEmpty(gaugeCtx, mgr, logger)) },
		func(gaugeCtx context.Context) int { return len(serviceInstancesOrEmpty(gaugeCtx, mgr, logger)) },
	); regErr != nil {
		logger.Error("registering daemon telemetry gauges", "error", regErr)
	}

	return daemonTelemetry{provider: otelProvider, handles: otelHandles, mgr: mgr}, nil
}

// catalogEntriesOrEmpty and serviceInstancesOrEmpty back the daemon-level
// registered/running gauge callbacks (see otelx.RegisterDaemonGauges): the
// SDK polls these on its own export interval, well off the hot path, so a
// query failure there is worth logging but never worth surfacing to the
// exporter callback's own error return.
func catalogEntriesOrEmpty(ctx context.Context, mgr *manager.LocalManager, logger *slog.Logger) []types.ServiceCatalogEntry {
	entries, err := mgr.GetAllServiceCatalogEntries(ctx)
	if err != nil {
		logger.Debug("telemetry: listing service catalog", "error", err)
		return nil
	}
	return entries
}

func serviceInstancesOrEmpty(ctx context.Context, mgr *manager.LocalManager, logger *slog.Logger) []types.ServiceInstance {
	instances, err := mgr.GetAllServiceInstances(ctx)
	if err != nil {
		logger.Debug("telemetry: listing service instances", "error", err)
		return nil
	}
	return instances
}

func (d *daemon) wait() {
	for {
		select {
		case sig := <-d.sigChan:
			if sig == syscall.SIGCHLD {
				handleSIGCHLDRequest(d.ctx, d.db, d.logger)
			}
		case <-d.ctx.Done():
			return
		}
	}
}

// reapAction tells handleSIGCHLDRequest whether to keep draining exited children
// or stop for this SIGCHLD.
type reapAction int

const (
	reapStop reapAction = iota
	reapContinue
)

// pgroupStillAlive reports whether the reaped PID's process group still has live
// members. The reaped PID may be the group leader (shell) while service
// processes keep running in the same group; in that case the health monitor,
// not the reaper, owns the liveness state.
func pgroupStillAlive(pid int, logger *slog.Logger) bool {
	if pid > 1 && syscall.Kill(-pid, 0) == nil {
		logger.Info(fmt.Sprintf("reaped process %d but process group still alive, skipping state update\n", pid))
		return true
	}
	return false
}

// recordReapedExit writes the terminal process-history state for a reaped PID:
// Stopped on a clean exit, Failed otherwise.
func recordReapedExit(ctx context.Context, db *database.DB, logger *slog.Logger, pid int, status syscall.WaitStatus) {
	updates := database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateStopped),
		StoppedAt: new(time.Now()),
	}
	if status.ExitStatus() != 0 {
		updates.State = new(types.ProcessStateFailed)
		updates.Error = new("Zombie process has been reaped")
	}
	if updateErr := db.UpdateProcessHistoryEntry(ctx, pid, updates); updateErr != nil {
		logger.Error("updating reaped process in database", "error", updateErr)
	}
}

// handleReapedChild processes one Wait4 result, returning reapStop when the
// drain loop should end.
func handleReapedChild(ctx context.Context, db *database.DB, logger *slog.Logger, pid int, waitErr error, status syscall.WaitStatus) reapAction {
	if waitErr != nil {
		if errors.Is(waitErr, syscall.ECHILD) {
			// No children left to reap: the expected terminal condition of this
			// drain loop, not a failure (e.g. the explicit stop path already
			// reaped everything before SIGCHLD delivery for this cycle).
			logger.Debug("reap loop drained: no child processes", "pid", pid)
			return reapStop
		}
		logger.Error("cleaning up child process", "pid", pid, "error", waitErr)
		return reapStop
	}
	if pid == 0 {
		return reapStop
	}
	if pid < 0 {
		logger.Error("cleaning up child process", "pid", pid)
		return reapContinue
	}

	logger.Info(fmt.Sprintf("reaped zombie process: %d\n", pid))
	if pgroupStillAlive(pid, logger) {
		return reapContinue
	}

	recordReapedExit(ctx, db, logger, pid, status)
	return reapContinue
}

func handleSIGCHLDRequest(ctx context.Context, db *database.DB, logger *slog.Logger) {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if handleReapedChild(ctx, db, logger, pid, err, status) == reapStop {
			break
		}
	}
}

// stopPollInterval and stopExitTimeout bound how long StopStandaloneDaemon
// waits for a SIGTERM'd daemon to actually exit before reporting killed=true.
// Returning immediately after signaling let callers (e.g. restartDaemonAfterUpdate
// in cmd/system.go) call Start() while the old process was still mid-shutdown,
// tripping its "already running" guard and leaving no daemon running at all
// after a successful update (issue #73).
//
// stopExitTimeout must comfortably cover the daemon's own worst-case shutdown
// latency, not just the time to deliver the initial SIGTERM: (*daemon).shutdown
// now waits for every running service's configured ShutdownConfig.GracePeriod
// (issue #93) before tearing down the rest (listener/pidfile/socket/db, then
// up to otelShutdownTimeout to flush telemetry). At the config default
// (GracePeriod 5s + otelShutdownTimeout 3s), a too-short stopExitTimeout would
// make this function report a spurious failure for a daemon that's still
// correctly, if slowly, shutting down.
const (
	stopPollInterval = 50 * time.Millisecond
	stopExitTimeout  = 15 * time.Second
)

// waitForProcessExit polls process with signal 0 until it reports
// os.ErrProcessDone or timeout elapses.
func waitForProcessExit(process *os.Process, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := process.Signal(syscall.Signal(0))
		if err != nil && errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		time.Sleep(stopPollInterval)
	}
	return fmt.Errorf("daemon (pid %d) did not exit within %s of SIGTERM", process.Pid, timeout)
}

func StopStandaloneDaemon(pidFile, socketPath string) (bool, error) {
	_, err := os.Stat(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting stat info on pid of daemon: %w", err)
	}

	_, err = os.Stat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting stat info socket of daemon: %w", err)
	}

	data, readPidErr := os.ReadFile(pidFile) // #nosec G304 -- path sanitized in config.NewDaemonConfig
	if readPidErr != nil {
		return false, fmt.Errorf("reading pid file: %w", readPidErr)
	}

	activePid, err := strconv.Atoi(string(data))
	if err != nil {
		return false, fmt.Errorf("converting pid data to int: %w", err)
	}

	process, err := os.FindProcess(activePid)
	if err != nil {
		return false, fmt.Errorf("finding process matching the pid: %w", err)
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return false, nil
		}
		return false, fmt.Errorf("checking active daemon: %w", err)
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		return false, fmt.Errorf("killing active daemon: %w", err)
	}

	if err := waitForProcessExit(process, stopExitTimeout); err != nil {
		return true, err
	}

	return true, nil
}

type DaemonStatus struct {
	Pid     *int
	Process *os.Process
	Running bool
}

func StatusStandaloneDaemon(daemonConfig *config.StandaloneDaemonConfig) (*DaemonStatus, error) {
	pidFile := filepath.Clean(daemonConfig.PIDFile)

	_, err := os.Stat(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &DaemonStatus{
				Running: false,
				Pid:     nil,
				Process: nil,
			}, nil
		}
		return nil, fmt.Errorf("describing pid file: %w", err)
	}

	data, readPidErr := os.ReadFile(pidFile)
	if readPidErr != nil {
		return nil, fmt.Errorf("reading pid file: %w", readPidErr)
	}

	activePid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("converting pid data to string: %w", err)
	}

	process, err := os.FindProcess(activePid)
	if err != nil {
		return nil, fmt.Errorf("finding process matching the pid: %w", err)
	}

	if err := process.Signal(syscall.Signal(0)); err != nil {
		// signal(0) failing means the process isn't alive - this is not an error
		// in the function's own operation, so we return a valid status with nil error
		return &DaemonStatus{Running: false, Pid: &activePid, Process: nil}, nil //lint:ignore nilerr intentional
	}

	return &DaemonStatus{
		Running: true,
		Pid:     &activePid,
		Process: process,
	}, nil
}

// DaemonSummary describes one user's standalone daemon, as discovered by DiscoverDaemons.
type DaemonSummary struct {
	Status      *DaemonStatus
	Err         error
	Username    string
	PIDFile     string
	StaleBinary bool
}

// DiscoverDaemons scans /home/*/.eos and /root/.eos for standalone daemon PID files and
// reports the status of each. It is Linux-only: it relies on /home as the convention for
// user home directories and /proc/<pid>/exe to detect a daemon still running against a
// binary that has since been replaced on disk (see issue #98 — the "who's still on the
// old binary after an update" gap). Requires root to observe other users' processes.
func DiscoverDaemons() ([]DaemonSummary, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("discovering daemons across users is only supported on linux")
	}

	homeDirs, err := candidateHomeDirs()
	if err != nil {
		return nil, err
	}

	return discoverDaemonsIn(homeDirs, currentExecutableInode()), nil
}

// readHomeDirs lists the entries under /home as full paths. A missing /home
// yields an empty list; other read errors are surfaced. Non-directory entries
// are harmless — discoverDaemonsIn skips any whose .eos dir doesn't stat.
func readHomeDirs() ([]string, error) {
	entries, err := os.ReadDir("/home")
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading /home: %w", err)
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		dirs = append(dirs, filepath.Join("/home", e.Name()))
	}
	return dirs, nil
}

// candidateHomeDirs returns the per-user home directories (plus /root) to scan
// for standalone daemon PID files.
func candidateHomeDirs() ([]string, error) {
	homeDirs, err := readHomeDirs()
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat("/root"); statErr == nil {
		homeDirs = append(homeDirs, "/root")
	}
	return homeDirs, nil
}

// discoverDaemonsIn is the testable core of DiscoverDaemons: given a set of candidate home
// directories and the inode of the currently installed binary, it reports the standalone
// daemon status found under each home's .eos directory, if any.
func discoverDaemonsIn(homeDirs []string, currentIno uint64) []DaemonSummary {
	var summaries []DaemonSummary
	for _, home := range homeDirs {
		baseDir := filepath.Join(home, "."+config.Name)
		if _, err := os.Stat(baseDir); err != nil {
			continue
		}

		pidFile := filepath.Join(baseDir, config.DaemonPIDFile)
		status, statusErr := StatusStandaloneDaemon(&config.StandaloneDaemonConfig{PIDFile: pidFile})
		summary := DaemonSummary{
			Username: filepath.Base(home),
			PIDFile:  pidFile,
			Status:   status,
			Err:      statusErr,
		}
		if statusErr == nil && status.Running && status.Pid != nil && currentIno != 0 {
			summary.StaleBinary = runningExeInode(*status.Pid) != currentIno
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Username < summaries[j].Username })
	return summaries
}

// currentExecutableInode returns the inode of the currently running eos binary, or 0 if
// it can't be determined.
func currentExecutableInode() uint64 {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	return inodeOf(exe)
}

// runningExeInode returns the inode backing pid's executable, resolved via /proc/<pid>/exe.
// This magic symlink stats the original inode a process exec'd, even after that path has
// been renamed or overwritten on disk — so it differs from currentExecutableInode() exactly
// when the process is still running the pre-update binary.
func runningExeInode(pid int) uint64 {
	return inodeOf(fmt.Sprintf("/proc/%d/exe", pid))
}

func inodeOf(path string) uint64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return sys.Ino
}

func RemoveStandaloneDaemon(daemonConfig *config.StandaloneDaemonConfig) (bool, error) {
	status, err := StatusStandaloneDaemon(daemonConfig)
	if err != nil {
		return false, err
	}
	if status.Running {
		return false, fmt.Errorf("standalone daemon is running; stop it before removing daemon files")
	}

	pidFile := daemonConfig.PIDFile
	socketPath := daemonConfig.SocketPath

	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("removing pid file: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("removing socket file: %w", err)
	}

	return true, nil
}

func handleIncomingCommands(ctx context.Context, listener net.Listener, mgr manager.ServiceManager, logger *slog.Logger, allowedUID uint32) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logger.Info("listener closed, shutting down gracefully")
				return
			}
			logger.Error("accepting the connection", "error", err)
			return
		}

		go handleConnection(ctx, conn, mgr, logger, allowedUID)
	}
}

// isAuthorizedPeer reports whether gotUID, the peer credential read off the
// connecting socket, is allowed to issue commands to a daemon owned by
// allowedUID. The base dir's 0750 mode alone still admits every member of
// the daemon owner's group, so this check — not file permissions — is what
// keeps another local user off the control socket.
//
// Root (uid 0) is always authorized regardless of allowedUID. eos's own
// privilege-drop (cmd/daemon.go's SysProcAttr.Credential) means a daemon
// started under sudo runs with allowedUID resolved down to the invoking
// user, not 0 — so a later `sudo eos <command>` (a supported pattern, see
// the SUDO_USER handling in internal/config/config.go) connects as raw root
// and would otherwise never match. Root already has unconditional
// filesystem/process access to everything the daemon owns regardless of
// this check, so refusing it here would block legitimate use without
// stopping any real attacker — the same reasoning as ownership.Align's
// root no-op.
func isAuthorizedPeer(gotUID, allowedUID uint32) bool {
	return gotUID == allowedUID || gotUID == 0
}

func handleConnection(ctx context.Context, conn net.Conn, mgr manager.ServiceManager, logger *slog.Logger, allowedUID uint32) {
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("closing daemon socket", "error", err)
		}
	}()

	gotUID, err := peerUID(conn)
	if err != nil {
		logger.Error("reading socket peer credentials", "error", err)
		sendErrorResponse(conn, "peer credential check failed", logger)
		return
	}
	if !isAuthorizedPeer(gotUID, allowedUID) {
		logger.Warn("rejecting connection from unauthorized peer", "peer_uid", gotUID)
		sendErrorResponse(conn, "unauthorized", logger)
		return
	}

	var request types.DaemonRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		sendErrorResponse(conn, fmt.Sprintf("decoding request: %v", err), logger)
		return
	}

	response := executeRequest(ctx, mgr, request)

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		logClientWriteError(logger, "sending response", err)
	}
}

// isClientDisconnect reports whether err reflects the client having gone away
// mid-write (broken pipe, reset connection, or an already-closed listener)
// rather than a daemon-side failure. A liveness probe that dials the socket
// and closes immediately (cmd/daemon_liveness.go's socketResponds) produces
// exactly this: the daemon's write to the now-closed conn fails, which is the
// expected outcome of a bare connectivity check, not an error worth alerting on.
func isClientDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}

// logClientWriteError logs a failed write to a client connection at the
// severity the cause warrants: Debug for a routine client hangup, Error for
// anything else.
func logClientWriteError(logger *slog.Logger, msg string, err error) {
	if isClientDisconnect(err) {
		logger.Debug(msg, "error", err)
		return
	}
	logger.Error(msg, "error", err)
}

// executeRequest dispatches a decoded daemon IPC request to the handler for
// its method. Each case below is a thin one-line call into a handleX
// function that owns the args-unmarshal / manager-call / response-marshal
// sequence for that method; this function's only job is routing.
// requestHandler is the shape every executeRequest dispatch target shares.
// Handlers that need no args (GetVersion, GetAllServiceInstances,
// GetAllServiceCatalogEntries) are wrapped below to ignore the unused
// rawArgs parameter, so every entry in requestHandlers has one uniform type.
type requestHandler func(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse

// requestHandlers routes each MethodName to its handler as a lookup table
// rather than a switch: a switch this wide (types.ValidMethods keeps
// growing) drives up executeRequest's own cyclomatic complexity with every
// method added, regardless of how simple the dispatch itself is — a map
// lookup stays O(1) complexity no matter how many methods exist.
var requestHandlers = map[types.MethodName]requestHandler{
	types.MethodGetAllServiceInstances: func(ctx context.Context, mgr manager.ServiceManager, _ json.RawMessage) types.DaemonResponse {
		return handleGetAllServiceInstances(ctx, mgr)
	},
	types.MethodGetServiceInstance:     handleGetServiceInstance,
	types.MethodRemoveServiceInstance:  handleRemoveServiceInstance,
	types.MethodStartService:           handleStartService,
	types.MethodRestartService:         handleRestartService,
	types.MethodStopService:            handleStopService,
	types.MethodForceStopService:       handleForceStopService,
	types.MethodReloadService:          handleReloadService,
	types.MethodAddServiceCatalogEntry: handleAddServiceCatalogEntry,
	types.MethodGetAllServiceCatalogEntries: func(ctx context.Context, mgr manager.ServiceManager, _ json.RawMessage) types.DaemonResponse {
		return handleGetAllServiceCatalogEntries(ctx, mgr)
	},
	types.MethodGetServiceCatalogEntry:           handleGetServiceCatalogEntry,
	types.MethodIsServiceRegistered:              handleIsServiceRegistered,
	types.MethodRemoveServiceCatalogEntry:        handleRemoveServiceCatalogEntry,
	types.MethodUpdateServiceCatalogEntry:        handleUpdateServiceCatalogEntry,
	types.MethodGetMostRecentProcessHistoryEntry: handleGetMostRecentProcessHistoryEntry,
	types.MethodSetDependencyWaitStatus:          handleSetDependencyWaitStatus,
	types.MethodClearDependencyWaitStatus:        handleClearDependencyWaitStatus,
	types.MethodGetDependencyWaitStatus:          handleGetDependencyWaitStatus,
	types.MethodNewServiceLogFiles:               handleNewServiceLogFiles,
	types.MethodGetServiceLogFilePath:            handleGetServiceLogFilePath,
	types.MethodGetVersion: func(ctx context.Context, mgr manager.ServiceManager, _ json.RawMessage) types.DaemonResponse {
		return handleGetVersion(ctx, mgr)
	},
}

func executeRequest(ctx context.Context, mgr manager.ServiceManager, request types.DaemonRequest) types.DaemonResponse {
	handler, ok := requestHandlers[request.Method]
	if !ok {
		return errorResponse(fmt.Sprintf("unknown method: %s", request.Method))
	}
	return handler(ctx, mgr, request.Args)
}

func handleGetVersion(ctx context.Context, mgr manager.ServiceManager) types.DaemonResponse {
	version, err := mgr.GetVersion(ctx)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(version)
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleGetAllServiceInstances(ctx context.Context, mgr manager.ServiceManager) types.DaemonResponse {
	result, err := mgr.GetAllServiceInstances(ctx)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	if result == nil {
		result = []types.ServiceInstance{}
	}
	data, err := json.Marshal(types.GetAllServiceInstancesResponse{
		Instances: result,
	})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleGetServiceInstance(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetServiceInstanceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetServiceInstance args: %v", err))
	}

	result, err := mgr.GetServiceInstance(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	if result == nil {
		return errorResponse("result returned nil")
	}
	data, err := json.Marshal(types.GetServiceInstanceResponse{
		Instance: *result,
	})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleRemoveServiceInstance(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.RemoveServiceInstanceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodRemoveServiceInstance args: %v", err))
	}
	removed, err := mgr.RemoveServiceInstance(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]bool{"removed": removed})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{Success: true, Data: data}
}

func handleStartService(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.StartServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodStartService args: %v", err))
	}
	pid, err := mgr.StartService(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]int{"pid": pid})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleRestartService(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.RestartServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse("invalid MethodRestartService args")
	}
	gracePeriod, err := time.ParseDuration(args.GracePeriod)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid grace period: %s", args.GracePeriod))
	}
	tickerPeriod, err := time.ParseDuration(args.TickerPeriod)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid ticker period: %s", args.TickerPeriod))
	}
	pid, err := mgr.RestartService(ctx, args.Name, gracePeriod, tickerPeriod)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]int{"pid": pid})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleStopService(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.StopServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse("invalid MethodStopService args")
	}
	gracePeriod, err := time.ParseDuration(args.GracePeriod)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid grace period: %s", args.GracePeriod))
	}
	tickerPeriod, err := time.ParseDuration(args.TickerPeriod)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid ticker period: %s", args.TickerPeriod))
	}
	result, err := mgr.StopService(ctx, args.Name, gracePeriod, tickerPeriod)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(result)

	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleForceStopService(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.ForceStopServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodForceStopService args: %v", err))
	}
	result, err := mgr.ForceStopService(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

// handleReloadService drives a zero-downtime cutover. Reload is not part of the
// ServiceManager interface — its readiness gate needs the monitor package, which
// imports manager — so it runs only against the concrete LocalManager the daemon
// owns, wiring monitor.ProbeReady in as the gate. A non-local manager (there is
// none in the live daemon) is rejected rather than silently unhandled.
func handleReloadService(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	lm, ok := mgr.(*manager.LocalManager)
	if !ok {
		return errorResponse("reload is only supported by the standalone daemon")
	}

	var args types.ReloadServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse("invalid MethodReloadService args")
	}
	gracePeriod, err := time.ParseDuration(args.GracePeriod)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid grace period: %s", args.GracePeriod))
	}
	tickerPeriod, err := time.ParseDuration(args.TickerPeriod)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid ticker period: %s", args.TickerPeriod))
	}
	readinessTimeout, err := time.ParseDuration(args.ReadinessTimeout)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid readiness timeout: %s", args.ReadinessTimeout))
	}
	probeInterval, err := time.ParseDuration(args.ProbeInterval)
	if err != nil {
		return errorResponse(fmt.Sprintf("invalid probe interval: %s", args.ProbeInterval))
	}

	result, err := lm.ReloadService(args.Name, monitor.ProbeReady, manager.ReloadConfig{
		GracePeriod:      gracePeriod,
		TickerPeriod:     tickerPeriod,
		ReadinessTimeout: readinessTimeout,
		ProbeInterval:    probeInterval,
	})
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(types.ReloadServiceResponse{OldPGID: result.OldPGID, NewPGID: result.NewPGID})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{Success: true, Data: data}
}

func handleAddServiceCatalogEntry(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.AddServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodAddServiceCatalogEntry args: %v", err))
	}
	err := mgr.AddServiceCatalogEntry(ctx, args.Service)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	return types.DaemonResponse{Success: true}
}

func handleGetAllServiceCatalogEntries(ctx context.Context, mgr manager.ServiceManager) types.DaemonResponse {
	result, err := mgr.GetAllServiceCatalogEntries(ctx)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleGetServiceCatalogEntry(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetServiceCatalogEntry args: %v", err))
	}
	result, err := mgr.GetServiceCatalogEntry(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleIsServiceRegistered(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.IsServiceRegisteredArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodIsServiceRegistered args: %v", err))
	}
	result, err := mgr.IsServiceRegistered(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]bool{"exists": result})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleRemoveServiceCatalogEntry(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.RemoveServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodRemoveServiceCatalogEntry args: %v", err))
	}
	removed, err := mgr.RemoveServiceCatalogEntry(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]bool{"removed": removed})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}

	return types.DaemonResponse{Success: true, Data: data}
}

func handleUpdateServiceCatalogEntry(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.UpdateServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodUpdateServiceCatalogEntry args: %v", err))
	}
	err := mgr.UpdateServiceCatalogEntry(ctx, args.Name, args.NewDirectoryPath, args.NewConfigFileName)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	return types.DaemonResponse{Success: true}
}

func handleGetMostRecentProcessHistoryEntry(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetMostRecentProcessHistoryEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetMostRecentProcessHistoryEntry args: %v", err))
	}
	result, err := mgr.GetMostRecentProcessHistoryEntry(ctx, args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	if result == nil {
		return errorResponse("no process history entry found")
	}
	data, err := json.Marshal(types.GetMostRecentProcessHistoryEntryResponse{
		ProcessEntry: *result,
	})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}

	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

// dependencyWaitStatusStore is the slice of a manager the 3 handlers below
// need: recording, clearing, and reading a service's current depends_on wait.
// Only *manager.LocalManager implements it (see local_manager.go) — every mgr
// executeRequest dispatches on is a *manager.LocalManager in practice, since
// only the real daemon serves these requests, but the assertion keeps this
// observability surface out of the broad manager.ServiceManager interface
// every implementer (and test fake) would otherwise have to carry.
type dependencyWaitStatusStore interface {
	SetDependencyWaitStatus(ctx context.Context, name string, pending []string, deadline time.Time) error
	ClearDependencyWaitStatus(ctx context.Context, name string) error
	GetDependencyWaitStatus(ctx context.Context, name string) (status types.DependencyWaitStatus, waiting bool, err error)
}

func handleSetDependencyWaitStatus(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	store, ok := mgr.(dependencyWaitStatusStore)
	if !ok {
		return errorResponse("dependency wait status not supported by this manager")
	}
	var args types.SetDependencyWaitStatusArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodSetDependencyWaitStatus args: %v", err))
	}
	if err := store.SetDependencyWaitStatus(ctx, args.ServiceName, args.Pending, args.Deadline); err != nil {
		return sentinelErrorResponse(err)
	}
	return types.DaemonResponse{Success: true}
}

func handleClearDependencyWaitStatus(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	store, ok := mgr.(dependencyWaitStatusStore)
	if !ok {
		return errorResponse("dependency wait status not supported by this manager")
	}
	var args types.ClearDependencyWaitStatusArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodClearDependencyWaitStatus args: %v", err))
	}
	if err := store.ClearDependencyWaitStatus(ctx, args.ServiceName); err != nil {
		return sentinelErrorResponse(err)
	}
	return types.DaemonResponse{Success: true}
}

func handleGetDependencyWaitStatus(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	store, ok := mgr.(dependencyWaitStatusStore)
	if !ok {
		return errorResponse("dependency wait status not supported by this manager")
	}
	var args types.GetDependencyWaitStatusArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetDependencyWaitStatus args: %v", err))
	}
	status, waiting, err := store.GetDependencyWaitStatus(ctx, args.ServiceName)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	resp := types.GetDependencyWaitStatusResponse{Waiting: waiting}
	if waiting {
		resp.Status = &status
	}
	// resp is bool/strings/time.Time only: nothing here can fail to marshal.
	data, _ := json.Marshal(resp)
	return types.DaemonResponse{
		Success: true,
		Data:    data,
	}
}

func handleNewServiceLogFiles(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.NewServiceLogFilesArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodNewServiceLogFiles args: %v", err))
	}

	logPath, errorLogPath, err := mgr.NewServiceLogFiles(ctx, args.ServiceName)
	if err != nil {
		return sentinelErrorResponse(err)
	}

	data, err := json.Marshal(map[string]string{"logPath": logPath, "errorLogPath": errorLogPath})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}

	return types.DaemonResponse{Success: true, Data: data}
}

func handleGetServiceLogFilePath(ctx context.Context, mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetServiceLogFilePathArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetServiceLogFilePath args: %v", err))
	}

	filepath, err := mgr.GetServiceLogFilePath(ctx, args.ServiceName, args.ErrorLog)
	if err != nil {
		return sentinelErrorResponse(err)
	}

	data, err := json.Marshal(map[string]*string{"filepath": filepath})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}

	return types.DaemonResponse{Success: true, Data: data}
}

func errorResponse(message string) types.DaemonResponse {
	return types.DaemonResponse{
		Success: false,
		Error:   message,
	}
}

func sentinelErrorResponse(err error) types.DaemonResponse {
	return types.DaemonResponse{
		Success:   false,
		Error:     err.Error(),
		ErrorCode: manager.ErrorCode(err),
	}
}

func sendErrorResponse(conn net.Conn, message string, logger *slog.Logger) {
	response := errorResponse(message)
	encoder := json.NewEncoder(conn)
	err := encoder.Encode(response)
	if err != nil {
		logClientWriteError(logger, "sending error response", err)
	}
}
