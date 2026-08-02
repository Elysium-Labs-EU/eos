package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// SnapshotFileName is the well-known name of the running-services snapshot
// file inside a base dir, written by "eos snapshot save" and read by
// "eos snapshot restore".
const SnapshotFileName = "running-services.json"

// Snapshot is the on-disk record of which services were running at the time
// it was saved.
type Snapshot struct {
	SavedAt  time.Time `json:"saved_at"`
	Services []string  `json:"services"`
}

// SnapshotFilePath returns the absolute path to the snapshot file inside
// baseDir. Pure — no I/O.
func SnapshotFilePath(baseDir string) string {
	return filepath.Join(baseDir, SnapshotFileName)
}

// BuildSnapshot turns the manager's currently-running service instances into
// a Snapshot, sorted for deterministic output. Pure — takes now rather than
// calling time.Now() itself, so callers resolve the timestamp once at the
// I/O boundary.
func BuildSnapshot(instances []types.ServiceInstance, now time.Time) Snapshot {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.Name)
	}
	sort.Strings(names)
	return Snapshot{SavedAt: now, Services: names}
}

// SaveSnapshot writes snap as JSON to path, replacing any existing file.
func SaveSnapshot(path string, snap Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing snapshot file %s: %w", path, err)
	}
	return nil
}

// LoadSnapshot reads and parses the snapshot file at path. A missing file
// surfaces as the underlying *os.PathError, checkable with os.IsNotExist /
// errors.Is(err, os.ErrNotExist), so callers can tell "never saved" apart
// from a corrupt file.
func LoadSnapshot(path string) (Snapshot, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("parsing snapshot file %s: %w", path, err)
	}
	return snap, nil
}

// OrderByDependencies topologically sorts names so that, for every service in
// the set, its depends_on entries that are also in the set come before it —
// restoring dependencies before the services that wait on them, rather than
// leaving that to gateDependencies' poll-until-max_wait loop, which would
// otherwise stall (or time out) waiting for a dependency this same sequential
// restore hasn't gotten to yet. depsOf supplies each name's configured
// DependsOn list (deps outside the set are ignored: they're either already
// running or not part of this restore, and gateDependencies handles that case
// exactly as it does for a plain "eos run" today).
//
// Ties (services with no ordering constraint between them) preserve their
// relative order from names, so output is deterministic for a given input.
// A dependency cycle within the set can never reach in-degree zero; those
// names are appended at the end in their original relative order rather than
// dropped, so restore still attempts them (and, same as today, gateDependencies'
// max_wait ultimately surfaces the cycle as a loud timeout instead of a silent
// omission).
func OrderByDependencies(names []string, depsOf map[string][]string) []string {
	indegree, dependents := snapBuildDependencyGraph(names, depsOf)
	ordered := snapTopologicalWalk(names, indegree, dependents)
	return snapAppendUnresolved(ordered, names)
}

// snapBuildDependencyGraph computes, for each name in the set, its in-degree
// (count of same-set dependencies it's waiting on) and the reverse edges
// (dependents), which snapTopologicalWalk consumes to peel off zero-in-degree
// names layer by layer. Dependencies outside the set don't count.
func snapBuildDependencyGraph(names []string, depsOf map[string][]string) (indegree map[string]int, dependents map[string][]string) {
	inSet := make(map[string]bool, len(names))
	for _, name := range names {
		inSet[name] = true
	}

	indegree = make(map[string]int, len(names))
	dependents = make(map[string][]string, len(names))
	for _, name := range names {
		for _, dep := range depsOf[name] {
			if !inSet[dep] {
				continue
			}
			indegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}
	return indegree, dependents
}

// snapTopologicalWalk runs a Kahn's-algorithm BFS over the graph built by
// snapBuildDependencyGraph, starting from names in their original order so
// ties preserve input order. Names inside a dependency cycle never reach
// in-degree zero and are left out of the result; snapAppendUnresolved
// handles them.
func snapTopologicalWalk(names []string, indegree map[string]int, dependents map[string][]string) []string {
	queue := make([]string, 0, len(names))
	for _, name := range names {
		if indegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	ordered := make([]string, 0, len(names))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		ordered = append(ordered, name)
		for _, dependent := range dependents[name] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	return ordered
}

// snapAppendUnresolved appends any names missing from ordered (i.e. caught in
// a dependency cycle) at the end, in their original relative order from
// names, so restore still attempts them instead of silently dropping them.
func snapAppendUnresolved(ordered []string, names []string) []string {
	if len(ordered) == len(names) {
		return ordered
	}

	orderedSet := make(map[string]bool, len(ordered))
	for _, name := range ordered {
		orderedSet[name] = true
	}
	for _, name := range names {
		if !orderedSet[name] {
			ordered = append(ordered, name)
		}
	}
	return ordered
}
