package manager

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
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

func (f *fakeProber) GetMostRecentProcessHistoryEntry(context.Context, string) (*types.ProcessHistory, error) {
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

// perDepProber lets a test control each dependency's readiness independently
// by name (unlike fakeProber's single shared threshold), so it can model one
// dependency becoming ready well before another.
type perDepProber struct {
	states map[string]types.ProcessState
	mu     sync.Mutex
}

func (p *perDepProber) GetMostRecentProcessHistoryEntry(_ context.Context, name string) (*types.ProcessHistory, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.states[name]
	if !ok {
		state = types.ProcessStateStarting
	}
	return &types.ProcessHistory{State: state}, nil
}

// markRunning flips name to Running; every caller only ever needs to move a
// dependency forward to ready, never to any other state.
func (p *perDepProber) markRunning(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[name] = types.ProcessStateRunning
}

func TestWaitForDependencies_NoDeps(t *testing.T) {
	if err := WaitForDependencies(context.Background(), &fakeProber{}, "svc", nil, time.Second, nil); err != nil {
		t.Fatalf("no deps should return nil, got %v", err)
	}
}

func TestWaitForDependencies_ReadyImmediately(t *testing.T) {
	p := &fakeProber{readyAfter: 1}
	if err := WaitForDependencies(context.Background(), p, "b", []string{"a"}, time.Second, nil); err != nil {
		t.Fatalf("dep already Running should return nil, got %v", err)
	}
}

func TestWaitForDependencies_BlocksUntilReady(t *testing.T) {
	// Not ready on the first read, ready on a later poll: proves it retries
	// rather than failing on the first not-ready check.
	p := &fakeProber{readyAfter: 3}
	start := time.Now()
	if err := WaitForDependencies(context.Background(), p, "b", []string{"a"}, 5*time.Second, nil); err != nil {
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
	err := WaitForDependencies(context.Background(), p, "web", []string{"db", "cache"}, 150*time.Millisecond, nil)
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
	if err := WaitForDependencies(ctx, p, "b", []string{"a"}, time.Minute, nil); err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
}

// TestWaitForDependencies_OnPendingChangeNarrows proves the fix for the review
// finding that the recorded pending set never reflected dependencies becoming
// ready one at a time: with deps [a, b], a becomes ready quickly while b stays
// pending until success; onPendingChange must be called with the full [a, b]
// set first, then a narrowed [b] set once a clears — never stuck reporting a
// as still blocking after it's ready.
func TestWaitForDependencies_OnPendingChangeNarrows(t *testing.T) {
	prober := &perDepProber{states: map[string]types.ProcessState{"a": types.ProcessStateStarting, "b": types.ProcessStateStarting}}

	var mu sync.Mutex
	var reported [][]string
	onChange := func(pending []string) {
		mu.Lock()
		defer mu.Unlock()
		reported = append(reported, append([]string(nil), pending...))
	}

	go func() {
		time.Sleep(dependencyPollInterval * 2)
		prober.markRunning("a")
		time.Sleep(dependencyPollInterval * 2)
		prober.markRunning("b")
	}()

	if err := WaitForDependencies(context.Background(), prober, "web", []string{"a", "b"}, 5*time.Second, onChange); err != nil {
		t.Fatalf("expected success once both deps become ready, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(reported) < 2 {
		t.Fatalf("expected at least 2 distinct reported pending sets, got %v", reported)
	}
	if !slices.Equal(reported[0], []string{"a", "b"}) {
		t.Errorf("expected the first reported set to be the full [a b], got %v", reported[0])
	}
	last := reported[len(reported)-1]
	if !slices.Equal(last, []string{"b"}) {
		t.Errorf("expected the final reported set to have narrowed to [b] once a became ready, got %v", last)
	}
}

// TestWaitForDependencies_OnPendingChangeDeduped proves onPendingChange is not
// called on every poll tick — only when the pending set actually changes —
// so a dependent waiting out most of max_wait doesn't hammer eos status's
// backing store with identical writes every 250ms.
func TestWaitForDependencies_OnPendingChangeDeduped(t *testing.T) {
	p := &fakeProber{readyAfter: 1 << 30}
	var calls atomic.Int64
	onChange := func([]string) { calls.Add(1) }

	err := WaitForDependencies(context.Background(), p, "web", []string{"a"}, 800*time.Millisecond, onChange)
	if err == nil {
		t.Fatal("expected a max_wait error since the dep never becomes ready")
	}
	// The pending set is always [a] here (never changes), so onPendingChange
	// must fire exactly once (the initial report) despite several poll ticks
	// happening over 800ms at a 250ms interval.
	if got := calls.Load(); got != 1 {
		t.Errorf("expected onPendingChange to fire exactly once for an unchanging pending set, got %d calls", got)
	}
}

// TestDepTimeoutResult exercises WaitForDependencies's timer.C branch directly:
// depTimeoutResult must take one last read at the deadline instant rather than
// trusting the tick before it, so a dependency that becomes ready exactly as
// max_wait expires still succeeds instead of failing on stale data.
func TestDepTimeoutResult(t *testing.T) {
	t.Run("ready at the deadline succeeds silently", func(t *testing.T) {
		p := &fakeProber{readyAfter: 1}
		var reported [][]string
		report := func(pending []string) { reported = append(reported, pending) }

		if err := depTimeoutResult(t.Context(), p, []string{"a"}, "web", time.Second, report); err != nil {
			t.Fatalf("expected nil when the deadline-instant read shows ready, got %v", err)
		}
		if len(reported) != 0 {
			t.Errorf("expected no report when the deadline read finds nothing pending, got %v", reported)
		}
	})

	t.Run("still pending reports then fails loud", func(t *testing.T) {
		p := &fakeProber{readyAfter: 1 << 30}
		var reported [][]string
		report := func(pending []string) { reported = append(reported, pending) }

		err := depTimeoutResult(t.Context(), p, []string{"a"}, "web", time.Second, report)
		if err == nil {
			t.Fatal("expected an error when still pending at the deadline")
		}
		if !strings.Contains(err.Error(), "not ready after") {
			t.Errorf("error %q missing the timeout message", err.Error())
		}
		if len(reported) != 1 || !slices.Equal(reported[0], []string{"a"}) {
			t.Errorf("expected exactly one report of [a], got %v", reported)
		}
	})
}

func TestDependencyWaitIsStale(t *testing.T) {
	if dependencyWaitIsStale(time.Now().Add(5 * time.Minute)) {
		t.Error("a wait whose deadline is still in the future must not be stale")
	}
	if dependencyWaitIsStale(time.Now()) {
		t.Error("a wait whose deadline just passed must not be stale yet — the grace period hasn't elapsed")
	}
	if dependencyWaitIsStale(time.Now().Add(-DependencyWaitStaleGrace / 2)) {
		t.Error("a wait whose deadline passed well within the grace period must not be stale")
	}
	if !dependencyWaitIsStale(time.Now().Add(-DependencyWaitStaleGrace - time.Second)) {
		t.Error("a wait whose deadline passed beyond the grace period must be stale")
	}
}

func TestResolveMaxWait(t *testing.T) {
	if got := resolveMaxWait(0); got != DependencyDefaultMaxWait {
		t.Errorf("expected 0 to resolve to the default %s, got %s", DependencyDefaultMaxWait, got)
	}
	if got := resolveMaxWait(-time.Second); got != DependencyDefaultMaxWait {
		t.Errorf("expected a negative value to resolve to the default %s, got %s", DependencyDefaultMaxWait, got)
	}
	if got := resolveMaxWait(30 * time.Second); got != 30*time.Second {
		t.Errorf("expected a positive value to pass through unchanged, got %s", got)
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

// recorderSpy implements DependencyWaitRecorder and records call order (and
// every Set's pending snapshot) so RecordDependencyWait's Set-before/
// Clear-after contract, and the live narrowing of pending, can be asserted.
type recorderSpy struct {
	setDeadline  time.Time
	setErr       error
	clearErr     error
	clearCtxErr  error
	calls        []string
	setPending   []string
	allSets      [][]string
	allDeadlines []time.Time
	setCtxErrs   []error
}

func (r *recorderSpy) SetDependencyWaitStatus(ctx context.Context, _ string, pending []string, deadline time.Time) error {
	r.calls = append(r.calls, "set")
	r.setPending = pending
	r.setDeadline = deadline
	r.allSets = append(r.allSets, append([]string(nil), pending...))
	r.allDeadlines = append(r.allDeadlines, deadline)
	r.setCtxErrs = append(r.setCtxErrs, ctx.Err())
	return r.setErr
}

func (r *recorderSpy) ClearDependencyWaitStatus(ctx context.Context, _ string) error {
	r.calls = append(r.calls, "clear")
	r.clearCtxErr = ctx.Err()
	return r.clearErr
}

// notARecorder is a manager.ServiceManager stand-in that does NOT implement
// DependencyWaitRecorder, modeling a test fake that predates this feature.
type notARecorder struct{}

func TestRecordDependencyWait_unsupportedManagerIsPassthrough(t *testing.T) {
	p := &fakeProber{readyAfter: 1}
	err := RecordDependencyWait(context.Background(), &notARecorder{}, p, "web", []string{"db"}, time.Second)
	if err != nil {
		t.Fatalf("expected passthrough with no error, got %v", err)
	}
}

func TestRecordDependencyWait_noPendingIsPassthrough(t *testing.T) {
	spy := &recorderSpy{}
	p := &fakeProber{readyAfter: 1}
	err := RecordDependencyWait(context.Background(), spy, p, "web", nil, time.Second)
	if err != nil {
		t.Fatalf("expected passthrough with no error, got %v", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("expected no Set/Clear calls when deps is empty, got %v", spy.calls)
	}
}

func TestRecordDependencyWait_setsThenClearsOnSuccess(t *testing.T) {
	spy := &recorderSpy{}
	// readyAfter: 3, not 1: fakeProber's readAfter counts reads across BOTH
	// deps sharing one counter, so this is the smallest threshold that leaves
	// neither "db" (read 1) nor "cache" (read 2) ready on the first round —
	// there's something to actually report before the wait resolves on its
	// second poll, unlike an immediate-ready case where Set is correctly
	// never called at all.
	p := &fakeProber{readyAfter: 3}
	err := RecordDependencyWait(context.Background(), spy, p, "web", []string{"db", "cache"}, time.Second)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(spy.calls) != 2 || spy.calls[0] != "set" || spy.calls[1] != "clear" {
		t.Fatalf("expected [set clear], got %v", spy.calls)
	}
	if !slices.Equal(spy.setPending, []string{"db", "cache"}) {
		t.Errorf("expected pending [db cache] passed to Set, got %v", spy.setPending)
	}
	if spy.setDeadline.IsZero() {
		t.Error("expected a non-zero deadline passed to Set")
	}
	if wantAround := time.Now().Add(time.Second); spy.setDeadline.After(wantAround.Add(time.Second)) || spy.setDeadline.Before(wantAround.Add(-time.Second)) {
		t.Errorf("expected deadline roughly maxWait (1s) from now, got %v (now %v)", spy.setDeadline, time.Now())
	}
}

func TestRecordDependencyWait_clearsEvenOnError(t *testing.T) {
	spy := &recorderSpy{}
	p := &fakeProber{readyAfter: 1 << 30}
	err := RecordDependencyWait(context.Background(), spy, p, "web", []string{"db"}, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a max_wait error, got nil")
	}
	if len(spy.calls) != 2 || spy.calls[0] != "set" || spy.calls[1] != "clear" {
		t.Fatalf("expected [set clear] even on error, got %v", spy.calls)
	}
}

// TestRecordDependencyWait_cleanupSurvivesCallerCancellation is the direct
// regression test for the review finding: if ctx is canceled (e.g. Ctrl-C),
// the deferred ClearDependencyWaitStatus must still run against a live
// context, not the same already-canceled one — otherwise the cleanup call
// itself fails immediately, leaving a stale row until DependencyWaitStaleGrace
// expires instead of clearing it right away.
func TestRecordDependencyWait_cleanupSurvivesCallerCancellation(t *testing.T) {
	spy := &recorderSpy{}
	p := &fakeProber{readyAfter: 1 << 30} // never ready
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RecordDependencyWait(ctx, spy, p, "web", []string{"db"}, time.Minute)
	if err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}

	if len(spy.calls) == 0 || spy.calls[len(spy.calls)-1] != "clear" {
		t.Fatalf("expected a final clear call even when ctx was canceled, got %v", spy.calls)
	}
	if spy.clearCtxErr != nil {
		t.Errorf("expected ClearDependencyWaitStatus to receive a live context despite the caller's ctx being canceled, got ctx.Err() = %v", spy.clearCtxErr)
	}
	for i, ctxErr := range spy.setCtxErrs {
		if ctxErr != nil {
			t.Errorf("expected SetDependencyWaitStatus call %d to receive a live context, got ctx.Err() = %v", i, ctxErr)
		}
	}
}

// TestRecordDependencyWait_narrowsPendingAsDependenciesBecomeReady is the
// direct regression test for the review finding: with depends_on [a, b], a
// becomes ready quickly while b stays pending. The recorded status must
// narrow from [a, b] to [b], not stay stuck reporting a as still blocking.
func TestRecordDependencyWait_narrowsPendingAsDependenciesBecomeReady(t *testing.T) {
	spy := &recorderSpy{}
	prober := &perDepProber{states: map[string]types.ProcessState{"a": types.ProcessStateStarting, "b": types.ProcessStateStarting}}

	go func() {
		time.Sleep(dependencyPollInterval * 2)
		prober.markRunning("a")
		time.Sleep(dependencyPollInterval * 2)
		prober.markRunning("b")
	}()

	if err := RecordDependencyWait(context.Background(), spy, prober, "web", []string{"a", "b"}, 5*time.Second); err != nil {
		t.Fatalf("expected success once both deps become ready, got %v", err)
	}

	if len(spy.allSets) < 2 {
		t.Fatalf("expected at least 2 distinct Set calls as pending narrowed, got %v", spy.allSets)
	}
	if !slices.Equal(spy.allSets[0], []string{"a", "b"}) {
		t.Errorf("expected the first Set to record the full [a b], got %v", spy.allSets[0])
	}
	lastSet := spy.allSets[len(spy.allSets)-1]
	if !slices.Equal(lastSet, []string{"b"}) {
		t.Errorf("expected the final Set before success to have narrowed to [b], got %v", lastSet)
	}
	if spy.calls[len(spy.calls)-1] != "clear" {
		t.Errorf("expected the very last call to be clear, got %v", spy.calls)
	}

	// The deadline must be the SAME value on every Set call for this one
	// wait, not recomputed fresh each time a dependency narrows — a
	// recomputed deadline would keep pushing the staleness ceiling forward
	// indefinitely and defeat the point of a fixed per-wait ceiling.
	for i, d := range spy.allDeadlines {
		if !d.Equal(spy.allDeadlines[0]) {
			t.Errorf("expected every Set call to carry the same deadline, call %d got %v, first was %v", i, d, spy.allDeadlines[0])
		}
	}
}

// TestRecordDependencyWait_realLocalManager proves the wiring end-to-end
// against a real LocalManager (the actual DependencyWaitRecorder used by both
// cmd/run.go's gateDependencies and internal/process/daemon.go's bootService):
// GetDependencyWaitStatus reflects the wait while it's in progress, and is
// cleared once RecordDependencyWait returns.
func TestRecordDependencyWait_realLocalManager(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	prober := &perDepProber{states: map[string]types.ProcessState{"db": types.ProcessStateStarting}}

	var midCallStatus types.DependencyWaitStatus
	var midCallWaiting bool
	go func() {
		time.Sleep(dependencyPollInterval)
		midCallStatus, midCallWaiting, _ = m.GetDependencyWaitStatus(t.Context(), "web")
		prober.markRunning("db")
	}()

	err := RecordDependencyWait(t.Context(), m, prober, "web", []string{"db"}, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !midCallWaiting || !slices.Equal(midCallStatus.Pending, []string{"db"}) {
		t.Fatalf("expected a recorded wait for [db] during the wait, got %+v waiting=%v", midCallStatus, midCallWaiting)
	}

	after, waiting, err := m.GetDependencyWaitStatus(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetDependencyWaitStatus after RecordDependencyWait: %v", err)
	}
	if waiting {
		t.Fatalf("expected the wait to be cleared after RecordDependencyWait returns, got %+v", after)
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
