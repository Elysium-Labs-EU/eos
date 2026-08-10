package manager

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
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
		return
	}
	services, err := manager.GetAllServiceCatalogEntries(t.Context())
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
	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)

	if err != nil {
		t.Fatalf("Adding service catalog entry should not error: %v", err)

	}
	services, err := manager.GetAllServiceCatalogEntries(t.Context())
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

	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)

	if err != nil {
		t.Fatalf("Adding service should not error: %v", err)
	}

	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)
	if err == nil {
		t.Fatalf("Expected error on adding the same service catalog entry twice")
		return
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
	if err = manager.AddServiceCatalogEntry(t.Context(), first); err != nil {
		t.Fatalf("adding first service should not error: %v", err)
	}

	// Same letters, different case: must be rejected as a case collision.
	collide, err := NewServiceCatalogEntry("foo", "./wB", "service.yaml")
	if err != nil {
		t.Fatalf("creating colliding catalog entry should not error: %v", err)
	}
	err = manager.AddServiceCatalogEntry(t.Context(), collide)
	if !errors.Is(err, ErrServiceNameCaseConflict) {
		t.Fatalf("expected ErrServiceNameCaseConflict adding case-colliding name, got: %v", err)
	}

	// The colliding service must NOT have been registered.
	services, err := manager.GetAllServiceCatalogEntries(t.Context())
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
	if err = manager.AddServiceCatalogEntry(t.Context(), other); err != nil {
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
	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	found, error := manager.GetServiceCatalogEntry(t.Context(), "test-service")
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

	serviceInstance, err := manager.GetServiceInstance(t.Context(), "non-existent")

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

	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	pgid, err := manager.StartService(t.Context(), "test-service")

	if err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}
	if pgid == 0 {
		t.Fatal("Starting service should return a non-zero PGID, got 0")
	}
}

// TestStartService_CommandBinaryMissing proves the command preflight rejects
// a start before ever spawning anything, when the command's binary can't be
// found on PATH — the same fakeExecutor used above only stubs the runtime
// binary lookup (validateRuntimeBinary), not this check.
func TestStartService_CommandBinaryMissing(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "nonexistent-binary-xyz-262",
	}

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-files-badcommand")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-files directory: %v", err)
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("test-service-badcommand", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}

	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	pgid, err := manager.StartService(t.Context(), "test-service-badcommand")
	if err == nil {
		t.Fatal("expected StartService to reject a command whose binary is absent from PATH")
	}
	if !strings.Contains(err.Error(), "nonexistent-binary-xyz-262") {
		t.Errorf("expected error to name the missing binary, got: %v", err)
	}
	if pgid != 0 {
		t.Errorf("expected zero pgid on validation failure, got %d", pgid)
	}
}

// TestLocalManager_GetServiceExitCode_CleanExit verifies captureIdentity's
// reaper goroutine records a clean exit-0 for a one-shot command (no server,
// nothing left running) so a caller with only the PGID can tell it apart from
// a crash, and that the record is consumed (returned once, then gone).
func TestLocalManager_GetServiceExitCode_CleanExit(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{Name: "one-shot", Command: "exit 0"}
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "one-shot-files")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test directory: %v", err)
	}
	if err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("one-shot", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	pgid, err := manager.StartService(t.Context(), "one-shot")
	if err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}

	var code int
	var ok bool
	for range 200 {
		if code, ok = manager.GetServiceExitCode(pgid); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ok {
		t.Fatal("expected an exit code to be captured for the one-shot command")
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	if _, ok = manager.GetServiceExitCode(pgid); ok {
		t.Error("expected the exit code record to be consumed by the first read")
	}
}

// TestLocalManager_GetServiceExitCode_NonZeroExit mirrors the clean-exit case
// for a command that fails, so a caller reading the code back can tell the
// two apart.
func TestLocalManager_GetServiceExitCode_NonZeroExit(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	testFile := &types.ServiceConfig{Name: "one-shot-fail", Command: "exit 3"}
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "one-shot-fail-files")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test directory: %v", err)
	}
	if err = os.WriteFile(filepath.Join(fullDirPath, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("one-shot-fail", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	pgid, err := manager.StartService(t.Context(), "one-shot-fail")
	if err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}

	var code int
	var ok bool
	for range 200 {
		if code, ok = manager.GetServiceExitCode(pgid); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ok {
		t.Fatal("expected an exit code to be captured for the failing command")
	}
	if code != 3 {
		t.Errorf("expected exit code 3, got %d", code)
	}
}

// TestLocalManager_GetServiceExitCode_NoEntry verifies a pgid nothing was ever
// captured for (the common case for any pgid not yet reaped) reports ok=false
// rather than a zero-value code that could be mistaken for a real exit 0.
func TestLocalManager_GetServiceExitCode_NoEntry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	const neverLaunchedPGID = 999996
	if _, ok := manager.GetServiceExitCode(neverLaunchedPGID); ok {
		t.Error("expected ok=false for a pgid with no recorded exit")
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

	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	// Simulate a daemon crash mid-start: a Starting entry whose PGID is dead.
	_, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, "test-service", types.ProcessStateStarting)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	pgid, err := manager.StartService(t.Context(), "test-service")
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
		return
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
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
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

	pgid, err := manager.StartService(t.Context(), "test-service")
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
		return
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
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
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

	_, err = manager.StartService(t.Context(), "test-service")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning for a live PGID, got: %v", err)
	}
}

// TestStartServiceBlocksWhenUnknownEntryLive is the direct regression test:
// a graceful stop that exceeds its grace period leaves the most recent
// history row Unknown (see updateProcessHistoryEntriesAsUnknown) while the
// process itself is still alive. livePGIDInHistory only recognizes
// Running/Starting rows, so lmCheckAlreadyRunning can't catch this case —
// reconcileStartHistory must. Before the fix, Unknown was grouped with the
// terminal states and StartService sailed past it, spawning a second
// instance on top of the live one.
func TestStartServiceBlocksWhenUnknownEntryLive(t *testing.T) {
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
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
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
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), livePGID, livePGIDStartedAtTicks, "test-service", types.ProcessStateUnknown); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	_, err = manager.StartService(t.Context(), "test-service")
	if err == nil {
		t.Fatal("expected StartService to refuse a start while an Unknown-state PGID is live")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected an already-running error, got: %v", err)
	}
}

