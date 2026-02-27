package monitor

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/ptr"
	"eos/internal/types"
)

type HealthMonitor struct {
	mgr                 *manager.LocalManager
	db                  *database.DB
	logger              *manager.DaemonLogger
	stopCh              chan struct{}
	checkInterval       time.Duration
	timeoutEnable       bool
	timeoutLimit        time.Duration
	maxRestartCount     int
	shutdownGracePeriod time.Duration
}

func NewHealthMonitor(mgr *manager.LocalManager, db *database.DB, logger *manager.DaemonLogger,
	healthConfig config.HealthConfig, shutdownConfig config.ShutdownConfig) *HealthMonitor {
	return &HealthMonitor{
		mgr:                 mgr,
		db:                  db,
		logger:              logger,
		stopCh:              make(chan struct{}),
		checkInterval:       2 * time.Second,
		timeoutEnable:       healthConfig.Timeout.Enable,
		timeoutLimit:        healthConfig.Timeout.Limit,
		maxRestartCount:     healthConfig.MaxRestart,
		shutdownGracePeriod: shutdownConfig.GracePeriod,
	}
}

func (hm *HealthMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(hm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hm.checkAllServices(ctx)
		case <-hm.stopCh:
			return
		}
	}
}

func (hm *HealthMonitor) Stop() {
	close(hm.stopCh)
}

func (hm *HealthMonitor) checkAllServices(ctx context.Context) {
	services, err := hm.mgr.GetAllServiceCatalogEntries()

	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("failed to get service: %v", err))
		return
	}

	for i := range services {
		service := &services[i]
		serviceName := service.Name
		instance, err := hm.mgr.GetServiceInstance(serviceName)
		if err != nil || instance == nil {
			continue
		}

		processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
		if err != nil || processHistoryEntry == nil {
			continue
		}

		switch processHistoryEntry.State {
		case types.ProcessStateStarting:
			hm.checkStartProcess(ctx, service, processHistoryEntry, hm.timeoutLimit, hm.timeoutEnable)
		case types.ProcessStateRunning:
			hm.checkRunningProcess(ctx, service, processHistoryEntry)
		case types.ProcessStateFailed:
			hm.checkFailedProcess(ctx, service, processHistoryEntry, instance, &hm.maxRestartCount)
		case types.ProcessStateUnknown:
			hm.checkUnknownProcess(ctx, service, processHistoryEntry)
		case types.ProcessStateStopped:
			continue
		}
	}
}

func (hm *HealthMonitor) checkStartProcess(
	ctx context.Context,
	service *types.ServiceCatalogEntry,
	process *types.ProcessHistory,
	timeoutLimit time.Duration,
	timeoutEnabled bool,
) {
	serviceName := service.Name
	pid := process.PID

	if !hm.isProcessAlive(pid) {
		errorString := fmt.Sprintf("service %s (PID %d) died during startup", serviceName, pid)

		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
		}
		hm.logger.Log(manager.LogLevelError, errorString)
		err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
			State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: ptr.TimePtr(time.Now()),
			Error:     ptr.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	if timeoutEnabled && time.Since(*process.StartedAt) > timeoutLimit {
		errorString := fmt.Sprintf("service %s taking too long to start", serviceName)

		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
		}
		hm.logger.Log(manager.LogLevelWarn, errorString)
		// TODO: Add more handeling to this
		err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
			State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: ptr.TimePtr(time.Now()),
			Error:     ptr.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("failed to load config for %s: %v", serviceName, err))
		return
	}

	configPort := config.Port

	// if configPort != 0 && !hm.canConnectToPort(configPort) {
	// 	errorString := fmt.Sprintf("service %s is not running on port %d", serviceName, configPort)

	// 	hm.logger.Log(manager.LogLevelInfo, errorString)
	// 	err = hm.mgr.LogToServiceStderr(serviceName, errorString)
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
	// 	}
	// 	err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
	// 		State:     ptr.ProcessStatePtr(types.ProcessStateStopped),
	// 		StoppedAt: ptr.TimePtr(time.Now()),
	// 		Error:     ptr.StringPtr(errorString),
	// 	})
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
	// 	}
	// 	return
	// }

	if configPort != 0 {
		hm.logger.Log(manager.LogLevelInfo, fmt.Sprintf("service %s is now running on port %d", serviceName, configPort))
	} else {
		hm.logger.Log(manager.LogLevelInfo, fmt.Sprintf("service %s is now running", serviceName))
	}

	err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
		State: ptr.ProcessStatePtr(types.ProcessStateRunning),
		Error: ptr.StringPtr(""),
	})
	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
	}
}

