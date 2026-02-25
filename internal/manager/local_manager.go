package manager

import (
	"context"
	"errors"
	"fmt"
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
	baseDir string
}

func NewLocalManager(db *database.DB, baseDir string, ctx context.Context) *LocalManager {
	return &LocalManager{db: db, baseDir: baseDir, ctx: ctx}
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

var ErrServiceNotRunning = errors.New("service is not running")

func (m *LocalManager) GetServiceInstance(name string) (*types.ServiceRuntime, error) {
	_, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("service is not registered, got:\n%w", err)
	}

	serviceInstance, err := m.db.GetServiceInstance(m.ctx, name)
	if errors.Is(err, database.ErrServiceNotFound) {
		return nil, ErrServiceNotRunning
	}
	if err != nil {
		return nil, fmt.Errorf("unknown error occurred when getting the registered service:\n%w", err)
	}

	return &serviceInstance, nil
}

var ErrServiceNotFound = errors.New("service not found")

func (m *LocalManager) GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error) {
	_, err := m.db.IsServiceRegistered(m.ctx, name)
	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("service is not registered, got:\n%w", err)
	}

	registeredService, err := m.db.GetServiceCatalogEntry(m.ctx, name)
	if errors.Is(err, ErrServiceNotFound) {
		return types.ServiceCatalogEntry{}, fmt.Errorf("service was not found, got:\n%w", err)
	}
	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("unknown error occurred when getting the registered service:\n%w", err)
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

func (m *LocalManager) StartService(name string) (int, error) {
	service, err := m.GetServiceCatalogEntry(name)
	if errors.Is(err, ErrServiceNotFound) {
		return 0, fmt.Errorf("service %s not found", name)
	}
	if err != nil {
		return 0, fmt.Errorf("an error occurred")
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
		// TODO: return found PID somehow instead?
		return 0, fmt.Errorf("service instance already found for %s. Use 'restart' to start this service again", name)
	}

	processHistory, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return 0, fmt.Errorf("unable to check for active process history for %s, got: %w", name, err)
	}

	for _, p := range processHistory {
		state := p.State
		pid := p.PID

		if state == types.ProcessStateRunning {
			process, _ := os.FindProcess(pid)
			signalErr := process.Signal(syscall.Signal(0))
			// TODO: Update the process history after failing this?
			if signalErr != nil {
				return 0, fmt.Errorf("service either has no active process or is inaccessible with PID %d", pid)
			}
			return 0, fmt.Errorf("service already running with PID %d", pid)
		}
		if state == types.ProcessStateStarting {
			return 0, fmt.Errorf("service already starting with PID %d", pid)
		}
	}

	logFile, errorLogFile, err := m.prepareLogFiles(service.Name)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare log files for %s: %w", name, err)
	}

	defer func() {
		if closeErr := logFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing the log file errored, got:\n %w", closeErr)
		}
	}()
	defer func() {
		if closeErr := errorLogFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close error log file for %s: %w", name, closeErr)
		}
	}()

	if config.Runtime.Path != "" {
		if runtimePathErr := validateRuntimePath(config.Runtime); runtimePathErr != nil {
			return 0, fmt.Errorf("runtime validation failed: %w", runtimePathErr)
		}
	} else {
		if config.Runtime.Type == "node" || config.Runtime.Type == "nodejs" {
			if _, lookPathErr := exec.LookPath("node"); lookPathErr != nil {
				return 0, fmt.Errorf("node not found in system PATH")
			}
		}
	}

	startCommand := exec.CommandContext(m.ctx, "/bin/sh", "-c", config.Command) // #nosec G204 -- command is user-defined in their service.yaml config

	// TODO: We are sourcing from a different object then the config here?
	// TODO: Add dynamic PATH variable addition
	startCommand.Dir = service.DirectoryPath
	env := buildEnvironment(config)
	if err != nil {
		return 0, fmt.Errorf("build environment failed with: %w", err)
	}
	startCommand.Env = env
	startCommand.Stdout = &logutil.TimestampWriter{W: logFile}
	startCommand.Stderr = &logutil.TimestampWriter{W: errorLogFile}

	// commandWithPath := filepath.Join(service.DirectoryPath, config.Command)
	err = startCommand.Start()

	if err != nil {
		return 0, fmt.Errorf("start command failed with: %w", err)
	}

	pid := startCommand.Process.Pid
	err = m.db.RegisterServiceInstance(m.ctx, service.Name)
	if err != nil {
		killErr := syscall.Kill(pid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to register service instance %d in database (%w) and failed to clean up process (%w) - manual intervention required", pid, err, killErr)
		}
		return pid, fmt.Errorf("unable to register the new service instance in the database - process has been cleaned up, got: %w", err)
	}

	err = m.db.UpdateServiceInstance(m.ctx, service.Name, database.ServiceInstanceUpdate{
		StartedAt: ptr.TimePtr(time.Now()),
	})

	if err != nil {
		killErr := syscall.Kill(pid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to update service instance %d in database (%w) and failed to clean up process (%w) - manual intervention required", pid, err, killErr)
		}
		return pid, fmt.Errorf("unable to update the new service instance in the database - process has been cleaned up, got: %w", err)
	}

	_, err = m.db.RegisterProcessHistoryEntry(m.ctx, pid, service.Name, types.ProcessStateUnknown)
	if err != nil {
		killErr := syscall.Kill(pid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to register process %d in database (%w) and failed to clean up process (%w) - manual intervention required", pid, err, killErr)
		}
		return pid, fmt.Errorf("unable to register the new process in the database - process has been cleaned up, got: %w", err)
	}

	updates := database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateStarting),
		StartedAt: ptr.TimePtr(time.Now()),
	}

	// TODO: Consider adding process cleanup (kill) here for consistency
	// with the rollback behavior of RegisterServiceInstance and
	// RegisterProcessHistoryEntry failures above. Currently a failure
	// here leaves a running process with inconsistent DB state.
	err = m.db.UpdateProcessHistoryEntry(m.ctx, pid, updates)
	if err != nil {
		return pid, fmt.Errorf("unable to update the new process in the database, got: %w", err)
	}
	return pid, nil
}

