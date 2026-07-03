// Package monitor implements health checking and automatic restart logic for managed daemons.
package monitor

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
)

type HealthMonitor struct {
	mgr                       *manager.LocalManager
	db                        *database.DB
	logger                    *slog.Logger
	lastMemSample             map[string]time.Time
	checkInterval             time.Duration
	memSampleInterval         time.Duration
	timeoutLimit              time.Duration
	restartCounterResetWindow time.Duration
	shutdownGracePeriod       time.Duration
	backoff                   config.BackoffConfig
	memory                    config.MemoryThresholdConfig
	procBuf                   [4096]byte
	maxRestartCount           int
	timeoutEnable             bool
}

func NewHealthMonitor(
	mgr *manager.LocalManager,
	db *database.DB,
	logger *slog.Logger,
	healthConfig *config.HealthConfig,
	shutdownConfig config.ShutdownConfig,
) *HealthMonitor {
	checkInterval := healthConfig.CheckInterval
	if checkInterval <= 0 {
		checkInterval = 2 * time.Second
	}

	memSampleInterval := healthConfig.MemSampleInterval
	if memSampleInterval <= 0 {
		memSampleInterval = 30 * time.Second
	}

	backoff := healthConfig.Backoff
	if backoff.BaseMs <= 0 {
		backoff.BaseMs = config.HealthBackoffBaseMs
	}
	if backoff.MaxMs <= 0 {
		backoff.MaxMs = config.HealthBackoffMaxMs
	}

	memory := healthConfig.Memory
	if memory.WarningThreshold <= 0 {
		memory.WarningThreshold = config.HealthMemoryWarningThreshold
	}
	if memory.SoftRestartThreshold <= 0 {
		memory.SoftRestartThreshold = config.HealthMemorySoftRestartThreshold
	}
	if memory.ForceRestartThreshold <= 0 {
		memory.ForceRestartThreshold = config.HealthMemoryForceRestartThreshold
	}

	return &HealthMonitor{
		mgr:                       mgr,
		db:                        db,
		logger:                    logger,
		checkInterval:             checkInterval,
		memSampleInterval:         memSampleInterval,
		lastMemSample:             make(map[string]time.Time),
		timeoutEnable:             healthConfig.Timeout.Enable,
		timeoutLimit:              healthConfig.Timeout.Limit,
		maxRestartCount:           healthConfig.MaxRestart,
		restartCounterResetWindow: healthConfig.RestartCounterResetWindow,
		shutdownGracePeriod:       shutdownConfig.GracePeriod,
		backoff:                   backoff,
		memory:                    memory,
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
				hm.logger.Error("failed to get services", "error", err)
				continue
			}

			hm.checkAllServices(ctx, services)
		case <-ctx.Done():
			return
		}
	}
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

		hm.logger.Debug("health tick", "service", serviceName, "state", processHistoryEntry.State)

		switch processHistoryEntry.State {
		case types.ProcessStateStarting:
			hm.checkStartProcess(ctx, service, processHistoryEntry, hm.timeoutLimit, hm.timeoutEnable)
		case types.ProcessStateRunning:
			hm.checkRunningProcess(ctx, service, processHistoryEntry, instance)
		case types.ProcessStateFailed:
			hm.checkFailedProcess(ctx, service, processHistoryEntry, instance.RestartCount, hm.maxRestartCount)
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
		hm.logger.Debug("startup check: process dead", "service", serviceName, "pgid", pgid)
		hm.markProcessFailed(ctx, pgid, serviceName, slog.LevelError, fmt.Sprintf("[%s] died during startup (PGID %d)", serviceName, pgid))
		return
	}

	if timeoutEnabled && time.Since(*process.StartedAt) > timeoutLimit {
		hm.logger.Debug("startup check: timeout exceeded", "service", serviceName, "elapsed", time.Since(*process.StartedAt))
		hm.markProcessFailed(ctx, pgid, serviceName, slog.LevelWarn, fmt.Sprintf("[%s] taking too long to start", serviceName))
		return
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Error("failed to load config", "service", serviceName, "error", err)
		return
	}

	configPort := config.Port

	var runningMsg string
	if configPort != 0 {
		runningMsg = fmt.Sprintf("[%s] now running on port %d", serviceName, configPort)
	} else {
		runningMsg = fmt.Sprintf("[%s] now running", serviceName)
	}
	hm.logger.Info(runningMsg)
	if logErr := hm.mgr.LogToServiceStdout(serviceName, runningMsg); logErr != nil {
		hm.logger.Error("failed to log service output", "service", serviceName, "error", logErr)
	}

	activeRssMemoryKb := hm.determineActiveRSSMemoryUsage(pgid, serviceName)
	hm.logger.Debug("startup→running", "service", serviceName, "mem_kb", activeRssMemoryKb)

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateRunning),
		Error:       new(""),
		RssMemoryKb: new(activeRssMemoryKb),
	})
	if err != nil {
		hm.logger.Error("failed to update process history entry", "service", serviceName, "error", err)
	}
}

