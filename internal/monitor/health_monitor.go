package monitor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/logutil"
	"eos/internal/manager"
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

func NewHealthMonitor(
	mgr *manager.LocalManager,
	db *database.DB,
	logger *manager.DaemonLogger,
	healthConfig config.HealthConfig,
	shutdownConfig config.ShutdownConfig,
) *HealthMonitor {
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
			services, err := hm.mgr.GetAllServiceCatalogEntries()

			if err != nil {
				hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("failed to get services: %v", err))
				continue
			}

			hm.checkAllServices(ctx, services)
		case <-hm.stopCh:
			return
		}
	}
}

func (hm *HealthMonitor) Stop() {
	close(hm.stopCh)
}

// TODO: Do we want this to only do state? Or become a check for all relevant health properties, just divided per state arm?
func (hm *HealthMonitor) checkAllServices(ctx context.Context, services []types.ServiceCatalogEntry) {
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
			hm.checkFailedProcess(ctx, service, processHistoryEntry, instance, hm.maxRestartCount)
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
	pgid := process.PGID

	if !hm.isProcessAlive(pgid) {
		hm.markProcessFailed(ctx, pgid, serviceName, logutil.LogLevelError, fmt.Sprintf("[%s] died during startup (PGID %d)", serviceName, pgid))
		return
	}

	if timeoutEnabled && time.Since(*process.StartedAt) > timeoutLimit {
		hm.markProcessFailed(ctx, pgid, serviceName, logutil.LogLevelWarn, fmt.Sprintf("[%s] taking too long to start", serviceName))
		return
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to load config: %v", serviceName, err))
		return
	}

	configPort := config.Port

	if configPort != 0 {
		hm.logger.Log(logutil.LogLevelInfo, fmt.Sprintf("[%s] now running on port %d", serviceName, configPort))
	} else {
		hm.logger.Log(logutil.LogLevelInfo, fmt.Sprintf("[%s] now running", serviceName))
	}

	activeRssMemoryKb := hm.determineActiveRSSMemoryUsage(pgid)

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateRunning),
		Error:       new(""),
		RssMemoryKb: new(activeRssMemoryKb),
	})
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to update process history entry: %v", serviceName, err))
	}
}

func (hm *HealthMonitor) updateProcessEntry(ctx context.Context, pgid int, activeRssMemoryKb int64, serviceName string) {
	err := hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		RssMemoryKb: new(activeRssMemoryKb),
	})
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to update process history entry: %v", serviceName, err))
	}
}

func (hm *HealthMonitor) checkRunningProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory) {
	serviceName := service.Name
	pgid := process.PGID

	if !hm.isProcessAlive(pgid) {
		hm.markProcessFailed(ctx, pgid, serviceName, logutil.LogLevelError, fmt.Sprintf("[%s] is not running", serviceName))
		return
	}

	activeRssMemoryKb := hm.determineActiveRSSMemoryUsage(pgid)
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)

	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] loading service config: %v", serviceName, err))
		return
	}

	memoryResult := hm.evaluateMemoryThresholds(config.MemoryLimitMb, activeRssMemoryKb)

	switch memoryResult {
	case ReasonWarning:
		hm.logger.Log(logutil.LogLevelWarn, fmt.Sprintf("[%s] memory usage warning", serviceName))
		hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
	case ReasonSoftRestart:
		newPgid, err := hm.mgr.RestartService(service.Name, 5*time.Second, 200*time.Millisecond)
		if err != nil {
			hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
			hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("[%s] restarting on soft restart threshold: %v", serviceName, err))
			return
		}
		hm.logger.Log(logutil.LogLevelWarn, fmt.Sprintf("[%s] auto soft restarted due to memory limits", serviceName))
		newRssMemoryKb := hm.determineActiveRSSMemoryUsage(newPgid)
		hm.updateProcessEntry(ctx, newPgid, newRssMemoryKb, serviceName)

	case ReasonForceRestart:
		newPgid, err := hm.mgr.RestartService(service.Name, 1*time.Second, 10*time.Millisecond)
		if err != nil {
			hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
			hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("[%s] restarting on force restart threshold: %v", serviceName, err))
			return
		}

		hm.logger.Log(logutil.LogLevelWarn, fmt.Sprintf("[%s] auto force restarted due to memory limits", serviceName))
		newRssMemoryKb := hm.determineActiveRSSMemoryUsage(newPgid)
		hm.updateProcessEntry(ctx, newPgid, newRssMemoryKb, serviceName)

	case ReasonNone:
		hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)

	default:
		hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
	}
}

func (hm *HealthMonitor) checkFailedProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceRuntime, maxRestartCount int) {
	serviceName := service.Name
	pgid := process.PGID
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)

	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to load config: %v", serviceName, err))
		return
	}

	if !hm.isProcessAlive(pgid) {
		// TODO: Do we want to incorporate instance.last_health_check instead process?
		elapsed := time.Since(*process.StoppedAt)
		requiredDelay := calculateBackoffDelay(instance.RestartCount)

		if instance.RestartCount >= maxRestartCount {
			return
		}
		if elapsed < requiredDelay {
			return
		}

		var errorString string

		if config.Port != 0 {
			errorString = fmt.Sprintf("[%s] restarting on port %d", serviceName, config.Port)
		} else {
			errorString = fmt.Sprintf("[%s] restarting", serviceName)
		}

		hm.logger.Log(logutil.LogLevelInfo, errorString)
		err = hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Log(logutil.LogLevelError,
				fmt.Sprintf("[%s] failed to log service error output: %v", serviceName, err))
		}
		_, err := hm.mgr.RestartService(serviceName, hm.shutdownGracePeriod, 200*time.Millisecond)

		if err != nil {
			hm.logger.Log(logutil.LogLevelError,
				fmt.Sprintf("[%s] failed to restart: %v", serviceName, err))
		}
		return
	}

	hm.markProcessRunning(ctx, pgid, serviceName)
}