// TestStartServiceSelfHealsStaleUnknownEntry confirms the flip side: an
// Unknown row whose process is actually gone must self-heal to Stopped and
// let the start proceed, not permanently block or leak a phantom entry.
func TestStartServiceSelfHealsStaleUnknownEntry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	const deadPGID = 999997
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test stale Unknown self-heal", deadPGID)
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
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	if err = db.RegisterServiceInstance(t.Context(), "test-service"); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, "test-service", types.ProcessStateUnknown); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	pgid, err := manager.StartService(t.Context(), "test-service")
	if err != nil {
		t.Fatalf("StartService should self-heal a dead Unknown entry, got error: %v", err)
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
		return
	}
	if staleEntry.State != types.ProcessStateStopped {
		t.Errorf("expected stale entry to be marked Stopped, got state %q", staleEntry.State)
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

	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	if _, err := manager.StartService(t.Context(), "test-service"); err != nil {
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

	err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	if _, err := manager.StartService(t.Context(), "test-service"); err == nil {
		t.Fatal("Starting service should error")
	}
}

func TestIsServiceRegistered(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	registered, err := mgr.IsServiceRegistered(t.Context(), "not-registered")
	if err != nil {
		t.Fatalf("IsServiceRegistered: %v", err)
	}
	if registered {
		t.Error("expected not registered")
	}

	if regErr := db.RegisterService(t.Context(), "my-svc", tempDir, "service.yaml"); regErr != nil {
		t.Fatalf("RegisterService: %v", regErr)
	}
	registered, err = mgr.IsServiceRegistered(t.Context(), "my-svc")
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

	removed, err := mgr.RemoveServiceInstance(t.Context(), "remove-inst-svc")
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

	removed, err := mgr.RemoveServiceCatalogEntry(t.Context(), "catalog-svc")
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

	instances, err := mgr.GetAllServiceInstances(t.Context())
	if err != nil {
		t.Fatalf("GetAllServiceInstances: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}

	if regErr := db.RegisterServiceInstance(t.Context(), "inst-svc"); regErr != nil {
		t.Fatalf("RegisterServiceInstance: %v", regErr)
	}
	instances, err = mgr.GetAllServiceInstances(t.Context())
	if err != nil {
		t.Fatalf("GetAllServiceInstances after add: %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(instances))
	}
}

func TestLocalManager_GetVersion(t *testing.T) {
	origVersion, origCommit, origDate := buildinfo.Version, buildinfo.GitCommit, buildinfo.BuildDate
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.GitCommit, buildinfo.BuildDate = origVersion, origCommit, origDate
	})
	buildinfo.Version, buildinfo.GitCommit, buildinfo.BuildDate = "v9.9.9", "deadbeef", "2026-07-21"

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	version, err := mgr.GetVersion(t.Context())
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	want := types.GetVersionResponse{Version: "v9.9.9", GitCommit: "deadbeef", BuildDate: "2026-07-21"}
	if version != want {
		t.Errorf("got %+v, want %+v", version, want)
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

// TestStopServiceWithSignal_deadHistoryRows proves a row whose recorded
// state was already terminal (Stopped/Failed) and is reconfirmed genuinely
// dead is left untouched: liveness is still actively checked (closing eos
// issue #215's second gate — previously lmIsSignalableState skipped these
// rows before any liveness check ever ran), but since nothing changed, the
// row is not rewritten into AlreadyDead/pending/errored. Doing so would
// refresh its StoppedAt to "now" on every future stop call across a
// service's entire accumulated history, corrupting when it actually stopped.
// See TestStopServiceWithSignal_failedStateStillAlive for the case that
// matters: a terminal-state row whose PGID is unexpectedly still alive.
func TestStopServiceWithSignal_deadHistoryRows(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "signal-stopped-svc"
	const stoppedPGID = 71001
	const failedPGID = 71002
	if isProcessAlive(stoppedPGID) {
		t.Skipf("pgid %d is alive — cannot test dead path", stoppedPGID)
	}
	if isProcessAlive(failedPGID) {
		t.Skipf("pgid %d is alive — cannot test dead path", failedPGID)
	}
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), stoppedPGID, 0, name, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry stopped: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), failedPGID, 0, name, types.ProcessStateFailed); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry failed: %v", err)
	}

	result, err := mgr.stopServiceWithSignal(name, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("stopServiceWithSignal: %v", err)
	}
	if len(result.Pending)+len(result.Errored)+len(result.AlreadyDead) != 0 {
		t.Errorf("expected empty result (already-terminal, still-dead rows left untouched), got %+v", result)
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

// TestStopServiceWithSignal_leaderReapedChildSurvives reproduces eos issue
// #215: eos launches every service through a /bin/sh -c "..." wrapper, and
// wrapper commands like `npm start` commonly exit shortly after spawning the
// real long-running child, which keeps running under the same PGID after the
// wrapper (the group leader, whose own pid is the stored PGID) has been
// reaped. Before the fix, IsAliveMatching read this as "already dead" (the
// leader's own StartTime lookup failed) and stopServiceWithSignal never sent
// a signal at all — the real process kept running while eos recorded a clean
// stop.
func TestStopServiceWithSignal_leaderReapedChildSurvives(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	wrapper := exec.Command("/bin/sh", "-c", "sleep 30 & exit 0")
	wrapper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := wrapper.Start(); err != nil {
		t.Fatalf("starting wrapper: %v", err)
	}
	pgid := wrapper.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	})

	startedAtTicks, ticksErr := procutil.StartTime(pgid)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	// Reap the wrapper; the backgrounded sleep it spawned is reparented and
	// keeps running under the same pgid.
	if err := wrapper.Wait(); err != nil {
		t.Fatalf("wait for wrapper: %v", err)
	}

	name := "leader-reaped-svc"
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, startedAtTicks, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.stopServiceWithSignal(name, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("stopServiceWithSignal: %v", err)
	}
	if _, ok := result.AlreadyDead[pgid]; ok {
		t.Errorf("pgid %d landed in AlreadyDead — signal was skipped, reproducing issue #215", pgid)
	}
	if _, ok := result.Pending[pgid]; !ok {
		t.Errorf("expected pgid %d in Pending (signal actually sent), got %+v", pgid, result)
	}
	if len(result.Errored) != 0 {
		t.Errorf("expected no errored, got %+v", result.Errored)
	}
}

// TestStopServiceWithSignal_failedStateStillAlive closes eos issue #215's
// second gate: a process_history row the health monitor (or a stale
// reconciliation) recorded Failed can still point at a genuinely live
// process. Before this fix, lmIsSignalableState returned false for
// ProcessStateFailed and lmSignalHistoryEntry returned immediately — neither
// IsAliveMatching nor syscall.Kill was ever reached, so a real, running
// process recorded as Failed could never be signaled again by any future
// eos stop, eos remove, or restart-loop attempt. This is exactly the state
// the original bug report's live reproduction was in.
func TestStopServiceWithSignal_failedStateStillAlive(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	proc := exec.Command("/bin/sh", "-c", "sleep 30")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("starting process: %v", err)
	}
	pgid, pgidErr := syscall.Getpgid(proc.Process.Pid)
	if pgidErr != nil {
		t.Fatalf("getpgid: %v", pgidErr)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = proc.Process.Wait()
	})

	startedAtTicks, ticksErr := procutil.StartTime(pgid)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	name := "failed-still-alive-svc"
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	// A row recorded Failed — but the process it points at is, in fact,
	// still running.
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, startedAtTicks, name, types.ProcessStateFailed); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.stopServiceWithSignal(name, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("stopServiceWithSignal: %v", err)
	}
	if _, ok := result.AlreadyDead[pgid]; ok {
		t.Errorf("pgid %d landed in AlreadyDead — Failed state skipped the liveness check, reproducing issue #215's second gate", pgid)
	}
	if _, ok := result.Pending[pgid]; !ok {
		t.Errorf("expected pgid %d in Pending (signal actually sent), got %+v", pgid, result)
	}
	if len(result.Errored) != 0 {
		t.Errorf("expected no errored, got %+v", result.Errored)
	}
}

