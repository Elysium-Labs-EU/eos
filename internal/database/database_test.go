package database_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestRegisterService(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterService(t.Context(), "web-api", "/opt/services/web-api", "service.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	entry, err := db.GetServiceCatalogEntry(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry failed: %v", err)
	}

	if entry.Name != "web-api" {
		t.Errorf("expected name 'web-api', got '%s'", entry.Name)
	}
	if entry.DirectoryPath != "/opt/services/web-api" {
		t.Errorf("expected path '/opt/services/web-api', got '%s'", entry.DirectoryPath)
	}
	if entry.ConfigFileName != "service.yaml" {
		t.Errorf("expected config 'service.yaml', got '%s'", entry.ConfigFileName)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestRegisterService_Upsert(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterService(t.Context(), "web-api", "/old/path", "old.yaml")
	if err != nil {
		t.Fatalf("First RegisterService failed: %v", err)
	}

	err = db.RegisterService(t.Context(), "web-api", "/new/path", "new.yaml")
	if err != nil {
		t.Fatalf("Second RegisterService failed: %v", err)
	}

	entry, err := db.GetServiceCatalogEntry(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry failed: %v", err)
	}

	if entry.DirectoryPath != "/new/path" {
		t.Errorf("expected upserted path '/new/path', got '%s'", entry.DirectoryPath)
	}
	if entry.ConfigFileName != "new.yaml" {
		t.Errorf("expected upserted config 'new.yaml', got '%s'", entry.ConfigFileName)
	}
}

func TestGetServiceCatalogEntry_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	_, err := db.GetServiceCatalogEntry(t.Context(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

func TestGetAllServiceCatalogEntries(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterService(t.Context(), "alpha", "/path/alpha", "a.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}
	err = db.RegisterService(t.Context(), "beta", "/path/beta", "b.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}
	err = db.RegisterService(t.Context(), "gamma", "/path/gamma", "c.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	entries, err := db.GetAllServiceCatalogEntries(t.Context())
	if err != nil {
		t.Fatalf("GetAllServiceCatalogEntries failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Should be ordered by name
	if entries[0].Name != "alpha" || entries[1].Name != "beta" || entries[2].Name != "gamma" {
		t.Errorf("expected alphabetical order, got: %s, %s, %s",
			entries[0].Name, entries[1].Name, entries[2].Name)
	}
}

func TestGetAllServiceCatalogEntries_Empty(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	entries, err := db.GetAllServiceCatalogEntries(t.Context())
	if err != nil {
		t.Fatalf("GetAllServiceCatalogEntries failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestIsServiceRegistered(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	registered, err := db.IsServiceRegistered(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("IsServiceRegistered failed: %v", err)
	}
	if registered {
		t.Error("expected false before registration")
	}

	err = db.RegisterService(t.Context(), "web-api", "/path", "config.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	registered, err = db.IsServiceRegistered(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("IsServiceRegistered failed: %v", err)
	}
	if !registered {
		t.Error("expected true after registration")
	}
}

// TestFindServiceNameCaseInsensitive covers the query behind issue #10's
// case-collision guard: names differing only in letter case must be reported
// as a collision (with the stored spelling returned), while an unrelated name
// must not match.
func TestFindServiceNameCaseInsensitive(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	if err := db.RegisterService(t.Context(), "Foo", "/path", "config.yaml"); err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	existing, found, err := db.FindServiceNameCaseInsensitive(t.Context(), "foo")
	if err != nil {
		t.Fatalf("FindServiceNameCaseInsensitive failed: %v", err)
	}
	if !found {
		t.Fatal("expected 'foo' to collide with registered 'Foo'")
	}
	if existing != "Foo" {
		t.Errorf("expected stored name 'Foo', got %q", existing)
	}

	_, found, err = db.FindServiceNameCaseInsensitive(t.Context(), "bar")
	if err != nil {
		t.Fatalf("FindServiceNameCaseInsensitive failed: %v", err)
	}
	if found {
		t.Error("expected no collision for unrelated name 'bar'")
	}
}

func TestUpdateServiceCatalogEntry(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterService(t.Context(), "web-api", "/old/path", "old.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	err = db.UpdateServiceCatalogEntry(t.Context(), "web-api", "/new/path", "new.yaml")
	if err != nil {
		t.Fatalf("UpdateServiceCatalogEntry failed: %v", err)
	}

	entry, err := db.GetServiceCatalogEntry(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry failed: %v", err)
	}

	if entry.DirectoryPath != "/new/path" {
		t.Errorf("expected '/new/path', got '%s'", entry.DirectoryPath)
	}
	if entry.ConfigFileName != "new.yaml" {
		t.Errorf("expected 'new.yaml', got '%s'", entry.ConfigFileName)
	}
}

func TestUpdateServiceCatalogEntry_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.UpdateServiceCatalogEntry(t.Context(), "ghost", "/path", "config.yaml")
	if err == nil {
		t.Fatal("expected error when updating nonexistent entry")
	}
}

func TestRemoveServiceCatalogEntry(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterService(t.Context(), "web-api", "/path", "config.yaml")
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	removed, err := db.RemoveServiceCatalogEntry(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RemoveServiceCatalogEntry failed: %v", err)
	}
	if !removed {
		t.Error("expected removal to succeed")
	}

	registered, _ := db.IsServiceRegistered(t.Context(), "web-api")
	if registered {
		t.Error("service should not exist after removal")
	}
}

func TestRemoveServiceCatalogEntry_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	removed, err := db.RemoveServiceCatalogEntry(t.Context(), "ghost")
	if err != nil {
		t.Fatalf("RemoveServiceCatalogEntry failed: %v", err)
	}
	if removed {
		t.Error("expected false when removing nonexistent entry")
	}
}

func TestRegisterServiceInstance(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	instance, err := db.GetServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("GetServiceInstance failed: %v", err)
	}

	if instance.Name != "web-api" {
		t.Errorf("expected name 'web-api', got '%s'", instance.Name)
	}
	if instance.RestartCount != 0 {
		t.Errorf("expected restart count 0, got %d", instance.RestartCount)
	}
	if instance.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGetServiceInstance_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	_, err := db.GetServiceInstance(t.Context(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent instance")
	}
}

func TestUpdateServiceInstance(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "cms")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	updates := database.ServiceInstanceUpdate{
		RestartCount: new(5),
		StartedAt:    &now,
	}
	err = db.UpdateServiceInstance(t.Context(), "cms", updates)
	if err != nil {
		t.Fatalf("UpdateServiceInstance failed: %v", err)
	}

	instance, err := db.GetServiceInstance(t.Context(), "cms")
	if err != nil {
		t.Fatalf("GetServiceInstance failed: %v", err)
	}

	if instance.RestartCount != 5 {
		t.Errorf("expected restart count 5, got %d", instance.RestartCount)
	}
}

func TestUpdateServiceInstance_NextRestartAt(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "cms")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	instance, err := db.GetServiceInstance(t.Context(), "cms")
	if err != nil {
		t.Fatalf("GetServiceInstance failed: %v", err)
	}
	if instance.NextRestartAt != nil {
		t.Fatalf("expected nil next_restart_at before it is scheduled, got %v", instance.NextRestartAt)
	}

	next := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	err = db.UpdateServiceInstance(t.Context(), "cms", database.ServiceInstanceUpdate{NextRestartAt: &next})
	if err != nil {
		t.Fatalf("UpdateServiceInstance failed: %v", err)
	}

	instance, err = db.GetServiceInstance(t.Context(), "cms")
	if err != nil {
		t.Fatalf("GetServiceInstance failed: %v", err)
	}
	if instance.NextRestartAt == nil || !instance.NextRestartAt.Equal(next) {
		t.Errorf("expected next_restart_at %v, got %v", next, instance.NextRestartAt)
	}
}

func TestUpdateServiceInstance_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.UpdateServiceInstance(t.Context(), "ghost", database.ServiceInstanceUpdate{
		RestartCount: new(1),
	})
	if err == nil {
		t.Fatal("expected error when updating nonexistent instance")
	}
}

func TestUpdateServiceInstance_NoFields(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "cms")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	err = db.UpdateServiceInstance(t.Context(), "cms", database.ServiceInstanceUpdate{})
	if err == nil {
		t.Fatal("expected error when no fields provided")
	}
}

func TestRemoveServiceInstance(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	removed, err := db.RemoveServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RemoveServiceInstance failed: %v", err)
	}
	if !removed {
		t.Error("expected removal to succeed")
	}

	_, err = db.GetServiceInstance(t.Context(), "web-api")
	if err == nil {
		t.Error("instance should not exist after removal")
	}
}

func TestRemoveServiceInstance_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	removed, err := db.RemoveServiceInstance(t.Context(), "ghost")
	if err != nil {
		t.Fatalf("RemoveServiceInstance failed: %v", err)
	}
	if removed {
		t.Error("expected false when removing nonexistent instance")
	}
}

func TestRegisterProcessHistoryEntry(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	entry, err := db.RegisterProcessHistoryEntry(t.Context(), 1234, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	if entry.PGID != 1234 {
		t.Errorf("expected PGID 1234, got %d", entry.PGID)
	}
	if entry.ServiceName != "web-api" {
		t.Errorf("expected service name 'web-api', got '%s'", entry.ServiceName)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

// TestProcessHistoryStartedAtHasNoMonotonicSuffix guards issue #144: time.Now()
// carries a monotonic-clock reading, and the sqlite driver used to persist it
// via time.Time.String(), appending " m=+0.000...". That is not a valid SQLite
// datetime, so datetime(started_at) returned NULL. Both the INSERT and UPDATE
// write paths must store a clean, SQLite-parseable timestamp.
func TestProcessHistoryStartedAtHasNoMonotonicSuffix(t *testing.T) {
	db, conn, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	if err := db.RegisterServiceInstance(t.Context(), "web-api"); err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 1234, 0, "web-api", types.ProcessStateStarting); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	// Rewrite started_at through the UPDATE path too, covering both writers.
	newStart := time.Now()
	if err := db.UpdateProcessHistoryEntry(t.Context(), 1234, database.ProcessHistoryUpdate{StartedAt: &newStart}); err != nil {
		t.Fatalf("UpdateProcessHistoryEntry failed: %v", err)
	}

	// CAST to TEXT to read the raw stored bytes, bypassing the driver's
	// DATETIME auto-parsing (which would hide the suffix on read).
	var raw string
	if err := conn.QueryRowContext(t.Context(),
		`SELECT CAST(started_at AS TEXT) FROM process_history WHERE pgid = ?`, 1234,
	).Scan(&raw); err != nil {
		t.Fatalf("read raw started_at: %v", err)
	}
	if strings.Contains(raw, "m=") {
		t.Errorf("stored started_at has monotonic suffix: %q", raw)
	}

	// SQLite must be able to parse the stored value as a datetime.
	var parsed *string
	if err := conn.QueryRowContext(t.Context(),
		`SELECT datetime(started_at) FROM process_history WHERE pgid = ?`, 1234,
	).Scan(&parsed); err != nil {
		t.Fatalf("datetime(started_at): %v", err)
	}
	if parsed == nil {
		t.Errorf("datetime(started_at) returned NULL for stored value %q", raw)
	}

	// Existing readers still round-trip started_at into a time.Time.
	entry, err := db.GetProcessHistoryEntryByPGID(t.Context(), 1234)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPGID failed: %v", err)
	}
	if entry.StartedAt == nil || entry.StartedAt.IsZero() {
		t.Errorf("expected StartedAt to round-trip, got %v", entry.StartedAt)
	}
}

func TestGetProcessHistoryEntryByPGID(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	_, err = db.RegisterProcessHistoryEntry(t.Context(), 1234, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPGID(t.Context(), 1234)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPGID failed: %v", err)
	}

	if entry.PGID != 1234 {
		t.Errorf("expected PGID 1234, got %d", entry.PGID)
	}
	if entry.ServiceName != "web-api" {
		t.Errorf("expected 'web-api', got '%s'", entry.ServiceName)
	}
	if entry.State != types.ProcessStateStarting {
		t.Errorf("expected state Starting, got '%s'", entry.State)
	}
}

func TestGetProcessHistoryEntryByPGID_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	_, err := db.GetProcessHistoryEntryByPGID(t.Context(), 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent PGID")
	}
}

func TestGetProcessHistoryEntriesByServiceName(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 100, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 200, 0, "web-api", types.ProcessStateRunning)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	err = db.RegisterServiceInstance(t.Context(), "worker")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 300, 0, "worker", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	entries, err := db.GetProcessHistoryEntriesByServiceName(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("GetProcessHistoryEntriesByServiceName failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for web-api, got %d", len(entries))
	}

	// Ordered by PGID
	if entries[0].PGID != 100 || entries[1].PGID != 200 {
		t.Errorf("expected PGIDs [100, 200], got [%d, %d]", entries[0].PGID, entries[1].PGID)
	}
}

func TestGetProcessHistoryEntriesByServiceName_Empty(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	entries, err := db.GetProcessHistoryEntriesByServiceName(t.Context(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestUpdateProcessHistoryEntry_RoundTrip guards against a regression where
// stopped_at was added to a SELECT query without a matching field in the
// Scan call, silently shifting every column after it (fixed in 6dbf0e4).
func TestUpdateProcessHistoryEntry_RoundTrip(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 42, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	startedAt := now.Add(-10 * time.Second)
	stoppedAt := now.Add(-2 * time.Second)
	errorMsg := "connection refused"

	err = db.UpdateProcessHistoryEntry(t.Context(), 42, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StartedAt: &startedAt,
		StoppedAt: &stoppedAt,
		Error:     &errorMsg,
	})
	if err != nil {
		t.Fatalf("UpdateProcessHistoryEntry failed: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPGID(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPGID failed: %v", err)
	}

	// Every field should survive the round trip
	if entry.PGID != 42 {
		t.Errorf("PGID: expected 42, got %d", entry.PGID)
	}
	if entry.ServiceName != "web-api" {
		t.Errorf("ServiceName: expected 'web-api', got '%s'", entry.ServiceName)
	}
	if entry.State != types.ProcessStateFailed {
		t.Errorf("State: expected Failed, got '%s'", entry.State)
	}
	if entry.Error == nil || *entry.Error != errorMsg {
		t.Errorf("Error: expected '%s', got %v", errorMsg, entry.Error)
	}
	if entry.StartedAt == nil {
		t.Error("StartedAt: expected non-nil")
	} else if !entry.StartedAt.Truncate(time.Second).Equal(startedAt) {
		t.Errorf("StartedAt: expected %v, got %v", startedAt, *entry.StartedAt)
	}
	if entry.StoppedAt == nil {
		t.Error("StoppedAt: expected non-nil")
	} else if !entry.StoppedAt.Truncate(time.Second).Equal(stoppedAt) {
		t.Errorf("StoppedAt: expected %v, got %v", stoppedAt, *entry.StoppedAt)
	}
	if entry.UpdatedAt == nil {
		t.Error("UpdatedAt: expected non-nil")
	}
	if entry.CreatedAt.IsZero() {
		t.Error("CreatedAt: expected non-zero")
	}
}

func TestUpdateProcessHistoryEntry_PartialUpdate(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 50, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	// Update only state - other nullable fields should remain nil
	err = db.UpdateProcessHistoryEntry(t.Context(), 50, database.ProcessHistoryUpdate{
		State: new(types.ProcessStateRunning),
	})
	if err != nil {
		t.Fatalf("UpdateProcessHistoryEntry failed: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPGID(t.Context(), 50)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPGID failed: %v", err)
	}

	if entry.State != types.ProcessStateRunning {
		t.Errorf("State: expected Running, got '%s'", entry.State)
	}
	// StoppedAt was never set, should still be nil
	if entry.StoppedAt != nil {
		t.Errorf("StoppedAt: expected nil for partial update, got %v", entry.StoppedAt)
	}
}

func TestUpdateProcessHistoryEntry_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.UpdateProcessHistoryEntry(t.Context(), 99999, database.ProcessHistoryUpdate{
		State: new(types.ProcessStateFailed),
	})
	if err == nil {
		t.Fatal("expected error when updating nonexistent PGID")
	}
}

func TestUpdateProcessHistoryEntry_NoFields(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	_, err = db.RegisterProcessHistoryEntry(t.Context(), 60, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	err = db.UpdateProcessHistoryEntry(t.Context(), 60, database.ProcessHistoryUpdate{})
	if err == nil {
		t.Fatal("expected error when no fields provided")
	}
}

// TestUpdateProcessHistoryEntry_ConcurrentWrites verifies concurrent writers
// don't hit SQLITE_BUSY; the connection is opened with WAL mode and a
// busy_timeout, so writers should queue instead of failing.
func TestUpdateProcessHistoryEntry_ConcurrentWrites(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("registering test service, got: %v", err)
	}

	const goroutines = 20
	for i := range goroutines {
		pgid := 1000 + i
		_, _ = db.RegisterProcessHistoryEntry(t.Context(), pgid, 0, "web-api", types.ProcessStateStarting)
	}

	var wg sync.WaitGroup
	errs := make([]error, 20)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = db.UpdateProcessHistoryEntry(t.Context(), 1000+idx, database.ProcessHistoryUpdate{State: new(types.ProcessStateStopped)})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}

}

func TestRemoveProcessHistoryEntryViaPGID(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 1234, 0, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	removed, err := db.RemoveProcessHistoryEntryViaPGID(t.Context(), 1234)
	if err != nil {
		t.Fatalf("RemoveProcessHistoryEntryViaPGID failed: %v", err)
	}
	if !removed {
		t.Error("expected removal to succeed")
	}

	_, err = db.GetProcessHistoryEntryByPGID(t.Context(), 1234)
	if err == nil {
		t.Error("entry should not exist after removal")
	}
}

func TestRemoveProcessHistoryEntryViaPGID_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	removed, err := db.RemoveProcessHistoryEntryViaPGID(t.Context(), 99999)
	if err != nil {
		t.Fatalf("RemoveProcessHistoryEntryViaPGID failed: %v", err)
	}
	if removed {
		t.Error("expected false when removing nonexistent PGID")
	}
}