func (m *LocalManager) RestartService(name string) (pid int, err error) {
	service, err := m.GetServiceCatalogEntry(name)
	if errors.Is(err, ErrServiceNotFound) {
		return 0, fmt.Errorf("service %s not found", name)
	}
	if err != nil {
		return 0, fmt.Errorf("an error occurred")
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := LoadServiceConfig(configPath)
	if err != nil {
		return 0, fmt.Errorf("service config for %s failed to load, got:\n %w", name, err)
	}

	serviceInstance, err := m.GetServiceInstance(name)
	if err != nil {
		return 0, fmt.Errorf("unable to check for service instance for %s, got: %w", name, err)
	}
	if serviceInstance == nil {
		return 0, fmt.Errorf("no service instance found for %s, got: %w", name, err)
	}

	logFile, errorLogFile, err := m.prepareLogFiles(service.Name)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare log files for %s: %w", name, err)
	}

	defer func() {
		if closeErr := logFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing the log file errored, got:\n %w", closeErr)
		}
	}()
	defer func() {
		if closeErr := errorLogFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close error log file for %s: %w", name, closeErr)
		}
	}()

	if config.Runtime.Path != "" {
		if runtimePathErr := validateRuntimePath(config.Runtime); runtimePathErr != nil {
			return 0, fmt.Errorf("runtime validation failed: %w", runtimePathErr)
		}
	} else {
		if config.Runtime.Type == "node" || config.Runtime.Type == "nodejs" {
			if _, lookPathErr := exec.LookPath("node"); lookPathErr != nil {
				return 0, fmt.Errorf("node not found in system PATH")
			}
		}
	}

	env := buildEnvironment(config)
	if err != nil {
		return 0, fmt.Errorf("build environment failed with: %w", err)
	}

	stopResult, err := m.StopService(name)
	if err != nil {
		return 0, fmt.Errorf("unable to stop for process(es) for %s, got: %w", name, err)
	}
	if len(stopResult.Failed) > 0 {
		return 0, fmt.Errorf("failed to stop for process(es) for %s, got: %w", name, err)
	}

	restartCommand := exec.CommandContext(m.ctx, "/bin/sh", "-c", config.Command) // #nosec G204 -- command is user-defined in their service.yaml config
	// TODO: We are sourcing from a different object then the config here?
	restartCommand.Dir = service.DirectoryPath
	restartCommand.Env = env
	restartCommand.Stdout = &logutil.TimestampWriter{W: logFile}
	restartCommand.Stderr = &logutil.TimestampWriter{W: errorLogFile}

	err = restartCommand.Start()

	if err != nil {
		return 0, fmt.Errorf("start command failed with: %w", err)
	}
	pid = restartCommand.Process.Pid

	err = m.db.UpdateServiceInstance(m.ctx, service.Name, database.ServiceInstanceUpdate{
		StartedAt:    ptr.TimePtr(time.Now()),
		RestartCount: ptr.IntPtr(serviceInstance.RestartCount + 1),
	})

	if err != nil {
		killErr := syscall.Kill(pid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to update service instance %d in database (%w) and failed to clean up process (%w) - manual intervention required", pid, err, killErr)
		}
		return pid, fmt.Errorf("unable to update the new service instance in the database - process has been cleaned up, got: %w", err)
	}

	_, err = m.db.RegisterProcessHistoryEntry(m.ctx, pid, service.Name, types.ProcessStateUnknown)
	if err != nil {
		killErr := syscall.Kill(pid, syscall.SIGKILL)
		if killErr != nil {
			return 0, fmt.Errorf("unable to register process %d in database (%w) and failed to clean up process (%w) - manual intervention required", pid, err, killErr)
		}
		return pid, fmt.Errorf("unable to register the new process in the database - process has been cleaned up, got: %w", err)
	}

	updates := database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateStarting),
		StartedAt: ptr.TimePtr(time.Now()),
	}

	// TODO: Consider adding process cleanup (kill) here for consistency
	// with the rollback behavior of RegisterServiceInstance and
	// RegisterProcessHistoryEntry failures above. Currently a failure
	// here leaves a running process with inconsistent DB state.
	err = m.db.UpdateProcessHistoryEntry(m.ctx, pid, updates)
	if err != nil {
		return pid, fmt.Errorf("unable to update the new process in the database, got: %w", err)
	}
	return pid, nil
}

