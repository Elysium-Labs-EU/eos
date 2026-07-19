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
	"syscall"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/buildinfo"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/monitor"
	"codeberg.org/Elysium_Labs/eos/internal/otelx"
	"codeberg.org/Elysium_Labs/eos/internal/procutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
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

func StartStandaloneDaemon(ctx context.Context, logToFileAndConsole bool, verbose bool, baseDir string, standaloneDaemonConfig *config.StandaloneDaemonConfig, healthConfig *config.HealthConfig, shutdownConfig config.ShutdownConfig, telemetryConfig config.TelemetryConfig, underSystemd bool) error {
	d, err := newStandaloneDaemon(ctx, logToFileAndConsole, verbose, baseDir, standaloneDaemonConfig, telemetryConfig)
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

	if underSystemd {
		err := d.recover()
		if err != nil {
			return err
		}
	}
	d.serve(healthConfig, shutdownConfig)

	d.logger.Info("daemon started successfully")

	d.wait()
	return nil
}

func bootPersistedServices(mgr *manager.LocalManager, logger *slog.Logger) error {
	allRegisteredServices, err := mgr.GetAllServiceCatalogEntries()
	if err != nil {
		errorMessage := fmt.Errorf("getting all service catalog entries: %w", err)
		logger.Info(errorMessage.Error())
		return errorMessage
	}

	for _, service := range allRegisteredServices {
		logger.Debug("booting persisted service", "service", service.Name)
		_, err := mgr.StartService(service.Name)
		if err != nil {
			errorMessage := fmt.Errorf("starting service: %w", err)
			logger.Info(errorMessage.Error())
			continue
		}
	}
	return nil
}

