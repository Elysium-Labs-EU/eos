package testmatrix

import (
	"context"
	"fmt"
	"time"
)

// Result is the outcome of running one suite against one target's clone.
type Result struct {
	Err      error
	Target   string
	Suite    string
	Clone    string
	Output   string
	Duration time.Duration
	Passed   bool
	Kept     bool
}

// RunTarget clones target's golden VM, runs suite's command inside the
// clone, and deletes the clone unless the suite failed and keepOnFailure is
// set (in which case the clone is left running for the caller to inspect).
func RunTarget(ctx context.Context, o *Orb, t Target, s Suite, runID string, keepOnFailure bool) Result {
	clone := fmt.Sprintf("run-%s-%s-%s", t.Name, s.Name, runID)
	start := time.Now()

	if err := o.Clone(ctx, t.Golden, clone); err != nil {
		return Result{
			Target: t.Name, Suite: s.Name, Clone: clone,
			Duration: time.Since(start), Err: fmt.Errorf("clone: %w", err),
		}
	}

	out, runErr := o.Run(ctx, clone, s.Command)
	passed := runErr == nil
	result := Result{
		Target: t.Name, Suite: s.Name, Clone: clone,
		Passed: passed, Duration: time.Since(start), Output: out, Err: runErr,
	}

	if passed || !keepOnFailure {
		if err := o.Delete(ctx, clone); err != nil && result.Err == nil {
			result.Err = fmt.Errorf("delete: %w", err)
		}
	} else {
		result.Kept = true
	}

	return result
}
