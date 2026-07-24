package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

type CatalogOption func(*catalogDefaults)

type catalogDefaults struct {
	pathTemplate   string
	configTemplate string
}

func WithCatalogPathTemplate(tmpl string) CatalogOption {
	return func(d *catalogDefaults) { d.pathTemplate = tmpl }
}

func WithCatalogConfigTemplate(tmpl string) CatalogOption {
	return func(d *catalogDefaults) { d.configTemplate = tmpl }
}

// SeedServiceCatalog inserts n catalog entries with names "svc-0" … "svc-n-1".
// Accepts testing.TB so it can be called from both *testing.T and *testing.B.
func SeedServiceCatalog(t testing.TB, ctx context.Context, db database.Database, n int, opts ...CatalogOption) []types.ServiceCatalogEntry {
	t.Helper()

	d := &catalogDefaults{
		pathTemplate:   "/srv/svc-%d",
		configTemplate: "svc-%d.yaml",
	}
	for _, o := range opts {
		o(d)
	}

	entries := make([]types.ServiceCatalogEntry, 0, n)
	for i := range n {
		name := fmt.Sprintf("svc-%d", i)
		path := fmt.Sprintf(d.pathTemplate, i)
		cfg := fmt.Sprintf(d.configTemplate, i)
		createdAt := time.Now()

		if err := db.RegisterService(ctx, name, path, cfg); err != nil {
			t.Fatalf("SeedServiceCatalog[%d]: %v", i, err)
		}

		entries = append(entries, types.ServiceCatalogEntry{
			Name:           name,
			DirectoryPath:  path,
			ConfigFileName: cfg,
			CreatedAt:      createdAt,
		})
	}

	return entries
}

type InstanceOption func(*instanceDefaults)

type instanceDefaults struct {
	restartCount int
}

func WithSeedRestartCount(n int) InstanceOption {
	return func(d *instanceDefaults) { d.restartCount = n }
}

// SeedServiceInstances inserts an instance for each name.
// Service instance must exist before calling SeedProcessHistory (FK: process_history.service_name).
func SeedServiceInstances(t testing.TB, ctx context.Context, db database.Database, names []string, opts ...InstanceOption) []types.ServiceInstance {
	t.Helper()

	d := &instanceDefaults{}
	for _, o := range opts {
		o(d)
	}

	instances := make([]types.ServiceInstance, 0, len(names))
	for _, name := range names {
		createdAt := time.Now()

		if err := db.RegisterServiceInstance(ctx, name); err != nil {
			t.Fatalf("SeedServiceInstances %q: %v", name, err)
		}

		applyInstanceRestartCount(t, ctx, db, name, d.restartCount)

		instances = append(instances, types.ServiceInstance{
			Name:         name,
			RestartCount: d.restartCount,
			CreatedAt:    createdAt,
		})
	}

	return instances
}

// applyInstanceRestartCount updates a seeded instance's restart count when the
// requested count is non-zero.
func applyInstanceRestartCount(t testing.TB, ctx context.Context, db database.Database, name string, restartCount int) {
	t.Helper()
	if restartCount <= 0 {
		return
	}
	if err := db.UpdateServiceInstance(ctx, name, database.ServiceInstanceUpdate{
		RestartCount: &restartCount,
	}); err != nil {
		t.Fatalf("SeedServiceInstances %q update restart count: %v", name, err)
	}
}

type HistoryOption func(*historyDefaults)

type historyDefaults struct {
	state          types.ProcessState
	basePGID       int
	startedAtTicks int64
}

func WithHistoryState(s types.ProcessState) HistoryOption {
	return func(d *historyDefaults) { d.state = s }
}

// WithBasePGID sets the starting PGID. Default: 10000. Increment across calls to avoid PK collisions.
func WithBasePGID(base int) HistoryOption {
	return func(d *historyDefaults) { d.basePGID = base }
}

// WithHistoryStartedAtTicks sets the started_at_ticks value stored alongside every seeded
// PGID (see procutil.StartTime). Default: 0 — fine for the common case of a PGID that
// isn't expected to pass a liveness match; set explicitly when a test needs
// procutil.IsAliveMatching to succeed against a real process.
func WithHistoryStartedAtTicks(ticks int64) HistoryOption {
	return func(d *historyDefaults) { d.startedAtTicks = ticks }
}

// SeedProcessHistory inserts n history entries for serviceName (requires instance to exist; FK constraint).
// PGIDs assigned as basePGID … basePGID+n-1.
func SeedProcessHistory(t testing.TB, ctx context.Context, db database.Database, serviceName string, n int, opts ...HistoryOption) []types.ProcessHistory {
	t.Helper()

	d := &historyDefaults{
		state:    types.ProcessStateRunning,
		basePGID: 10000,
	}
	for _, o := range opts {
		o(d)
	}

	entries := make([]types.ProcessHistory, 0, n)
	for i := range n {
		pgid := d.basePGID + i

		entry, err := db.RegisterProcessHistoryEntry(ctx, pgid, d.startedAtTicks, serviceName, d.state)
		if err != nil {
			t.Fatalf("SeedProcessHistory[%d] pgid=%d: %v", i, pgid, err)
		}

		entries = append(entries, entry)
	}

	return entries
}
