package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"eos/internal/database"
	"eos/internal/logutil"
	"eos/internal/ptr"
	"eos/internal/types"
)

type LocalManager struct {
	db      database.Database
	ctx     context.Context
	logger  logutil.ProcessLogger
	baseDir string
}

func NewLocalManager(db *database.DB, baseDir string, ctx context.Context, logger logutil.ProcessLogger) *LocalManager {
	return &LocalManager{db: db, baseDir: baseDir, ctx: ctx, logger: logger}
}

var ErrServiceAlreadyRegistered = errors.New("service already registered")

func (m *LocalManager) AddServiceCatalogEntry(newServiceCatalogEntry *types.ServiceCatalogEntry) error {
	isRegistered, err := m.db.IsServiceRegistered(m.ctx, newServiceCatalogEntry.Name)
	if err != nil {
		return fmt.Errorf("unable to check: %w", err)
	}
	if isRegistered {
		return ErrServiceAlreadyRegistered
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
		return false, fmt.Errorf("unable to remove service, got: %w", err)
	}
	return removed, nil
}

func (m *LocalManager) RemoveServiceCatalogEntry(name string) (bool, error) {
	removed, err := m.db.RemoveServiceCatalogEntry(m.ctx, name)
	if err != nil {
		return false, fmt.Errorf("unable to remove the service from the catalog, got: %w", err)
	}
	return removed, nil
}

func (m *LocalManager) IsServiceRegistered(name string) (bool, error) {
	isRegistered, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return false, fmt.Errorf("unable to check: \n %w", err)
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

var ErrServiceNotRunning = errors.New("service not running")

func (m *LocalManager) GetServiceInstance(name string) (*types.ServiceRuntime, error) {
	_, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("service %q not registered: %w", name, err)
	}

	serviceInstance, err := m.db.GetServiceInstance(m.ctx, name)
	if isNotFound(err) {
		return nil, ErrServiceNotRunning
	}
	if err != nil {
		return nil, fmt.Errorf("unknown error occurred getting the registered service instance:\n%w", err)
	}

	return &serviceInstance, nil
}

var ErrServiceNotRegistered = errors.New("service not registered")

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
		return types.ServiceCatalogEntry{}, fmt.Errorf("unknown error occurred getting the service entry:\n%w", err)
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

var ErrNotFound = errors.New("not found")

func (m *LocalManager) GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error) {
	processHistory, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("unable to check for active process history for %s, got: %w", name, err)
	}

	if len(processHistory) == 0 {
		return nil, ErrNotFound
	}

	mostRecentIdx := 0
	for i := 1; i < len(processHistory); i++ {
		if processHistory[i].StartedAt.After(*processHistory[mostRecentIdx].StartedAt) {
			mostRecentIdx = i
		}
	}

	return &processHistory[mostRecentIdx], nil
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

func (m *LocalManager) pipeToLogFile(r *os.File, w *os.File, name string) {
	tw := &logutil.TimestampWriter{W: w}
	if _, copyErr := io.Copy(tw, r); copyErr != nil {
		m.logger.Log(logutil.LogLevelError, fmt.Sprintf("copying read log pipe data to timestamp writer for %s: %v", name, copyErr))
	}
	if err := r.Close(); err != nil {
		m.logger.Log(logutil.LogLevelError, fmt.Sprintf("closing read log file pipe for %s: %v", name, err))
	}
	if err := w.Close(); err != nil {
		m.logger.Log(logutil.LogLevelError, fmt.Sprintf("closing write file for %s: %v", name, err))
	}
}

func (m *LocalManager) pipeToErrorLogFile(r *os.File, w *os.File, name string) {
	tw := &logutil.TimestampWriter{W: w}
	if _, copyErr := io.Copy(tw, r); copyErr != nil {
		m.logger.Log(logutil.LogLevelError, fmt.Sprintf("copying read error log pipe data to timestamp writer for %s: %v", name, copyErr))
	}
	if err := r.Close(); err != nil {
		m.logger.Log(logutil.LogLevelError, fmt.Sprintf("closing read error log file pipe for %s: %v", name, err))
	}
	if err := w.Close(); err != nil {
		m.logger.Log(logutil.LogLevelError, fmt.Sprintf("closing write error file for %s: %v", name, err))
	}
}

var ErrAlreadyRunning = errors.New("already running")

