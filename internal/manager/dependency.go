package manager

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// DependencyDefaultMaxWait bounds how long a dependent service waits for its
// DependsOn to report ready when its config sets no max_wait of its own.
// Generous on purpose: the point of depends_on is to outlast a slow dependency,
// unlike a fixed healthcheck timeout that gives up.
const DependencyDefaultMaxWait = 60 * time.Second

// DependencyWaitStaleAfter bounds how long a recorded dependency wait is
// trusted before GetDependencyWaitStatus treats it as orphaned. Now that the
// wait is persisted to the shared state.db (see LocalManager.SetDependencyWaitStatus),
// it survives the process that recorded it dying before its own
// RecordDependencyWait defer could clear it (a SIGKILL, a crash) — without
// this ceiling a stale row would report "waiting" forever. Set well above
// DependencyDefaultMaxWait so it never fires on a wait that's merely slow,
// only on one whose recording process is provably gone.
const DependencyWaitStaleAfter = 10 * time.Minute

// dependencyWaitIsStale reports whether a dependency wait recorded at since is
// old enough to be orphaned rather than merely still in progress.
func dependencyWaitIsStale(since time.Time) bool {
	return time.Since(since) > DependencyWaitStaleAfter
}

// dependencyPollInterval is how often the gate re-reads a dependency's recorded
// state. Short enough that a ready dependency releases the dependent promptly,
// long enough not to hammer the datastore while waiting.
const dependencyPollInterval = 250 * time.Millisecond

// dependencyReadinessProber is the slice of a manager the gate needs: the
// most-recent recorded process state for a service, which the health monitor
// advances to Running once the process is up. Both LocalManager and
// DaemonManager satisfy it, so the gate runs on either side of the socket.
type dependencyReadinessProber interface {
	GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error)
}

// ParseMaxWait resolves a service's configured max_wait to a duration, falling
// back to DependencyDefaultMaxWait when empty. A malformed value is a config
// error surfaced at validation time, so callers past that point can ignore it.
func ParseMaxWait(maxWait string) (time.Duration, error) {
	if strings.TrimSpace(maxWait) == "" {
		return DependencyDefaultMaxWait, nil
	}
	d, err := time.ParseDuration(maxWait)
	if err != nil {
		return 0, fmt.Errorf("invalid max_wait %q: %w", maxWait, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid max_wait %q: must be positive", maxWait)
	}
	return d, nil
}

// WaitForDependencies blocks until every service in deps reports ready, reusing
// the health monitor's own signal — a dependency is ready once its most recent
// process-history state is Running. It retries on dependencyPollInterval rather
// than giving up on a fixed per-check timeout; maxWait is only the outer ceiling
// that stops an unmet dependency (a crash loop, a typo'd name, a cycle) from
// hanging the dependent forever. When the ceiling is hit it fails loud, naming
// the dependencies still not ready, never a silent timeout.
//
// onPendingChange, if non-nil, is called with the current pending subset every
// time it narrows (deduped against the last-reported set, so a poll tick that
// finds nothing new ready is silent). This is the single place that recomputes
// "what's still outstanding" — RecordDependencyWait mirrors that same value
// into eos status rather than maintaining its own, independently-computed
// copy that could drift from it.
func WaitForDependencies(ctx context.Context, prober dependencyReadinessProber, serviceName string, deps []string, maxWait time.Duration, onPendingChange func(pending []string)) error {
	if len(deps) == 0 {
		return nil
	}
	if maxWait <= 0 {
		maxWait = DependencyDefaultMaxWait
	}

	var lastReported []string
	report := func(pending []string) {
		if onPendingChange == nil || slices.Equal(pending, lastReported) {
			return
		}
		lastReported = pending
		onPendingChange(pending)
	}

	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	ticker := time.NewTicker(dependencyPollInterval)
	defer ticker.Stop()

	for {
		pending := pendingDependencies(prober, deps)
		if len(pending) == 0 {
			return nil
		}
		report(pending)

		select {
		case <-ctx.Done():
			return fmt.Errorf("service %q: waiting for dependencies [%s]: %w", serviceName, strings.Join(pending, ", "), ctx.Err())
		case <-timer.C:
			// One last read so the reported set reflects the deadline instant,
			// not the tick before it.
			pending = pendingDependencies(prober, deps)
			if len(pending) == 0 {
				return nil
			}
			report(pending)
			return fmt.Errorf(
				"service %q not started: dependencies not ready after %s: [%s]; confirm each is registered and starting cleanly (eos status, eos logs <name>)",
				serviceName, maxWait, strings.Join(pending, ", "))
		case <-ticker.C:
		}
	}
}

// pendingDependencies returns the deps that are not yet ready, preserving input
// order so the failure message reads deterministically. A dependency with no
// process history yet (never started, or unregistered) counts as pending.
func pendingDependencies(prober dependencyReadinessProber, deps []string) []string {
	var pending []string
	for _, dep := range deps {
		if !dependencyReady(prober, dep) {
			pending = append(pending, dep)
		}
	}
	return pending
}

func dependencyReady(prober dependencyReadinessProber, dep string) bool {
	entry, err := prober.GetMostRecentProcessHistoryEntry(dep)
	if err != nil || entry == nil {
		return false
	}
	return entry.State == types.ProcessStateRunning
}

// DependencyWaitRecorder is the slice of a manager RecordDependencyWait needs
// to make an in-progress depends_on wait visible to a concurrent `eos status`/
// `eos api status`. Only LocalManager and DaemonManager implement it; mgr is
// typed any rather than manager.ServiceManager so a caller passing either the
// interface or a concrete *LocalManager (as internal/process/daemon.go's boot
// path does) both work without widening manager.ServiceManager itself — every
// other implementer (and test fake) would otherwise have to carry these two
// methods too, for a feature that's pure status-display observability.
type DependencyWaitRecorder interface {
	SetDependencyWaitStatus(serviceName string, pending []string) error
	ClearDependencyWaitStatus(serviceName string) error
}

// RecordDependencyWait calls WaitForDependencies, mirroring its live pending
// subset into mgr's recorded dependency-wait status as WaitForDependencies's
// own poll loop narrows it down — so a dependent depends_on [A, B] where A
// comes ready quickly reports "waiting for: B", not a stale "waiting for: A,
// B" for the rest of max_wait — and clears the mark once the wait returns,
// regardless of outcome. If mgr doesn't implement DependencyWaitRecorder, or
// deps is empty, it's a transparent passthrough straight to
// WaitForDependencies: this is best-effort observability for status output,
// never a reason to fail or change the behavior of the wait itself.
func RecordDependencyWait(ctx context.Context, mgr any, prober dependencyReadinessProber, serviceName string, deps []string, maxWait time.Duration) error {
	recorder, ok := mgr.(DependencyWaitRecorder)
	if !ok || len(deps) == 0 {
		return WaitForDependencies(ctx, prober, serviceName, deps, maxWait, nil)
	}
	defer func() { _ = recorder.ClearDependencyWaitStatus(serviceName) }()
	return WaitForDependencies(ctx, prober, serviceName, deps, maxWait, func(pending []string) {
		_ = recorder.SetDependencyWaitStatus(serviceName, pending)
	})
}
