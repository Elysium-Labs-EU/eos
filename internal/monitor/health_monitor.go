package monitor

import (
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/types"
	"eos/internal/util"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type HealthMonitor struct {
	mgr    *manager.LocalManager
	db     *database.DB
	logger *manager.DaemonLogger
	stopCh chan struct{}
}

func NewHealthMonitor(mgr *manager.LocalManager, db *database.DB, logger *manager.DaemonLogger) *HealthMonitor {
	return &HealthMonitor{
		mgr:    mgr,
		db:     db,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

func (hm *HealthMonitor) Start() {
	ticker := time.NewTicker(2 * time.Second)
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

	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to get service: %v", err))
		return
	}

	for _, service := range services {
		instance, found, err := hm.mgr.GetServiceInstance(service.Name)
		if err != nil {
			continue
		}
		if !found {
			continue
		}

		processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(service.Name)
		if err != nil || processHistoryEntry == nil {
			continue
		}

		switch processHistoryEntry.State {
		case types.ProcessStateStarting:
			hm.checkStartProcess(service, processHistoryEntry)
		case types.ProcessStateRunning:
			hm.checkRunningProcess(service, processHistoryEntry)
		case types.ProcessStateFailed:
			hm.checkFailedProcess(service, processHistoryEntry, instance)
		}
	}
}

func (hm *HealthMonitor) checkStartProcess(
	service types.ServiceCatalogEntry,
	process *types.ProcessHistory,
) {
	if !hm.isProcessAlive(process.PID) {
		hm.logger.Log(manager.LogLevelError, fmt.Sprintf("Service %s (PID %d) died during startup",
			service.Name, process.PID))

		hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: util.TimePtr(time.Now()),
		})
		return
	}

	// TODO: Determine a clause for this, what is too long?
	if time.Since(*process.StartedAt) > 30*time.Second {
		hm.logger.Log(manager.LogLevelWarn,
			fmt.Sprintf("Service %s taking too long to start", service.Name))

		// TODO: Add more handeling to this

		hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: util.TimePtr(time.Now()),
			Error:     util.StringPtr("Service taking too long to start"),
		})
		return
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to load config for %s: %v", service.Name, err))
		return
	}

	if !hm.canConnectToPort(config.Port) {
		hm.logger.Log(manager.LogLevelInfo,
			fmt.Sprintf("Service %s is not running on port %d",
				service.Name, config.Port))
		hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateStopped),
			StoppedAt: util.TimePtr(time.Now()),
		})
		return
	}

	if !hm.isProcessAlive(process.PID) {
		hm.logger.Log(manager.LogLevelInfo,
			fmt.Sprintf("Process for service %s is not alive",
				service.Name))
		hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateStopped),
			StoppedAt: util.TimePtr(time.Now()),
		})
		return
	}

	hm.logger.Log(manager.LogLevelInfo,
		fmt.Sprintf("Service %s is now running on port %d",
			service.Name, config.Port))

	hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
		State: util.ProcessStatePtr(types.ProcessStateRunning),
	})
}

func (hm *HealthMonitor) checkRunningProcess(service types.ServiceCatalogEntry, process *types.ProcessHistory) {
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)

	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to load config for %s: %v", service.Name, err))
		return
	}

	if !hm.isProcessAlive(process.PID) {
		hm.logger.Log(manager.LogLevelInfo,
			fmt.Sprintf("Service %s is not running on port %d",
				service.Name, config.Port))

		hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
			State:     util.ProcessStatePtr(types.ProcessStateFailed),
			StoppedAt: util.TimePtr(time.Now()),
			Error:     util.StringPtr("Process detected as not alive"),
		})
	}
}

func (hm *HealthMonitor) checkFailedProcess(service types.ServiceCatalogEntry, process *types.ProcessHistory, instance types.ServiceRuntime) {
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)

	if err != nil {
		hm.logger.Log(manager.LogLevelError,
			fmt.Sprintf("Failed to load config for %s: %v", service.Name, err))
		return
	}

	if hm.isProcessAlive(process.PID) {
		hm.logger.Log(manager.LogLevelInfo,
			fmt.Sprintf("Service %s is not running on port %d",
				service.Name, config.Port))

		hm.db.UpdateProcessHistoryEntry(process.PID, database.ProcessHistoryUpdate{
			State: util.ProcessStatePtr(types.ProcessStateRunning),
		})
		return
	}

	// TODO: Do we want to incorporate instnace.last_health_check instead process?
	elapsed := time.Since(*process.StartedAt)
	requiredDelay := calculateBackoffDelay(instance.RestartCount)

	if instance.RestartCount < 15 && elapsed >= requiredDelay {
		hm.logger.Log(manager.LogLevelInfo,
			fmt.Sprintf("Restarting service %s on port %d", service.Name, config.Port))
		_, err := hm.mgr.RestartService(service.Name)

		if err != nil {
			hm.logger.Log(manager.LogLevelError,
				fmt.Sprintf("Failed to restart the service %s: %v", service.Name, err))
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

func (hm *HealthMonitor) canConnectToPort(port int) bool {
	address := fmt.Sprintf("localhost:%d", port)
	conn, err := net.DialTimeout("tcp", address, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (hm *HealthMonitor) isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
