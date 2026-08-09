package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// TestStopCommandStopServiceErrorLogged covers cmd/stop.go's StopService
// error-handling branch (line 49): when the manager's StopService call itself
// errors (as opposed to individual PGIDs failing to stop), the CLI must print
// the wrapped error and return helpers.ErrCommandFailed. Dropping the
// process_history table after registering the service reliably reproduces a
// manager-level error without touching any table ensureServiceRegistered
// depends on (service_catalog).
func TestStopCommandStopServiceErrorLogged(t *testing.T) {
	db, dbConn, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}

	// The service is registered (service_catalog), but process_history itself
	// is now gone: StopService's read of process history for this service
	// must error, distinct from "no running processes found".
	if _, err = dbConn.Exec("DROP TABLE process_history"); err != nil {
		t.Fatalf("failed to drop process_history table: %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "stopping service:") {
		t.Errorf("expected 'stopping service:' error, got: %s", output)
	}
}

// TestStopCommandStaleDataWarningLogged covers cmd/stop.go's StaleData warning
// branch (lines 69-70). A real, live process (Pending) is stopped alongside a
// leftover dead PGID (AlreadyDead); deleting the dead PGID's process_history
// row while StopService is waiting on the live one (a 200ms-ticker window
// hardcoded in cmd/stop.go) makes only that PGID's history update fail,
// producing StaleData without racing the tick itself: the deletion is
// scheduled at 20ms, an order of magnitude before the earliest the ticker can
// fire at 200ms.
func TestStopCommandStaleDataWarningLogged(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "1s")

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	if err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755); err != nil {
		t.Fatalf("error writing start-script.sh: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: unexpected error: %v", err)
	}

	const deadPGID = 999995
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, testFile.Name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = db.RemoveProcessHistoryEntryViaPGID(t.Context(), deadPGID)
	}()

	cmd.SetArgs([]string{"stop", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop: unexpected error: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "data may be stale") {
		t.Errorf("expected 'data may be stale' warning, got: %s", output)
	}
}

// TestStopCommandGracePeriodExceeded_ErroredAndDeclined covers three branches
// together: the "failed to gracefully stop" header and per-PGID error listing
// (lines 79, 81) when a process survives the full grace period, and the
// force-quit-declined path (line 86) when the user answers "n" to the
// resulting prompt. The script touches a "trap-installed" marker file right
// after installing its SIGTERM-ignoring trap, and the test polls for that
// marker before sending SIGTERM — sending it on a fixed sleep (as
// TestStopCommandGracePeriod does) races bash's own startup and is flaky: the
// default (terminate) disposition can still win if the trap isn't installed
// yet by the time the sleep elapses (see that test's TODO for the same
// finding); polling for the marker removes the race entirely.
//
// Note this test genuinely waits out the grace period: newTestRootCmd's
// getConfig hardcodes a 5s GracePeriod and does not read SHUTDOWN_GRACE_PERIOD
// (unlike the production config path), so setting that env var here would have
// no effect. The ~5s runtime is real, not a leftover sleep.
func TestStopCommandGracePeriodExceeded_ErroredAndDeclined(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}

	stubbornScript := `#!/bin/bash
trap '' SIGTERM
touch trap-installed
while true; do
    sleep 1
done`

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	if err = os.WriteFile(fullPathScript, []byte(stubbornScript), 0755); err != nil {
		t.Fatalf("error writing start-script.sh: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: unexpected error: %v", err)
	}

	// Wait for the trap-installed marker so SIGTERM is never sent before bash
	// has actually installed its SIGTERM-ignoring trap.
	markerPath := filepath.Join(fullDirPath, "trap-installed")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := os.Stat(markerPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s to appear", markerPath)
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		if latest, latestErr := db.GetMostRecentProcessHistoryEntryByName(t.Context(), testFile.Name); latestErr == nil {
			_ = syscall.Kill(-latest.PGID, syscall.SIGKILL)
		}
	})

	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed (force quit aborted), got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "failed to gracefully stop") {
		t.Errorf("expected 'failed to gracefully stop', got: %s", output)
	}
	if !strings.Contains(output, "PGID") {
		t.Errorf("expected a per-PGID error listing, got: %s", output)
	}

	combined := output + outBuf.String()
	if !strings.Contains(combined, "force quit aborted") {
		t.Errorf("expected 'force quit aborted', got: %s", combined)
	}

	// The process is still alive and the operator declined to kill it: the
	// boot-recovery flag must not have been touched, or nothing would ever
	// adopt or reap this process on the next daemon start.
	entry, err := mgr.GetServiceCatalogEntry(t.Context(), testFile.Name)
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if !entry.Enabled {
		t.Error("expected Enabled to remain true after a declined force quit leaves the process running")
	}
}

// TestStopCommandForceFlagStopServiceErrorLogged covers cmd/stop.go's
// ForceStopService error-handling branch (line 101), the --force counterpart
// of TestStopCommandStopServiceErrorLogged.
func TestStopCommandForceFlagStopServiceErrorLogged(t *testing.T) {
	db, dbConn, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}

	if _, err = dbConn.Exec("DROP TABLE process_history"); err != nil {
		t.Fatalf("failed to drop process_history table: %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name, "--force"})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop --force should not itself return an error (forceStopService only logs): %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "force stopping service:") {
		t.Errorf("expected 'force stopping service:' error, got: %s", output)
	}
}

// TestStopCommandForceFlagMultipleProcesses covers cmd/stop.go's "force
// stopped N processes" plural branch (line 112), the --force counterpart of
// TestStopCommandMultipleProcesses.
func TestStopCommandForceFlagMultipleProcesses(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	if err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755); err != nil {
		t.Fatalf("error writing start-script.sh: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: unexpected error: %v", err)
	}

	const deadPGID = 999994
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, testFile.Name, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name, "--force"})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop --force: unexpected error: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "force stopped 2 processes") {
		t.Errorf("expected 'force stopped 2 processes', got: %s", output)
	}
}

// TestStopCommandForceFlagNoProcesses covers cmd/stop.go's "force stopped no
// processes" default branch (line 114): a registered service that was never
// run has no process_history rows at all, so ForceStopService's Stopped map
// is empty.
func TestStopCommandForceFlagNoProcesses(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}

	// Never started; ForceStopService should find nothing to stop.
	cmd.SetArgs([]string{"stop", testFile.Name, "--force"})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop --force: unexpected error: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "force stopped no processes") {
		t.Errorf("expected 'force stopped no processes', got: %s", output)
	}
}

// TestStopCommandCleanupServiceInstanceErrorLogged covers cmd/stop.go's
// cleanupServiceInstance error branch (line 138): once StopService succeeds,
// RemoveServiceInstance failing (here, because service_instances itself is
// gone) must be logged, not silently swallowed.
func TestStopCommandCleanupServiceInstanceErrorLogged(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "1s")

	db, dbConn, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	if err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755); err != nil {
		t.Fatalf("error writing start-script.sh: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: unexpected error: %v", err)
	}

	// StopService itself only touches process_history, so dropping
	// service_instances here only affects the cleanup step at the end.
	if _, err = dbConn.Exec("DROP TABLE service_instances"); err != nil {
		t.Fatalf("failed to drop service_instances table: %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop: unexpected error: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "cleaning up service instance:") {
		t.Errorf("expected 'cleaning up service instance:' error, got: %s", output)
	}
}
