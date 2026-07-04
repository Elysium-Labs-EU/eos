package manager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

func TestIsServiceRegistered(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	registered, err := mgr.IsServiceRegistered("not-registered")
	if err != nil {
		t.Fatalf("IsServiceRegistered: %v", err)
	}
	if registered {
		t.Error("expected not registered")
	}

	if regErr := db.RegisterService(t.Context(), "my-svc", tempDir, "service.yaml"); regErr != nil {
		t.Fatalf("RegisterService: %v", regErr)
	}
	registered, err = mgr.IsServiceRegistered("my-svc")
	if err != nil {
		t.Fatalf("IsServiceRegistered after register: %v", err)
	}
	if !registered {
		t.Error("expected registered")
	}
}

func TestRemoveServiceInstance(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := db.RegisterServiceInstance(t.Context(), "remove-inst-svc"); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}

	removed, err := mgr.RemoveServiceInstance("remove-inst-svc")
	if err != nil {
		t.Fatalf("RemoveServiceInstance: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}
}

func TestRemoveServiceCatalogEntry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := db.RegisterService(t.Context(), "catalog-svc", tempDir, "service.yaml"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	removed, err := mgr.RemoveServiceCatalogEntry("catalog-svc")
	if err != nil {
		t.Fatalf("RemoveServiceCatalogEntry: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}
}

func TestGetAllServiceInstances(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	instances, err := mgr.GetAllServiceInstances()
	if err != nil {
		t.Fatalf("GetAllServiceInstances: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}

	if regErr := db.RegisterServiceInstance(t.Context(), "inst-svc"); regErr != nil {
		t.Fatalf("RegisterServiceInstance: %v", regErr)
	}
	instances, err = mgr.GetAllServiceInstances()
	if err != nil {
		t.Fatalf("GetAllServiceInstances after add: %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(instances))
	}
}

func TestIsProcessAlive(t *testing.T) {
	if isProcessAlive(0) {
		t.Error("pgid=0 should be dead")
	}
	if isProcessAlive(-1) {
		t.Error("pgid=-1 should be dead")
	}
	if isProcessAlive(1) {
		t.Error("pgid=1 should be dead (short-circuits at <=1)")
	}

	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	if !isProcessAlive(pgid) {
		t.Errorf("own pgid %d should be alive", pgid)
	}

	const deadPGID = 999993
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is actually alive — skipping dead check", deadPGID)
	}
}

func TestDoesEnvVarAlreadyExist(t *testing.T) {
	env := []string{"FOO=bar", "BAZ=qux", "PATH=/usr/bin"}

	idx, after := doesEnvVarAlreadyExist("FOO=", env)
	if idx != 0 || after != "bar" {
		t.Errorf("FOO: idx=%d after=%q", idx, after)
	}

	idx, after = doesEnvVarAlreadyExist("PATH=", env)
	if idx != 2 || after != "/usr/bin" {
		t.Errorf("PATH: idx=%d after=%q", idx, after)
	}

	idx, after = doesEnvVarAlreadyExist("MISSING=", env)
	if idx != -1 || after != "" {
		t.Errorf("MISSING: expected -1/'', got idx=%d after=%q", idx, after)
	}
}

func TestStopServiceWithSignal_stopped(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "signal-stopped-svc"
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 71001, name, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry stopped: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 71002, name, types.ProcessStateFailed); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	result, err := mgr.stopServiceWithSignal(name, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("stopServiceWithSignal: %v", err)
	}
	if len(result.Pending)+len(result.Errored)+len(result.AlreadyDead) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestStopServiceWithSignal_deadPGID(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "signal-dead-svc"
	const deadPGID = 999991
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test ESRCH path", deadPGID)
	}
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.stopServiceWithSignal(name, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("stopServiceWithSignal: %v", err)
	}
	if _, ok := result.AlreadyDead[deadPGID]; !ok {
		t.Errorf("expected deadPGID in AlreadyDead, got %+v", result)
	}
}

func TestStopService_noLiveProcesses(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "stop-nolive-svc"
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 72001, name, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.StopService(name, time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("StopService: %v", err)
	}
	if len(result.Stopped)+len(result.Errored)+len(result.StaleData) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestStopService_deadPGID(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "stop-dead-svc"
	const deadPGID = 999992
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test dead path", deadPGID)
	}
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.StopService(name, time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("StopService: %v", err)
	}
	if _, ok := result.Stopped[deadPGID]; !ok {
		t.Errorf("expected deadPGID in Stopped, got %+v", result)
	}
}

func TestForceStopService_deadPGID(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "force-dead-svc"
	const deadPGID = 999990
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test dead path", deadPGID)
	}
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.ForceStopService(name)
	if err != nil {
		t.Fatalf("ForceStopService: %v", err)
	}
	if _, ok := result.Stopped[deadPGID]; !ok {
		t.Errorf("expected deadPGID in Stopped, got %+v", result)
	}
}

func TestUpdateProcessHistoryEntriesAsStopped(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "update-stopped-svc"
	const pgid = 73001
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	errored := updateProcessHistoryEntriesAsStopped(mgr, map[int]bool{pgid: true})
	if len(errored) != 0 {
		t.Errorf("expected no errors, got %v", errored)
	}
}

func TestUpdateProcessHistoryEntriesAsUnknown(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "update-unknown-svc"
	const pgid = 74001
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	errored := updateProcessHistoryEntriesAsUnknown(mgr, map[int]string{pgid: "kill failed"})
	if len(errored) != 0 {
		t.Errorf("expected no errors, got %v", errored)
	}
}

func TestValidateRuntimePath_nonExistent(t *testing.T) {
	rt := types.Runtime{Type: "bun", Path: "/nonexistent/path/99999"}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestValidateRuntimePath_notDirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "notadir")
	_ = os.WriteFile(f, []byte(""), 0644)
	rt := types.Runtime{Type: "bun", Path: f}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when path is not a directory")
	}
}

func TestValidateRuntimePath_bun_noBinary(t *testing.T) {
	rt := types.Runtime{Type: "bun", Path: t.TempDir()}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when bun binary missing")
	}
}

func TestValidateRuntimePath_bun_success(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bun"), []byte("#!/bin/sh"), 0755)
	rt := types.Runtime{Type: "bun", Path: dir}
	if err := ValidateRuntimePath(rt); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRuntimePath_deno_success(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "deno"), []byte("#!/bin/sh"), 0755)
	rt := types.Runtime{Type: "deno", Path: dir}
	if err := ValidateRuntimePath(rt); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRuntimePath_node_success(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "node"), []byte("#!/bin/sh"), 0755)
	rt := types.Runtime{Type: "node", Path: dir}
	if err := ValidateRuntimePath(rt); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRuntimePath_unknownRuntime(t *testing.T) {
	rt := types.Runtime{Type: "python", Path: t.TempDir()}
	if err := ValidateRuntimePath(rt); err != nil {
		t.Errorf("expected nil for unknown runtime, got: %v", err)
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
