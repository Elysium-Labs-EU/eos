package database_test

import (
	"eos/internal/database"
	"eos/internal/testutil"
	"eos/internal/types"
	"errors"
	"testing"
	"time"
)

func TestServiceCatalogCRUD(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	serviceName := "integration-test-service"
	servicePath := "/path/to/service"
	configFile := "service.yaml"

	// Create
	err := db.RegisterService(serviceName, servicePath, configFile)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Read
	entry, err := db.GetServiceCatalogEntry(serviceName)
	if err != nil {
		t.Fatalf("Failed to get service catalog entry: %v", err)
	}
	if entry.Name != serviceName {
		t.Errorf("Expected name %s, got %s", serviceName, entry.Name)
	}
	if entry.DirectoryPath != servicePath {
		t.Errorf("Expected path %s, got %s", servicePath, entry.DirectoryPath)
	}
	if entry.ConfigFileName != configFile {
		t.Errorf("Expected config file %s, got %s", configFile, entry.ConfigFileName)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	// Update
	newPath := "/new/path"
	newConfig := "new-config.yaml"
	err = db.UpdateServiceCatalogEntry(serviceName, newPath, newConfig)
	if err != nil {
		t.Fatalf("Failed to update service catalog entry: %v", err)
	}

	// Verify update
	entry, err = db.GetServiceCatalogEntry(serviceName)
	if err != nil {
		t.Fatalf("Failed to get updated service catalog entry: %v", err)
	}
	if entry.DirectoryPath != newPath {
		t.Errorf("Expected updated path %s, got %s", newPath, entry.DirectoryPath)
	}
	if entry.ConfigFileName != newConfig {
		t.Errorf("Expected updated config file %s, got %s", newConfig, entry.ConfigFileName)
	}

	// List all services
	allServices, err := db.GetAllServiceCatalogEntries()
	if err != nil {
		t.Fatalf("Failed to get all services: %v", err)
	}
	found := false
	for _, svc := range allServices {
		if svc.Name == serviceName {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected service to appear in list of all services")
	}

	// Delete
	deleted, err := db.RemoveServiceCatalogEntry(serviceName)
	if err != nil {
		t.Fatalf("Failed to remove service catalog entry: %v", err)
	}
	if !deleted {
		t.Error("Expected service to be deleted")
	}

	// Verify deletion
	_, err = db.GetServiceCatalogEntry(serviceName)
	if err == nil {
		t.Error("Expected error when getting deleted service, got nil")
	}
	_, err = db.GetServiceCatalogEntry(serviceName)
	if err == nil {
		t.Error("Expected error when getting deleted service, got nil")
	}
	if !errors.Is(err, database.ErrServiceNotFound) {
		t.Errorf("Expected ErrServiceNotFound, got: %v", err)
	}

	// Try to delete again - should return false
	deleted, err = db.RemoveServiceCatalogEntry(serviceName)
	if err != nil {
		t.Fatalf("Failed on second delete attempt: %v", err)
	}
	if deleted {
		t.Error("Expected second delete to return false")
	}
}

func TestServiceInstanceCRUD(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	serviceName := "integration-instance-test"

	// Create
	err := db.RegisterServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	// Read
	instance, err := db.GetServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	if instance.Name != serviceName {
		t.Errorf("Expected name %s, got %s", serviceName, instance.Name)
	}
	if instance.RestartCount != 0 {
		t.Errorf("Expected restart count 0, got %d", instance.RestartCount)
	}
	if instance.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	// Delete
	deleted, err := db.RemoveServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to remove service instance: %v", err)
	}
	if !deleted {
		t.Error("Expected service instance to be deleted")
	}

	// Verify deletion
	_, err = db.GetServiceInstance(serviceName)
	if err == nil {
		t.Error("Expected error when getting deleted service instance, got nil")
	}
}

func TestProcessHistoryCRUD(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	// First create a service instance (required for FK constraint)
	serviceName := "process-history-test-service"
	err := db.RegisterServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	pid := 12345
	state := types.ProcessStateRunning

	// Create
	entry, err := db.RegisterProcessHistoryEntry(pid, serviceName, state)
	if err != nil {
		t.Fatalf("Failed to register process history entry: %v", err)
	}
	if entry.PID != pid {
		t.Errorf("Expected PID %d, got %d", pid, entry.PID)
	}
	if entry.ServiceName != serviceName {
		t.Errorf("Expected service name %s, got %s", serviceName, entry.ServiceName)
	}

	// Read
	retrieved, err := db.GetProcessHistoryEntryByPid(pid)
	if err != nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if retrieved.PID != pid {
		t.Errorf("Expected PID %d, got %d", pid, retrieved.PID)
	}
	if retrieved.ServiceName != serviceName {
		t.Errorf("Expected service name %s, got %s", serviceName, retrieved.ServiceName)
	}

	// Delete
	deleted, err := db.RemoveProcessHistoryEntryViaPid(pid)
	if err != nil {
		t.Fatalf("Failed to remove process history entry: %v", err)
	}
	if !deleted {
		t.Error("Expected process history entry to be deleted")
	}

	// Verify deletion
	_, err = db.GetProcessHistoryEntryByPid(pid)
	if err == nil {
		t.Error("Expected error when getting deleted process history entry, got nil")
	}

	t.Cleanup(func() {
		removed, err := db.RemoveServiceInstance(serviceName)
		if err != nil {
			t.Fatalf("Failed to remove service instance: %v", err)
		}
		if !removed {
			t.Fatalf("Failed to remove service instance due to an unknown reason")
		}
	})
}

func TestIsServiceRegistered_integration(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	serviceName := "registration-check-test"

	// Should not be registered initially
	isRegistered, err := db.IsServiceRegistered(serviceName)
	if err != nil {
		t.Fatalf("Failed to check if service is registered: %v", err)
	}
	if isRegistered {
		t.Error("Expected service to not be registered initially")
	}

	// Register service
	err = db.RegisterService(serviceName, "/path", "config.yaml")
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Should be registered now
	isRegistered, err = db.IsServiceRegistered(serviceName)
	if err != nil {
		t.Fatalf("Failed to check if service is registered: %v", err)
	}
	if !isRegistered {
		t.Error("Expected service to be registered")
	}

	t.Cleanup(func() {
		removed, err := db.RemoveServiceCatalogEntry(serviceName)
		if err != nil {
			t.Errorf("Failed to remove service catalog entry during cleanup, got: %v\n", err)
		}
		if !removed {
			t.Errorf("Failed to remove service catalog entry during cleanup")
		}
	})
}

func TestServiceInstanceUpdates(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	serviceName := "update-test-instance"

	// Create instance
	err := db.RegisterServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	// Update restart count
	newRestartCount := 5
	err = db.UpdateServiceInstance(serviceName, database.ServiceInstanceUpdate{
		RestartCount: &newRestartCount,
	})
	if err != nil {
		t.Fatalf("Failed to update restart count: %v", err)
	}

	// Verify update
	instance, err := db.GetServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	if instance.RestartCount != newRestartCount {
		t.Errorf("Expected restart count %d, got %d", newRestartCount, instance.RestartCount)
	}

	// Update last health check
	now := time.Now()
	err = db.UpdateServiceInstance(serviceName, database.ServiceInstanceUpdate{
		LastHealthCheck: &now,
	})
	if err != nil {
		t.Fatalf("Failed to update last health check: %v", err)
	}

	// Verify update
	instance, err = db.GetServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	if instance.LastHealthCheck == nil {
		t.Error("Expected LastHealthCheck to be set")
	}

	// Update started at
	startTime := time.Now()
	err = db.UpdateServiceInstance(serviceName, database.ServiceInstanceUpdate{
		StartedAt: &startTime,
	})
	if err != nil {
		t.Fatalf("Failed to update started_at: %v", err)
	}

	// Verify update
	instance, err = db.GetServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	if instance.StartedAt == nil {
		t.Error("Expected StartedAt to be set")
	}

	// Update multiple fields at once
	newCount := 10
	newHealthCheck := time.Now()
	err = db.UpdateServiceInstance(serviceName, database.ServiceInstanceUpdate{
		RestartCount:    &newCount,
		LastHealthCheck: &newHealthCheck,
	})
	if err != nil {
		t.Fatalf("Failed to update multiple fields: %v", err)
	}

	// Verify both updates
	instance, err = db.GetServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	if instance.RestartCount != newCount {
		t.Errorf("Expected restart count %d, got %d", newCount, instance.RestartCount)
	}
	if instance.LastHealthCheck == nil {
		t.Error("Expected LastHealthCheck to be set after multi-field update")
	}

	t.Cleanup(func() {
		removed, err := db.RemoveServiceInstance(serviceName)
		if err != nil {
			t.Errorf("Failed to remove service instance during cleanup, got: %v\n", err)
		}
		if !removed {
			t.Errorf("Failed to remove service instance during cleanup")
		}
	})
}

func TestProcessHistoryUpdates(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	serviceName := "process-update-test"

	err := db.RegisterServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	pid := 54321
	initialState := types.ProcessStateStarting

	_, err = db.RegisterProcessHistoryEntry(pid, serviceName, initialState)
	if err != nil {
		t.Fatalf("Failed to register process history entry: %v", err)
	}

	newState := types.ProcessStateRunning
	err = db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
		State: &newState,
	})
	if err != nil {
		t.Fatalf("Failed to update state: %v", err)
	}

	entry, err := db.GetProcessHistoryEntryByPid(pid)
	if err != nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if entry.State != newState {
		t.Errorf("Expected state %v, got %v", newState, entry.State)
	}

	// Update started_at
	startTime := time.Now()
	err = db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
		StartedAt: &startTime,
	})
	if err != nil {
		t.Fatalf("Failed to update started_at: %v", err)
	}

	// Verify update
	entry, err = db.GetProcessHistoryEntryByPid(pid)
	if err != nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if entry.StartedAt == nil {
		t.Error("Expected StartedAt to be set")
	}

	errorMsg := "test error message"
	err = db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
		Error: &errorMsg,
	})
	if err != nil {
		t.Fatalf("Failed to update error: %v", err)
	}

	entry, err = db.GetProcessHistoryEntryByPid(pid)
	if err != nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if entry.Error == nil || *entry.Error != errorMsg {
		t.Errorf("Expected error '%s', got '%v'", errorMsg, entry.Error)
	}

	stopTime := time.Now()
	stoppedState := types.ProcessStateStopped
	err = db.UpdateProcessHistoryEntry(pid, database.ProcessHistoryUpdate{
		StoppedAt: &stopTime,
		State:     &stoppedState,
	})
	if err != nil {
		t.Fatalf("Failed to update stopped_at: %v", err)
	}

	entry, err = db.GetProcessHistoryEntryByPid(pid)
	if err != nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if entry.StoppedAt == nil {
		t.Error("Expected StoppedAt to be set")
	}
	if entry.State != stoppedState {
		t.Errorf("Expected state %v, got %v", stoppedState, entry.State)
	}

	t.Cleanup(func() {
		processRemoved, err := db.RemoveProcessHistoryEntryViaPid(pid)
		if err != nil {
			t.Fatalf("Failed to removed process history entry in cleanup, got: %v", err)
		}
		if !processRemoved {
			t.Fatal("Failed to removed process history entry in cleanup")
		}
		instanceRemoved, err := db.RemoveServiceInstance(serviceName)
		if err != nil {
			t.Fatalf("Failed to removed service instance in cleanup, got: %v", err)
		}
		if !instanceRemoved {
			t.Fatal("Failed to removed service instance in cleanup")
		}
	})
}