func (m *LocalManager) StartService(name string) (pgid int, err error) {
	service, err := m.GetServiceCatalogEntry(name)
	if errors.Is(err, ErrServiceNotRegistered) {
		return 0, fmt.Errorf("service %s not registered", name)
	}
	if err != nil {
		return 0, fmt.Errorf("an error occurred: %w", err)
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := LoadServiceConfig(configPath)

	if err != nil {
		return 0, fmt.Errorf("service config for %s failed to load, got:\n %w", name, err)
	}

	serviceInstance, err := m.GetServiceInstance(name)
	if err != nil && !errors.Is(err, ErrServiceNotRunning) {
		return 0, fmt.Errorf("unable to check for service instance for %s, got: %w", name, err)
	}
	if serviceInstance != nil {
		// TODO: return found PGID somehow instead?
		return 0, ErrAlreadyRunning
	}

	processHistory, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return 0, fmt.Errorf("unable to check for active process history for %s, got: %w", name, err)
	}

	for _, p := range processHistory {
		state := p.State
		processPGID := p.PGID

		if state == types.ProcessStateRunning {
			process, _ := os.FindProcess(processPGID)
			signalErr := process.Signal(syscall.Signal(0))
			// TODO: Update the process history after failing this?
			if signalErr != nil {
				return 0, fmt.Errorf("service either has no active process or is inaccessible with PGID %d", processPGID)
			}
			return 0, fmt.Errorf("service already running with PGID %d", processPGID)
		}
		if state == types.ProcessStateStarting {
			return 0, fmt.Errorf("service already starting with PGID %d", processPGID)
		}
	}

	logFile, errorLogFile, err := m.prepareLogFiles(service.Name)
	if err != nil {
		return 0, fmt.Errorf("preparing log files for %s: %w", name, err)
	}

	// defer func() {
	// 	if closeErr := logFile.Close(); closeErr != nil && err == nil {
	// 		err = fmt.Errorf("closing log file for %s: %w", name, closeErr)
	// 	}
	// }()
	// defer func() {
	// 	if closeErr := errorLogFile.Close(); closeErr != nil && err == nil {
	// 		err = fmt.Errorf("closing error log file for %s: %w", name, closeErr)
	// 	}
	// }()

	readLogFilePipe, writeLogFilePipe, err := newPipeForStd()
	if err != nil {
		return 0, fmt.Errorf("creating log file pipe for %s: %w", name, err)
	}

	readErrorLogFilePipe, writeErrorLogFilePipe, err := newPipeForStd()
	if err != nil {
		return 0, fmt.Errorf("creating error log file pipe for %s: %w", name, err)
	}

	startSuccess := false
	defer func() {
		if !startSuccess {
			var closeErrs []error
			if closeErr := readLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing read log file pipe: %w", closeErr))
			}
			if closeErr := writeLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing write log file pipe: %w", closeErr))
			}
			if closeErr := readErrorLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing read error log file pipe: %w", closeErr))
			}
			if closeErr := writeErrorLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing write error log file pipe: %w", closeErr))
			}
			if closeErr := logFile.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing log file: %w", closeErr))
			}
			if closeErr := errorLogFile.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing error log file: %w", closeErr))
			}
			if len(closeErrs) > 0 {
				err = errors.Join(err, errors.Join(closeErrs...))
			}
		}
	}()

	if config.Runtime.Path != "" {
		if runtimePathErr := validateRuntimePath(config.Runtime); runtimePathErr != nil {
			return 0, fmt.Errorf("validating config runtime: %w", runtimePathErr)
		}
	} else {
		if config.Runtime.Type == "node" || config.Runtime.Type == "nodejs" {
			if _, lookPathErr := exec.LookPath("node"); lookPathErr != nil {
				return 0, fmt.Errorf("node not found in system PATH")
			}
		}
	}

	// commandWithPath := filepath.Join(service.DirectoryPath, config.Command)
	startCommand := exec.CommandContext(m.ctx, "/bin/sh", "-c", config.Command) // #nosec G204 -- command is user-defined in their service.yaml config
	startCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// TODO: We are sourcing from a different object then the config here?
	// TODO: Add dynamic PATH variable addition
	startCommand.Dir = service.DirectoryPath
	env := buildEnvironment(config)

	startCommand.Env = env
	startCommand.Stdout = writeLogFilePipe
	startCommand.Stderr = writeErrorLogFilePipe

	if startErr := startCommand.Start(); startErr != nil {
		return 0, fmt.Errorf("start command failed with: %w", startErr)
	}
	startSuccess = true

	if closeErr := writeLogFilePipe.Close(); closeErr != nil {
		return 0, fmt.Errorf("closing write log file pipe for %s: %w", name, closeErr)
	}

	if closeErr := writeErrorLogFilePipe.Close(); closeErr != nil {
		return 0, fmt.Errorf("closing write error log file pipe for %s: %w", name, closeErr)
	}

	go m.pipeToLogFile(readLogFilePipe, logFile, name)
	go m.pipeToErrorLogFile(readErrorLogFilePipe, errorLogFile, name)

	go func() {
		_ = startCommand.Wait()
	}()

	pgid, err = syscall.Getpgid(startCommand.Process.Pid)
	if err != nil {
		return 0, fmt.Errorf("getting pgid: %w", err)
	}

	err = m.db.RegisterServiceInstance(m.ctx, service.Name)
	if err != nil {
		killErr := syscall.Kill(-pgid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to register service instance %d in database (%w) and failed to clean up process (%w) - manual intervention required", pgid, err, killErr)
		}
		return pgid, fmt.Errorf("unable to register the new service instance in the database - process has been cleaned up, got: %w", err)
	}

	err = m.db.UpdateServiceInstance(m.ctx, service.Name, database.ServiceInstanceUpdate{
		StartedAt: ptr.TimePtr(time.Now()),
	})
	if err != nil {
		killErr := syscall.Kill(-pgid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to update service instance %d in database (%w) and failed to clean up process (%w) - manual intervention required", pgid, err, killErr)
		}
		return pgid, fmt.Errorf("unable to update the new service instance in the database - process has been cleaned up, got: %w", err)
	}

	_, err = m.db.RegisterProcessHistoryEntry(m.ctx, pgid, service.Name, types.ProcessStateUnknown)
	if err != nil {
		killErr := syscall.Kill(-pgid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to register process %d in database (%w) and failed to clean up process (%w) - manual intervention required", pgid, err, killErr)
		}
		return pgid, fmt.Errorf("unable to register the new process in the database - process has been cleaned up, got: %w", err)
	}

	updates := database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateStarting),
		StartedAt: ptr.TimePtr(time.Now()),
	}

	// TODO: Consider adding process cleanup (kill) here for consistency
	// with the rollback behavior of RegisterServiceInstance and
	// RegisterProcessHistoryEntry failures above. Currently a failure
	// here leaves a running process with inconsistent DB state.
	err = m.db.UpdateProcessHistoryEntry(m.ctx, pgid, updates)
	if err != nil {
		return pgid, fmt.Errorf("unable to update the new process in the database, got: %w", err)
	}
	return pgid, nil
}