func (hm *HealthMonitor) checkRunningProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory) {
	// configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	// config, err := manager.LoadServiceConfig(configPath)
	serviceName := service.Name
	pid := process.PID

	// if err != nil {
	// 	hm.logger.Log(manager.LogLevelError,
	// 		fmt.Sprintf("failed to load config for %s: %v", serviceName, err))
	// 	return
	// }

	if !hm.isProcessAlive(pid) {
		errorString := fmt.Sprintf("service %s is not running", serviceName)

		hm.logger.Log(manager.LogLevelInfo, errorString)
		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
		}

		err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
			State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: ptr.TimePtr(time.Now()),
			Error:     ptr.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
		}
	}

	// configPort := config.Port
	// //NOTE: Does a failed check on the port mean the process is wrong, or the check is wrong?
	// connectable, err := hm.canConnectToPort(configPort)
	// if err != nil {
	// 	hm.logger.Log(manager.LogLevelInfo, err.Error())
	// 	err = hm.mgr.LogToServiceStderr(serviceName, err.Error())
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
	// 	}
	// 	return
	// }

	// if configPort != 0 && !connectable {
	// 	errorString := fmt.Sprintf("service %s is not running on port %d", serviceName, configPort)

	// 	hm.logger.Log(manager.LogLevelInfo, errorString)
	// 	err = hm.mgr.LogToServiceStderr(serviceName, errorString)
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
	// 	}
	// 	err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
	// 		State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
	// 		StoppedAt: ptr.TimePtr(time.Now()),
	// 		Error:     ptr.StringPtr(errorString),
	// 	})
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
	// 	}
	// 	return
	// }
}

func (hm *HealthMonitor) checkFailedProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceRuntime, maxRestartCount *int) {
	serviceName := service.Name
	pid := process.PID
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)

	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("failed to load config for %s: %v", serviceName, err))
		return
	}

	if hm.isProcessAlive(pid) {
		updateString := fmt.Sprintf("service %s is running", serviceName)

		hm.logger.Log(manager.LogLevelInfo, updateString)
		err = hm.mgr.LogToServiceStdout(serviceName, updateString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service output for %s: %v", serviceName, err))
		}

		err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
			State: ptr.ProcessStatePtr(types.ProcessStateRunning),
			Error: ptr.StringPtr(""),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	// TODO: Do we want to incorporate instance.last_health_check instead process?
	elapsed := time.Since(*process.StartedAt)
	requiredDelay := calculateBackoffDelay(instance.RestartCount)

	if instance.RestartCount < *maxRestartCount && elapsed >= requiredDelay {
		var errorString string

		if config.Port != 0 {
			errorString = fmt.Sprintf("restarting service %s on port %d", serviceName, config.Port)
		} else {
			errorString = fmt.Sprintf("restarting service %s", serviceName)
		}

		hm.logger.Log(manager.LogLevelInfo, errorString)
		err = hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
		}
		_, err := hm.mgr.RestartService(serviceName, hm.shutdownGracePeriod, 200*time.Millisecond)

		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to restart the service %s: %v", serviceName, err))
			return
		}
	}
}

func (hm *HealthMonitor) checkUnknownProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory) {
	serviceName := service.Name
	pid := process.PID
	processIsAlive := hm.isProcessAlive(pid)

	if processIsAlive {
		updateString := fmt.Sprintf("service %s is running", serviceName)

		hm.logger.Log(manager.LogLevelInfo, updateString)
		err := hm.mgr.LogToServiceStdout(serviceName, updateString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service output for %s: %v", serviceName, err))
		}

		err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
			State: ptr.ProcessStatePtr(types.ProcessStateRunning),
			Error: ptr.StringPtr(""),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	if !processIsAlive {
		errorString := fmt.Sprintf("service %s is not running", serviceName)

		hm.logger.Log(manager.LogLevelInfo, errorString)
		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to log service error output for %s: %v", serviceName, err))
		}

		err = hm.db.UpdateProcessHistoryEntry(ctx, pid, database.ProcessHistoryUpdate{
			State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: ptr.TimePtr(time.Now()),
			Error:     ptr.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("failed to updated process history entry for %s: %v", serviceName, err))
		}
	}
}

func calculateBackoffDelay(restartCount int) time.Duration {
	baseBackOff := 300
	maxDelay := 60000
	calculatedDelay := float64(baseBackOff) * math.Pow(float64(2), float64(restartCount))
	calculatedDelayAsInt := int(calculatedDelay)

	if calculatedDelayAsInt > maxDelay {
		return time.Duration(maxDelay) * time.Millisecond
	}
	return time.Duration(calculatedDelayAsInt) * time.Millisecond
}

// func (hm *HealthMonitor) canConnectToPort(port int) (bool, error) {
// 	address := fmt.Sprintf("localhost:%d", port)
// 	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
// 	if err != nil {
// 		return false, fmt.Errorf("unable to connect to the tcp address via dial, got: %v", err)
// 	}

// 	if err := conn.Close(); err != nil {
// 		return false, fmt.Errorf("unable to close the connection on port check, got: %v", err)
// 	}
// 	return true, nil
// }

func (hm *HealthMonitor) isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