// TestRestartService_failedStateStillAlive proves the restart flow shares the
// same fix: RestartService's stop-before-restart step (lmStopForRestart ->
// stopServiceLocked -> stopServiceWithSignal) must also kill a Failed-marked
// but genuinely live old instance before launching the new one, not leave it
// running alongside the freshly started replacement.
func TestRestartService_failedStateStillAlive(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	proc := exec.Command("/bin/sh", "-c", "sleep 30")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("starting process: %v", err)
	}
	oldPGID, pgidErr := syscall.Getpgid(proc.Process.Pid)
	if pgidErr != nil {
		t.Fatalf("getpgid: %v", pgidErr)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-oldPGID, syscall.SIGKILL)
		_, _ = proc.Process.Wait()
	})

	startedAtTicks, ticksErr := procutil.StartTime(oldPGID)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	name := "restart-failed-alive-svc"
	dir := filepath.Join(tempDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := &types.ServiceConfig{Name: name, Command: "sleep 30"}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	entry, err := NewServiceCatalogEntry(name, dir, "service.yaml")
	if err != nil {
		t.Fatalf("catalog entry: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(t.Context(), entry); err != nil {
		t.Fatalf("register service: %v", err)
	}
	if err = db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), oldPGID, startedAtTicks, name, types.ProcessStateFailed); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	newPGID, err := mgr.RestartService(t.Context(), name, time.Second, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("RestartService: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-newPGID, syscall.SIGKILL) })

	if procutil.IsAlive(oldPGID) {
		t.Errorf("old pgid %d (recorded Failed) should have been killed by restart, not left running", oldPGID)
	}
}

// TestRestartService_escalatesToSIGKILL_whenSIGTERMIgnored proves
// lmStopForRestart's grace-period escalation: an old process group that
// ignores SIGTERM must not permanently veto a restart. Once the SIGTERM
// grace period is exceeded, the restart force-kills it and proceeds, instead
// of returning an error that leaves the service unrestartable forever.
func TestRestartService_escalatesToSIGKILL_whenSIGTERMIgnored(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	proc := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("starting process: %v", err)
	}
	oldPGID, pgidErr := syscall.Getpgid(proc.Process.Pid)
	if pgidErr != nil {
		t.Fatalf("getpgid: %v", pgidErr)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-oldPGID, syscall.SIGKILL)
		_, _ = proc.Process.Wait()
	})

	startedAtTicks, ticksErr := procutil.StartTime(oldPGID)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	name := "restart-sigterm-immune-svc"
	dir := filepath.Join(tempDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := &types.ServiceConfig{Name: name, Command: "sleep 30"}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	entry, err := NewServiceCatalogEntry(name, dir, "service.yaml")
	if err != nil {
		t.Fatalf("catalog entry: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(t.Context(), entry); err != nil {
		t.Fatalf("register service: %v", err)
	}
	if err = db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), oldPGID, startedAtTicks, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	newPGID, err := mgr.RestartService(t.Context(), name, 150*time.Millisecond, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("RestartService should escalate to SIGKILL and succeed, got error: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-newPGID, syscall.SIGKILL) })

	if procutil.IsAlive(oldPGID) {
		t.Errorf("old pgid %d (ignoring SIGTERM) should have been force-killed by the restart's grace-period escalation", oldPGID)
	}
	if !procutil.IsAlive(newPGID) {
		t.Errorf("new pgid %d should be running after restart", newPGID)
	}
}

// failOnCallHistoryDB fails GetProcessHistoryEntriesByServiceName on a chosen
// call number, delegating that call and every other method to the real
// database.Database it wraps. Used to force the SIGKILL-phase fetch inside
// lmStopForRestart's escalation to fail without touching the SIGTERM-phase
// fetch that has to succeed first for escalation to trigger at all.
type failOnCallHistoryDB struct {
	database.Database
	err        error
	callCount  int
	failOnCall int
}

func (f *failOnCallHistoryDB) GetProcessHistoryEntriesByServiceName(ctx context.Context, name string) ([]types.ProcessHistory, error) {
	f.callCount++
	if f.callCount == f.failOnCall {
		return nil, f.err
	}
	return f.Database.GetProcessHistoryEntriesByServiceName(ctx, name)
}

// TestLmStopForRestart_escalationFetchFails proves lmStopForRestart surfaces
// it when the SIGKILL escalation itself can't even fetch process history,
// rather than swallowing the failure or misattributing it to the earlier
// SIGTERM phase. The escalation's own fetch is a second, independent point
// of failure distinct from the SIGTERM phase's.
func TestLmStopForRestart_escalationFetchFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	proc := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("starting process: %v", err)
	}
	pgid, pgidErr := syscall.Getpgid(proc.Process.Pid)
	if pgidErr != nil {
		t.Fatalf("getpgid: %v", pgidErr)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = proc.Process.Wait()
	})

	// Give the shell time to actually execute "trap '' TERM" before any
	// signal is sent — without this, a SIGTERM racing the shell's own
	// startup can kill it via the default disposition before the trap is
	// installed, defeating the whole point of this fixture.
	time.Sleep(100 * time.Millisecond)

	startedAtTicks, ticksErr := procutil.StartTime(pgid)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	name := "escalation-fetch-fails-svc"
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, startedAtTicks, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	sentinel := errors.New("db unavailable")
	mgr.db = &failOnCallHistoryDB{Database: db, failOnCall: 2, err: sentinel}

	err := mgr.lmStopForRestart(name, 150*time.Millisecond, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected lmStopForRestart to fail when the SIGKILL escalation can't fetch process history")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected error to wrap the underlying fetch failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "force-killing process(es) for") {
		t.Errorf("expected error to be attributed to the force-kill escalation, got: %v", err)
	}
}

// injectPhantomHistoryRowDB appends an extra, never-persisted ProcessHistory
// row to the result of GetProcessHistoryEntriesByServiceName on a chosen
// call. The row is alive-matching-false (no real process), so it gets
// classified as already-dead and its state update is attempted — and that
// update is guaranteed to fail since the row was never inserted, giving a
// real "not found" persistence failure without needing OS-level
// signal-permission tricks.
type injectPhantomHistoryRowDB struct {
	database.Database
	phantom   types.ProcessHistory
	callCount int
	injectOn  int
}

func (f *injectPhantomHistoryRowDB) GetProcessHistoryEntriesByServiceName(ctx context.Context, name string) ([]types.ProcessHistory, error) {
	f.callCount++
	rows, err := f.Database.GetProcessHistoryEntriesByServiceName(ctx, name)
	if err != nil {
		return rows, err
	}
	if f.callCount == f.injectOn {
		rows = append(rows, f.phantom)
	}
	return rows, nil
}

// TestLmStopForRestart_escalationLeavesPhantomRowErrored proves
// lmStopForRestart surfaces it when the SIGKILL escalation signals every row
// successfully but fails to persist the outcome for one of them — a
// distinct failure mode from the escalation's fetch failing outright, since
// here the escalation otherwise succeeds and only its bookkeeping write
// fails.
func TestLmStopForRestart_escalationLeavesPhantomRowErrored(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	proc := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := proc.Start(); err != nil {
		t.Fatalf("starting process: %v", err)
	}
	pgid, pgidErr := syscall.Getpgid(proc.Process.Pid)
	if pgidErr != nil {
		t.Fatalf("getpgid: %v", pgidErr)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = proc.Process.Wait()
	})

	// See the identical comment in TestLmStopForRestart_escalationFetchFails:
	// let the shell install its TERM trap before anything can signal it.
	time.Sleep(100 * time.Millisecond)

	startedAtTicks, ticksErr := procutil.StartTime(pgid)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	name := "escalation-phantom-row-svc"
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, startedAtTicks, name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	const phantomPGID = 999996
	if isProcessAlive(phantomPGID) {
		t.Skipf("pgid %d is alive — cannot test the never-registered-row path", phantomPGID)
	}
	phantom := types.ProcessHistory{
		PGID:        phantomPGID,
		ServiceName: name,
		State:       types.ProcessStateRunning,
	}
	mgr.db = &injectPhantomHistoryRowDB{Database: db, injectOn: 2, phantom: phantom}

	err := mgr.lmStopForRestart(name, 150*time.Millisecond, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected lmStopForRestart to fail when the SIGKILL escalation can't persist a row's outcome")
	}
	if !strings.Contains(err.Error(), "force-killing process(es) for") {
		t.Errorf("expected error to be attributed to the force-kill escalation, got: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", phantomPGID)) {
		t.Errorf("expected error to name the unpersisted pgid %d, got: %v", phantomPGID, err)
	}
}

