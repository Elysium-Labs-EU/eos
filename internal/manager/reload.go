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

	target, err := m.prepareReloadTarget(name)
	if err != nil {
		return ReloadResult{}, err
	}

	lio, err := m.prepareLaunchIO(target.service.Name, target.config)
	if err != nil {
		return ReloadResult{}, err
	}

	launchSuccess := false
	defer func() {
		if !launchSuccess {
			if closeErr := lio.closeAll(m, target.service.Name); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	if binaryErr := m.validateRuntimeBinary(target.config); binaryErr != nil {
		return ReloadResult{}, binaryErr
	}

	m.logger.Debug("reload: launching new instance alongside old", "service", name, "old_pgid", target.oldPGID)
	newPGID, newStartedAtTicks, err := m.launchAndCapture(&target.service, target.config, lio, target.resolvedSinks, &launchSuccess, "reload command")
	if err != nil {
		return ReloadResult{}, err
	}

	if cleanPGID, regErr := m.registerIncomingInstance(target.service.Name, newPGID, newStartedAtTicks); regErr != nil {
		return ReloadResult{NewPGID: cleanPGID}, regErr
	}

	// Probe the incoming instance before touching the outgoing one — the
	// acceptance guarantee is that health probing starts before the old instance
	// stops, so a new instance that never comes up leaves the old one serving.
	if !m.awaitReady(probe, newPGID, newStartedAtTicks, target.config.Port, cfg) {
		return m.abortUnreadyReload(name, newPGID, target.oldPGID, cfg.ReadinessTimeout)
	}

	m.logger.Debug("reload: new instance ready, draining old", "service", name, "new_pgid", newPGID, "old_pgid", target.oldPGID)
	if drainErr := m.drainInstance(name, target.oldPGID, cfg.GracePeriod, cfg.TickerPeriod); drainErr != nil {
		return ReloadResult{OldPGID: target.oldPGID, NewPGID: newPGID}, fmt.Errorf("draining old instance for %s: %w", name, drainErr)
	}

	m.recordReloadCutover(name, target.instance.RestartCount)
	return ReloadResult{OldPGID: target.oldPGID, NewPGID: newPGID}, nil
}

// reloadTarget is the resolved, validated input a cutover launches from: the
// service and its config, the sinks to wire, the current instance row (for its
// restart count), and the exact PGID of the live outgoing instance to drain.
type reloadTarget struct {
	service       types.ServiceCatalogEntry
	config        *types.ServiceConfig
	instance      *types.ServiceInstance
	resolvedSinks []types.LogSink
	oldPGID       int
}

// prepareReloadTarget resolves everything a reload needs before it launches the
// incoming instance: it loads the service config, confirms an instance row
// exists, and pins the live outgoing PGID. Reload swaps a running instance, so a
// service with no live process group is ErrServiceNotRunning rather than a cold
// start. Pinning the exact PGID here means the later drain signals only it,
// never the incoming instance that shares the same service name in history.
func (m *LocalManager) prepareReloadTarget(name string) (reloadTarget, error) {
	service, config, resolvedSinks, err := m.loadServiceForLaunch(name)
	if err != nil {
		return reloadTarget{}, err
	}

	instance, err := m.GetServiceInstance(m.ctx, name)
	if err != nil {
		return reloadTarget{}, fmt.Errorf("get service instance for %s: %w", name, err)
	}
	if instance == nil {
		return reloadTarget{}, fmt.Errorf("no service instance for %s", name)
	}

	history, err := m.db.GetProcessHistoryEntriesByServiceName(m.ctx, name)
	if err != nil {
		return reloadTarget{}, fmt.Errorf("get process history for %s: %w", name, err)
	}
	oldPGID := livePGIDInHistory(history)
	if oldPGID == 0 {
		return reloadTarget{}, ErrServiceNotRunning
	}

	return reloadTarget{
		service:       service,
		config:        config,
		resolvedSinks: resolvedSinks,
		instance:      instance,
		oldPGID:       oldPGID,
	}, nil
}

// registerIncomingInstance records the freshly launched incoming instance as its
// own Starting row. It shares the service name, so it becomes the most-recent
// process-history entry the health monitor tracks; once the old instance is
// drained the monitor drives its Starting→Running transition on its own tick,
// exactly as for a start. On a DB failure the new group is killed so a
// launched-but-untracked process can't leak; the returned int is the cleaned-up
// PGID to report (see killAndWrap), and newPGID on success.
func (m *LocalManager) registerIncomingInstance(serviceName string, newPGID int, newStartedAtTicks int64) (int, error) {
	if _, histErr := m.db.RegisterProcessHistoryEntry(m.ctx, newPGID, newStartedAtTicks, serviceName, types.ProcessStateStarting); histErr != nil {
		return killAndWrap(newPGID, histErr, "register reload process history entry")
	}
	return newPGID, nil
}

// abortUnreadyReload tears down an incoming instance that never passed the
// readiness gate: it force-kills the new group and DELETES its history row
// rather than marking it Failed. That row is the most-recent one for the
// service, so a Failed row would make the health monitor's next tick see the
// service as failed and restart it — killing the old instance this abort just
// protected. Removing the row restores the still-running old instance as the
// most-recent entry, so the monitor keeps supervising it unchanged. Returns
// ErrReloadNotReady so a broken deploy degrades to "no change".
func (m *LocalManager) abortUnreadyReload(name string, newPGID, oldPGID int, readinessTimeout time.Duration) (ReloadResult, error) {
	if killErr := syscall.Kill(-newPGID, syscall.SIGKILL); killErr != nil {
		m.logger.Error("reload: killing unready new instance", "service", name, "pgid", newPGID, "error", killErr)
	}
	if _, delErr := m.db.RemoveProcessHistoryEntryViaPGID(m.ctx, newPGID); delErr != nil {
		m.logger.Error("reload: removing aborted instance history row", "service", name, "pgid", newPGID, "error", delErr)
	}
	return ReloadResult{OldPGID: oldPGID, NewPGID: newPGID}, fmt.Errorf("%w: new instance for %s not ready within %s", ErrReloadNotReady, name, readinessTimeout)
}

// recordReloadCutover reaffirms the instance row against the new generation,
// mirroring recordRestartedInstance: a reload is a restart-class transition, so
// it bumps the restart count and stamps the fresh start. A DB failure here is
// logged, not fatal — the cutover already happened.
func (m *LocalManager) recordReloadCutover(name string, priorRestartCount int) {
	if updErr := m.db.UpdateServiceInstance(m.ctx, name, database.ServiceInstanceUpdate{
		StartedAt:    new(time.Now()),
		RestartCount: new(priorRestartCount + 1),
	}); updErr != nil {
		m.logger.Error("reload: recording cutover on service instance", "service", name, "error", updErr)
	}
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

	drained, err := m.terminateInstance(name, pgid, entry.StartedAtTicks, gracePeriod, tickerPeriod)
	if err != nil {
		return err
	}
	if !drained {
		// Manager shutting down; leave the row for startup reconciliation.
		return nil
	}

	m.markInstanceStopped(pgid)
	return nil
}

// terminateInstance SIGTERMs a single live process group and waits up to
// gracePeriod for it to exit, force-killing it if it overstays so the cutover
// can't stall on a service ignoring SIGTERM. It returns drained=false only when
// the manager context is canceled mid-wait, so the caller leaves the history
// row for startup reconciliation instead of marking it Stopped. A group already
// gone by the time the signal lands counts as drained.
func (m *LocalManager) terminateInstance(name string, pgid int, startedAtTicks int64, gracePeriod, tickerPeriod time.Duration) (drained bool, err error) {
	requestStartTime := time.Now()
	if killErr := syscall.Kill(-pgid, syscall.SIGTERM); killErr != nil {
		if !procutil.IsAliveMatching(pgid, startedAtTicks) {
			return true, nil
		}
		return false, fmt.Errorf("signaling old instance pgid %d: %w", pgid, killErr)
	}

	pending := map[int]bool{pgid: true}
	errored := map[int]string{}
	_, canceled := m.waitForPendingStops(name, pending, errored, requestStartTime, gracePeriod, tickerPeriod)
	if canceled {
		return false, nil
	}
	if len(errored) > 0 {
		m.forceKillInstance(name, pgid)
	}
	return true, nil
}

// forceKillInstance SIGKILLs a process group that outlived its grace period. An
// ESRCH (no such process) means it exited in the race between the grace check
// and the signal, which is a clean drain, not an error.
func (m *LocalManager) forceKillInstance(name string, pgid int) {
	if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		m.logger.Error("reload: force-killing old instance", "service", name, "pgid", pgid, "error", killErr)
	}
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