func (m *LocalManager) RestartService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (pgid int, err error) {
	service, err := m.GetServiceCatalogEntry(name)
	if errors.Is(err, ErrServiceNotRegistered) {
		return 0, fmt.Errorf("service %s not registered", name)
	}
	if err != nil {
		return 0, fmt.Errorf("an error occurred: %w", err)
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := LoadServiceConfig(configPath)
	if err != nil {
		return 0, fmt.Errorf("service config for %s failed to load, got:\n %w", name, err)
	}

	serviceInstance, err := m.GetServiceInstance(name)
	if err != nil {
		return 0, fmt.Errorf("getting service instance for %s: %w", name, err)
	}
	if serviceInstance == nil {
		return 0, fmt.Errorf("no service instance found for %s: %w", name, err)
	}

	logFile, errorLogFile, err := m.prepareLogFiles(service.Name)
	if err != nil {
		return 0, fmt.Errorf("preparing log files for %s: %w", name, err)
	}

	// defer func() {
	// 	if closeErr := logFile.Close(); closeErr != nil && err == nil {
	// 		err = fmt.Errorf("closing log file for %s: %w", name, closeErr)
	// 	}
	// }()
	// defer func() {
	// 	if closeErr := errorLogFile.Close(); closeErr != nil && err == nil {
	// 		err = fmt.Errorf("closing error log file for %s: %w", name, closeErr)
	// 	}
	// }()

	readLogFilePipe, writeLogFilePipe, err := newPipeForStd()
	if err != nil {
		return 0, fmt.Errorf("creating log file pipe for %s: %w", name, err)
	}

	readErrorLogFilePipe, writeErrorLogFilePipe, err := newPipeForStd()
	if err != nil {
		return 0, fmt.Errorf("creating error log file pipe for %s: %w", name, err)
	}

	restartSuccess := false
	defer func() {
		if !restartSuccess {
			var closeErrs []error
			if closeErr := readLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing read log file pipe: %w", closeErr))
			}
			if closeErr := writeLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing write log file pipe: %w", closeErr))
			}
			if closeErr := readErrorLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing read error log file pipe: %w", closeErr))
			}
			if closeErr := writeErrorLogFilePipe.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing write error log file pipe: %w", closeErr))
			}
			if closeErr := logFile.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing log file: %w", closeErr))
			}
			if closeErr := errorLogFile.Close(); closeErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("closing error log file: %w", closeErr))
			}
			if len(closeErrs) > 0 {
				err = errors.Join(err, errors.Join(closeErrs...))
			}
		}
	}()

	if config.Runtime.Path != "" {
		if runtimePathErr := validateRuntimePath(config.Runtime); runtimePathErr != nil {
			return 0, fmt.Errorf("validating config runtime: %w", runtimePathErr)
		}
	} else {
		if config.Runtime.Type == "node" || config.Runtime.Type == "nodejs" {
			if _, lookPathErr := exec.LookPath("node"); lookPathErr != nil {
				return 0, fmt.Errorf("node not found in system PATH")
			}
		}
	}

	stopResult, err := m.StopService(name, gracePeriod, tickerPeriod)
	if err != nil {
		return 0, fmt.Errorf("stopping process(es) for %s: %w", name, err)
	}
	if len(stopResult.Errored) > 0 {
		return 0, fmt.Errorf("stopping process(es) for %s: %w", name, err)
	}

	restartCommand := exec.CommandContext(m.ctx, "/bin/sh", "-c", config.Command) // #nosec G204 -- command is user-defined in their service.yaml config
	restartCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// TODO: We are sourcing from a different object then the config here?
	restartCommand.Dir = service.DirectoryPath
	env := buildEnvironment(config)

	restartCommand.Env = env
	restartCommand.Stdout = writeLogFilePipe
	restartCommand.Stderr = writeErrorLogFilePipe

	if restartErr := restartCommand.Start(); restartErr != nil {
		return 0, fmt.Errorf("restart command failed with: %w", restartErr)
	}
	restartSuccess = true

	if closeErr := writeLogFilePipe.Close(); closeErr != nil {
		return 0, fmt.Errorf("closing write log file pipe for %s: %w", name, closeErr)
	}

	if closeErr := writeErrorLogFilePipe.Close(); closeErr != nil {
		return 0, fmt.Errorf("closing write error log file pipe for %s: %w", name, closeErr)
	}

	go m.pipeToLogFile(readLogFilePipe, logFile, name)
	go m.pipeToErrorLogFile(readErrorLogFilePipe, errorLogFile, name)

	go func() {
		_ = restartCommand.Wait()
	}()

	pgid, err = syscall.Getpgid(restartCommand.Process.Pid)
	if err != nil {
		return 0, fmt.Errorf("getting pgid: %w", err)
	}

	err = m.db.UpdateServiceInstance(m.ctx, service.Name, database.ServiceInstanceUpdate{
		StartedAt:    ptr.TimePtr(time.Now()),
		RestartCount: ptr.IntPtr(serviceInstance.RestartCount + 1),
	})

	if err != nil {
		killErr := syscall.Kill(-pgid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to update service instance %d in database (%w) and failed to clean up process (%w) - manual intervention required", pgid, err, killErr)
		}
		return pgid, fmt.Errorf("unable to update the new service instance in the database - process has been cleaned up, got: %w", err)
	}

	_, err = m.db.RegisterProcessHistoryEntry(m.ctx, pgid, service.Name, types.ProcessStateUnknown)
	if err != nil {
		killErr := syscall.Kill(-pgid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to register process %d in database (%w) and failed to clean up process (%w) - manual intervention required", pgid, err, killErr)
		}
		return pgid, fmt.Errorf("unable to register the new process in the database - process has been cleaned up, got: %w", err)
	}

	updates := database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateStarting),
		StartedAt: ptr.TimePtr(time.Now()),
	}

	// TODO: Consider adding process cleanup (kill) here for consistency
	// with the rollback behavior of RegisterServiceInstance and
	// RegisterProcessHistoryEntry failures above. Currently a failure
	// here leaves a running process with inconsistent DB state.
	err = m.db.UpdateProcessHistoryEntry(m.ctx, pgid, updates)
	if err != nil {
		return pgid, fmt.Errorf("unable to update the new process in the database, got: %w", err)
	}
	return pgid, nil
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
		err = logFile.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to close log file - during clean up: %w", err)
		}
		return nil, nil, fmt.Errorf("failed to open error log file: %w", err)
	}

	return logFile, errorLogFile, nil
}

