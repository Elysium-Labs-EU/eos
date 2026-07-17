// Package monitor implements health checking and automatic restart logic for managed daemons.
package monitor

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/cronutil"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
)

type Monitor interface {
	Start(ctx context.Context)
}

type monitorManager interface {
	GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error)
	GetServiceInstance(name string) (*types.ServiceInstance, error)
	GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error)
	LogToServiceStdout(serviceName string, message string) error
	LogToServiceStderr(serviceName string, message string) error
	RestartService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (int, error)
}

var _ monitorManager = (*manager.LocalManager)(nil)

type HealthMonitor struct {
	mgr                       monitorManager
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
	mgr monitorManager,
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
		hm.checkService(ctx, &services[i])
	}
}

// checkService runs the health check for a single service, recovering from any panic
// so that one misbehaving service can't stop the tick from checking the rest.
func (hm *HealthMonitor) checkService(ctx context.Context, service *types.ServiceCatalogEntry) {
	serviceName := service.Name
	defer func() {
		if r := recover(); r != nil {
			hm.logger.Error("recovered from panic during health check", "service", serviceName, "panic", r)
		}
	}()

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		return
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		return
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

	activeRssMemoryKb, sampled := hm.measureRSS(pgid, serviceName)
	hm.logger.Debug("startup→running", "service", serviceName, "mem_kb", activeRssMemoryKb)
	var rssPtr *int64
	if sampled {
		rssPtr = &activeRssMemoryKb
	}

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateRunning),
		Error:       new(""),
		RssMemoryKb: rssPtr,
	})
	if err != nil {
		hm.logger.Error("failed to update process history entry", "service", serviceName, "error", err)
	}
}

func (hm *HealthMonitor) updateProcessEntry(ctx context.Context, pgid int, rssMemoryKb *int64, serviceName string) {
	err := hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		RssMemoryKb: rssMemoryKb,
	})
	if err != nil {
		hm.logger.Error("failed to update process history entry", "service", serviceName, "error", err)
	}
}

func (hm *HealthMonitor) checkRunningProcess(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceInstance) {
	serviceName := service.Name
	pgid := process.PGID

	if !hm.isProcessAlive(pgid) {
		hm.handleLivenessFailure(ctx, pgid, serviceName)
		return
	}

	configPath := filepath.Join(service.DirectoryPath, service.ConfigFileName)
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		hm.logger.Error("loading service config", "service", serviceName, "error", err)
		return
	}

	if config.Port != 0 && !hm.isPortReachable(ctx, config.Port) {
		hm.markProcessFailed(ctx, pgid, serviceName, slog.LevelError, fmt.Sprintf("[%s] is not reachable on port %d", serviceName, config.Port))
		return
	}

	hm.checkCronRestart(ctx, service, instance, config.CronRestart)
	hm.resetRestartCounterIfStable(ctx, serviceName, process, instance)

	rssKb, sampled := hm.measureRSS(pgid, serviceName)

	action := hm.evaluateMemoryThresholds(config.MemoryLimitMb, rssKb)
	hm.dispatchMemoryAction(ctx, service, process, instance, action, pgid, rssKb, sampled)
}

// checkCronRestart restarts a running service when its cron_restart schedule
// is due, and (re)computes NextRestartAt in the DB. A missing NextRestartAt
// (e.g. first tick after start, or a cron_restart that was just added) only
// schedules the next fire time — it does not restart immediately.
func (hm *HealthMonitor) checkCronRestart(ctx context.Context, service *types.ServiceCatalogEntry, instance *types.ServiceInstance, cronExpr string) {
	serviceName := service.Name
	if cronExpr == "" {
		return
	}

	now := time.Now()

	if instance.NextRestartAt == nil {
		hm.scheduleNextCronRestart(ctx, serviceName, cronExpr, now)
		return
	}

	if instance.NextRestartAt.After(now) {
		return
	}

	restartMsg := fmt.Sprintf("[%s] cron restart triggered", serviceName)
	hm.logger.Info(restartMsg)
	if logErr := hm.mgr.LogToServiceStdout(serviceName, restartMsg); logErr != nil {
		hm.logger.Error("failed to log service output", "service", serviceName, "error", logErr)
	}

	if _, err := hm.mgr.RestartService(serviceName, hm.shutdownGracePeriod, 200*time.Millisecond); err != nil {
		hm.logger.Error("cron restart failed", "service", serviceName, "error", err)
		return
	}

	hm.scheduleNextCronRestart(ctx, serviceName, cronExpr, time.Now())
}