// reconcileOrphans runs once at daemon startup and checks every known PGID
// for every service against the real OS process table, regardless of what
// the DB's last-known state for that row says. A row recorded Stopped/Failed
// can still point at a live process (e.g. a SIGCHLD race lost the real exit
// event), and a row recorded Running/Starting can point at a process that's
// actually dead (e.g. after an out-of-band kill or crash) — both cases are
// corrected here instead of trusting the single most-recent row's state.
func reconcileOrphans(ctx context.Context, db *database.DB, logger *slog.Logger) {
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

			if procutil.IsAlive(hist.PGID) {
				if killErr := syscall.Kill(-hist.PGID, syscall.SIGKILL); killErr != nil {
					logger.Info("reconcile orphans: kill PGID", "service", entry.Name, "pgid", hist.PGID, "error", killErr)
				}
				reconcileMarkStopped(ctx, db, logger, entry.Name, hist.PGID)
				continue
			}

			switch hist.State {
			case types.ProcessStateRunning, types.ProcessStateStarting, types.ProcessStateUnknown:
				reconcileMarkStopped(ctx, db, logger, entry.Name, hist.PGID)
			case types.ProcessStateStopped, types.ProcessStateFailed:
				// already terminal and confirmed dead above — no-op
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
func (d *daemon) shutdown(ctx context.Context) {
	d.stop()
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
	return bootPersistedServices(d.mgr, d.logger)
}

func (d *daemon) serve(healthConfig *config.HealthConfig, shutdownConfig config.ShutdownConfig) {
	go handleIncomingCommands(d.listener, d.mgr, d.logger)

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

func newStandaloneDaemon(ctx context.Context, logToFileAndConsole bool, verbose bool, baseDir string, standaloneDaemonConfig *config.StandaloneDaemonConfig, telemetryConfig config.TelemetryConfig) (*daemon, error) {
	startedAt := time.Now()

	logger, err := manager.NewDaemonLogger(logToFileAndConsole, verbose, standaloneDaemonConfig.Log.LogDir, standaloneDaemonConfig.Log.LogFileName, standaloneDaemonConfig.Log.LogMaxFiles, config.DaemonLogFileSizeLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to setup daemon logger: %w", err)
	}

	logger.Info("daemon logger started")
	pidFile := standaloneDaemonConfig.PIDFile
	socketPath := standaloneDaemonConfig.SocketPath

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
	logger.Debug("socket listening", "path", socketPath)

	db, err := database.NewDB(ctx, baseDir)
	if err != nil {
		errorMessage := fmt.Errorf("failed to connect to database: %w", err)
		logger.Info(errorMessage.Error())
		return nil, errorMessage
	}
	logger.Debug("database connected")

	otelProvider, err := otelx.NewProvider(ctx, otelx.Config{
		Enable:   telemetryConfig.Enable,
		Endpoint: telemetryConfig.Endpoint,
		Insecure: telemetryConfig.Insecure,
	}, "eos", buildinfo.Version)
	if err != nil {
		// Telemetry export is an add-on to process supervision, not a
		// prerequisite for it: a misconfigured collector shouldn't stop the
		// daemon from managing services. Fall back to the disabled (no-op)
		// providers — cfg.Enable false, which NewProvider never errors on.
		logger.Error("telemetry setup failed, continuing without it", "error", err)
		otelProvider, err = otelx.NewProvider(ctx, otelx.Config{}, "eos", buildinfo.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to set up fallback telemetry provider: %w", err)
		}
	}

	otelHandles, err := otelx.NewHandles(otelProvider.TracerProvider, otelProvider.MeterProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to set up telemetry instruments: %w", err)
	}

	mgr := manager.NewLocalManager(db, baseDir, ctx, logger, manager.WithTelemetry(otelHandles))

	if regErr := otelx.RegisterDaemonGauges(otelProvider.MeterProvider, startedAt,
		func() int { return len(catalogEntriesOrEmpty(mgr, logger)) },
		func() int { return len(serviceInstancesOrEmpty(mgr, logger)) },
	); regErr != nil {
		logger.Error("registering daemon telemetry gauges", "error", regErr)
	}

	return &daemon{
		logger:       logger,
		db:           db,
		mgr:          mgr,
		otelProvider: otelProvider,
		otelHandles:  otelHandles,
		listener:     listener,
		ctx:          ctx,
		stop:         stop,
		sigChan:      sigChan,
		pidFile:      pidFile,
		socketPath:   socketPath,
	}, nil
}

// catalogEntriesOrEmpty and serviceInstancesOrEmpty back the daemon-level
// registered/running gauge callbacks (see otelx.RegisterDaemonGauges): the
// SDK polls these on its own export interval, well off the hot path, so a
// query failure there is worth logging but never worth surfacing to the
// exporter callback's own error return.
func catalogEntriesOrEmpty(mgr *manager.LocalManager, logger *slog.Logger) []types.ServiceCatalogEntry {
	entries, err := mgr.GetAllServiceCatalogEntries()
	if err != nil {
		logger.Debug("telemetry: listing service catalog", "error", err)
		return nil
	}
	return entries
}

func serviceInstancesOrEmpty(mgr *manager.LocalManager, logger *slog.Logger) []types.ServiceInstance {
	instances, err := mgr.GetAllServiceInstances()
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

func handleIncomingCommands(listener net.Listener, mgr manager.ServiceManager, logger *slog.Logger) {
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

		go handleConnection(conn, mgr, logger)
	}
}

func handleConnection(conn net.Conn, mgr manager.ServiceManager, logger *slog.Logger) {
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("closing daemon socket", "error", err)
		}
	}()

	var request types.DaemonRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		sendErrorResponse(conn, fmt.Sprintf("decoding request: %v", err), logger)
		return
	}

	response := executeRequest(mgr, request)

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		logger.Error("sending response", "error", err)
	}
}

// executeRequest dispatches a decoded daemon IPC request to the handler for
// its method. Each case below is a thin one-line call into a handleX
// function that owns the args-unmarshal / manager-call / response-marshal
// sequence for that method; this function's only job is routing.
func executeRequest(mgr manager.ServiceManager, request types.DaemonRequest) types.DaemonResponse {
	switch request.Method {
	case types.MethodGetAllServiceInstances:
		return handleGetAllServiceInstances(mgr)
	case types.MethodGetServiceInstance:
		return handleGetServiceInstance(mgr, request.Args)
	case types.MethodRemoveServiceInstance:
		return handleRemoveServiceInstance(mgr, request.Args)
	case types.MethodStartService:
		return handleStartService(mgr, request.Args)
	case types.MethodRestartService:
		return handleRestartService(mgr, request.Args)
	case types.MethodStopService:
		return handleStopService(mgr, request.Args)
	case types.MethodForceStopService:
		return handleForceStopService(mgr, request.Args)
	case types.MethodAddServiceCatalogEntry:
		return handleAddServiceCatalogEntry(mgr, request.Args)
	case types.MethodGetAllServiceCatalogEntries:
		return handleGetAllServiceCatalogEntries(mgr)
	case types.MethodGetServiceCatalogEntry:
		return handleGetServiceCatalogEntry(mgr, request.Args)
	case types.MethodIsServiceRegistered:
		return handleIsServiceRegistered(mgr, request.Args)
	case types.MethodRemoveServiceCatalogEntry:
		return handleRemoveServiceCatalogEntry(mgr, request.Args)
	case types.MethodUpdateServiceCatalogEntry:
		return handleUpdateServiceCatalogEntry(mgr, request.Args)
	case types.MethodGetMostRecentProcessHistoryEntry:
		return handleGetMostRecentProcessHistoryEntry(mgr, request.Args)
	case types.MethodNewServiceLogFiles:
		return handleNewServiceLogFiles(mgr, request.Args)
	case types.MethodGetServiceLogFilePath:
		return handleGetServiceLogFilePath(mgr, request.Args)
	default:
		return errorResponse(fmt.Sprintf("unknown method: %s", request.Method))
	}
}