func (hm *HealthMonitor) updateProcessEntry(ctx context.Context, pgid int, activeRssMemoryKb int64, serviceName string) {
	err := hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		RssMemoryKb: new(activeRssMemoryKb),
	})
	if err != nil {
		hm.logger.Error("failed to update process history entry", "service", serviceName, "error", err)
	}
}

func (hm *HealthMonitor) checkRunningProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceInstance) {
	serviceName := service.Name
	pgid := process.PGID

	if !hm.isProcessAlive(pgid) {
		hm.markProcessFailed(ctx, pgid, serviceName, slog.LevelError, fmt.Sprintf("[%s] is not running", serviceName))
		return
	}

	if instance.RestartCount > 0 && hm.restartCounterResetWindow > 0 && process.StartedAt != nil {
		if time.Since(*process.StartedAt) >= hm.restartCounterResetWindow {
			zero := 0
			if err := hm.db.UpdateServiceInstance(ctx, serviceName, database.ServiceInstanceUpdate{RestartCount: &zero}); err != nil {
				hm.logger.Error("failed to reset restart counter", "service", serviceName, "error", err)
			} else {
				hm.logger.Info(fmt.Sprintf("[%s] restart counter reset after stable uptime", serviceName))
				hm.logger.Debug("restart counter reset", "service", serviceName, "uptime", time.Since(*process.StartedAt))
			}
		}
	}

	activeRssMemoryKb := hm.determineActiveRSSMemoryUsage(pgid, serviceName)
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)

	if err != nil {
		hm.logger.Error("loading service config", "service", serviceName, "error", err)
		return
	}

	memoryResult := hm.evaluateMemoryThresholds(config.MemoryLimitMb, activeRssMemoryKb)

	switch memoryResult {
	case ReasonWarning:
		warnMsg := fmt.Sprintf("[%s] memory usage warning", serviceName)
		hm.logger.Warn(warnMsg)
		hm.logger.Debug("memory threshold: warning", "service", serviceName, "mem_kb", activeRssMemoryKb)
		if logErr := hm.mgr.LogToServiceStdout(serviceName, warnMsg); logErr != nil {
			hm.logger.Error("failed to log service output", "service", serviceName, "error", logErr)
		}
		hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
	case ReasonSoftRestart:
		if !canRestart(instance.RestartCount, hm.maxRestartCount, process.StartedAt, hm.backoff) {
			return
		}

		hm.logger.Debug("memory threshold: soft restart", "service", serviceName, "mem_kb", activeRssMemoryKb, "attempt", instance.RestartCount+1, "max", hm.maxRestartCount)
		newPgid, err := hm.mgr.RestartService(service.Name, 5*time.Second, 200*time.Millisecond)
		if err != nil {
			hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
			hm.logger.Error("restarting on soft restart threshold", "service", serviceName, "error", err)
			return
		}
		softRestartMsg := fmt.Sprintf("[%s] auto soft restarted due to memory limits", serviceName)
		hm.logger.Warn(softRestartMsg)
		if logErr := hm.mgr.LogToServiceStderr(serviceName, softRestartMsg); logErr != nil {
			hm.logger.Error("failed to log service error output", "service", serviceName, "error", logErr)
		}
		newRssMemoryKb := hm.determineActiveRSSMemoryUsage(newPgid, serviceName)
		hm.updateProcessEntry(ctx, newPgid, newRssMemoryKb, serviceName)

	case ReasonForceRestart:
		if !canRestart(instance.RestartCount, hm.maxRestartCount, process.StartedAt, hm.backoff) {
			return
		}

		hm.logger.Debug("memory threshold: force restart", "service", serviceName, "mem_kb", activeRssMemoryKb, "attempt", instance.RestartCount+1, "max", hm.maxRestartCount)
		newPgid, err := hm.mgr.RestartService(service.Name, 1*time.Second, 10*time.Millisecond)
		if err != nil {
			hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
			hm.logger.Error("restarting on force restart threshold", "service", serviceName, "error", err)
			return
		}
		forceRestartMsg := fmt.Sprintf("[%s] auto force restarted due to memory limits", serviceName)
		hm.logger.Warn(forceRestartMsg)
		if logErr := hm.mgr.LogToServiceStderr(serviceName, forceRestartMsg); logErr != nil {
			hm.logger.Error("failed to log service error output", "service", serviceName, "error", logErr)
		}
		newRssMemoryKb := hm.determineActiveRSSMemoryUsage(newPgid, serviceName)
		hm.updateProcessEntry(ctx, newPgid, newRssMemoryKb, serviceName)

	case ReasonNone:
		hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)

	default:
		hm.updateProcessEntry(ctx, pgid, activeRssMemoryKb, serviceName)
	}
}