// TestStopService_noLiveProcesses proves a Stopped-state row with a dead PGID
// is actively re-checked (closing issue #215's second gate) and, since
// nothing changed, left untouched rather than rewritten into the result —
// see lmSignalHistoryEntry's doc comment for why re-touching an
// already-terminal, still-dead row's StoppedAt on every stop call would
// corrupt its history.
func TestStopService_noLiveProcesses(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	name := "stop-nolive-svc"
	const deadPGID = 72001
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test dead path", deadPGID)
	}
	if err := db.RegisterServiceInstance(t.Context(), name); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, name, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	result, err := mgr.StopService(t.Context(), name, time.Second, 50*time.Millisecond)
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

	result, err := mgr.StopService(t.Context(), name, time.Second, 50*time.Millisecond)
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

	result, err := mgr.ForceStopService(t.Context(), name)
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

func TestParseEnvFile_NoEnvFileConfigured(t *testing.T) {
	config := &types.ServiceConfig{Name: "svc"}
	vars, err := ParseEnvFile(config, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vars != nil {
		t.Errorf("expected nil vars when EnvFile is unset, got %v", vars)
	}
}

func TestParseEnvFile_PathEscapesServiceDir(t *testing.T) {
	config := &types.ServiceConfig{Name: "svc", EnvFile: "../outside.env"}
	if _, err := ParseEnvFile(config, t.TempDir()); err == nil {
		t.Fatal("expected error for env file path escaping service directory")
	}
}

func TestParseEnvFile_MissingFile(t *testing.T) {
	config := &types.ServiceConfig{Name: "svc", EnvFile: "missing.env"}
	if _, err := ParseEnvFile(config, t.TempDir()); err == nil {
		t.Fatal("expected error reading a nonexistent env file")
	}
}

func TestParseEnvFile_ParsesCommentsBlanksAndOverrides(t *testing.T) {
	dir := t.TempDir()
	contents := "# comment\n\n  \nFOO=bar\nno-equals-sign\nFOO=baz\nBAR=qux\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	config := &types.ServiceConfig{Name: "svc", EnvFile: ".env"}
	vars, err := ParseEnvFile(config, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"FOO=baz", "BAR=qux"}
	if len(vars) != len(want) {
		t.Fatalf("vars = %v, want %v", vars, want)
	}
	for i, w := range want {
		if vars[i] != w {
			t.Errorf("vars[%d] = %q, want %q", i, vars[i], w)
		}
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

func TestValidateRuntimePath_relativePathJoinsHomeDir(t *testing.T) {
	// A relative runtime path is resolved against the user's home directory;
	// exercise that branch with a path that won't exist there, without writing
	// anything under the real home directory.
	rt := types.Runtime{Type: "bun", Path: "eos-test-runtime-path-does-not-exist-99999"}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error for a relative path that doesn't exist under $HOME")
	}
}

func TestValidateRuntimePath_bun_binaryIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "bun"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rt := types.Runtime{Type: "bun", Path: dir}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when bun binary path is a directory")
	}
}

func TestValidateRuntimePath_bun_notExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bun"), []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rt := types.Runtime{Type: "bun", Path: dir}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when bun binary is not executable")
	}
}

func TestValidateRuntimePath_deno_binaryIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "deno"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rt := types.Runtime{Type: "deno", Path: dir}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when deno binary path is a directory")
	}
}

func TestValidateRuntimePath_deno_notExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deno"), []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rt := types.Runtime{Type: "deno", Path: dir}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when deno binary is not executable")
	}
}

func TestValidateRuntimePath_node_binaryIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "node"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rt := types.Runtime{Type: "node", Path: dir}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when node binary path is a directory")
	}
}

func TestValidateRuntimePath_node_notExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "node"), []byte("#!/bin/sh"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rt := types.Runtime{Type: "node", Path: dir}
	if err := ValidateRuntimePath(rt); err == nil {
		t.Fatal("expected error when node binary is not executable")
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
	entry, err := mgr.GetMostRecentProcessHistoryEntry(t.Context(), serviceName)
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	if entry.PGID != 1002 {
		t.Errorf("expected PGID 1002 (newer), got %d", entry.PGID)
	}
}

// TestLiveOrphanRows_excludesMostRecentAndDeadRows proves the pure scanner
// underlying GetLiveOrphanProcessGroups: an older row whose PGID is alive
// counts as an orphan, the most-recent row's own PGID is never reported back
// as its own orphan even if alive, and a dead PGID is skipped entirely.
func TestLiveOrphanRows_excludesMostRecentAndDeadRows(t *testing.T) {
	livePGID, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	liveTicks, err := procutil.StartTime(livePGID)
	if err != nil {
		t.Fatalf("StartTime: %v", err)
	}

	const deadPGID = 999994
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test the dead-row branch", deadPGID)
	}

	history := []types.ProcessHistory{
		{PGID: livePGID, StartedAtTicks: liveTicks},
		{PGID: deadPGID, StartedAtTicks: 0},
		{PGID: 0, StartedAtTicks: 0}, // never-started placeholder row, must be skipped
	}

	orphans := liveOrphanRows(history, deadPGID)
	if len(orphans) != 1 || orphans[0].PGID != livePGID {
		t.Fatalf("expected only the live PGID as an orphan, got %+v", orphans)
	}

	// The live row is excluded when it IS the most recent PGID.
	if orphans := liveOrphanRows(history, livePGID); len(orphans) != 0 {
		t.Errorf("expected no orphans when the live PGID is the most recent row, got %+v", orphans)
	}

	if orphans := liveOrphanRows(nil, livePGID); len(orphans) != 0 {
		t.Errorf("expected no orphans for empty history, got %+v", orphans)
	}
}

// TestLocalManager_GetLiveOrphanProcessGroups_surfacesOlderLiveRow is the
// direct regression test for the read-path bug: a live process group
// belonging to an OLDER process_history row must be reported even though the
// newest row for the same service is unrelated and inactive.
func TestLocalManager_GetLiveOrphanProcessGroups_surfacesOlderLiveRow(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceName := "orphan-surface-svc"
	if err := db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("RegisterServiceInstance: %v", err)
	}

	livePGID, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	liveTicks, err := procutil.StartTime(livePGID)
	if err != nil {
		t.Fatalf("StartTime: %v", err)
	}

	// Older row: still alive, but not the most recent — this is exactly the
	// row a most-recent-only read would hide.
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), livePGID, liveTicks, serviceName, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry (older, live): %v", err)
	}

	const deadPGID = 999995
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test the inactive most-recent row", deadPGID)
	}
	// Newer row: the one GetMostRecentProcessHistoryEntry alone would return.
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, serviceName, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry (newer, dead): %v", err)
	}

	orphans, err := mgr.GetLiveOrphanProcessGroups(t.Context(), serviceName)
	if err != nil {
		t.Fatalf("GetLiveOrphanProcessGroups: %v", err)
	}
	if len(orphans) != 1 || orphans[0].PGID != livePGID {
		t.Fatalf("expected the older live PGID %d as the sole orphan, got %+v", livePGID, orphans)
	}
}

