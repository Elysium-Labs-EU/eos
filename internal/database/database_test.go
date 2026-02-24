package database_test

import (
	"testing"
	"time"

	"eos/internal/database"
	"eos/internal/ptr"
	"eos/internal/testutil"
	"eos/internal/types"
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
		RestartCount: ptr.IntPtr(5),
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

func TestUpdateServiceInstance_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.UpdateServiceInstance(t.Context(), "ghost", database.ServiceInstanceUpdate{
		RestartCount: ptr.IntPtr(1),
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

	entry, err := db.RegisterProcessHistoryEntry(t.Context(), 1234, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	if entry.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", entry.PID)
	}
	if entry.ServiceName != "web-api" {
		t.Errorf("expected service name 'web-api', got '%s'", entry.ServiceName)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGetProcessHistoryEntryByPid(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	_, err = db.RegisterProcessHistoryEntry(t.Context(), 1234, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPid(t.Context(), 1234)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPid failed: %v", err)
	}

	if entry.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", entry.PID)
	}
	if entry.ServiceName != "web-api" {
		t.Errorf("expected 'web-api', got '%s'", entry.ServiceName)
	}
	if entry.State != types.ProcessStateStarting {
		t.Errorf("expected state Starting, got '%s'", entry.State)
	}
}

func TestGetProcessHistoryEntryByPid_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	_, err := db.GetProcessHistoryEntryByPid(t.Context(), 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent PID")
	}
}

func TestGetProcessHistoryEntriesByServiceName(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 100, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 200, "web-api", types.ProcessStateRunning)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	err = db.RegisterServiceInstance(t.Context(), "worker")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 300, "worker", types.ProcessStateStarting)
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

	// Ordered by PID
	if entries[0].PID != 100 || entries[1].PID != 200 {
		t.Errorf("expected PIDs [100, 200], got [%d, %d]", entries[0].PID, entries[1].PID)
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

func TestUpdateProcessHistoryEntry_RoundTrip(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 42, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	startedAt := now.Add(-10 * time.Second)
	stoppedAt := now.Add(-2 * time.Second)
	errorMsg := "connection refused"

	err = db.UpdateProcessHistoryEntry(t.Context(), 42, database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
		StartedAt: &startedAt,
		StoppedAt: &stoppedAt,
		Error:     &errorMsg,
	})
	if err != nil {
		t.Fatalf("UpdateProcessHistoryEntry failed: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPid(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPid failed: %v", err)
	}

	// Every field should survive the round trip
	if entry.PID != 42 {
		t.Errorf("PID: expected 42, got %d", entry.PID)
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
		t.Error("StoppedAt: expected non-nil — this catches the column/scan mismatch bug")
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
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 50, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	// Update only state — other nullable fields should remain nil
	err = db.UpdateProcessHistoryEntry(t.Context(), 50, database.ProcessHistoryUpdate{
		State: ptr.ProcessStatePtr(types.ProcessStateRunning),
	})
	if err != nil {
		t.Fatalf("UpdateProcessHistoryEntry failed: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPid(t.Context(), 50)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPid failed: %v", err)
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
		State: ptr.ProcessStatePtr(types.ProcessStateFailed),
	})
	if err == nil {
		t.Fatal("expected error when updating nonexistent PID")
	}
}

func TestUpdateProcessHistoryEntry_NoFields(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}

	_, err = db.RegisterProcessHistoryEntry(t.Context(), 60, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	err = db.UpdateProcessHistoryEntry(t.Context(), 60, database.ProcessHistoryUpdate{})
	if err == nil {
		t.Fatal("expected error when no fields provided")
	}
}

func TestRemoveProcessHistoryEntryViaPid(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	err := db.RegisterServiceInstance(t.Context(), "web-api")
	if err != nil {
		t.Fatalf("RegisterServiceInstance failed: %v", err)
	}
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 1234, "web-api", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	removed, err := db.RemoveProcessHistoryEntryViaPid(t.Context(), 1234)
	if err != nil {
		t.Fatalf("RemoveProcessHistoryEntryViaPid failed: %v", err)
	}
	if !removed {
		t.Error("expected removal to succeed")
	}

	_, err = db.GetProcessHistoryEntryByPid(t.Context(), 1234)
	if err == nil {
		t.Error("entry should not exist after removal")
	}
}

func TestRemoveProcessHistoryEntryViaPid_NotFound(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	removed, err := db.RemoveProcessHistoryEntryViaPid(t.Context(), 99999)
	if err != nil {
		t.Fatalf("RemoveProcessHistoryEntryViaPid failed: %v", err)
	}
	if removed {
		t.Error("expected false when removing nonexistent PID")
	}
}
