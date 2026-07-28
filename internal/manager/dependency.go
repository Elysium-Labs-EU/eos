package manager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// DependencyDefaultMaxWait bounds how long a dependent service waits for its
// DependsOn to report ready when its config sets no max_wait of its own.
// Generous on purpose: the point of depends_on is to outlast a slow dependency,
// unlike a fixed healthcheck timeout that gives up.
const DependencyDefaultMaxWait = 60 * time.Second

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
func WaitForDependencies(ctx context.Context, prober dependencyReadinessProber, serviceName string, deps []string, maxWait time.Duration) error {
	if len(deps) == 0 {
		return nil
	}
	if maxWait <= 0 {
		maxWait = DependencyDefaultMaxWait
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
