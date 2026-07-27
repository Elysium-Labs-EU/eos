package manager

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// ReadinessProbe reports whether the instance identified by pgid (launched at
// startedAtTicks, serving port — 0 when the service declares none) is ready to
// take over serving. It is injected rather than implemented here so the reload
// cutover can gate on the health monitor's own liveness+port check without this
// package importing internal/monitor, which imports this one. The daemon passes
// monitor.ProbeReady; tests pass a fake.
type ReadinessProbe func(ctx context.Context, pgid int, startedAtTicks int64, port int) bool

// ReloadConfig carries the timing knobs for a zero-downtime reload cutover.
// Values are already resolved by the caller; nothing here reads config or env.
type ReloadConfig struct {
	// GracePeriod bounds how long the outgoing instance gets to exit after
	// SIGTERM before it is force-killed to finish the cutover.
	GracePeriod time.Duration
	// TickerPeriod is the poll interval while waiting for the outgoing instance
	// to exit.
	TickerPeriod time.Duration
	// ReadinessTimeout gives up the reload (keeping the outgoing instance
	// serving) if the incoming instance never passes the readiness probe.
	ReadinessTimeout time.Duration
	// ProbeInterval is how often the readiness probe runs while waiting.
	ProbeInterval time.Duration
}

// ReloadResult reports the process groups a completed reload swapped between.
type ReloadResult struct {
	OldPGID int
	NewPGID int
}

