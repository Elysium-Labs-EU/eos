package testmatrix

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
)

// countingRunner is a CmdRunner that succeeds on every call and records how
// many calls were in flight at once, to prove concurrency actually happens.
type countingRunner struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	calls       int
}

func (r *countingRunner) Run(_ context.Context, _ string, _ ...string) (string, error) {
	r.mu.Lock()
	r.inFlight++
	r.calls++
	if r.inFlight > r.maxInFlight {
		r.maxInFlight = r.inFlight
	}
	r.mu.Unlock()

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
	return "", nil
}

func testConfig() Config {
	return Config{
		Targets: []Target{
			{Name: "debian", Golden: "eos-golden-debian"},
			{Name: "alpine", Golden: "eos-golden-alpine"},
		},
		Suites: []Suite{
			{Name: "lifecycle", Command: "go test ./..."},
			{Name: "openrc", Command: "go test -run OpenRC", Only: []string{"alpine"}},
			{Name: "fixtures", Command: "bash scripts/test-fixtures-orb.sh", Nightly: true},
		},
	}
}

func pairKeys(results []Result) []string {
	keys := make([]string, len(results))
	for i, r := range results {
		keys[i] = r.Target + "/" + r.Suite
	}
	sort.Strings(keys)
	return keys
}

func TestPairs_DefaultExcludesNightlyAndHonorsOnly(t *testing.T) {
	got := pairs(testConfig(), Options{})

	want := []string{"alpine/lifecycle", "alpine/openrc", "debian/lifecycle"}
	if strings.Join(pairKeysFromPairs(got), ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, pairKeysFromPairs(got))
	}
}

func TestPairs_IncludeNightly(t *testing.T) {
	got := pairs(testConfig(), Options{IncludeNightly: true})

	want := []string{"alpine/fixtures", "alpine/lifecycle", "alpine/openrc", "debian/fixtures", "debian/lifecycle"}
	if strings.Join(pairKeysFromPairs(got), ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, pairKeysFromPairs(got))
	}
}

func TestPairs_SuiteFilter(t *testing.T) {
	got := pairs(testConfig(), Options{SuiteFilter: "openrc"})

	want := []string{"alpine/openrc"}
	if strings.Join(pairKeysFromPairs(got), ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, pairKeysFromPairs(got))
	}
}

func pairKeysFromPairs(ps []pair) []string {
	keys := make([]string, len(ps))
	for i := range ps {
		keys[i] = ps[i].target.Name + "/" + ps[i].suite.Name
	}
	sort.Strings(keys)
	return keys
}

func TestRunMatrix_RunsSelectedPairs(t *testing.T) {
	r := &countingRunner{}
	o := &Orb{Runner: r}

	results := RunMatrix(context.Background(), o, testConfig(), Options{RunID: "run1"})

	want := []string{"alpine/lifecycle", "alpine/openrc", "debian/lifecycle"}
	if strings.Join(pairKeys(results), ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, pairKeys(results))
	}
	for _, res := range results {
		if !res.Passed {
			t.Fatalf("expected all pairs to pass, got %+v", res)
		}
	}
}

func TestRunMatrix_RespectsMaxConcurrency(t *testing.T) {
	r := &countingRunner{}
	o := &Orb{Runner: r}

	results := RunMatrix(context.Background(), o, testConfig(), Options{RunID: "run1", MaxConcurrency: 1})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if r.maxInFlight > 1 {
		t.Fatalf("expected at most 1 in-flight orb call with MaxConcurrency=1, got %d", r.maxInFlight)
	}
}

func TestRunMatrix_EmptySelection(t *testing.T) {
	r := &countingRunner{}
	o := &Orb{Runner: r}

	results := RunMatrix(context.Background(), o, testConfig(), Options{SuiteFilter: "does-not-exist"})

	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}