func (hm *HealthMonitor) checkUnknownProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory) {
	serviceName := service.Name
	pgid := process.PGID

	if !hm.isProcessAlive(pgid) {
		hm.markProcessFailed(ctx, pgid, serviceName, logutil.LogLevelWarn, fmt.Sprintf("[%s] is not running", serviceName))
		return
	}
	hm.markProcessRunning(ctx, pgid, serviceName)
}

func (hm *HealthMonitor) markProcessRunning(ctx context.Context, pgid int, serviceName string) {
	updateString := fmt.Sprintf("[%s] is running", serviceName)

	hm.logger.Log(logutil.LogLevelInfo, updateString)
	err := hm.mgr.LogToServiceStdout(serviceName, updateString)
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to log service output: %v", serviceName, err))
	}

	activeRssMemoryKb := hm.determineActiveRSSMemoryUsage(pgid)

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateRunning),
		Error:       new(""),
		RssMemoryKb: new(activeRssMemoryKb),
	})
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to update process history entry: %v", serviceName, err))
	}
}

func (hm *HealthMonitor) markProcessFailed(ctx context.Context, pgid int, serviceName string, logLevel logutil.LogLevel, errorString string) {
	hm.logger.Log(logLevel, errorString)
	err := hm.mgr.LogToServiceStderr(serviceName, errorString)
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to log service error output: %v", serviceName, err))
	}

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateFailed),
		StoppedAt:   new(time.Now()),
		RssMemoryKb: new(int64(0)),
		Error:       new(errorString),
	})
	if err != nil {
		hm.logger.Log(logutil.LogLevelError,
			fmt.Sprintf("[%s] failed to update process history entry: %v", serviceName, err))
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

func (hm *HealthMonitor) isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func scanStatusField(contents []byte, field string) (fieldValue string, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	fieldPrefix := field + ":"
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, fieldPrefix) {
			continue
		}

		fieldValues := strings.Split(line, "\t")
		if len(fieldValues) != 2 {
			//TODO: Handle this better
			continue
		}
		fieldValue = fieldValues[len(fieldValues)-1]
		break
	}
	err = scanner.Err()
	if err != nil {
		return "", err
	}
	return fieldValue, nil
}

func (hm *HealthMonitor) determineActiveRSSMemoryUsage(pgid int) int64 {
	userOS := runtime.GOOS

	if userOS == "linux" {
		return hm.checkMemoryLinux(pgid)
	}

	return 0
}

func (hm *HealthMonitor) checkMemoryLinux(pgid int) int64 {
	totalRssMemory := int64(0)

	dirEntries, err := os.ReadDir("/proc")
	if err != nil {
		hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("error reading dir %v", err))
		return 0

	}
	for _, dirEntry := range dirEntries {
		folderNameNumerical, err := strconv.Atoi(dirEntry.Name())
		if err != nil {
			continue
		}

		contents, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", folderNameNumerical))
		if err != nil {
			hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("err %v", err))
			continue
		}
		pgidValue, err := scanStatusField(contents, "NSpgid")
		if err != nil {
			hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("scanning NSpgid status field: %v", err))
			continue
		}
		if pgidValue == "" {
			// TODO: Handle error
			continue
		}

		if pgidValue != strconv.Itoa(pgid) {
			continue
		}

		vmRSSValue, err := scanStatusField(contents, "VmRSS")
		if err != nil {
			hm.logger.Log(logutil.LogLevelError, fmt.Sprintf("scanning vmrss status field: %v", err))
			continue
		}
		if vmRSSValue == "" {
			hm.logger.Log(logutil.LogLevelInfo, fmt.Sprintf("no match on vmRSSValue: %v", contents))
			continue
		}
		vmRSSValueFields := strings.Fields(vmRSSValue)
		if len(vmRSSValueFields) != 2 {
			hm.logger.Log(logutil.LogLevelInfo, "vmRSSValueFields invalid content")
			continue
		}
		vmRssValueStrippedNumerical, err := strconv.Atoi(vmRSSValueFields[0])
		if err != nil {
			hm.logger.Log(logutil.LogLevelInfo, fmt.Sprintf("vmRssValueStrippedNumerical ERROR: %v", err))
			continue
		}
		totalRssMemory += int64(vmRssValueStrippedNumerical)
	}
	return totalRssMemory
}

type RestartReason int

const (
	ReasonNone RestartReason = iota
	ReasonWarning
	ReasonSoftRestart
	ReasonForceRestart
)

func (hm *HealthMonitor) evaluateMemoryThresholds(configMemoryLimitMb int, activeRssMemoryKb int64) RestartReason {
	if configMemoryLimitMb == 0 {
		return ReasonNone
	}
	fmt.Printf("configMemoryLimitMb: %v, activeRssMemoryKb: %v", configMemoryLimitMb, activeRssMemoryKb)
	memoryLimitKb := float64(configMemoryLimitMb) * 1024.0

	warningThreshold := memoryLimitKb * 0.75
	softRestartThreshold := memoryLimitKb * 0.85
	forceRestartThreshold := memoryLimitKb * 0.95

	activeRss := float64(activeRssMemoryKb)

	switch {
	case activeRss >= forceRestartThreshold:
		return ReasonForceRestart
	case activeRss >= softRestartThreshold:
		return ReasonSoftRestart
	case activeRss >= warningThreshold:
		return ReasonWarning
	default:
		return ReasonNone
	}
}