func TestProcessHistoryQueryByService(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	serviceName := "multi-process-test"

	// Create service instance
	err := db.RegisterServiceInstance(serviceName)
	if err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	// Create multiple process history entries
	pids := []int{11111, 22222, 33333}
	for _, pid := range pids {
		_, err := db.RegisterProcessHistoryEntry(pid, serviceName, types.ProcessStateRunning)
		if err != nil {
			t.Fatalf("Failed to register process %d: %v", pid, err)
		}
	}

	// Query all processes for this service
	entries, err := db.GetProcessHistoryEntriesByServiceName(serviceName)
	if err != nil {
		t.Fatalf("Failed to get process history entries: %v", err)
	}

	if len(entries) != len(pids) {
		t.Errorf("Expected %d entries, got %d", len(pids), len(entries))
	}

	// Verify all PIDs are present
	foundPIDs := make(map[int]bool)
	for _, entry := range entries {
		foundPIDs[entry.PID] = true
		if entry.ServiceName != serviceName {
			t.Errorf("Expected service name %s, got %s", serviceName, entry.ServiceName)
		}
	}

	for _, pid := range pids {
		if !foundPIDs[pid] {
			t.Errorf("Expected to find PID %d in results", pid)
		}
	}

	t.Cleanup(func() {
		for _, pid := range pids {
			removed, err := db.RemoveProcessHistoryEntryViaPid(pid)
			if err != nil {
				t.Logf("Failed to remove process history entry in cleanup, got: %v", err)
				continue
			}
			if !removed {
				t.Logf("Failed to remove process history entry in cleanup")
				continue
			}
		}
		removed, err := db.RemoveServiceInstance(serviceName)
		if err != nil {
			t.Fatalf("Failed to remove service instance in cleanup, got: %v", err)
		}
		if !removed {
			t.Fatalf("Failed to remove process history entry in cleanup")
		}
	})
}
