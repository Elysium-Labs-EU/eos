package manager

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/logutil"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

type LocalManager struct {
	db           database.Database
	ctx          context.Context
	executor     Executor
	logger       *slog.Logger
	sinkRegistry map[string]types.LogSink
	telemetry    *otelx.Handles
	baseDir      string
	pipeWg       sync.WaitGroup
}

// WaitPipes blocks until all pipe-forwarding goroutines have exited.
// Call this in test cleanup after stopping services to avoid goroutine leaks.
func (m *LocalManager) WaitPipes() {
	m.pipeWg.Wait()
}

type LocalManagerOption func(*LocalManager)

func WithExecutor(e Executor) LocalManagerOption {
	return func(m *LocalManager) {
		if e == nil {
			return
		}
		m.executor = e
	}
}

// WithSinkRegistry sets the named log sink registry (from the daemon's
// ~/.eos/config.yaml sinks:) used to resolve log_sinks name references in
// service.yaml. Services with only inline sink configs work fine without it.
func WithSinkRegistry(registry map[string]types.LogSink) LocalManagerOption {
	return func(m *LocalManager) {
		m.sinkRegistry = registry
	}
}

// WithTelemetry sets the tracer and metric instruments the service lifecycle
// (StartService/StopService/RestartService/ForceStopService) records
// through. Callers that don't supply this get otelx.NoopHandles(), so
// telemetry-less construction (every test, and any daemon with telemetry
// disabled) costs nothing beyond a few no-op interface calls.
func WithTelemetry(h *otelx.Handles) LocalManagerOption {
	return func(m *LocalManager) {
		m.telemetry = h
	}
}

func NewLocalManager(db *database.DB, baseDir string, ctx context.Context, logger *slog.Logger, opts ...LocalManagerOption) *LocalManager {
	m := &LocalManager{db: db, baseDir: baseDir, ctx: ctx, logger: logger, executor: osExecutor{}, telemetry: otelx.NoopHandles()}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *LocalManager) AddServiceCatalogEntry(newServiceCatalogEntry *types.ServiceCatalogEntry) error {
	isRegistered, err := m.db.IsServiceRegistered(m.ctx, newServiceCatalogEntry.Name)
	if err != nil {
		return fmt.Errorf("check service registration: %w", err)
	}
	if isRegistered {
		return ErrServiceAlreadyRegistered
	}

	// Reject a name that collides case-insensitively with an existing service.
	// Log filenames are derived verbatim from the service name, so names like
	// "Foo" and "foo" alias onto one log file on case-insensitive filesystems
	// (macOS APFS), silently intermingling the two services' output. Distinct
	// catalog identities must map to distinct log files. See issue #10.
	existing, conflict, err := m.db.FindServiceNameCaseInsensitive(m.ctx, newServiceCatalogEntry.Name)
	if err != nil {
		return fmt.Errorf("check service name case collision: %w", err)
	}
	if conflict {
		return fmt.Errorf("%w: %q conflicts with registered service %q", ErrServiceNameCaseConflict, newServiceCatalogEntry.Name, existing)
	}

	err = m.db.RegisterService(m.ctx, newServiceCatalogEntry.Name, newServiceCatalogEntry.DirectoryPath, newServiceCatalogEntry.ConfigFileName)
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}
	return nil

}

func (m *LocalManager) RemoveServiceInstance(name string) (bool, error) {
	removed, err := m.db.RemoveServiceInstance(m.ctx, name)
	if err != nil {
		return false, fmt.Errorf("remove service instance: %w", err)
	}
	return removed, nil
}

func (m *LocalManager) RemoveServiceCatalogEntry(name string) (bool, error) {
	removed, err := m.db.RemoveServiceCatalogEntry(m.ctx, name)
	if err != nil {
		return false, fmt.Errorf("remove service catalog entry: %w", err)
	}
	return removed, nil
}

func (m *LocalManager) IsServiceRegistered(name string) (bool, error) {
	isRegistered, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return false, fmt.Errorf("check service registration: %w", err)
	}
	if isRegistered {
		return true, nil
	}
	return false, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, database.ErrServiceNotFound) ||
		strings.Contains(err.Error(), database.ErrServiceNotFound.Error())
}

func (m *LocalManager) GetServiceInstance(name string) (*types.ServiceInstance, error) {
	_, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("service %q not registered: %w", name, err)
	}

	serviceInstance, err := m.db.GetServiceInstance(m.ctx, name)
	if isNotFound(err) {
		return nil, ErrServiceNotRunning
	}
	if err != nil {
		return nil, fmt.Errorf("get service instance: %w", err)
	}

	return &serviceInstance, nil
}

func (m *LocalManager) GetAllServiceInstances() ([]types.ServiceInstance, error) {
	serviceInstances, err := m.db.GetAllServiceInstances(m.ctx)
	if err != nil {
		return nil, fmt.Errorf("get all service runtime entries: %w", err)
	}
	return serviceInstances, nil
}

func (m *LocalManager) GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error) {
	_, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("service %q not registered: %w", name, err)
	}

	registeredService, err := m.db.GetServiceCatalogEntry(m.ctx, name)
	if isNotFound(err) {
		return types.ServiceCatalogEntry{}, ErrServiceNotRegistered
	}
	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("get service catalog entry: %w", err)
	}
	return registeredService, nil
}