// scheduleNextCronRestart computes the next fire time for cronExpr after from
// and persists it on the service instance.
func (hm *HealthMonitor) scheduleNextCronRestart(ctx context.Context, serviceName, cronExpr string, from time.Time) {
	next, err := cronutil.Next(cronExpr, from)
	if err != nil {
		hm.logger.Error("parsing cron_restart", "service", serviceName, "error", err)
		return
	}
	if err := hm.db.UpdateServiceInstance(ctx, serviceName, database.ServiceInstanceUpdate{NextRestartAt: &next}); err != nil {
		hm.logger.Error("failed to persist next cron restart", "service", serviceName, "error", err)
	}
}

// isPortReachable does a best-effort TCP dial to confirm the configured port still
// accepts connections. It only catches a listener that stopped accepting entirely
// (e.g. crashed internally without exiting the process) — a raw TCP connect can
// still succeed against a hung app via the kernel's accept backlog, so this is not
// a substitute for an application-level health check.
func (hm *HealthMonitor) isPortReachable(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// handleLivenessFailure marks a running-state process as failed because it is no longer alive.
func (hm *HealthMonitor) handleLivenessFailure(ctx context.Context, pgid int, serviceName string) {
	hm.markProcessFailed(ctx, pgid, serviceName, slog.LevelError, fmt.Sprintf("[%s] is not running", serviceName))
}

// resetRestartCounterIfStable zeroes the restart counter once a service has stayed
// up past the reset window, so a single flaky restart doesn't count against future backoff.
func (hm *HealthMonitor) resetRestartCounterIfStable(ctx context.Context, serviceName string, process *types.ProcessHistory, instance *types.ServiceInstance) {
	if instance.RestartCount == 0 || hm.restartCounterResetWindow <= 0 || process.StartedAt == nil {
		return
	}
	if time.Since(*process.StartedAt) < hm.restartCounterResetWindow {
		return
	}

	zero := 0
	if err := hm.db.UpdateServiceInstance(ctx, serviceName, database.ServiceInstanceUpdate{RestartCount: &zero}); err != nil {
		hm.logger.Error("failed to reset restart counter", "service", serviceName, "error", err)
		return
	}
	hm.logger.Info(fmt.Sprintf("[%s] restart counter reset after stable uptime", serviceName))
	hm.logger.Debug("restart counter reset", "service", serviceName, "uptime", time.Since(*process.StartedAt))
}

// dispatchMemoryAction acts on the outcome of evaluateMemoryThresholds: log-only for
// warnings, a graduated restart for soft/force thresholds, or a plain history update.
func (hm *HealthMonitor) dispatchMemoryAction(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceInstance, action RestartReason, pgid int, rssKb int64, sampled bool) {
	serviceName := service.Name
	rssPtr := rssKbPtr(rssKb, sampled)

	switch action {
	case ReasonWarning:
		warnMsg := fmt.Sprintf("[%s] memory usage warning", serviceName)
		hm.logger.Warn(warnMsg)
		hm.logger.Debug("memory threshold: warning", "service", serviceName, "mem_kb", rssKb)
		if logErr := hm.mgr.LogToServiceStdout(serviceName, warnMsg); logErr != nil {
			hm.logger.Error("failed to log service output", "service", serviceName, "error", logErr)
		}
		hm.updateProcessEntry(ctx, pgid, rssPtr, serviceName)
	case ReasonSoftRestart:
		hm.restartOnMemoryThreshold(ctx, service, process, instance, pgid, rssKb, rssPtr, "soft", 5*time.Second, 200*time.Millisecond)
	case ReasonForceRestart:
		hm.restartOnMemoryThreshold(ctx, service, process, instance, pgid, rssKb, rssPtr, "force", 1*time.Second, 10*time.Millisecond)
	case ReasonNone:
		if sampled {
			hm.updateProcessEntry(ctx, pgid, rssPtr, serviceName)
		}
	}
}

// restartOnMemoryThreshold restarts a service that crossed a soft or force memory
// threshold, using the grace/ticker periods appropriate to that threshold's urgency.
func (hm *HealthMonitor) restartOnMemoryThreshold(ctx context.Context, service *types.ServiceCatalogEntry, process *types.ProcessHistory, instance *types.ServiceInstance, pgid int, rssKb int64, rssPtr *int64, label string, gracePeriod, tickerPeriod time.Duration) {
	serviceName := service.Name

	if !canRestart(instance.RestartCount, hm.maxRestartCount, process.StartedAt, hm.backoff) {
		return
	}

	hm.logger.Debug("memory threshold: "+label+" restart", "service", serviceName, "mem_kb", rssKb, "attempt", instance.RestartCount+1, "max", hm.maxRestartCount)
	newPgid, err := hm.mgr.RestartService(service.Name, gracePeriod, tickerPeriod)
	if err != nil {
		hm.updateProcessEntry(ctx, pgid, rssPtr, serviceName)
		hm.logger.Error("restarting on "+label+" restart threshold", "service", serviceName, "error", err)
		return
	}

	restartMsg := fmt.Sprintf("[%s] auto %s restarted due to memory limits", serviceName, label)
	hm.logger.Warn(restartMsg)
	if logErr := hm.mgr.LogToServiceStderr(serviceName, restartMsg); logErr != nil {
		hm.logger.Error("failed to log service error output", "service", serviceName, "error", logErr)
	}
	delete(hm.lastMemSample, serviceName)
	newRssKb, newSampled := hm.measureRSS(newPgid, serviceName)
	hm.updateProcessEntry(ctx, newPgid, rssKbPtr(newRssKb, newSampled), serviceName)
}

func rssKbPtr(rssKb int64, sampled bool) *int64 {
	if !sampled {
		return nil
	}
	return &rssKb
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

	activeRssMemoryKb, sampled := hm.measureRSS(pgid, serviceName)
	var rssPtr *int64
	if sampled {
		rssPtr = &activeRssMemoryKb
	}

	err = hm.db.UpdateProcessHistoryEntry(ctx, pgid, database.ProcessHistoryUpdate{
		State:       new(types.ProcessStateRunning),
		Error:       new(""),
		RssMemoryKb: rssPtr,
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

// measureRSS returns (rssKb, true) when a sample was taken,
// or (0, false) when the throttle interval has not elapsed.
func (hm *HealthMonitor) measureRSS(pgid int, serviceName string) (int64, bool) {
	if time.Since(hm.lastMemSample[serviceName]) < hm.memSampleInterval {
		return 0, false
	}
	hm.lastMemSample[serviceName] = time.Now()

	if runtime.GOOS == "linux" {
		return hm.checkMemoryLinux(pgid), true
	}
	return 0, true
}

var (
	procStatusNSpgid = []byte("NSpgid:\t")
	procStatusVMRSS  = []byte("VmRSS:\t")
)

// readProcPIDs returns the entry names under /proc (candidate PIDs), or ok=false
// if /proc can't be read.
func (hm *HealthMonitor) readProcPIDs() (names []string, ok bool) {
	procDir, err := os.Open("/proc")
	if err != nil {
		hm.logger.Error("error reading /proc dir", "error", err)
		return nil, false
	}
	names, err = procDir.Readdirnames(-1)
	_ = procDir.Close()
	if err != nil {
		return nil, false
	}
	return names, true
}

// readProcStatus reads /proc/<pid>/status into hm's scratch buffer, returning
// the bytes and ok=false if the file couldn't be read.
func (hm *HealthMonitor) readProcStatus(pid int, pathBuf []byte) (contents []byte, ok bool) {
	path := fmt.Appendf(pathBuf[:0], "/proc/%d/status", pid)
	fd, err := syscall.Open(string(path), syscall.O_RDONLY, 0)
	if err != nil {
		return nil, false
	}
	n, _ := syscall.Read(fd, hm.procBuf[:])
	_ = syscall.Close(fd)
	if n <= 0 {
		return nil, false
	}
	return hm.procBuf[:n], true
}

// parseVMRSSKB parses the numeric kB prefix of a VmRSS field value ("1234 kB").
func parseVMRSSKB(vmRSSValue []byte) int64 {
	spaceIdx := bytes.IndexByte(vmRSSValue, ' ')
	if spaceIdx <= 0 {
		return 0
	}
	kb, err := strconv.Atoi(string(vmRSSValue[:spaceIdx]))
	if err != nil {
		return 0
	}
	return int64(kb)
}

// rssIfPgidMatches returns the VmRSS (kB) from a /proc status blob only when its
// NSpgid matches pgidBytes, else 0.
func rssIfPgidMatches(contents, pgidBytes []byte) int64 {
	if !bytes.Equal(scanStatusFieldBytes(contents, procStatusNSpgid), pgidBytes) {
		return 0
	}
	vmRSSValue := scanStatusFieldBytes(contents, procStatusVMRSS)
	if vmRSSValue == nil {
		return 0
	}
	return parseVMRSSKB(vmRSSValue)
}

// rssForProcName resolves a /proc entry name to a PID, reads its status, and
// returns its VmRSS (kB) if it belongs to pgidBytes, else 0.
func (hm *HealthMonitor) rssForProcName(name string, pgidBytes, pathBuf []byte) int64 {
	pid, err := strconv.Atoi(name)
	if err != nil {
		return 0
	}
	contents, ok := hm.readProcStatus(pid, pathBuf)
	if !ok {
		return 0
	}
	return rssIfPgidMatches(contents, pgidBytes)
}

func (hm *HealthMonitor) checkMemoryLinux(pgid int) int64 {
	names, ok := hm.readProcPIDs()
	if !ok {
		return 0
	}

	var pgidBuf [16]byte
	pgidBytes := strconv.AppendInt(pgidBuf[:0], int64(pgid), 10)

	var pathBuf [32]byte
	totalRssMemory := int64(0)
	for _, name := range names {
		totalRssMemory += hm.rssForProcName(name, pgidBytes, pathBuf[:])
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