// TestLocalManager_GetLiveOrphanProcessGroups_noHistory proves an
// unregistered/never-started service reports zero orphans rather than an
// error — GetMostRecentProcessHistoryEntryByName's ErrProcessHistoryNotFound
// must be absorbed, not propagated.
func TestLocalManager_GetLiveOrphanProcessGroups_noHistory(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	orphans, err := mgr.GetLiveOrphanProcessGroups(t.Context(), "never-started-svc")
	if err != nil {
		t.Fatalf("expected no error for a service with no history, got: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected zero orphans, got %+v", orphans)
	}
}

// failOnMostRecentDB fails GetMostRecentProcessHistoryEntryByName
// unconditionally, delegating every other method to the real
// database.Database it wraps.
type failOnMostRecentDB struct {
	database.Database
	err error
}

func (f *failOnMostRecentDB) GetMostRecentProcessHistoryEntryByName(ctx context.Context, name string) (types.ProcessHistory, error) {
	return types.ProcessHistory{}, f.err
}

// TestLocalManager_GetLiveOrphanProcessGroups_dbErrors proves both of
// GetLiveOrphanProcessGroups's DB calls surface a genuine (non-NotFound)
// failure instead of silently reporting zero orphans.
func TestLocalManager_GetLiveOrphanProcessGroups_dbErrors(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	sentinel := errors.New("db unavailable")

	mgr.db = &failOnCallHistoryDB{Database: db, failOnCall: 1, err: sentinel}
	if _, err := mgr.GetLiveOrphanProcessGroups(t.Context(), "svc"); !errors.Is(err, sentinel) {
		t.Errorf("expected the history-fetch error to be surfaced, got: %v", err)
	}

	mgr.db = &failOnMostRecentDB{Database: db, err: sentinel}
	if _, err := mgr.GetLiveOrphanProcessGroups(t.Context(), "svc"); !errors.Is(err, sentinel) {
		t.Errorf("expected the most-recent-lookup error to be surfaced, got: %v", err)
	}
}

func TestUpdateServiceCatalogEntry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := db.RegisterService(t.Context(), "update-catalog-svc", tempDir, "service.yaml"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	newDir := filepath.Join(tempDir, "moved")
	if err := mgr.UpdateServiceCatalogEntry(t.Context(), "update-catalog-svc", newDir, "new-service.yaml"); err != nil {
		t.Fatalf("UpdateServiceCatalogEntry: %v", err)
	}

	entry, err := mgr.GetServiceCatalogEntry(t.Context(), "update-catalog-svc")
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

	if err := mgr.UpdateServiceCatalogEntry(t.Context(), "no-such-service", tempDir, "service.yaml"); err == nil {
		t.Fatal("expected error updating an unregistered service")
	}
}

// TestSetServiceEnabled proves the persisted desired-boot-state flag flips
// both ways through LocalManager, backing issue #172's stop/run persistence.
func TestSetServiceEnabled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := db.RegisterService(t.Context(), "toggle-svc", tempDir, "service.yaml"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	entry, err := mgr.GetServiceCatalogEntry(t.Context(), "toggle-svc")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if !entry.Enabled {
		t.Fatal("expected a freshly registered service to default Enabled=true")
	}

	if err = mgr.SetServiceEnabled(t.Context(), "toggle-svc", false); err != nil {
		t.Fatalf("SetServiceEnabled(false): %v", err)
	}
	entry, err = mgr.GetServiceCatalogEntry(t.Context(), "toggle-svc")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if entry.Enabled {
		t.Error("expected Enabled=false after SetServiceEnabled(false)")
	}

	if err = mgr.SetServiceEnabled(t.Context(), "toggle-svc", true); err != nil {
		t.Fatalf("SetServiceEnabled(true): %v", err)
	}
	entry, err = mgr.GetServiceCatalogEntry(t.Context(), "toggle-svc")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if !entry.Enabled {
		t.Error("expected Enabled=true after SetServiceEnabled(true)")
	}
}

func TestSetServiceEnabled_unregisteredService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := mgr.SetServiceEnabled(t.Context(), "no-such-service", false); err == nil {
		t.Fatal("expected error setting enabled state on an unregistered service")
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
	if addErr := manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); addErr != nil {
		t.Fatalf("Add service catalog entry should not error: %v", addErr)
	}

	originalPGID, err := manager.StartService(t.Context(), "restart-service")
	if err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}
	if originalPGID == 0 {
		t.Fatal("Starting service should return a non-zero PGID, got 0")
	}

	newPGID, err := manager.RestartService(t.Context(), "restart-service", time.Second, 50*time.Millisecond)
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