func handleGetAllServiceInstances(mgr manager.ServiceManager) types.DaemonResponse {
	result, err := mgr.GetAllServiceInstances()
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

func handleGetServiceInstance(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetServiceInstanceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetServiceInstance args: %v", err))
	}

	result, err := mgr.GetServiceInstance(args.Name)
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

func handleRemoveServiceInstance(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.RemoveServiceInstanceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodRemoveServiceInstance args: %v", err))
	}
	removed, err := mgr.RemoveServiceInstance(args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]bool{"removed": removed})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}
	return types.DaemonResponse{Success: true, Data: data}
}

func handleStartService(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.StartServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodStartService args: %v", err))
	}
	pid, err := mgr.StartService(args.Name)
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

func handleRestartService(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
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
	pid, err := mgr.RestartService(args.Name, gracePeriod, tickerPeriod)
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

func handleStopService(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
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
	result, err := mgr.StopService(args.Name, gracePeriod, tickerPeriod)
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

func handleForceStopService(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.ForceStopServiceArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodForceStopService args: %v", err))
	}
	result, err := mgr.ForceStopService(args.Name)
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

func handleAddServiceCatalogEntry(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.AddServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodAddServiceCatalogEntry args: %v", err))
	}
	err := mgr.AddServiceCatalogEntry(args.Service)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	return types.DaemonResponse{Success: true}
}

func handleGetAllServiceCatalogEntries(mgr manager.ServiceManager) types.DaemonResponse {
	result, err := mgr.GetAllServiceCatalogEntries()
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

func handleGetServiceCatalogEntry(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetServiceCatalogEntry args: %v", err))
	}
	result, err := mgr.GetServiceCatalogEntry(args.Name)
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

func handleIsServiceRegistered(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.IsServiceRegisteredArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodIsServiceRegistered args: %v", err))
	}
	result, err := mgr.IsServiceRegistered(args.Name)
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

func handleRemoveServiceCatalogEntry(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.RemoveServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodRemoveServiceCatalogEntry args: %v", err))
	}
	removed, err := mgr.RemoveServiceCatalogEntry(args.Name)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	data, err := json.Marshal(map[string]bool{"removed": removed})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}

	return types.DaemonResponse{Success: true, Data: data}
}

func handleUpdateServiceCatalogEntry(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.UpdateServiceCatalogEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodUpdateServiceCatalogEntry args: %v", err))
	}
	err := mgr.UpdateServiceCatalogEntry(args.Name, args.NewDirectoryPath, args.NewConfigFileName)
	if err != nil {
		return sentinelErrorResponse(err)
	}
	return types.DaemonResponse{Success: true}
}

func handleGetMostRecentProcessHistoryEntry(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetMostRecentProcessHistoryEntryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetMostRecentProcessHistoryEntry args: %v", err))
	}
	result, err := mgr.GetMostRecentProcessHistoryEntry(args.Name)
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

func handleNewServiceLogFiles(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.NewServiceLogFilesArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodNewServiceLogFiles args: %v", err))
	}

	logPath, errorLogPath, err := mgr.NewServiceLogFiles(args.ServiceName)
	if err != nil {
		return sentinelErrorResponse(err)
	}

	data, err := json.Marshal(map[string]string{"logPath": logPath, "errorLogPath": errorLogPath})
	if err != nil {
		return errorResponse(fmt.Sprintf("marshaling response: %v", err))
	}

	return types.DaemonResponse{Success: true, Data: data}
}

func handleGetServiceLogFilePath(mgr manager.ServiceManager, rawArgs json.RawMessage) types.DaemonResponse {
	var args types.GetServiceLogFilePathArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(fmt.Sprintf("invalid MethodGetServiceLogFilePath args: %v", err))
	}

	filepath, err := mgr.GetServiceLogFilePath(args.ServiceName, args.ErrorLog)
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
		logger.Error("sending error response", "error", err)
	}
}