func (m *LocalManager) prepareLogFiles(serviceName string) (logFile *os.File, errorLogFile *os.File, err error) {
	logPath, errorLogPath, err := m.CreateServiceLogFiles(serviceName)
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

type StopResult struct {
	Failed  map[int]string
	Stopped []int
}

func (m *LocalManager) StopService(name string) (StopResult, error) {
	return m.stopServiceWithSignal(name, syscall.SIGTERM)
}

func (m *LocalManager) ForceStopService(name string) (StopResult, error) {
	return m.stopServiceWithSignal(name, syscall.SIGKILL)
}

func (m *LocalManager) stopServiceWithSignal(name string, signal syscall.Signal) (StopResult, error) {
	processHistory, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return StopResult{}, fmt.Errorf("unable to check for active process history to stop for %s, got: %w", name, err)
	}

	var stopped []int
	failed := make(map[int]string)

	for _, p := range processHistory {
		processState := p.State
		processPID := p.PID
		if processState == types.ProcessStateRunning || processState == types.ProcessStateStarting {
			err := syscall.Kill(processPID, signal)
			if err != nil {
				failed[processPID] = fmt.Sprintf("process '%v' for service '%s' errored with: %v", processPID, name, err)
			} else {
				updates := database.ProcessHistoryUpdate{
					State:     ptr.ProcessStatePtr(types.ProcessStateStopped),
					StoppedAt: ptr.TimePtr(time.Now()),
				}

				err := m.db.UpdateProcessHistoryEntry(m.ctx, processPID, updates)
				if err != nil {
					failed[processPID] = fmt.Sprintf("recording the change for process '%v' for service '%s' errored with: %v", processPID, name, err)
				} else {
					stopped = append(stopped, processPID)
				}
			}
		}
	}

	return StopResult{
		Stopped: stopped,
		Failed:  failed,
	}, nil
}

func validateRuntimePath(runtime types.Runtime) error {
	runtimePath := runtime.Path

	if !filepath.IsAbs(runtime.Path) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get homeDir during runtime validation, got: %w", err)
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
