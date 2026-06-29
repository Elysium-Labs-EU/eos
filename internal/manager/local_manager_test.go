package manager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// fakeExecutor satisfies Executor without requiring runtime binaries in PATH.
// LookPath always succeeds; CommandContext delegates to the real os/exec.
type fakeExecutor struct{}

func (fakeExecutor) LookPath(file string) (string, error) {
	return file, nil
}

func (fakeExecutor) CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, arg...)
}

func TestNewManager(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}
	services, err := manager.GetAllServiceCatalogEntries()
	if err != nil {
		t.Errorf("GetAllRegisteredServices shouldn't error, got: %v\n", err)
	}
	if len(services) != 0 {
		t.Errorf("Expected 0 services, got %d", len(services))
	}

}

func TestAddService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", "./test-files", "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)

	if err != nil {
		t.Fatalf("Adding service catalog entry should not error: %v", err)

	}
	services, err := manager.GetAllServiceCatalogEntries()
	if err != nil {
		t.Fatalf("Getting all service catalog entries should not error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("Expected 1 service catalog entry, got %d", len(services))
	}
	if services[0].Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", services[0].Name)
	}
}

func TestAddServiceMultipleTimes(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", "./test-files", "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)

	if err != nil {
		t.Fatalf("Adding service should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err == nil {
		t.Fatalf("Expected error on adding the same service catalog entry twice")
	}
	if strings.Contains(err.Error(), "service name cannot be empty") {
		t.Errorf("Test failed due to invalid test input, got: %v\n", err)
	}

}

func TestGetService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", "./test-files", "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	found, error := manager.GetServiceCatalogEntry("test-service")
	if error != nil {
		t.Errorf("Service should exist")
	}
	if found.Name != "test-service" {
		t.Errorf("Expected service name 'test-service', got %s", found.Name)
	}
}

func TestGetInvalidServiceInstance(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceInstance, err := manager.GetServiceInstance("non-existent")

	if serviceInstance != nil {
		t.Error("Non-existent service should not exist")
	}
	if !errors.Is(err, ErrServiceNotRunning) {
		t.Error("Non-existent service should throw an error")
	}
}

func TestStartService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "./start-script.sh",
		Port:    1337,
		Runtime: types.Runtime{
			Type: "nodejs",
		},
	}

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-files")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-files directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	pgid, err := manager.StartService("test-service")

	if err != nil {
		t.Fatalf("Starting service should not error: %v\n", err)
	}
	if pgid == 0 {
		t.Fatalf("Starting service should have a failed PGID, got: %v\n", err)
	}
}

func TestStartServiceWithValidEnvLocation(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "./start-script.sh",
		Port:    1337,
		Runtime: types.Runtime{
			Type: "nodejs",
		},
		EnvFile: ".env",
	}

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-files")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-files directory: %v\n", err)
		return
	}

	err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	err = os.WriteFile(filepath.Join(fullDirPath, ".env"), nil, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the env file, got: %v\n", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	if _, err := manager.StartService("test-service"); err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}
}

func TestStartServiceWithInvalidEnvLocation(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "./start-script.sh",
		Port:    1337,
		Runtime: types.Runtime{
			Type: "nodejs",
		},
		EnvFile: "../../test/../../dummy",
	}

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-files")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-files directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	if _, err := manager.StartService("test-service"); err == nil {
		t.Fatal("Starting service should error")
	}
}

// func TestLocalManager_RemoveServiceInstance(t *testing.T) {}
// func TestLocalManager_RemoveServiceCatalogEntry(t *testing.T) {}
// func TestLocalManager_IsServiceRegistered(t *testing.T) {}
func TestLocalManager_GetMostRecentProcessHistoryEntry_NilStartedAt(t *testing.T) {
	db, rawDB, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceName := "nil-started-at-svc"
	if err := db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}

	// Register a first entry — started_at will be set by the INSERT.
	_, err := db.RegisterProcessHistoryEntry(t.Context(), 1001, serviceName, types.ProcessStateFailed)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry first: %v", err)
	}

	// Force started_at to NULL on the first entry to simulate pre-fix DB state.
	if _, execErr := rawDB.ExecContext(t.Context(),
		`UPDATE process_history SET started_at = NULL WHERE pgid = ?`, 1001,
	); execErr != nil {
		t.Fatalf("nullify started_at: %v", execErr)
	}

	// Register a second (newer) entry — this is the one we expect to get back.
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 1002, serviceName, types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry second: %v", err)
	}

	// Must not panic, and must return the newer entry.
	entry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	if entry.PGID != 1002 {
		t.Errorf("expected PGID 1002 (newer), got %d", entry.PGID)
	}
}

// func TestLocalManager_GetMostRecentProcessHistoryEntry(t *testing.T) {}
// func TestLocalManager_UpdateServiceCatalogEntry(t *testing.T) {}
// func TestLocalManager_RestartService(t *testing.T) {}
// func TestLocalManager_StopService(t *testing.T) {}
// func TestIsProcessAlive(t *testing.T) {}
// func TestLocalManager_ForceStopService(t *testing.T) {}
// func TestUpdateProcessHistoryEntriesAsStopped(t *testing.T) {}
// func TestUpdateProcessHistoryEntriesAsUnknown(t *testing.T) {}
// func TestStopServiceWithSignal(t *testing.T) {}
// func TestValidateRuntimePath(t *testing.T) {}
// func TestLocalManager_GetServiceLogFilePath(t *testing.T) {}
// func TestLocalManager_LogToServiceStdout(t *testing.T) {}
// func TestLocalManager_LogToServiceStderr(t *testing.T) {}
// func TestAppendToFile(t *testing.T) {}
