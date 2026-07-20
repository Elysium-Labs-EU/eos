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

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// Several tests below use large, arbitrary PGID constants (e.g. 999990-999993,
// 71001-74001, 1001-1002) as stand-ins for a "dead" process group: real PGIDs on
// a typical dev/CI machine stay well below these values, so they're unlikely to
// collide with a live process. Since this isn't guaranteed, each such test guards
// itself with an isProcessAlive(pgid) check and skips if the guess turned out to
// be wrong (i.e. the PGID is actually alive).

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
		t.Errorf("GetAllRegisteredServices shouldn't error, got: %v", err)
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
		t.Errorf("Expected a duplicate-entry error, got the unrelated empty-name error: %v", err)
	}

}

// TestAddServiceCaseInsensitiveCollision guards against issue #10: two service
// names differing only in letter case are distinct catalog rows but their log
// filenames (derived verbatim from the name) alias onto one file on
// case-insensitive filesystems (macOS APFS), silently intermingling their
// output. Registration must reject the second, case-colliding name so distinct
// catalog identities never share a log file. A plain single-case name must
// still register.
func TestAddServiceCaseInsensitiveCollision(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	first, err := NewServiceCatalogEntry("Foo", "./wA", "service.yaml")
	if err != nil {
		t.Fatalf("creating first catalog entry should not error: %v", err)
	}
	if err = manager.AddServiceCatalogEntry(first); err != nil {
		t.Fatalf("adding first service should not error: %v", err)
	}

	// Same letters, different case: must be rejected as a case collision.
	collide, err := NewServiceCatalogEntry("foo", "./wB", "service.yaml")
	if err != nil {
		t.Fatalf("creating colliding catalog entry should not error: %v", err)
	}
	err = manager.AddServiceCatalogEntry(collide)
	if !errors.Is(err, ErrServiceNameCaseConflict) {
		t.Fatalf("expected ErrServiceNameCaseConflict adding case-colliding name, got: %v", err)
	}

	// The colliding service must NOT have been registered.
	services, err := manager.GetAllServiceCatalogEntries()
	if err != nil {
		t.Fatalf("listing services should not error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected only the first service registered, got %d", len(services))
	}

	// A distinct single-case name must still register fine.
	other, err := NewServiceCatalogEntry("bar", "./wC", "service.yaml")
	if err != nil {
		t.Fatalf("creating unrelated catalog entry should not error: %v", err)
	}
	if err = manager.AddServiceCatalogEntry(other); err != nil {
		t.Fatalf("adding a distinct single-case service should not error: %v", err)
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
		t.Fatalf("could not create test-files directory: %v", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
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
		t.Fatalf("Starting service should not error: %v", err)
	}
	if pgid == 0 {
		t.Fatal("Starting service should return a non-zero PGID, got 0")
	}
}

func TestStartServiceStaleStartingEntryIsIgnored(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	const deadPGID = 999994
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test stale Starting cleanup", deadPGID)
	}

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
		t.Fatalf("could not create test-files directory: %v", err)
	}

	err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	// Simulate a daemon crash mid-start: a Starting entry whose PGID is dead.
	_, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, "test-service", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	pgid, err := manager.StartService("test-service")
	if err != nil {
		t.Fatalf("StartService should ignore a stale Starting entry with a dead PGID, got error: %v", err)
	}
	if pgid == 0 {
		t.Fatal("StartService should return a non-zero PGID, got 0")
	}

	entries, err := db.GetProcessHistoryEntriesByServiceName(t.Context(), "test-service")
	if err != nil {
		t.Fatalf("GetProcessHistoryEntriesByServiceName: %v", err)
	}
	var staleEntry *types.ProcessHistory
	for i := range entries {
		if entries[i].PGID == deadPGID {
			staleEntry = &entries[i]
		}
	}
	if staleEntry == nil {
		t.Fatalf("expected stale entry with PGID %d to still exist, got %+v", deadPGID, entries)
	}
	if staleEntry.State != types.ProcessStateFailed {
		t.Errorf("expected stale entry to be marked Failed, got state %q", staleEntry.State)
	}
}