func (hm *HealthMonitor) checkFailedProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, restartCount int, maxRestartCount int) {
	serviceName := service.Name
	pgid := process.PGID
	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)

	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Error("failed to load config", "service", serviceName, "error", err)
		return
	}

	if !hm.isProcessAlive(pgid) {
		// TODO: Do we want to incorporate instance.last_health_check instead process?
		if !canRestart(restartCount, maxRestartCount, process.StoppedAt, hm.backoff) {
			hm.logger.Debug("max restarts reached", "service", serviceName, "count", restartCount, "max", maxRestartCount)
			return
		}

		var errorString string

		if config.Port != 0 {
			errorString = fmt.Sprintf("[%s] restarting on port %d", serviceName, config.Port)
		} else {
			errorString = fmt.Sprintf("[%s] restarting", serviceName)
		}

		backoff := calculateBackoffDelay(restartCount, hm.backoff.BaseMs, hm.backoff.MaxMs)
		hm.logger.Debug("scheduling restart", "service", serviceName, "attempt", restartCount+1, "max", maxRestartCount, "backoff", backoff)
		hm.logger.Info(errorString)
		err = hm.mgr.LogToServiceStderr(serviceName, errorString)
		if err != nil {
			hm.logger.Error("failed to log service error output", "service", serviceName, "error", err)
		}
		_, err := hm.mgr.RestartService(serviceName, hm.shutdownGracePeriod, 200*time.Millisecond)

		if err != nil {
			hm.logger.Error("failed to restart", "service", serviceName, "error", err)
		}
		return
	}

	hm.markProcessRunning(ctx, pgid, serviceName)
}

func (hm *HealthMonitor) checkUnknownProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory) {
	serviceName := service.Name
	pgid := process.PGID

	if !hm.isProcessAlive(pgid) {
		hm.markProcessFailed(ctx, pgid, serviceName, slog.LevelWarn, fmt.Sprintf("[%s] is not running", serviceName))
		return
	}
	hm.markProcessRunning(ctx, pgid, serviceName)
}

func (hm *HealthMonitor) markProcessRunning(ctx context.Context, pgid int, serviceName string) {
	var msgBuf [128]byte
	updateString := string(fmt.Appendf(msgBuf[:0], "[%s] is running", serviceName))

	hm.logger.Info(updateString)
	hm.logger.Debug("state→Running", "service", serviceName, "pgid", pgid)
	err := hm.mgr.LogToServiceStdout(serviceName, updateString)
	if err != nil {
		hm.logger.Error("failed to log service output", "service", serviceName, "error", err)
	}

	activeRssMemoryKb := hm.determineActiveRSSMemoryUsage(pgid, serviceName)

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateRunning),
		Error:       new(""),
		RssMemoryKb: new(activeRssMemoryKb),
	})
	if err != nil {
		hm.logger.Error("failed to update process history entry", "service", serviceName, "error", err)
	}
}

func (hm *HealthMonitor) markProcessFailed(ctx context.Context, pgid int, serviceName string, level slog.Level, errorString string) {
	hm.logger.Log(ctx, level, errorString)
	hm.logger.Debug("state→Failed", "service", serviceName, "pgid", pgid)
	err := hm.mgr.LogToServiceStderr(serviceName, errorString)
	if err != nil {
		hm.logger.Error("failed to log service error output", "service", serviceName, "error", err)
	}

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateFailed),
		StoppedAt:   new(time.Now()),
		RssMemoryKb: new(int64(0)),
		Error:       new(errorString),
	})
	if err != nil {
		hm.logger.Error("failed to update process history entry", "service", serviceName, "error", err)
	}
}

func canRestart(restartCount, maxRestartCount int, since *time.Time, backoff config.BackoffConfig) bool {
	if restartCount >= maxRestartCount {
		return false
	}
	if since != nil && time.Since(*since) < calculateBackoffDelay(restartCount, backoff.BaseMs, backoff.MaxMs) {
		return false
	}
	return true
}

