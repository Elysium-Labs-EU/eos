package manager

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// fakeProber returns a fixed state for every service, flipping to Running once
// readyAfter reads have happened. It models the health monitor advancing a
// dependency to Running some ticks after it was asked for.
type fakeProber struct {
	err        error
	reads      atomic.Int64
	readyAfter int64
}

func (f *fakeProber) GetMostRecentProcessHistoryEntry(string) (*types.ProcessHistory, error) {
	n := f.reads.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	state := types.ProcessStateStarting
	if n >= f.readyAfter {
		state = types.ProcessStateRunning
	}
	return &types.ProcessHistory{State: state}, nil
}

func TestWaitForDependencies_NoDeps(t *testing.T) {
	if err := WaitForDependencies(context.Background(), &fakeProber{}, "svc", nil, time.Second); err != nil {
		t.Fatalf("no deps should return nil, got %v", err)
	}
}

func TestWaitForDependencies_ReadyImmediately(t *testing.T) {
	p := &fakeProber{readyAfter: 1}
	if err := WaitForDependencies(context.Background(), p, "b", []string{"a"}, time.Second); err != nil {
		t.Fatalf("dep already Running should return nil, got %v", err)
	}
}

func TestWaitForDependencies_BlocksUntilReady(t *testing.T) {
	// Not ready on the first read, ready on a later poll: proves it retries
	// rather than failing on the first not-ready check.
	p := &fakeProber{readyAfter: 3}
	start := time.Now()
	if err := WaitForDependencies(context.Background(), p, "b", []string{"a"}, 5*time.Second); err != nil {
		t.Fatalf("dep becoming ready should return nil, got %v", err)
	}
	if p.reads.Load() < 3 {
		t.Fatalf("expected at least 3 readiness reads, got %d", p.reads.Load())
	}
	if time.Since(start) < dependencyPollInterval {
		t.Fatalf("returned before polling; expected to wait at least one interval")
	}
}

func TestWaitForDependencies_MaxWaitFailsLoud(t *testing.T) {
	// readyAfter far beyond what maxWait allows: the gate must give up loudly.
	p := &fakeProber{readyAfter: 1 << 30}
	err := WaitForDependencies(context.Background(), p, "web", []string{"db", "cache"}, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a max_wait error, got nil")
	}
	for _, want := range []string{"web", "db", "cache", "not ready"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestWaitForDependencies_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &fakeProber{readyAfter: 1 << 30}
	if err := WaitForDependencies(ctx, p, "b", []string{"a"}, time.Minute); err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
}

func TestParseMaxWait(t *testing.T) {
	if d, err := ParseMaxWait(""); err != nil || d != DependencyDefaultMaxWait {
		t.Fatalf("empty should default to %s, got %s (%v)", DependencyDefaultMaxWait, d, err)
	}
	if d, err := ParseMaxWait("30s"); err != nil || d != 30*time.Second {
		t.Fatalf("30s parse: got %s (%v)", d, err)
	}
	if _, err := ParseMaxWait("nonsense"); err == nil {
		t.Fatal("malformed value should error")
	}
	if _, err := ParseMaxWait("0s"); err == nil {
		t.Fatal("non-positive value should error")
	}
}

func TestValidateDependencies(t *testing.T) {
	if errs := ValidateDependencies("web", []string{"db"}, "10s"); len(errs) != 0 {
		t.Fatalf("valid config produced errors: %v", errs)
	}
	if errs := ValidateDependencies("web", []string{"web"}, ""); len(errs) == 0 {
		t.Error("self-dependency should be rejected")
	}
	if errs := ValidateDependencies("web", []string{"db", "db"}, ""); len(errs) == 0 {
		t.Error("duplicate dependency should be rejected")
	}
	if errs := ValidateDependencies("web", []string{"db"}, "nope"); len(errs) == 0 {
		t.Error("malformed max_wait should be rejected")
	}
}