// TestRestartService_CommandBinaryMissing rewrites a running service's
// config to a command whose binary can't be found on PATH, then restarts
// it: the command preflight must reject the restart before the still-live
// original instance is ever stopped.
func TestRestartService_CommandBinaryMissing(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	fullDirPath := filepath.Join(tempDir, "test-files")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("could not create test-files directory: %v", mkdirErr)
	}
	yamlPath := filepath.Join(fullDirPath, "service.yaml")

	goodConfig := &types.ServiceConfig{Name: "cms", Command: "sleep 30"}
	yamlData, err := yaml.Marshal(goodConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	if writeErr := os.WriteFile(yamlPath, yamlData, 0644); writeErr != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", writeErr)
	}

	serviceCatalogEntry, err := NewServiceCatalogEntry("restart-service-badcommand", fullDirPath, "service.yaml")
	if err != nil {
		t.Fatalf("Create service catalog entry should not error: %v", err)
	}
	if addErr := manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); addErr != nil {
		t.Fatalf("Add service catalog entry should not error: %v", addErr)
	}

	originalPGID, err := manager.StartService(t.Context(), "restart-service-badcommand")
	if err != nil {
		t.Fatalf("Starting service should not error: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-originalPGID, syscall.SIGKILL) })

	badConfig := &types.ServiceConfig{Name: "cms", Command: "nonexistent-binary-xyz-262"}
	badYamlData, err := yaml.Marshal(badConfig)
	if err != nil {
		t.Fatalf("Failed to marshal bad test config: %v", err)
	}
	if writeErr := os.WriteFile(yamlPath, badYamlData, 0644); writeErr != nil {
		t.Fatalf("error occurred during rewriting the yaml file, got: %v", writeErr)
	}

	newPGID, err := manager.RestartService(t.Context(), "restart-service-badcommand", time.Second, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected RestartService to reject a command whose binary is absent from PATH")
	}
	if !strings.Contains(err.Error(), "nonexistent-binary-xyz-262") {
		t.Errorf("expected error to name the missing binary, got: %v", err)
	}
	if newPGID != 0 {
		t.Errorf("expected zero pgid on validation failure, got %d", newPGID)
	}
	if !isProcessAlive(originalPGID) {
		t.Error("expected the original instance to still be running: a rejected restart must not have stopped it")
	}
}

func TestRestartService_notRegistered(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	_, err := manager.RestartService(t.Context(), "no-such-service", time.Second, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error restarting an unregistered service")
	}
}

// TestStartServiceConcurrentStartsSpawnOnce is the regression test for issue #1:
// concurrent StartService calls for the same service used to race the
// read-decide-act sequence (no per-service lock), each independently spawning a
// live process group while only the last one stayed tracked — leaking the rest.
// With the per-service mutex, exactly one call must win and start a process; the
// rest must observe the winner's live instance and return ErrAlreadyRunning
// without spawning anything. The invariant asserted here: across all concurrent
// callers there is exactly one live process group, and it is the tracked one.
func TestStartServiceConcurrentStartsSpawnOnce(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	// A long-lived command so every winning start leaves a group alive long
	// enough for the liveness assertions below.
	testFile := &types.ServiceConfig{
		Name:    "sleeper",
		Command: "sleep 30",
		Runtime: types.Runtime{Type: "nodejs"},
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
	if err = manager.AddServiceCatalogEntry(t.Context(), serviceCatalogEntry); err != nil {
		t.Fatalf("Add service catalog entry should not error: %v", err)
	}

	const concurrency = 8
	var wg sync.WaitGroup
	pgids := make([]int, concurrency)
	errs := make([]error, concurrency)
	start := make(chan struct{})
	wg.Add(concurrency)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			<-start // release all goroutines at once to maximize contention
			pgids[idx], errs[idx] = manager.StartService(t.Context(), "test-service")
		}(i)
	}
	close(start)
	wg.Wait()

	// Guarantee no started group survives the test regardless of assertions.
	t.Cleanup(func() {
		for _, pgid := range pgids {
			if pgid > 0 {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}
		manager.WaitPipes()
	})

	// Exactly one caller must succeed; the rest must be ErrAlreadyRunning.
	successes := 0
	var winnerPGID int
	for i := range concurrency {
		switch {
		case errs[i] == nil:
			successes++
			winnerPGID = pgids[i]
			if pgids[i] == 0 {
				t.Errorf("successful start returned a zero PGID")
			}
		case errors.Is(errs[i], ErrAlreadyRunning):
			if pgids[i] != 0 {
				t.Errorf("ErrAlreadyRunning caller should not report a PGID, got %d", pgids[i])
			}
		default:
			t.Errorf("unexpected error from concurrent StartService: %v", errs[i])
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful concurrent start, got %d", successes)
	}

	// Exactly one live process group must exist across every reported PGID, and
	// it must be the winner — i.e. no untracked group leaked.
	liveCount := 0
	for i := range concurrency {
		if pgids[i] > 0 && procutil.IsAlive(pgids[i]) {
			liveCount++
			if pgids[i] != winnerPGID {
				t.Errorf("found a live PGID %d that is not the tracked winner %d (leaked process)", pgids[i], winnerPGID)
			}
		}
	}
	if liveCount != 1 {
		t.Fatalf("expected exactly 1 live process group, got %d", liveCount)
	}

	// The tracked instance in the DB must point at the single live winner.
	instance, err := manager.GetServiceInstance(t.Context(), "test-service")
	if err != nil {
		t.Fatalf("GetServiceInstance after concurrent starts: %v", err)
	}
	if instance == nil {
		t.Fatal("expected a tracked service instance after concurrent starts")
	}
}

// fakeDependencyWaitDB implements database.Database, overriding only the
// dependency-wait methods, so LocalManager's own error-wrapping around each
// can be tested independently of what a real *database.DB can be made to
// fail on (e.g. Get succeeding with a stale row while Clear specifically
// fails, which a single real connection can't produce — closing it would
// fail Get too).
type fakeDependencyWaitDB struct {
	database.Database
	setErr    error
	clearErr  error
	getErr    error
	getStatus types.DependencyWaitStatus
	getWait   bool
}

func (f *fakeDependencyWaitDB) SetDependencyWaitStatus(context.Context, string, []string, time.Time) error {
	return f.setErr
}

func (f *fakeDependencyWaitDB) ClearDependencyWaitStatus(context.Context, string) error {
	return f.clearErr
}

func (f *fakeDependencyWaitDB) GetDependencyWaitStatus(context.Context, string) (types.DependencyWaitStatus, bool, error) {
	return f.getStatus, f.getWait, f.getErr
}

func TestSetDependencyWaitStatus_dbError(t *testing.T) {
	m := &LocalManager{db: &fakeDependencyWaitDB{setErr: errors.New("disk full")}, ctx: t.Context()}
	if err := m.SetDependencyWaitStatus(t.Context(), "web", []string{"db"}, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("expected the db error to be wrapped and returned")
	}
}

func TestClearDependencyWaitStatus_dbError(t *testing.T) {
	m := &LocalManager{db: &fakeDependencyWaitDB{clearErr: errors.New("disk full")}, ctx: t.Context()}
	if err := m.ClearDependencyWaitStatus(t.Context(), "web"); err == nil {
		t.Fatal("expected the db error to be wrapped and returned")
	}
}

func TestGetDependencyWaitStatus_dbError(t *testing.T) {
	m := &LocalManager{db: &fakeDependencyWaitDB{getErr: errors.New("disk full")}, ctx: t.Context()}
	if _, _, err := m.GetDependencyWaitStatus(t.Context(), "web"); err == nil {
		t.Fatal("expected the db error to be wrapped and returned")
	}
}

// TestGetDependencyWaitStatus_staleClearError proves that when a stale wait's
// opportunistic cleanup itself fails, that failure — not a silent "not
// waiting" — is what callers see.
func TestGetDependencyWaitStatus_staleClearError(t *testing.T) {
	m := &LocalManager{db: &fakeDependencyWaitDB{
		getWait:   true,
		getStatus: types.DependencyWaitStatus{ServiceName: "web", Deadline: time.Now().Add(-DependencyWaitStaleGrace - time.Minute)},
		clearErr:  errors.New("disk full"),
	}, ctx: t.Context()}
	if _, _, err := m.GetDependencyWaitStatus(t.Context(), "web"); err == nil {
		t.Fatal("expected the clear error for a stale wait to be wrapped and returned")
	}
}

func TestDependencyWaitStatus(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if status, waiting, err := m.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Fatalf("expected no wait recorded initially, got %+v waiting=%v (err %v)", status, waiting, err)
	}

	deadline := time.Now().Add(5 * time.Minute)
	if err := m.SetDependencyWaitStatus(t.Context(), "web", []string{"db", "cache"}, deadline); err != nil {
		t.Fatalf("SetDependencyWaitStatus: %v", err)
	}

	status, waiting, err := m.GetDependencyWaitStatus(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetDependencyWaitStatus: %v", err)
	}
	if !waiting {
		t.Fatal("expected a recorded wait after Set")
	}
	if status.ServiceName != "web" {
		t.Errorf("expected service name %q, got %q", "web", status.ServiceName)
	}
	if !slices.Equal(status.Pending, []string{"db", "cache"}) {
		t.Errorf("expected pending [db cache], got %v", status.Pending)
	}
	if status.Since.IsZero() {
		t.Error("expected Since to be set")
	}

	// A second service's wait must not be visible under the first's name, and
	// mutating the returned slice must not corrupt the stored copy.
	if _, _, err := m.GetDependencyWaitStatus(t.Context(), "unrelated"); err != nil {
		t.Fatalf("GetDependencyWaitStatus for unrelated service: %v", err)
	}
	status.Pending[0] = "corrupted"
	if again, _, _ := m.GetDependencyWaitStatus(t.Context(), "web"); again.Pending[0] != "db" {
		t.Fatalf("mutating a returned status must not affect the stored copy, got %v", again.Pending)
	}

	if err := m.ClearDependencyWaitStatus(t.Context(), "web"); err != nil {
		t.Fatalf("ClearDependencyWaitStatus: %v", err)
	}
	if status, waiting, err := m.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Fatalf("expected no wait after Clear, got %+v waiting=%v (err %v)", status, waiting, err)
	}

	// Clearing a service with no recorded wait must be a harmless no-op.
	if err := m.ClearDependencyWaitStatus(t.Context(), "never-waited"); err != nil {
		t.Fatalf("ClearDependencyWaitStatus on unrecorded service: %v", err)
	}
}