// TestStartServiceSelfHealsStaleServiceInstance is the direct regression test
// for #96's comment: a service_instances row can survive a daemon restart
// that wasn't preceded by a clean `eos stop` (e.g. the daemon itself was
// killed out-of-band). Before the fix, GetServiceInstance returning non-nil
// alone was enough to block StartService with ErrAlreadyRunning, even though
// nothing in process history is actually alive. StartService must self-heal
// instead of refusing to start.
func TestStartServiceSelfHealsStaleServiceInstance(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	const deadPGID = 999995
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test stale service_instances self-heal", deadPGID)
	}

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
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-files directory: %v", err)
	}
	if err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	if err = manager.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	// Simulate an out-of-band daemon kill: service_instances row is present
	// (never cleaned up by an `eos stop`), and the last process history row
	// still says Running, but the PGID is actually dead.
	if err = db.RegisterServiceInstance(t.Context(), "test-service"); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, "test-service", types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	pgid, err := manager.StartService("test-service")
	if err != nil {
		t.Fatalf("StartService should self-heal a stale service_instances row with a dead PGID, got error: %v", err)
	}
	if pgid == 0 {
		t.Fatal("StartService should return a non-zero PGID, got 0")
	}

	entries, err := db.GetProcessHistoryEntriesByServiceName(t.Context(), "test-service")
	if err != nil {
		t.Fatalf("GetProcessHistoryEntriesByServiceName: %v", err)
	}
	var staleEntry *types.ProcessHistory
	for i := range entries {
		if entries[i].PGID == deadPGID {
			staleEntry = &entries[i]
		}
	}
	if staleEntry == nil {
		t.Fatalf("expected stale entry with PGID %d to still exist, got %+v", deadPGID, entries)
	}
	if staleEntry.State != types.ProcessStateStopped {
		t.Errorf("expected stale entry to be marked Stopped, got state %q", staleEntry.State)
	}
}