func calculateBackoffDelay(restartCount, baseMs, maxMs int) time.Duration {
	calculatedDelay := float64(baseMs) * math.Pow(float64(2), float64(restartCount))
	calculatedDelayAsInt := int(calculatedDelay)

	if calculatedDelayAsInt > maxMs {
		return time.Duration(maxMs) * time.Millisecond
	}
	return time.Duration(calculatedDelayAsInt) * time.Millisecond
}

// isProcessAlive reports whether any live process exists in the given process group.
//
// On Linux, kill(-pgid, 0) returns nil even when the only remaining process is
// a zombie — a process that has exited but has not yet been reaped by its
// parent's Wait call. A zombie is not running, so we read /proc/<pgid>/stat and
// treat state 'Z' as dead.
//
// On macOS, kill(-pgid, 0) returns EPERM for zombies (caught by the err != nil
// check below), so the /proc path is not needed there.
func (hm *HealthMonitor) isProcessAlive(pgid int) bool {
	if pgid <= 1 {
		return false
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		return false
	}
	if runtime.GOOS == "linux" {
		var pathBuf [32]byte
		path := fmt.Appendf(pathBuf[:0], "/proc/%d/stat", pgid)
		fd, err := syscall.Open(string(path), syscall.O_RDONLY, 0)
		if err != nil {
			return false
		}
		n, _ := syscall.Read(fd, hm.procBuf[:])
		_ = syscall.Close(fd)
		if n <= 0 {
			return false
		}
		contents := hm.procBuf[:n]
		if i := bytes.LastIndexByte(contents, ')'); i >= 0 && i+2 < len(contents) {
			return contents[i+2] != 'Z'
		}
	}
	return true
}

// scanStatusFieldBytes finds a field in /proc/N/status without allocating.
// Returns a slice into contents for the value after "field:\t", or nil if not found.
func scanStatusFieldBytes(contents []byte, field []byte) []byte {
	remaining := contents
	for len(remaining) > 0 {
		newline := bytes.IndexByte(remaining, '\n')
		var line []byte
		if newline < 0 {
			line = remaining
			remaining = nil
		} else {
			line = remaining[:newline]
			remaining = remaining[newline+1:]
		}
		if !bytes.HasPrefix(line, field) {
			continue
		}
		return bytes.TrimSpace(line[len(field):])
	}
	return nil
}

func (hm *HealthMonitor) determineActiveRSSMemoryUsage(pgid int, serviceName string) int64 {
	if time.Since(hm.lastMemSample[serviceName]) < hm.memSampleInterval {
		return 0
	}
	hm.lastMemSample[serviceName] = time.Now()

	if runtime.GOOS == "linux" {
		return hm.checkMemoryLinux(pgid)
	}
	return 0
}

var (
	procStatusNSpgid = []byte("NSpgid:\t")
	procStatusVMRSS  = []byte("VmRSS:\t")
)

func (hm *HealthMonitor) checkMemoryLinux(pgid int) int64 {
	totalRssMemory := int64(0)

	procDir, err := os.Open("/proc")
	if err != nil {
		hm.logger.Error("error reading /proc dir", "error", err)
		return 0
	}
	names, err := procDir.Readdirnames(-1)
	_ = procDir.Close()
	if err != nil {
		return 0
	}

	var pgidBuf [16]byte
	pgidBytes := strconv.AppendInt(pgidBuf[:0], int64(pgid), 10)

	var pathBuf [32]byte
	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}

		path := fmt.Appendf(pathBuf[:0], "/proc/%d/status", pid)
		fd, err := syscall.Open(string(path), syscall.O_RDONLY, 0)
		if err != nil {
			continue
		}
		n, _ := syscall.Read(fd, hm.procBuf[:])
		_ = syscall.Close(fd)
		if n <= 0 {
			continue
		}
		contents := hm.procBuf[:n]

		if !bytes.Equal(scanStatusFieldBytes(contents, procStatusNSpgid), pgidBytes) {
			continue
		}

		vmRSSValue := scanStatusFieldBytes(contents, procStatusVMRSS)
		if vmRSSValue == nil {
			continue
		}
		// vmRSSValue is "1234 kB" — parse the numeric prefix only
		spaceIdx := bytes.IndexByte(vmRSSValue, ' ')
		if spaceIdx <= 0 {
			continue
		}
		kb, err := strconv.Atoi(string(vmRSSValue[:spaceIdx]))
		if err != nil {
			continue
		}
		totalRssMemory += int64(kb)
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
	memoryLimitKb := float64(configMemoryLimitMb) * 1024.0

	warningThreshold := memoryLimitKb * hm.memory.WarningThreshold
	softRestartThreshold := memoryLimitKb * hm.memory.SoftRestartThreshold
	forceRestartThreshold := memoryLimitKb * hm.memory.ForceRestartThreshold

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