// ReloadService performs a health-gated, zero-downtime cutover: it launches a
// fresh instance of an already-running service alongside the live one, waits
// for the new instance to pass the readiness probe, and only then drains the old
// one. If the new instance never becomes ready it is killed and the old instance
// is left untouched, so a broken deploy degrades to "no change" rather than an
// outage.
//
// eos never owns the service's listening socket. Overlapping two instances on
// one port without dropping connections is the service's job, via SO_REUSEPORT:
// both instances bind the same address, the kernel load-balances new connections
// across whichever are listening, and connections already accepted by the old
// instance drain on it after its SIGTERM. eos only sequences the cutover so a
// listener is always present. This is a parallel path to RestartService (which
// stops-then-starts, dropping the port in between) and deliberately leaves it
// unchanged.
//
// The caller must not hold the per-service lock; this takes it for the whole
// launch→probe→drain sequence so a concurrent run/stop/restart can't race the
// cutover.
func (m *LocalManager) ReloadService(name string, probe ReadinessProbe, cfg ReloadConfig) (result ReloadResult, err error) {
	unlock := m.lockService(name)
	defer unlock()

	// Suspend the health monitor's own action for this service for the whole
	// cutover. Set before the incoming Starting row is registered below and held
	// until this returns, so a crash-on-start incoming instance can't make the
	// monitor mark the service Failed and queue a RestartService that bounces the
	// surviving old instance (or blocks on this lock and stalls other services).
	m.beginReload(name)
	defer m.endReload(name)

	_, span := m.telemetry.StartSpan(m.ctx, "eos.service.reload", name)
	defer func() {
		otelx.End(span, err)
		otelx.RecordOutcome(m.ctx, m.telemetry.ServiceRestarts, name, err)
	}()

	service, config, resolvedSinks, err := m.loadServiceForLaunch(name)
	if err != nil {
		return ReloadResult{}, err
	}

	instance, err := m.GetServiceInstance(name)
	if err != nil {
		return ReloadResult{}, fmt.Errorf("get service instance for %s: %w", name, err)
	}
	if instance == nil {
		return ReloadResult{}, fmt.Errorf("no service instance for %s", name)
	}

	// The outgoing instance must already be serving: reload swaps a live
	// instance, it does not start a cold one. Pin the exact PGID now so the
	// drain below signals only it, never the incoming instance that shares the
	// same service name in process history.
	history, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return ReloadResult{}, fmt.Errorf("get process history for %s: %w", name, err)
	}
	oldPGID := livePGIDInHistory(history)
	if oldPGID == 0 {
		return ReloadResult{}, ErrServiceNotRunning
	}

	lio, err := m.prepareLaunchIO(service.Name, config)
	if err != nil {
		return ReloadResult{}, err
	}

	launchSuccess := false
	defer func() {
		if !launchSuccess {
			if closeErr := lio.closeAll(m, service.Name); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	if binaryErr := m.validateRuntimeBinary(config); binaryErr != nil {
		return ReloadResult{}, binaryErr
	}

	m.logger.Debug("reload: launching new instance alongside old", "service", name, "old_pgid", oldPGID)
	newPGID, newStartedAtTicks, err := m.launchAndCapture(service, config, lio, resolvedSinks, &launchSuccess, "reload command")
	if err != nil {
		return ReloadResult{}, err
	}

	// Record the incoming instance as its own Starting row. It shares the
	// service name, so it becomes the most-recent process-history entry the
	// health monitor tracks; once the old instance is drained the monitor drives
	// its Starting→Running transition on its own tick, exactly as for a start.
	if _, histErr := m.db.RegisterProcessHistoryEntry(m.ctx, newPGID, newStartedAtTicks, service.Name, types.ProcessStateStarting); histErr != nil {
		cleanPGID, wrapErr := killAndWrap(newPGID, histErr, "register reload process history entry")
		return ReloadResult{NewPGID: cleanPGID}, wrapErr
	}

	// Probe the incoming instance before touching the outgoing one — the
	// acceptance guarantee is that health probing starts before the old instance
	// stops, so a new instance that never comes up leaves the old one serving.
	if !m.awaitReady(probe, newPGID, newStartedAtTicks, config.Port, cfg) {
		if killErr := syscall.Kill(-newPGID, syscall.SIGKILL); killErr != nil {
			m.logger.Error("reload: killing unready new instance", "service", name, "pgid", newPGID, "error", killErr)
		}
		// Delete the aborted instance's history row rather than mark it Failed.
		// It is the most-recent row for this service, so a Failed row here would
		// make the health monitor's next tick see the service as failed and
		// restart it — killing the old instance this abort just protected.
		// Removing the row restores the still-running old instance as the
		// most-recent entry, so the monitor keeps supervising it unchanged.
		if _, delErr := m.db.RemoveProcessHistoryEntryViaPGID(m.ctx, newPGID); delErr != nil {
			m.logger.Error("reload: removing aborted instance history row", "service", name, "pgid", newPGID, "error", delErr)
		}
		return ReloadResult{OldPGID: oldPGID, NewPGID: newPGID}, fmt.Errorf("%w: new instance for %s not ready within %s", ErrReloadNotReady, name, cfg.ReadinessTimeout)
	}

	m.logger.Debug("reload: new instance ready, draining old", "service", name, "new_pgid", newPGID, "old_pgid", oldPGID)
	if drainErr := m.drainInstance(name, oldPGID, cfg.GracePeriod, cfg.TickerPeriod); drainErr != nil {
		return ReloadResult{OldPGID: oldPGID, NewPGID: newPGID}, fmt.Errorf("draining old instance for %s: %w", name, drainErr)
	}

	// Reaffirm the instance row against the new generation, mirroring
	// recordRestartedInstance: a reload is a restart-class transition, so bump
	// the restart count and stamp the fresh start.
	if updErr := m.db.UpdateServiceInstance(m.ctx, name, database.ServiceInstanceUpdate{
		StartedAt:    new(time.Now()),
		RestartCount: new(instance.RestartCount + 1),
	}); updErr != nil {
		m.logger.Error("reload: recording cutover on service instance", "service", name, "error", updErr)
	}

	return ReloadResult{OldPGID: oldPGID, NewPGID: newPGID}, nil
}

// reloadReadyConsecutivePasses is how many back-to-back probe successes the new
// instance must post before it is considered ready to take over. One pass is not
// enough under SO_REUSEPORT: the old instance already makes the port reachable,
// so the very first probe can read "ready" the instant the new process forks —
// before it has bound its own socket. Draining the old one then would open the
// exact no-listener gap this whole path exists to avoid (a real dropped
// connection surfaced this). Requiring the probe to hold across several
// intervals gives the new instance time to bind and rules out a one-tick blip,
// while a new instance that dies or hangs during warmup resets the count and
// eventually times out, keeping the old one serving.
const reloadReadyConsecutivePasses = 3

// awaitReady returns true only once the readiness probe passes on
// reloadReadyConsecutivePasses consecutive intervals within ReadinessTimeout.
// Any failing probe resets the streak. A nil probe is a misconfiguration:
// without a gate there is no safe moment to drain the old instance, so it
// reports not ready rather than cutting over blind. Cancellation of the manager
// context also aborts the wait.
//
// The timeout is a dedicated timer in the select, not a deadline checked only
// inside the tick branch: with ReadinessTimeout < ProbeInterval the wait must
// give up at ReadinessTimeout rather than blocking a full ProbeInterval for the
// first tick to arrive.
func (m *LocalManager) awaitReady(probe ReadinessProbe, pgid int, startedAtTicks int64, port int, cfg ReloadConfig) bool {
	if probe == nil {
		m.logger.Error("reload: no readiness probe configured; refusing to cut over")
		return false
	}

	timeout := time.NewTimer(cfg.ReadinessTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(cfg.ProbeInterval)
	defer ticker.Stop()

	consecutive := 0
	if probe(m.ctx, pgid, startedAtTicks, port) {
		consecutive = 1
	}
	for consecutive < reloadReadyConsecutivePasses {
		select {
		case <-m.ctx.Done():
			return false
		case <-timeout.C:
			return false
		case <-ticker.C:
			if probe(m.ctx, pgid, startedAtTicks, port) {
				consecutive++
			} else {
				consecutive = 0
			}
		}
	}
	return true
}

// drainInstance stops exactly one process group belonging to name and marks its
// history row Stopped. Unlike stopServiceLocked it never signals the whole
// service's history, so the freshly started reload instance sharing the same
// service name is left running. After the grace period a still-live group is
// force-killed to guarantee the cutover completes.
func (m *LocalManager) drainInstance(name string, pgid int, gracePeriod, tickerPeriod time.Duration) error {
	entry, err := m.db.GetProcessHistoryEntryByPGID(m.ctx, pgid)
	if err != nil {
		return fmt.Errorf("get process history for pgid %d: %w", pgid, err)
	}

	if !procutil.IsAliveMatching(pgid, entry.StartedAtTicks) {
		// Already gone (or a recycled PGID that isn't ours): just record it.
		m.markInstanceStopped(pgid)
		return nil
	}

	requestStartTime := time.Now()
	if killErr := syscall.Kill(-pgid, syscall.SIGTERM); killErr != nil {
		if !procutil.IsAliveMatching(pgid, entry.StartedAtTicks) {
			m.markInstanceStopped(pgid)
			return nil
		}
		return fmt.Errorf("signaling old instance pgid %d: %w", pgid, killErr)
	}

	pending := map[int]bool{pgid: true}
	errored := map[int]string{}
	_, canceled := m.waitForPendingStops(name, pending, errored, requestStartTime, gracePeriod, tickerPeriod)
	if canceled {
		// Manager shutting down; leave the row for startup reconciliation.
		return nil
	}
	if len(errored) > 0 {
		// Exceeded the grace period while still alive: force the exit so the
		// cutover can't stall on a service ignoring SIGTERM.
		if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			m.logger.Error("reload: force-killing old instance", "service", name, "pgid", pgid, "error", killErr)
		}
	}

	m.markInstanceStopped(pgid)
	return nil
}

// markInstanceStopped records a single drained instance's history row as
// Stopped. A DB failure here is logged, not returned: the process is already
// dealt with, and the health monitor's own reconciliation will correct a missed
// state update.
func (m *LocalManager) markInstanceStopped(pgid int) {
	if err := m.db.UpdateProcessHistoryEntry(m.ctx, pgid, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateStopped),
		StoppedAt: new(time.Now()),
	}); err != nil {
		m.logger.Error("reload: recording drained instance as stopped", "pgid", pgid, "error", err)
	}
}