// TestStartServiceBlocksWhenServiceInstanceLive confirms the fix didn't
// weaken the already-running guard: a live PGID in process history must still
// block StartService with ErrAlreadyRunning.
func TestStartServiceBlocksWhenServiceInstanceLive(t *testing.T) {
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
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-files directory: %v", err)
	}
	if err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	if err = manager.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	livePGID, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	livePGIDStartedAtTicks, err := procutil.StartTime(livePGID)
	if err != nil {
		t.Fatalf("StartTime: %v", err)
	}

	if err = db.RegisterServiceInstance(t.Context(), "test-service"); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), livePGID, livePGIDStartedAtTicks, "test-service", types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	_, err = manager.StartService("test-service")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning for a live PGID, got: %v", err)
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
		t.Fatalf("could not create test-files directory: %v", err)
		return
	}

	err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	err = os.WriteFile(filepath.Join(fullDirPath, ".env"), nil, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the env file, got: %v", err)
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
		t.Fatalf("could not create test-files directory: %v", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
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
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 71001, 0, name, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry stopped: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 71002, 0, name, types.ProcessStateFailed); err != nil {
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
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, name, types.ProcessStateRunning); err != nil {
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

// TestStopServiceWithSignal_reusedPGID verifies that a history entry whose PGID
// is alive but whose recorded start time no longer matches (i.e. the kernel
// recycled the PGID onto an unrelated, later process) is treated as already
// dead and never signaled. Without the start-time guard, StopService would
// SIGTERM a bystander process — killing it if it's ours, or erroring with
// EPERM if it belongs to another user, which surfaced as a flaky
// "graceful stop failed" in the api stop tests.
func TestStopServiceWithSignal_reusedPGID(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	// Launch a real, live process group to stand in for the recycled PGID.
	bystander := exec.Command("/bin/sh", "-c", "sleep 30")
	bystander.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := bystander.Start(); err != nil {
		t.Fatalf("starting bystander: %v", err)
	}
	pgid, pgidErr := syscall.Getpgid(bystander.Process.Pid)
	if pgidErr != nil {
		t.Fatalf("getpgid: %v", pgidErr)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = bystander.Process.Wait()
	})

	realTicks, ticksErr := procutil.StartTime(pgid)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	name := "reused-pgid-svc"
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	// Record a start time that deliberately does NOT match the live process,
	// simulating a stale record left behind after PGID reuse.
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, realTicks+1, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.stopServiceWithSignal(name, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("stopServiceWithSignal: %v", err)
	}
	if _, ok := result.AlreadyDead[pgid]; !ok {
		t.Errorf("expected reused pgid %d in AlreadyDead, got %+v", pgid, result)
	}
	if len(result.Errored) != 0 || len(result.Pending) != 0 {
		t.Errorf("expected no errored/pending, got %+v", result)
	}
	// The bystander must be untouched — it was never the process we started.
	if !procutil.IsAliveMatching(pgid, realTicks) {
		t.Errorf("bystander process (pgid %d) was signaled but should have been left alone", pgid)
	}
}

func TestStopService_noLiveProcesses(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "stop-nolive-svc"
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 72001, 0, name, types.ProcessStateStopped); err != nil {
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
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, name, types.ProcessStateRunning); err != nil {
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
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, name, types.ProcessStateRunning); err != nil {
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
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, 0, name, types.ProcessStateRunning); err != nil {
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
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, 0, name, types.ProcessStateRunning); err != nil {
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

func TestLocalManager_GetMostRecentProcessHistoryEntry_NilStartedAt(t *testing.T) {
	db, rawDB, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceName := "nil-started-at-svc"
	if err := db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}

	// Register a first entry — started_at will be set by the INSERT.
	_, err := db.RegisterProcessHistoryEntry(t.Context(), 1001, 0, serviceName, types.ProcessStateFailed)
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
	_, err = db.RegisterProcessHistoryEntry(t.Context(), 1002, 0, serviceName, types.ProcessStateStarting)
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

func TestUpdateServiceCatalogEntry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := db.RegisterService(t.Context(), "update-catalog-svc", tempDir, "service.yaml"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	newDir := filepath.Join(tempDir, "moved")
	if err := mgr.UpdateServiceCatalogEntry("update-catalog-svc", newDir, "new-service.yaml"); err != nil {
		t.Fatalf("UpdateServiceCatalogEntry: %v", err)
	}

	entry, err := mgr.GetServiceCatalogEntry("update-catalog-svc")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if entry.DirectoryPath != newDir {
		t.Errorf("expected DirectoryPath %q, got %q", newDir, entry.DirectoryPath)
	}
	if entry.ConfigFileName != "new-service.yaml" {
		t.Errorf("expected ConfigFileName 'new-service.yaml', got %q", entry.ConfigFileName)
	}
}

func TestUpdateServiceCatalogEntry_unregisteredService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := mgr.UpdateServiceCatalogEntry("no-such-service", tempDir, "service.yaml"); err == nil {
		t.Fatal("expected error updating an unregistered service")
	}
}

func TestWaitPipes(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	// No pipes started: WaitPipes must return immediately rather than block.
	done := make(chan struct{})
	go func() {
		mgr.WaitPipes()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitPipes blocked with no pending pipes")
	}
}

func TestRestartService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "sleep 30",
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
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("could not create test-files directory: %v", mkdirErr)
	}

	if writeErr := os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644); writeErr != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", writeErr)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("restart-service", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	if addErr := manager.AddServiceCatalogEntry(serviceCatalogEntry); addErr != nil {
		t.Fatalf("Add service catalog entry should not error: %v", addErr)
	}

	originalPGID, err := manager.StartService("restart-service")
	if err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}
	if originalPGID == 0 {
		t.Fatal("Starting service should return a non-zero PGID, got 0")
	}

	newPGID, err := manager.RestartService("restart-service", time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("RestartService should not error: %v", err)
	}
	if newPGID == 0 {
		t.Fatal("RestartService should return a non-zero PGID, got 0")
	}
	if !isProcessAlive(newPGID) {
		t.Errorf("expected restarted process group %d to be alive", newPGID)
	}
	_ = syscall.Kill(-newPGID, syscall.SIGKILL)
}

func TestRestartService_notRegistered(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	_, err := manager.RestartService("no-such-service", time.Second, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error restarting an unregistered service")
	}
}
