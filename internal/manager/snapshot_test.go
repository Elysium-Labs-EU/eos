package manager

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestSnapshotFilePath(t *testing.T) {
	got := SnapshotFilePath("/home/user/.eos")
	want := filepath.Join("/home/user/.eos", "running-services.json")
	if got != want {
		t.Errorf("SnapshotFilePath() = %q, want %q", got, want)
	}
}

func TestBuildSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	instances := []types.ServiceInstance{
		{Name: "web"},
		{Name: "api"},
		{Name: "cache"},
	}

	got := BuildSnapshot(instances, now)

	if !got.SavedAt.Equal(now) {
		t.Errorf("SavedAt = %v, want %v", got.SavedAt, now)
	}
	want := []string{"api", "cache", "web"}
	if !reflect.DeepEqual(got.Services, want) {
		t.Errorf("Services = %v, want %v (sorted)", got.Services, want)
	}
}

func TestBuildSnapshotEmpty(t *testing.T) {
	got := BuildSnapshot(nil, time.Now())
	if len(got.Services) != 0 {
		t.Errorf("expected no services, got %v", got.Services)
	}
}

func TestSaveAndLoadSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := SnapshotFilePath(dir)
	now := time.Date(2026, 7, 29, 8, 30, 0, 0, time.UTC)
	snap := Snapshot{SavedAt: now, Services: []string{"api", "web"}}

	if err := SaveSnapshot(path, snap); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}

	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if !got.SavedAt.Equal(now) {
		t.Errorf("SavedAt = %v, want %v", got.SavedAt, now)
	}
	if !reflect.DeepEqual(got.Services, snap.Services) {
		t.Errorf("Services = %v, want %v", got.Services, snap.Services)
	}
}

func TestSaveSnapshotOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := SnapshotFilePath(dir)

	if err := SaveSnapshot(path, Snapshot{Services: []string{"old"}}); err != nil {
		t.Fatalf("first SaveSnapshot() error = %v", err)
	}
	if err := SaveSnapshot(path, Snapshot{Services: []string{"new"}}); err != nil {
		t.Fatalf("second SaveSnapshot() error = %v", err)
	}

	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	want := []string{"new"}
	if !reflect.DeepEqual(got.Services, want) {
		t.Errorf("Services = %v, want %v", got.Services, want)
	}
}

func TestSaveSnapshotUnwritableDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "running-services.json")
	err := SaveSnapshot(path, Snapshot{})
	if err == nil {
		t.Fatal("expected an error writing to a nonexistent directory")
	}
}

func TestLoadSnapshotMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadSnapshot(SnapshotFilePath(dir))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestLoadSnapshotCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := SnapshotFilePath(dir)
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	_, err := LoadSnapshot(path)
	if err == nil {
		t.Fatal("expected an error parsing corrupt snapshot file")
	}
}

func TestOrderByDependenciesNoDeps(t *testing.T) {
	names := []string{"c", "a", "b"}
	got := OrderByDependencies(names, nil)
	if !reflect.DeepEqual(got, names) {
		t.Errorf("OrderByDependencies() = %v, want unchanged %v", got, names)
	}
}

func TestOrderByDependenciesSimpleChain(t *testing.T) {
	// web depends on api, api depends on db — restore order must put db
	// first, then api, then web, even though the input lists them backwards.
	names := []string{"web", "api", "db"}
	depsOf := map[string][]string{
		"web": {"api"},
		"api": {"db"},
	}

	got := OrderByDependencies(names, depsOf)
	want := []string{"db", "api", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrderByDependencies() = %v, want %v", got, want)
	}
}

func TestOrderByDependenciesIgnoresDepsOutsideSet(t *testing.T) {
	// api depends on a "db" that isn't part of this restore set — should be
	// ignored rather than blocking ordering (existing gateDependencies
	// behavior handles it at start time, same as a plain "eos run").
	names := []string{"api", "web"}
	depsOf := map[string][]string{
		"api": {"db"},
		"web": {"api"},
	}

	got := OrderByDependencies(names, depsOf)
	want := []string{"api", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrderByDependencies() = %v, want %v", got, want)
	}
}

func TestOrderByDependenciesPreservesTieOrder(t *testing.T) {
	// worker1 and worker2 both depend on db but have no ordering constraint
	// between each other — their relative input order should survive.
	names := []string{"worker2", "worker1", "db"}
	depsOf := map[string][]string{
		"worker1": {"db"},
		"worker2": {"db"},
	}

	got := OrderByDependencies(names, depsOf)
	want := []string{"db", "worker2", "worker1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrderByDependencies() = %v, want %v", got, want)
	}
}

func TestOrderByDependenciesCycleStillReturnsAllNames(t *testing.T) {
	names := []string{"a", "b"}
	depsOf := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}

	got := OrderByDependencies(names, depsOf)
	if len(got) != len(names) {
		t.Fatalf("expected all %d names back even with a cycle, got %v", len(names), got)
	}
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for _, n := range names {
		if !gotSet[n] {
			t.Errorf("expected %q in result, got %v", n, got)
		}
	}
}

func TestOrderByDependenciesPartialCyclePreservesResolvedPrefix(t *testing.T) {
	// x has no deps and resolves normally; a and b form a cycle between
	// themselves and are appended afterward in original relative order.
	names := []string{"a", "x", "b"}
	depsOf := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}

	got := OrderByDependencies(names, depsOf)
	want := []string{"x", "a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OrderByDependencies() = %v, want %v", got, want)
	}
}

func TestOrderByDependenciesEmpty(t *testing.T) {
	got := OrderByDependencies(nil, nil)
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}
