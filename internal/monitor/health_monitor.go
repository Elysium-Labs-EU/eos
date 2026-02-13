package monitor

import (
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/types"
	"eos/internal/util"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type HealthMonitor struct {
	mgr           *manager.LocalManager
	db            *database.DB
	logger        *manager.DaemonLogger
	stopCh        chan struct{}
	checkInterval time.Duration
}

func NewHealthMonitor(mgr *manager.LocalManager, db *database.DB, logger *manager.DaemonLogger) *HealthMonitor {
	return &HealthMonitor{
		mgr:           mgr,
		db:            db,
		logger:        logger,
		stopCh:        make(chan struct{}),
		checkInterval: 2 * time.Second,
	}
}

func (hm *HealthMonitor) Start() {
	ticker := time.NewTicker(hm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hm.checkAllServices()
		case <-hm.stopCh:
			return
		}
	}
}

func (hm *HealthMonitor) Stop() {
	close(hm.stopCh)
}

func (hm *HealthMonitor) checkAllServices() {
	services, err := hm.mgr.GetAllServiceCatalogEntries()
	timoutLimit := 30 * time.Second
	maxRestartCount := 15

	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to get service: %v", err))
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
			hm.checkStartProcess(service, processHistoryEntry, &timoutLimit)
		case types.ProcessStateRunning:
			hm.checkRunningProcess(service, processHistoryEntry)
		case types.ProcessStateFailed:
			hm.checkFailedProcess(service, processHistoryEntry, instance, &maxRestartCount)
		}
	}
}

func (hm *HealthMonitor) checkStartProcess(
	service *types.ServiceCatalogEntry,
	process *types.ProcessHistory,
	timeoutLimit *time.Duration,
) {
	serviceName := service.Name
	pid := process.PID

	if !hm.isProcessAlive(pid) {
		errorString := fmt.Sprintf("Service %s (PID %d) died during startup", serviceName, pid)

		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
		}
		hm.logger.Log(manager.LogLevelError, errorString)
		err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: util.TimePtr(time.Now()),
			Error:     util.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	if time.Since(*process.StartedAt) > *timeoutLimit {
		errorString := fmt.Sprintf("Service %s taking too long to start", serviceName)

		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
		}
		hm.logger.Log(manager.LogLevelWarn, errorString)
		// TODO: Add more handeling to this
		err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: util.TimePtr(time.Now()),
			Error:     util.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to load config for %s: %v", serviceName, err))
		return
	}

	configPort := config.Port

	// if configPort != 0 && !hm.canConnectToPort(configPort) {
	// 	errorString := fmt.Sprintf("Service %s is not running on port %d", serviceName, configPort)

	// 	hm.logger.Log(manager.LogLevelInfo, errorString)
	// 	err = hm.mgr.LogToServiceStderr(serviceName, errorString)
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
	// 	}
	// 	err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
	// 		State:     util.ProcessStatePtr(types.ProcessStateStopped),
	// 		StoppedAt: util.TimePtr(time.Now()),
	// 		Error:     util.StringPtr(errorString),
	// 	})
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
	// 	}
	// 	return
	// }

	if configPort != 0 {
		hm.logger.Log(manager.LogLevelInfo, fmt.Sprintf("Service %s is now running on port %d", serviceName, configPort))
	} else {
		hm.logger.Log(manager.LogLevelInfo, fmt.Sprintf("Service %s is now running", serviceName))
	}

	err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
		State: util.ProcessStatePtr(types.ProcessStateRunning),
		Error: util.StringPtr(""),
	})
	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
	}
}

func (hm *HealthMonitor) checkRunningProcess(service *types.ServiceCatalogEntry, process *types.ProcessHistory) {
	// configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	// config, err := manager.LoadServiceConfig(configPath)
	serviceName := service.Name
	pid := process.PID

	// if err != nil {
	// 	hm.logger.Log(manager.LogLevelError,
	// 		fmt.Sprintf("Failed to load config for %s: %v", serviceName, err))
	// 	return
	// }

	if !hm.isProcessAlive(pid) {
		errorString := fmt.Sprintf("Service %s is not running", serviceName)

		hm.logger.Log(manager.LogLevelInfo, errorString)
		err := hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
		}

		err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: util.TimePtr(time.Now()),
			Error:     util.StringPtr(errorString),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
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
	// 			fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
	// 	}
	// 	return
	// }

	// if configPort != 0 && !connectable {
	// 	errorString := fmt.Sprintf("Service %s is not running on port %d", serviceName, configPort)

	// 	hm.logger.Log(manager.LogLevelInfo, errorString)
	// 	err = hm.mgr.LogToServiceStderr(serviceName, errorString)
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
	// 	}
	// 	err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
	// 		State:     util.ProcessStatePtr(types.ProcessStateFailed),
	// 		StoppedAt: util.TimePtr(time.Now()),
	// 		Error:     util.StringPtr(errorString),
	// 	})
	// 	if err != nil {
	// 		hm.logger.Log(manager.LogLevelError,
	// 			fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
	// 	}
	// 	return
	// }
}

func (hm *HealthMonitor) checkFailedProcess(service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceRuntime, maxRestartCount *int) {
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	serviceName := service.Name
	pid := process.PID

	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to load config for %s: %v", serviceName, err))
		return
	}

	if hm.isProcessAlive(pid) {
		err = hm.db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
			State: util.ProcessStatePtr(types.ProcessStateRunning),
			Error: util.StringPtr(""),
		})
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to updated process history entry for %s: %v", serviceName, err))
		}
		return
	}

	// TODO: Do we want to incorporate instance.last_health_check instead process?
	elapsed := time.Since(*process.StartedAt)
	requiredDelay := calculateBackoffDelay(instance.RestartCount)

	if instance.RestartCount < *maxRestartCount && elapsed >= requiredDelay {
		var errorString string

		if config.Port != 0 {
			errorString = fmt.Sprintf("Restarting service %s on port %d", serviceName, config.Port)
		} else {
			errorString = fmt.Sprintf("Restarting service %s", serviceName)
		}

		hm.logger.Log(manager.LogLevelInfo, errorString)
		err = hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to log service error output for %s: %v", serviceName, err))
		}
		_, err := hm.mgr.RestartService(serviceName)

		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to restart the service %s: %v", serviceName, err))
			return
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