// TestDependencyWaitStatus_StaleIsSelfHealed proves a wait whose own Deadline
// has passed by more than DependencyWaitStaleGrace — modeling a process that
// recorded it (e.g. via RecordDependencyWait) and was killed before its own
// defer could clear it — reads as not-waiting and is opportunistically
// removed, instead of misreporting an indefinite "waiting" forever.
func TestDependencyWaitStatus_StaleIsSelfHealed(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	pastDeadline := time.Now().Add(-DependencyWaitStaleGrace - time.Minute)
	if err := m.SetDependencyWaitStatus(t.Context(), "web", []string{"db"}, pastDeadline); err != nil {
		t.Fatalf("SetDependencyWaitStatus: %v", err)
	}

	status, waiting, err := m.GetDependencyWaitStatus(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetDependencyWaitStatus: %v", err)
	}
	if waiting {
		t.Fatalf("expected a stale wait to read as not-waiting, got %+v", status)
	}

	if _, waiting, err := db.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Errorf("expected the stale row to be opportunistically deleted, not just skipped: waiting=%v err=%v", waiting, err)
	}
}

// TestDependencyWaitStatus_LongMaxWaitNotPrematurelyStale is the direct
// regression test for the review finding: a wait recorded 11 minutes ago
// (past what used to be a fixed 10-minute staleness window) with a deadline
// still 4 minutes in the future — modeling a single slow dependency under a
// generous max_wait, where Since hasn't moved because pending never narrowed
// — must still report waiting=true. Judging staleness against Since alone
// would have wrongly cleared this still-legitimate wait.
func TestDependencyWaitStatus_LongMaxWaitNotPrematurelyStale(t *testing.T) {
	db, rawConn, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	// A wait started under a 15-minute max_wait: deadline is 4 minutes out.
	futureDeadline := time.Now().Add(4 * time.Minute)
	if err := m.SetDependencyWaitStatus(t.Context(), "web", []string{"slow-dep"}, futureDeadline); err != nil {
		t.Fatalf("SetDependencyWaitStatus: %v", err)
	}

	// Backdate Since only, to 11 minutes ago — past the OLD fixed
	// DependencyWaitStaleAfter-style window this bug used to compare against
	// — while leaving Deadline (the real signal) untouched and still future.
	longAgo := time.Now().Add(-11 * time.Minute)
	if _, err := rawConn.ExecContext(t.Context(), `UPDATE dependency_waits SET since = ? WHERE service_name = ?`, longAgo, "web"); err != nil {
		t.Fatalf("backdating since: %v", err)
	}

	status, waiting, err := m.GetDependencyWaitStatus(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetDependencyWaitStatus: %v", err)
	}
	if !waiting {
		t.Fatal("expected a wait with a still-future deadline to report waiting=true regardless of how old Since is")
	}
	if !slices.Equal(status.Pending, []string{"slow-dep"}) {
		t.Errorf("expected pending [slow-dep], got %v", status.Pending)
	}
}

func TestDependencyWaitStatusConcurrent(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("svc-%d", n)
			_ = m.SetDependencyWaitStatus(t.Context(), name, []string{"dep"}, time.Now().Add(time.Minute))
			_, _, _ = m.GetDependencyWaitStatus(t.Context(), name)
			_ = m.ClearDependencyWaitStatus(t.Context(), name)
		}(i)
	}
	wg.Wait()
}

// TestDependencyWaitStatusCrossProcess proves the fix for the review finding
// that dependencyWaits was an in-memory map: two independent *database.DB
// connections opened against the SAME state.db file, each wrapped in its own
// LocalManager, model two separate CLI invocations in --no-daemon or
// systemd-managed mode (newLocalManagerWithCleanup opens a fresh connection
// per invocation). A wait set through one must be visible — and clearable —
// through the other, not just within the process that set it.
func TestDependencyWaitStatusCrossProcess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")

	dbA, _, err := database.NewTestDB(t.Context(), dbPath, database.MigrationsFS, database.MigrationsPath)
	if err != nil {
		t.Fatalf("opening first connection: %v", err)
	}
	t.Cleanup(func() { _ = dbA.CloseDBConnection() })
	mgrA := NewLocalManager(dbA, filepath.Dir(dbPath), t.Context(), testutil.NewTestLogger(t))

	dbB, _, err := database.NewTestDB(t.Context(), dbPath, database.MigrationsFS, database.MigrationsPath)
	if err != nil {
		t.Fatalf("opening second connection: %v", err)
	}
	t.Cleanup(func() { _ = dbB.CloseDBConnection() })
	mgrB := NewLocalManager(dbB, filepath.Dir(dbPath), t.Context(), testutil.NewTestLogger(t))

	if setErr := mgrA.SetDependencyWaitStatus(t.Context(), "web", []string{"db", "cache"}, time.Now().Add(5*time.Minute)); setErr != nil {
		t.Fatalf("SetDependencyWaitStatus on mgrA: %v", setErr)
	}

	status, waiting, err := mgrB.GetDependencyWaitStatus(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetDependencyWaitStatus on mgrB: %v", err)
	}
	if !waiting {
		t.Fatal("expected mgrB (a separate connection/process) to see the wait mgrA recorded")
	}
	if !slices.Equal(status.Pending, []string{"db", "cache"}) {
		t.Errorf("expected pending [db cache], got %v", status.Pending)
	}

	if err := mgrB.ClearDependencyWaitStatus(t.Context(), "web"); err != nil {
		t.Fatalf("ClearDependencyWaitStatus on mgrB: %v", err)
	}
	if _, waiting, err := mgrA.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Fatalf("expected mgrA to see the clear mgrB made, waiting=%v err=%v", waiting, err)
	}
}