func (m *LocalManager) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) {
	services, err := m.db.GetAllServiceCatalogEntries(m.ctx)
	if err != nil {
		return nil, fmt.Errorf("get all service catalog entries: %w", err)
	}
	return services, nil
}

func (m *LocalManager) GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error) {
	entry, err := m.db.GetMostRecentProcessHistoryEntryByName(m.ctx, name)
	if errors.Is(err, database.ErrProcessHistoryNotFound) {
		return nil, ErrProcessNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get process history for %s: %w", name, err)
	}
	return &entry, nil
}

func (m *LocalManager) UpdateServiceCatalogEntry(name string, newDirectoryPath string, newConfigFileName string) error {
	err := m.db.UpdateServiceCatalogEntry(m.ctx, name, newDirectoryPath, newConfigFileName)
	if err != nil {
		return fmt.Errorf("update service catalog entry %q: %w", name, err)
	}
	return nil
}

func newPipeForStd() (r *os.File, w *os.File, err error) {
	r, w, err = os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating pipe: %w", err)
	}

	return r, w, nil
}

func (m *LocalManager) pipeToLogFile(r *os.File, w *os.File, name string, sinks []*sinkProcess, wg *sync.WaitGroup) {
	defer m.pipeWg.Done()
	if wg != nil {
		defer wg.Done()
	}
	stop := context.AfterFunc(m.ctx, func() { _ = r.Close() })
	defer stop()
	logger := logutil.NewJSONLogger(w, false)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		logger.Info(line, "service", name, "source", "stdout")
		for _, s := range sinks {
			if sinkWantsStream(s, "stdout") {
				s.Send(line, "stdout")
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && m.ctx.Err() == nil {
		m.logger.Error("scanning log pipe", "service", name, "error", scanErr)
	}
	if err := r.Close(); err != nil && m.ctx.Err() == nil {
		m.logger.Error("closing read log file pipe", "service", name, "error", err)
	}
	if err := w.Close(); err != nil {
		m.logger.Error("closing write log file", "service", name, "error", err)
	}
}

func (m *LocalManager) pipeToErrorLogFile(r *os.File, w *os.File, errFileLogger *slog.Logger, name string, sinks []*sinkProcess, wg *sync.WaitGroup) {
	defer m.pipeWg.Done()
	if wg != nil {
		defer wg.Done()
	}
	stop := context.AfterFunc(m.ctx, func() { _ = r.Close() })
	defer stop()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		errFileLogger.Info(line, "service", name, "source", "stderr")
		for _, s := range sinks {
			if sinkWantsStream(s, "stderr") {
				s.Send(line, "stderr")
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && m.ctx.Err() == nil {
		m.logger.Error("scanning error log pipe", "service", name, "error", scanErr)
	}
	if err := r.Close(); err != nil && m.ctx.Err() == nil {
		m.logger.Error("closing read error log file pipe", "service", name, "error", err)
	}
	if err := w.Close(); err != nil {
		m.logger.Error("closing write error log file", "service", name, "error", err)
	}
}

// livePGIDInHistory returns the PGID of the first Running or Starting history
// entry that still has a live OS process, or 0 if none do.
func livePGIDInHistory(history []types.ProcessHistory) int {
	for i := range history {
		p := &history[i]
		if p.State != types.ProcessStateRunning && p.State != types.ProcessStateStarting {
			continue
		}
		if procutil.IsAliveMatching(p.PGID, p.StartedAtTicks) {
			return p.PGID
		}
	}
	return 0
}

// launchIO bundles the log files and stdout/stderr pipes created for a service
// launch so every failure path can clean them up together.
type launchIO struct {
	logFile      *os.File
	errorLogFile *os.File
	readLog      *os.File
	writeLog     *os.File
	readErr      *os.File
	writeErr     *os.File
}

// prepareLaunchIO opens the service log files and the two stdout/stderr pipes.
// On any partial failure it closes whatever was already opened so the caller
// never leaks a descriptor.
func (m *LocalManager) prepareLaunchIO(name string) (launchIO, error) {
	logFile, errorLogFile, err := m.prepareLogFiles(name)
	if err != nil {
		return launchIO{}, fmt.Errorf("preparing log files for %s: %w", name, err)
	}
	readLog, writeLog, err := newPipeForStd()
	if err != nil {
		_ = logFile.Close()
		_ = errorLogFile.Close()
		return launchIO{}, fmt.Errorf("creating log file pipe for %s: %w", name, err)
	}
	readErr, writeErr, err := newPipeForStd()
	if err != nil {
		_ = logFile.Close()
		_ = errorLogFile.Close()
		_ = readLog.Close()
		_ = writeLog.Close()
		return launchIO{}, fmt.Errorf("creating error log file pipe for %s: %w", name, err)
	}
	return launchIO{
		logFile:      logFile,
		errorLogFile: errorLogFile,
		readLog:      readLog,
		writeLog:     writeLog,
		readErr:      readErr,
		writeErr:     writeErr,
	}, nil
}

// closeAll closes every file in the bundle, joining any close errors. Used by
// the launch failure path before the pipe goroutines take ownership.
func (lio launchIO) closeAll() error {
	closers := []struct {
		f   *os.File
		msg string
	}{
		{lio.readLog, "closing read log file pipe"},
		{lio.writeLog, "closing write log file pipe"},
		{lio.readErr, "closing read error log file pipe"},
		{lio.writeErr, "closing write error log file pipe"},
		{lio.logFile, "closing log file"},
		{lio.errorLogFile, "closing error log file"},
	}
	var errs []error
	for _, c := range closers {
		if closeErr := c.f.Close(); closeErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.msg, closeErr))
		}
	}
	return errors.Join(errs...)
}

// buildLaunchCommand constructs the /bin/sh command that runs a service, wiring
// its process group, working directory, environment, and stdout/stderr pipes.
func (m *LocalManager) buildLaunchCommand(service types.ServiceCatalogEntry, config *types.ServiceConfig, lio launchIO) (*exec.Cmd, error) {
	cmd := m.executor.CommandContext(m.ctx, "/bin/sh", "-c", config.Command) // #nosec G204 -- command is user-defined in their service.yaml config
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = service.DirectoryPath
	env, err := buildEnvironment(config, service.DirectoryPath)
	if err != nil {
		return nil, fmt.Errorf("building environment for %s: %w", service.Name, err)
	}
	cmd.Env = env
	cmd.Stdout = lio.writeLog
	cmd.Stderr = lio.writeErr
	return cmd, nil
}

// wireLogPipes closes the now-handed-off write ends, starts any log sinks, and
// launches the goroutines that forward the process's stdout/stderr to the log
// files and sinks. It must be called once Start has succeeded.
func (m *LocalManager) wireLogPipes(lio launchIO, resolvedSinks []types.LogSink, name string) error {
	if closeErr := lio.writeLog.Close(); closeErr != nil {
		return fmt.Errorf("closing write log file pipe for %s: %w", name, closeErr)
	}
	if closeErr := lio.writeErr.Close(); closeErr != nil {
		return fmt.Errorf("closing write error log file pipe for %s: %w", name, closeErr)
	}

	errFileLogger := logutil.NewJSONLogger(lio.errorLogFile, false)
	sinks := startSinkProcesses(m.ctx, resolvedSinks, name, m.logger, errFileLogger)
	var sinkWg *sync.WaitGroup
	if len(sinks) > 0 {
		sinkWg = &sync.WaitGroup{}
		sinkWg.Add(2)
		go func() {
			sinkWg.Wait()
			stopSinkProcesses(sinks)
		}()
	}

	m.pipeWg.Add(2)
	go m.pipeToLogFile(lio.readLog, lio.logFile, name, sinks, sinkWg)
	go m.pipeToErrorLogFile(lio.readErr, lio.errorLogFile, errFileLogger, name, sinks, sinkWg)
	return nil
}

// killAndWrap kills a launched process group after a post-start bookkeeping
// step failed, and wraps err with action context. If the kill itself fails the
// process may still be alive, so pgid 0 is returned to flag manual cleanup;
// otherwise the (now cleaned-up) pgid is returned.
func killAndWrap(pgid int, err error, action string) (int, error) {
	if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil {
		return 0, fmt.Errorf("%s %d: %w; kill process: %w - manual intervention required", action, pgid, err, killErr)
	}
	return pgid, fmt.Errorf("%s (process cleaned up): %w", action, err)
}

// captureIdentity derives the process-group id from a freshly started leader
// (its PID, since Setpgid makes it the group leader), reads its start time
// before the reaper can collect it, then launches the async reaper. On a
// start-time read failure it kills the group and reaps synchronously.
func captureIdentity(cmd *exec.Cmd) (pgid int, startedAtTicks int64, err error) {
	pgid = cmd.Process.Pid
	startedAtTicks, err = procutil.StartTime(pgid)
	if err != nil {
		cleanPGID, wrapErr := killAndWrap(pgid, err, "reading process start time")
		_ = cmd.Wait() // reap; the async reaper below never launched on this path
		return cleanPGID, 0, wrapErr
	}
	go func() {
		_ = cmd.Wait()
	}()
	return pgid, startedAtTicks, nil
}

// reconcileStartHistory scans prior process history before a start. It errors
// if a still-live Running/Starting process for this service is found, and
// otherwise self-heals stale rows whose processes are gone (Running->Stopped,
// Starting->Failed) so status displays don't report phantom processes.
func (m *LocalManager) reconcileStartHistory(name string, processHistory []types.ProcessHistory) error {
	for i := range processHistory {
		p := &processHistory[i]
		switch p.State {
		case types.ProcessStateRunning:
			if procutil.IsAliveMatching(p.PGID, p.StartedAtTicks) {
				return fmt.Errorf("service already running with PGID %d", p.PGID)
			}
			if updateErr := m.db.UpdateProcessHistoryEntry(m.ctx, p.PGID, database.ProcessHistoryUpdate{
				State:     new(types.ProcessStateStopped),
				StoppedAt: new(time.Now()),
			}); updateErr != nil {
				m.logger.Error("failed to mark stale running entry as stopped", "service", name, "pgid", p.PGID, "error", updateErr)
			}
		case types.ProcessStateStarting:
			if procutil.IsAliveMatching(p.PGID, p.StartedAtTicks) {
				return fmt.Errorf("service already starting with PGID %d", p.PGID)
			}
			if updateErr := m.db.UpdateProcessHistoryEntry(m.ctx, p.PGID, database.ProcessHistoryUpdate{
				State:     new(types.ProcessStateFailed),
				StoppedAt: new(time.Now()),
			}); updateErr != nil {
				m.logger.Error("failed to mark stale starting entry as failed", "service", name, "pgid", p.PGID, "error", updateErr)
			}
		case types.ProcessStateStopped, types.ProcessStateFailed, types.ProcessStateUnknown:
			// Already terminal; nothing to reconcile.
		}
	}
	return nil
}

// loadServiceForLaunch resolves the catalog entry, parsed service config, and
// resolved log sinks shared by Start and Restart. An unregistered service is
// normalized into a plain error.
func (m *LocalManager) loadServiceForLaunch(name string) (types.ServiceCatalogEntry, *types.ServiceConfig, []types.LogSink, error) {
	service, err := m.GetServiceCatalogEntry(name)
	if errors.Is(err, ErrServiceNotRegistered) {
		return types.ServiceCatalogEntry{}, nil, nil, fmt.Errorf("service %s not registered", name)
	}
	if err != nil {
		return types.ServiceCatalogEntry{}, nil, nil, fmt.Errorf("get service catalog entry %q: %w", name, err)
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := LoadServiceConfig(configPath)
	if err != nil {
		return types.ServiceCatalogEntry{}, nil, nil, fmt.Errorf("load service config for %s: %w", name, err)
	}

	resolvedSinks, err := ResolveLogSinks(name, config.LogSinks, m.sinkRegistry)
	if err != nil {
		return types.ServiceCatalogEntry{}, nil, nil, err
	}

	return service, config, resolvedSinks, nil
}

// launchAndCapture builds the service command, starts it, wires its log pipes,
// and captures its process identity. On a successful Start it sets
// *launchSuccess so the caller's deferred IO cleanup is skipped. startErrLabel
// distinguishes "start command" from "restart command" in the error.
func (m *LocalManager) launchAndCapture(service types.ServiceCatalogEntry, config *types.ServiceConfig, lio launchIO, resolvedSinks []types.LogSink, launchSuccess *bool, startErrLabel string) (pgid int, startedAtTicks int64, err error) {
	cmd, err := m.buildLaunchCommand(service, config, lio)
	if err != nil {
		return 0, 0, err
	}

	if startErr := cmd.Start(); startErr != nil {
		return 0, 0, fmt.Errorf("%s: %w", startErrLabel, startErr)
	}
	*launchSuccess = true

	if wireErr := m.wireLogPipes(lio, resolvedSinks, service.Name); wireErr != nil {
		return 0, 0, wireErr
	}

	// See captureIdentity: derive PGID from the leader's PID and read its start
	// time before the reaper runs, so an instant-exit process is still readable
	// and Getpgid can't race the reap into an ESRCH failure.
	m.logger.Debug("process started", "service", service.Name, "pgid", cmd.Process.Pid)
	return captureIdentity(cmd)
}

// recordStartedInstance persists the service instance and process-history rows
// for a freshly started service. On any DB failure it kills the process group.
func (m *LocalManager) recordStartedInstance(service types.ServiceCatalogEntry, pgid int, startedAtTicks int64) (int, error) {
	if regErr := m.db.RegisterServiceInstance(m.ctx, service.Name); regErr != nil {
		return killAndWrap(pgid, regErr, "register service instance")
	}
	if updErr := m.db.UpdateServiceInstance(m.ctx, service.Name, database.ServiceInstanceUpdate{
		StartedAt: new(time.Now()),
	}); updErr != nil {
		return killAndWrap(pgid, updErr, "update service instance")
	}
	if _, histErr := m.db.RegisterProcessHistoryEntry(m.ctx, pgid, startedAtTicks, service.Name, types.ProcessStateStarting); histErr != nil {
		return killAndWrap(pgid, histErr, "register process history entry")
	}
	return pgid, nil
}

// recordRestartedInstance bumps the restart count and records the new process
// history row for a restarted service. On any DB failure it kills the group.
func (m *LocalManager) recordRestartedInstance(service types.ServiceCatalogEntry, restartCount, pgid int, startedAtTicks int64) (int, error) {
	if updErr := m.db.UpdateServiceInstance(m.ctx, service.Name, database.ServiceInstanceUpdate{
		StartedAt:    new(time.Now()),
		RestartCount: new(restartCount + 1),
	}); updErr != nil {
		return killAndWrap(pgid, updErr, "update service instance")
	}
	if _, histErr := m.db.RegisterProcessHistoryEntry(m.ctx, pgid, startedAtTicks, service.Name, types.ProcessStateStarting); histErr != nil {
		return killAndWrap(pgid, histErr, "register process history entry")
	}
	return pgid, nil
}

func (m *LocalManager) StartService(name string) (pgid int, err error) {
	_, span := m.telemetry.StartSpan(m.ctx, "eos.service.start", name)
	defer func() {
		otelx.End(span, err)
		otelx.RecordOutcome(m.ctx, m.telemetry.ServiceStarts, name, err)
	}()

	service, config, resolvedSinks, err := m.loadServiceForLaunch(name)
	if err != nil {
		return 0, err
	}

	serviceInstance, err := m.GetServiceInstance(name)
	if err != nil && !errors.Is(err, ErrServiceNotRunning) {
		return 0, fmt.Errorf("get service instance for %s: %w", name, err)
	}

	processHistory, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return 0, fmt.Errorf("get process history for %s: %w", name, err)
	}

	if serviceInstance != nil {
		if livePGID := livePGIDInHistory(processHistory); livePGID > 0 {
			// TODO: return found PGID somehow instead?
			return 0, ErrAlreadyRunning
		}
		// service_instances row is stale: nothing in process history is
		// actually alive (e.g. the daemon was killed out-of-band without a
		// clean `eos stop`, which never got the chance to remove this row).
		// Self-heal by proceeding — RegisterServiceInstance below replaces
		// this row on a successful start.
	}

	if reconcileErr := m.reconcileStartHistory(name, processHistory); reconcileErr != nil {
		return 0, reconcileErr
	}

	lio, err := m.prepareLaunchIO(service.Name)
	if err != nil {
		return 0, err
	}

	launchSuccess := false
	defer func() {
		if !launchSuccess {
			if closeErr := lio.closeAll(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	if binaryErr := m.validateRuntimeBinary(config); binaryErr != nil {
		return 0, binaryErr
	}

	m.logger.Debug("launching service", "service", name, "cmd", config.Command)
	pgid, startedAtTicks, err := m.launchAndCapture(service, config, lio, resolvedSinks, &launchSuccess, "start command")
	if err != nil {
		return pgid, err
	}

	pgid, err = m.recordStartedInstance(service, pgid, startedAtTicks)
	if err != nil {
		return pgid, err
	}
	m.logger.Debug("state=Starting recorded", "service", name, "pgid", pgid)

	return pgid, nil
}

func (m *LocalManager) RestartService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (pgid int, err error) {
	_, span := m.telemetry.StartSpan(m.ctx, "eos.service.restart", name)
	defer func() {
		otelx.End(span, err)
		otelx.RecordOutcome(m.ctx, m.telemetry.ServiceRestarts, name, err)
	}()

	service, config, resolvedSinks, err := m.loadServiceForLaunch(name)
	if err != nil {
		return 0, err
	}

	serviceInstance, err := m.GetServiceInstance(name)
	if err != nil {
		return 0, fmt.Errorf("get service instance for %s: %w", name, err)
	}
	if serviceInstance == nil {
		return 0, fmt.Errorf("no service instance for %s", name)
	}

	lio, err := m.prepareLaunchIO(service.Name)
	if err != nil {
		return 0, err
	}

	launchSuccess := false
	defer func() {
		if !launchSuccess {
			if closeErr := lio.closeAll(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	if binaryErr := m.validateRuntimeBinary(config); binaryErr != nil {
		return 0, binaryErr
	}

	stopResult, err := m.StopService(name, gracePeriod, tickerPeriod)
	if err != nil {
		return 0, fmt.Errorf("stopping process(es) for %s: %w", name, err)
	}
	if len(stopResult.Errored) > 0 {
		return 0, fmt.Errorf("stopping process(es) for %s: %v", name, stopResult.Errored)
	}

	m.logger.Debug("stop complete, launching restart", "service", name)
	pgid, startedAtTicks, err := m.launchAndCapture(service, config, lio, resolvedSinks, &launchSuccess, "restart command")
	if err != nil {
		return pgid, err
	}

	return m.recordRestartedInstance(service, serviceInstance.RestartCount, pgid, startedAtTicks)
}

func (m *LocalManager) prepareLogFiles(serviceName string) (logFile *os.File, errorLogFile *os.File, err error) {
	logPath, errorLogPath, err := m.NewServiceLogFiles(serviceName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log file paths: %w", err)
	}
	logFile, err = OpenLogFile(logPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open log file: %w", err)
	}
	errorLogFile, err = OpenLogFile(errorLogPath)
	if err != nil {
		openErr := err
		if closeErr := logFile.Close(); closeErr != nil {
			return nil, nil, errors.Join(
				fmt.Errorf("open error log file: %w", openErr),
				fmt.Errorf("close log file during cleanup: %w", closeErr),
			)
		}
		return nil, nil, fmt.Errorf("open error log file: %w", openErr)
	}

	return logFile, errorLogFile, nil
}

type StopServiceResult struct {
	Errored   map[int]string
	Stopped   map[int]bool
	StaleData map[int]string
}

// waitForPendingStops polls until every pending PID has exited or the grace
// period elapses. PIDs still alive once the grace period is exceeded are marked
// in errored ("exceeded grace period"). Returns the set that exited cleanly, or
// canceled=true when m.ctx is done (caller should abandon and re-check later).
func (m *LocalManager) waitForPendingStops(name string, pending map[int]bool, errored map[int]string, requestStartTime time.Time, gracePeriod, tickerPeriod time.Duration) (stopped map[int]bool, canceled bool) {
	ticker := time.NewTicker(tickerPeriod)
	defer ticker.Stop()

	countPending := len(pending)
	stopped = make(map[int]bool)

	for {
		select {
		case <-ticker.C:
			if time.Since(requestStartTime) > gracePeriod {
				for pendingPID := range pending {
					if _, ok := stopped[pendingPID]; !ok {
						errored[pendingPID] = "killing service: exceeded grace period"
					}
				}
				return stopped, false
			}

			for pendingPID := range pending {
				if _, ok := stopped[pendingPID]; ok {
					continue
				}
				if !isProcessAlive(pendingPID) {
					stopped[pendingPID] = true
				}
			}

			if len(stopped) == countPending {
				m.logger.Debug("all processes exited", "service", name, "elapsed", time.Since(requestStartTime))
				return stopped, false
			}

		case <-m.ctx.Done():
			return nil, true
		}
	}
}

func (m *LocalManager) StopService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (result StopServiceResult, err error) {
	_, span := m.telemetry.StartSpan(m.ctx, "eos.service.stop", name)
	defer func() {
		otelx.End(span, err)
		otelx.RecordOutcome(m.ctx, m.telemetry.ServiceStops, name, err)
	}()

	requestStartTime := time.Now()
	m.logger.Debug("sending SIGTERM", "service", name)
	stopResult, err := m.stopServiceWithSignal(name, syscall.SIGTERM)

	if err != nil {
		return StopServiceResult{}, err
	}

	countError := len(stopResult.Errored)
	countPending := len(stopResult.Pending)
	countAlreadyDead := len(stopResult.AlreadyDead)
	countTotal := countError + countPending + countAlreadyDead

	if countTotal == 0 {
		return StopServiceResult{}, nil
	}
	if countTotal == countError {
		return StopServiceResult{Errored: stopResult.Errored, Stopped: nil}, nil
	}

	if countPending == 0 {
		staleDataErrors := make(map[int]string)

		errorErrored := updateProcessHistoryEntriesAsUnknown(m, stopResult.Errored)
		maps.Copy(staleDataErrors, errorErrored)

		adErrored := updateProcessHistoryEntriesAsStopped(m, stopResult.AlreadyDead)
		maps.Copy(staleDataErrors, adErrored)

		return StopServiceResult{
			Errored:   stopResult.Errored,
			Stopped:   stopResult.AlreadyDead,
			StaleData: staleDataErrors,
		}, nil
	}

	erroredProcesses := stopResult.Errored
	stoppedProcesses, canceled := m.waitForPendingStops(name, stopResult.Pending, erroredProcesses, requestStartTime, gracePeriod, tickerPeriod)
	if canceled {
		// User canceled, return empty result. System will check all again.
		return StopServiceResult{}, nil
	}

	staleDataErrors := make(map[int]string)

	errorErrored := updateProcessHistoryEntriesAsUnknown(m, erroredProcesses)
	maps.Copy(staleDataErrors, errorErrored)

	adErrored := updateProcessHistoryEntriesAsStopped(m, stopResult.AlreadyDead)
	maps.Copy(staleDataErrors, adErrored)

	stoppedErrored := updateProcessHistoryEntriesAsStopped(m, stoppedProcesses)
	maps.Copy(staleDataErrors, stoppedErrored)

	stoppedAndAlreadyDeadProcesses := stopResult.AlreadyDead
	maps.Copy(stoppedAndAlreadyDeadProcesses, stoppedProcesses)

	return StopServiceResult{Errored: erroredProcesses, Stopped: stoppedAndAlreadyDeadProcesses, StaleData: staleDataErrors}, nil
}

// isProcessAlive reports whether any live process exists in the given process group.
func isProcessAlive(pgid int) bool {
	return procutil.IsAlive(pgid)
}

func (m *LocalManager) ForceStopService(name string) (result StopServiceResult, err error) {
	_, span := m.telemetry.StartSpan(m.ctx, "eos.service.force_stop", name)
	defer func() {
		otelx.End(span, err)
		otelx.RecordOutcome(m.ctx, m.telemetry.ServiceStops, name, err)
	}()

	stopResult, err := m.stopServiceWithSignal(name, syscall.SIGKILL)
	if err != nil {
		return StopServiceResult{}, err
	}

	allErrors := stopResult.Errored

	errorErrored := updateProcessHistoryEntriesAsUnknown(m, stopResult.Errored)
	maps.Copy(allErrors, errorErrored)

	forceKilledProcesses := make(map[int]bool)
	maps.Copy(forceKilledProcesses, stopResult.AlreadyDead)
	maps.Copy(forceKilledProcesses, stopResult.Pending)

	updateErrors := updateProcessHistoryEntriesAsStopped(m, forceKilledProcesses)
	maps.Copy(allErrors, updateErrors)

	return StopServiceResult{Errored: allErrors, Stopped: forceKilledProcesses}, nil
}

func updateProcessHistoryEntriesAsStopped(m *LocalManager, processes map[int]bool) map[int]string {
	errored := make(map[int]string, len(processes))
	for pgid := range processes {
		updates := database.ProcessHistoryUpdate{
			State:     new(types.ProcessStateStopped),
			StoppedAt: new(time.Now()),
		}

		err := m.db.UpdateProcessHistoryEntry(m.ctx, pgid, updates)
		if err != nil {
			errored[pgid] = fmt.Sprintf("recording the change for process '%v': %v", pgid, err)
		}
	}

	return errored
}

func updateProcessHistoryEntriesAsUnknown(m *LocalManager, processes map[int]string) map[int]string {
	errored := make(map[int]string, len(processes))
	for pgid := range processes {
		updates := database.ProcessHistoryUpdate{
			State: new(types.ProcessStateUnknown),
		}

		err := m.db.UpdateProcessHistoryEntry(m.ctx, pgid, updates)
		if err != nil {
			errored[pgid] = fmt.Sprintf("recording the change for process '%v': %v (original: %v)", pgid, err, processes[pgid])
		}
	}

	return errored
}

type StopRequestResult struct {
	AlreadyDead map[int]bool
	Errored     map[int]string
	Pending     map[int]bool
}

func (m *LocalManager) stopServiceWithSignal(name string, signal syscall.Signal) (StopRequestResult, error) {
	processHistory, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return StopRequestResult{}, fmt.Errorf("getting process history: %w", err)
	}

	pending := make(map[int]bool)
	alreadyDead := make(map[int]bool)
	errored := make(map[int]string)

	for i := range processHistory {
		p := &processHistory[i]
		processState := p.State
		processPGID := p.PGID

		switch processState {
		case types.ProcessStateStarting, types.ProcessStateRunning, types.ProcessStateUnknown:
			// Guard against PGID reuse before signaling. The kernel recycles
			// PGIDs, so a stored record whose process has since exited may now
			// point at an unrelated, later process. Signaling it blindly would
			// kill an innocent bystander (or, if it belongs to another user,
			// fail with EPERM and surface as a spurious stop error). Only signal
			// when the PGID is still alive AND its start time matches what we
			// recorded; otherwise the process we started is already gone.
			if !procutil.IsAliveMatching(processPGID, p.StartedAtTicks) {
				alreadyDead[processPGID] = true
				continue
			}
			err := syscall.Kill(-processPGID, signal)
			switch {
			case errors.Is(err, syscall.ESRCH):
				alreadyDead[processPGID] = true
			case err != nil:
				// The process was alive and ours a moment ago (IsAliveMatching
				// above), so an error here means it raced from running into an
				// exited/zombie state before the signal landed — e.g. a service
				// that exits the instant it's stopped. On macOS, signaling a
				// process group whose leader is now a zombie returns EPERM.
				// Classify by liveness, not the raw errno: if it's no longer
				// alive-matching it's already gone, not a stop failure.
				if !procutil.IsAliveMatching(processPGID, p.StartedAtTicks) {
					alreadyDead[processPGID] = true
				} else {
					errored[processPGID] = fmt.Sprintf("killing service: %v", err)
				}
			default:
				pending[processPGID] = true
			}
		case types.ProcessStateFailed, types.ProcessStateStopped:
			continue
		}
	}

	return StopRequestResult{
		AlreadyDead: alreadyDead,
		Errored:     errored,
		Pending:     pending,
	}, nil
}

// type SupportedRuntime string

// const (
// 	Bun    SupportedRuntime = "bun"
// 	Deno   SupportedRuntime = "deno"
// 	Node   SupportedRuntime = "node"
// 	NodeJs SupportedRuntime = "nodejs"
// )

// runtimeBinaryName maps a service's runtime type to the executable expected on
// the system PATH, or "" when no PATH check applies (custom/unknown runtimes).
func runtimeBinaryName(runtimeType string) string {
	switch runtimeType {
	case "bun":
		return "bun"
	case "deno":
		return "deno"
	case "node", "nodejs":
		return "node"
	default:
		return ""
	}
}

func (m *LocalManager) validateRuntimeBinary(config *types.ServiceConfig) error {
	if config.Runtime.Path != "" {
		if runtimePathErr := ValidateRuntimePath(config.Runtime); runtimePathErr != nil {
			return fmt.Errorf("validating config runtime: %w", runtimePathErr)
		}
		// Custom path validated successfully; skip system PATH check
		return nil
	}

	binary := runtimeBinaryName(config.Runtime.Type)
	if binary == "" {
		return nil
	}
	if _, lookPathErr := m.executor.LookPath(binary); lookPathErr != nil {
		return fmt.Errorf("%s not found in system PATH: %w", binary, lookPathErr)
	}
	return nil
}

func ValidateRuntimePath(runtime types.Runtime) error {
	runtimePath := runtime.Path

	if !filepath.IsAbs(runtime.Path) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting homeDir for runtime validation: %w", err)
		}
		runtimePath = filepath.Join(homeDir, runtime.Path)
	}

	dirInfo, err := os.Stat(runtimePath)
	if err != nil {
		return fmt.Errorf("stat runtime path: %w", err)
	}

	if !dirInfo.IsDir() {
		return fmt.Errorf("runtime path is not a directory")
	}

	switch runtime.Type {
	case "bun":
		bunPath := filepath.Join(runtimePath, "bun")
		bunInfo, err := os.Stat(bunPath)
		if err != nil {
			return fmt.Errorf("find bun binary in runtime path: %w", err)
		}

		if bunInfo.IsDir() {
			return fmt.Errorf("bun runtime binary path is a directory")
		}

		if bunInfo.Mode()&0111 == 0 {
			return fmt.Errorf("bun binary is not executable: %s", bunPath)
		}
		return nil
	case "deno":
		denoPath := filepath.Join(runtimePath, "deno")
		denoInfo, err := os.Stat(denoPath)
		if err != nil {
			return fmt.Errorf("find deno binary in runtime path: %w", err)
		}

		if denoInfo.IsDir() {
			return fmt.Errorf("deno runtime binary path is a directory")
		}

		if denoInfo.Mode()&0111 == 0 {
			return fmt.Errorf("deno binary is not executable: %s", denoPath)
		}

		return nil
	case "node", "nodejs":
		nodePath := filepath.Join(runtimePath, "node")
		nodeInfo, err := os.Stat(nodePath)
		if err != nil {
			return fmt.Errorf("find node binary in runtime path: %w", err)
		}

		if nodeInfo.IsDir() {
			return fmt.Errorf("node runtime binary path is a directory")
		}

		if nodeInfo.Mode()&0111 == 0 {
			return fmt.Errorf("node binary is not executable: %s", nodePath)
		}

		return nil
	}
	return nil
}

func buildEnvironment(config *types.ServiceConfig, serviceDirectoryPath string) ([]string, error) {
	env := os.Environ()

	if config.Runtime.Path != "" {
		index, after := doesEnvVarAlreadyExist("PATH=", env)

		if index > -1 {
			env[index] = fmt.Sprintf("PATH=%s:%s", config.Runtime.Path, after)
		} else {
			env = append(env, "PATH="+config.Runtime.Path)
		}
	}

	if config.Port != 0 {
		env = append(env, fmt.Sprintf("PORT=%d", config.Port))
	}

	if config.EnvFile != "" {
		envFileVars, err := ParseEnvFile(config, serviceDirectoryPath)
		if err != nil {
			return nil, err
		}
		for _, envVar := range envFileVars {
			before, _, _ := strings.Cut(envVar, "=")
			index, _ := doesEnvVarAlreadyExist(before+"=", env)
			if index > -1 {
				env[index] = envVar
			} else {
				env = append(env, envVar)
			}
		}
	}

	return env, nil
}

// ResolveEnvFilePath returns the absolute path to a service's env_file,
// rejecting paths that escape the service directory. Returns an empty string
// if config.EnvFile is unset.
func ResolveEnvFilePath(config *types.ServiceConfig, serviceDirectoryPath string) (string, error) {
	if config.EnvFile == "" {
		return "", nil
	}

	cleanedServiceDirectoryPath := filepath.Clean(serviceDirectoryPath)
	envFilePath := filepath.Clean(filepath.Join(cleanedServiceDirectoryPath, config.EnvFile))

	// Prevents path traversal outside service directory
	if !strings.HasPrefix(envFilePath, cleanedServiceDirectoryPath+string(filepath.Separator)) && envFilePath != cleanedServiceDirectoryPath {
		return "", fmt.Errorf("env file path %q escapes service directory", config.EnvFile)
	}

	return envFilePath, nil
}

// ParseEnvFile resolves the KEY=VALUE pairs defined in a service's env_file,
// relative to its service directory. Returns nil if config.EnvFile is unset.
// Later duplicate keys within the file override earlier ones.
func ParseEnvFile(config *types.ServiceConfig, serviceDirectoryPath string) ([]string, error) {
	if config.EnvFile == "" {
		return nil, nil
	}

	envFilePath, pathErr := ResolveEnvFilePath(config, serviceDirectoryPath)
	if pathErr != nil {
		return nil, pathErr
	}

	// #nosec G304 - envFilePath validated against traversal by ResolveEnvFilePath above
	envFileContents, readErr := os.ReadFile(envFilePath)
	if readErr != nil {
		return nil, fmt.Errorf("reading env file: %w", readErr)
	}

	envFileVars := []string{}
	for envVar := range strings.SplitSeq(string(envFileContents), "\n") {
		envVar = strings.TrimSpace(envVar)
		if envVar == "" {
			continue
		}
		if strings.HasPrefix(envVar, "#") {
			continue
		}
		before, _, found := strings.Cut(envVar, "=")
		if !found {
			continue
		}

		index, _ := doesEnvVarAlreadyExist(before+"=", envFileVars)
		if index > -1 {
			envFileVars[index] = envVar
		} else {
			envFileVars = append(envFileVars, envVar)
		}
	}

	return envFileVars, nil
}

func doesEnvVarAlreadyExist(envName string, env []string) (int, string) {
	for i, envVar := range env {
		if after, ok := strings.CutPrefix(envVar, envName); ok {
			return i, after
		}
	}
	return -1, ""
}