type StopServiceResult struct {
	Errored   map[int]string
	Stopped   map[int]bool
	StaleData map[int]string
}

func (m *LocalManager) StopService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (StopServiceResult, error) {
	requestStartTime := time.Now()
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

	ticker := time.NewTicker(tickerPeriod)
	defer ticker.Stop()

	erroredProcesses := stopResult.Errored
	stoppedProcesses := make(map[int]bool)

OuterLoop:
	for {
		select {
		case <-ticker.C:
			if time.Since(requestStartTime) > gracePeriod {
				if len(stoppedProcesses) != countPending {
					for pendingPID := range stopResult.Pending {
						_, ok := stoppedProcesses[pendingPID]
						if ok {
							continue
						}
						erroredProcesses[pendingPID] = "killing service: exceeded grace period"
					}
				}

				break OuterLoop
			}

			for pendingPID := range stopResult.Pending {
				_, ok := stoppedProcesses[pendingPID]
				if ok {
					continue
				}
				if !isProcessAlive(pendingPID) {
					stoppedProcesses[pendingPID] = true
				}
			}

			if len(stoppedProcesses) == countPending {
				break OuterLoop
			}

		case <-m.ctx.Done():
			// User canceled, return empty result. System will check all again.
			return StopServiceResult{}, nil
		}
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

// TODO: Rewrite this?
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func (m *LocalManager) ForceStopService(name string) (StopServiceResult, error) {
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
			State:     ptr.ProcessStatePtr(types.ProcessStateStopped),
			StoppedAt: ptr.TimePtr(time.Now()),
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
			State: ptr.ProcessStatePtr(types.ProcessStateUnknown),
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

	for _, p := range processHistory {
		processState := p.State
		processPGID := p.PGID

		switch processState {
		case types.ProcessStateStarting, types.ProcessStateRunning, types.ProcessStateUnknown:
			err := syscall.Kill(-processPGID, signal)
			if errors.Is(err, syscall.ESRCH) {
				alreadyDead[processPGID] = true
			} else if err != nil {
				errored[processPGID] = fmt.Sprintf("killing service: %v", err)
			} else {
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

func validateRuntimePath(runtime types.Runtime) error {
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
		return fmt.Errorf("the specified runtime path is not a valid location, got: %w", err)
	}

	if !dirInfo.IsDir() {
		return fmt.Errorf("the specified runtime path is not a directory")
	}

	if runtime.Type == "nodejs" || runtime.Type == "node" {
		nodePath := filepath.Join(runtimePath, "node")
		nodeInfo, err := os.Stat(nodePath)
		if err != nil {
			return fmt.Errorf("unable to find node binary in specified path")
		}

		if nodeInfo.IsDir() {
			return fmt.Errorf("the constructed full path for the runtime is a directory")
		}

		if nodeInfo.Mode()&0111 == 0 {
			return fmt.Errorf("node binary is not executable: %s", nodePath)
		}

		return nil
	}
	return nil
}

func buildEnvironment(config *types.ServiceConfig) []string {
	env := os.Environ()

	if config.Runtime.Path != "" {
		pathFound := false

		for i, envVar := range env {
			if after, ok := strings.CutPrefix(envVar, "PATH="); ok {
				currentPath := after

				env[i] = fmt.Sprintf("PATH=%s:%s", config.Runtime.Path, currentPath)
				pathFound = true
				break
			}
		}

		if !pathFound {
			updatedPath := fmt.Sprintf("PATH=%s", config.Runtime.Path)
			env = append(env, updatedPath)
		}
	}

	if config.Port != 0 {
		env = append(env, fmt.Sprintf("PORT=%d", config.Port))
	}

	return env
}