func TestLmScanAndForward(t *testing.T) {
	t.Run("forwards lines to logLine and subscribed sinks", func(t *testing.T) {
		r, w := io.Pipe()
		go func() {
			_, _ = w.Write([]byte("line1\nline2\n"))
			_ = w.Close()
		}()

		logger := testutil.NewTestLogger(t)
		sink := newSinkProcess(&types.LogSink{Type: "test"}, "svc", logger, logger)

		var got []string
		scanner := bufio.NewScanner(r)
		err := lmScanAndForward(scanner, "stdout", []*sinkProcess{sink}, func(line string) {
			got = append(got, line)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []string{"line1", "line2"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i, w := range want {
			if got[i] != w {
				t.Errorf("got[%d] = %q, want %q", i, got[i], w)
			}
		}
		if sink.buf.Len() != len(want) {
			t.Errorf("expected sink to receive %d records, got %d", len(want), sink.buf.Len())
		}
	})

	t.Run("does not forward to a sink not subscribed to the stream", func(t *testing.T) {
		r, w := io.Pipe()
		go func() {
			_, _ = w.Write([]byte("line1\n"))
			_ = w.Close()
		}()

		logger := testutil.NewTestLogger(t)
		sink := newSinkProcess(&types.LogSink{Type: "test", Streams: []string{"stderr"}}, "svc", logger, logger)

		scanner := bufio.NewScanner(r)
		if err := lmScanAndForward(scanner, "stdout", []*sinkProcess{sink}, func(string) {}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sink.buf.Len() != 0 {
			t.Errorf("expected sink not subscribed to stdout to receive nothing, got %d records", sink.buf.Len())
		}
	})

	t.Run("returns the scanner's terminal error", func(t *testing.T) {
		r, w := io.Pipe()
		boom := errors.New("boom")
		go func() {
			_, _ = w.Write([]byte("partial"))
			_ = w.CloseWithError(boom)
		}()

		scanner := bufio.NewScanner(r)
		err := lmScanAndForward(scanner, "stdout", nil, func(string) {})
		if !errors.Is(err, boom) {
			t.Fatalf("expected wrapped boom error, got %v", err)
		}
	})
}

func TestLmApplyRuntimePathEnv(t *testing.T) {
	t.Run("empty runtime path leaves env untouched", func(t *testing.T) {
		env := []string{"FOO=bar"}
		got := lmApplyRuntimePathEnv("", env)
		if len(got) != 1 || got[0] != "FOO=bar" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("replaces an existing PATH entry", func(t *testing.T) {
		env := []string{"PATH=/usr/bin"}
		got := lmApplyRuntimePathEnv("/opt/bun/bin", env)
		want := "PATH=/opt/bun/bin:/usr/bin"
		if len(got) != 1 || got[0] != want {
			t.Errorf("got %v, want [%q]", got, want)
		}
	})

	t.Run("appends PATH when absent", func(t *testing.T) {
		env := []string{"FOO=bar"}
		got := lmApplyRuntimePathEnv("/opt/bun/bin", env)
		want := []string{"FOO=bar", "PATH=/opt/bun/bin"}
		if len(got) != len(want) || got[1] != want[1] {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestLmOverlayEnvVars(t *testing.T) {
	env := []string{"FOO=old"}
	got := lmOverlayEnvVars(env, []string{"FOO=new", "BAR=baz"})
	want := []string{"FOO=new", "BAR=baz"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLmResolveRuntimeDir(t *testing.T) {
	t.Run("absolute path is returned unchanged", func(t *testing.T) {
		got, err := lmResolveRuntimeDir("/abs/path")
		if err != nil || got != "/abs/path" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("relative path joins the home dir", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		got, err := lmResolveRuntimeDir("bin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(home, "bin")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("errors when the home dir cannot be resolved", func(t *testing.T) {
		t.Setenv("HOME", "")
		if _, err := lmResolveRuntimeDir("bin"); err == nil {
			t.Fatal("expected error when HOME is unset")
		}
	})
}

func TestLmPollPendingExits(t *testing.T) {
	const deadPGID = 999992
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test dead-pgid path", deadPGID)
	}

	// alreadyStoppedPGID stands in for a PID already recorded in stopped: it
	// must be left alone (never re-checked via isProcessAlive) rather than
	// re-evaluated.
	const alreadyStoppedPGID = 999993
	pending := map[int]bool{deadPGID: true, alreadyStoppedPGID: true}
	stopped := map[int]bool{alreadyStoppedPGID: true}

	lmPollPendingExits(pending, stopped)

	if !stopped[deadPGID] {
		t.Errorf("expected dead pgid %d to be marked stopped", deadPGID)
	}
	if len(stopped) != 2 {
		t.Errorf("expected stopped to have 2 entries, got %v", stopped)
	}
}

func TestLmDeferCleanupIO(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	readLog, writeLog, err := newPipeForStd()
	if err != nil {
		t.Fatalf("newPipeForStd: %v", err)
	}
	readErr, writeErr, err := newPipeForStd()
	if err != nil {
		t.Fatalf("newPipeForStd: %v", err)
	}
	// Pre-close one fd so closeAll's Close() call on it fails, forcing
	// lmDeferCleanupIO's error-join branch.
	if closeErr := readLog.Close(); closeErr != nil {
		t.Fatalf("pre-closing readLog: %v", closeErr)
	}

	lio := launchIO{readLog: readLog, writeLog: writeLog, readErr: readErr, writeErr: writeErr}

	launchSuccess := false
	var runErr error
	cleanup := lmDeferCleanupIO(mgr, lio, "cleanup-svc", &launchSuccess, &runErr)
	cleanup()

	if runErr == nil {
		t.Fatal("expected the pre-close error to be joined into runErr")
	}
}

// TestReconcileStartHistory_LiveEntryErrors exercises reconcileStartHistory's
// error path, reached when a Running or Starting history row's process is
// still alive: this only happens when the caller's own live-instance check
// (lmCheckAlreadyRunning) didn't already short-circuit — e.g. the
// service_instances row is missing entirely while process history still has
// a live entry.
func TestReconcileStartHistory_LiveEntryErrors(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

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

	ticks, ticksErr := procutil.StartTime(pgid)
	if ticksErr != nil {
		t.Fatalf("StartTime: %v", ticksErr)
	}

	t.Run("running", func(t *testing.T) {
		err := mgr.reconcileStartHistory("svc-running", []types.ProcessHistory{
			{PGID: pgid, StartedAtTicks: ticks, State: types.ProcessStateRunning},
		})
		if err == nil {
			t.Fatal("expected error for a live running entry")
		}
	})

	t.Run("starting", func(t *testing.T) {
		err := mgr.reconcileStartHistory("svc-starting", []types.ProcessHistory{
			{PGID: pgid, StartedAtTicks: ticks, State: types.ProcessStateStarting},
		})
		if err == nil {
			t.Fatal("expected error for a live starting entry")
		}
	})

	t.Run("unknown", func(t *testing.T) {
		err := mgr.reconcileStartHistory("svc-unknown", []types.ProcessHistory{
			{PGID: pgid, StartedAtTicks: ticks, State: types.ProcessStateUnknown},
		})
		if err == nil {
			t.Fatal("expected error for a live unknown entry")
		}
	})
}

// TestReconcileStartHistory_UnknownDeadEntrySelfHeals confirms an Unknown row
// left behind by a stop that couldn't confirm the kill (see
// updateProcessHistoryEntriesAsUnknown) self-heals to Stopped once the
// process is actually gone, rather than being permanently skipped as
// terminal.
func TestReconcileStartHistory_UnknownDeadEntrySelfHeals(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	const deadPGID = 999996
	if isProcessAlive(deadPGID) {
		t.Skipf("pgid %d is alive — cannot test stale Unknown cleanup", deadPGID)
	}

	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, "svc-unknown-dead", types.ProcessStateUnknown); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	err := mgr.reconcileStartHistory("svc-unknown-dead", []types.ProcessHistory{
		{PGID: deadPGID, StartedAtTicks: 0, State: types.ProcessStateUnknown},
	})
	if err != nil {
		t.Fatalf("expected a dead unknown entry to self-heal, got error: %v", err)
	}

	entries, err := db.GetProcessHistoryEntriesByServiceName(t.Context(), "svc-unknown-dead")
	if err != nil {
		t.Fatalf("GetProcessHistoryEntriesByServiceName: %v", err)
	}
	if len(entries) != 1 || entries[0].State != types.ProcessStateStopped {
		t.Errorf("expected the unknown entry to be marked Stopped, got %+v", entries)
	}
}
