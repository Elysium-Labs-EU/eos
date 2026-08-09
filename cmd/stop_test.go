package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestStopCommand(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "1s")

	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	testStartScript := `#!/bin/bash
exec sleep 3600`

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	output := outBuf.String()

	if !strings.Contains(output, "stopped 1 process") {
		t.Errorf("Expected stop to show 'stopped 1 process', got: %s", output)
	}
	if !strings.Contains(output, "service instance cleaned up") {
		t.Errorf("Expected stop to show 'service instance cleaned up', got: %s", output)
	}
}

func TestStopCommandShortLivedScript(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "250ms")

	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	testStartScript := `#!/bin/bash
						echo TESTING BOOTED UP`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(testStartScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	output := outBuf.String()

	if !strings.Contains(output, "stopped 1 process") {
		t.Errorf("Expected stop to show 'stopped 1 process', got: %s", output)
	}
	if !strings.Contains(output, "service instance cleaned up") {
		t.Errorf("Expected stop to show 'service instance cleaned up', got: %s", output)
	}
}

func TestStopCommandGracePeriod(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "250ms")

	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	stubbornScript := `#!/bin/bash
						# stubborn-service.sh - ignores SIGTERM, only dies to SIGKILL
trap '' SIGTERM   # <-- this is the key line: empty handler = ignore
echo "Stubborn service running with PGID $$"
while true; do
    sleep 1
done`

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte(stubbornScript), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	output := outBuf.String()

	if !strings.Contains(output, "stopped 1 process") {
		t.Errorf("Expected stop to show 'stopped 1 process', got: %s", output)
	}
	if !strings.Contains(output, "service instance cleaned up") {
		t.Errorf("Expected stop to show 'service instance cleaned up', got: %s", output)
	}
}

func TestStopCommandForceFlag(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name, "--force"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "forcefully stopping") {
		t.Errorf("Expected stop to show 'forcefully stopping', got: %s", output)
	}
	if !strings.Contains(output, "force stopped 1 process") {
		t.Errorf("Expected stop to show 'force stopped 1 process', got: %s", output)
	}
	if !strings.Contains(output, "service instance cleaned up") {
		t.Errorf("Expected stop to show 'service instance cleaned up', got: %s", output)
	}
}

// TestStopCommandPersistsDisabled proves issue #172's fix: "eos stop"
// persists the service's desired boot state to disabled, regardless of
// whether a running process was actually found to kill.
func TestStopCommandPersistsDisabled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v", err)
	}

	var outBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	// Never started; StopService finds nothing to kill, but the desired boot
	// state must still flip to disabled.
	cmd.SetArgs([]string{"stop", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	entry, err := mgr.GetServiceCatalogEntry(t.Context(), testFile.Name)
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if entry.Enabled {
		t.Error("expected Enabled=false after 'eos stop'")
	}
}

func TestStopCommandNotRegistered(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	cmd.SetArgs([]string{"stop", "cms"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "is not registered") {
		t.Errorf("Expected 'is not registered', got: %s", output)
	}
}

func TestStopCommandNoRunningProcesses(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	// Never started; StopService should find nothing to stop.
	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "no running processes found") {
		t.Errorf("Expected 'no running processes found', got: %s", output)
	}
}

// TestStopCommandForceQuitDeclined exercises the "n" answer to the
// force-quit prompt ("force quit aborted" -> ErrCommandFailed), which
// requires a process that genuinely survives the graceful-stop grace period
// so countError > 0. TestStopCommandGracePeriod's stubborn/SIGTERM-trap
// script (a shebang file invoked via "./start-script.sh") does not reliably
// survive when started via LocalManager.StartService in this environment: sh
// -c "./start-script.sh" execs an external file, and the leader PID captured
// before that exec can end up not matching the long-lived trapping process,
// so procutil.IsAliveMatching reports it dead well within the grace period
// regardless of the trap -- masking TestStopCommandGracePeriod's "y" answer
// too, since the plain success-path assertions happen to match either way.
// Inlining the trap directly into service.yaml's command (run via plain
// "/bin/sh -c <command>", no external file/exec) avoids that hazard; this is
// the same pattern already proven reliable by
// TestDaemonShutdown_ForceKillsServiceThatIgnoresSIGTERM in
// internal/process/daemon_graceful_shutdown_test.go.
func TestStopCommandForceQuitDeclined(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "250ms")

	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithCommand(`trap '' TERM; echo READY; while true; do sleep 0.1; done`),
		testutil.WithoutRuntime())

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	// Give the shell a moment to install its TERM trap and reach the wait
	// loop before SIGTERM is sent, same as
	// internal/process/daemon_graceful_shutdown_test.go's
	// newTestDaemonWithService: without this, SIGTERM can race the shell's
	// startup and hit it before the trap is installed, killing it via the
	// default action and defeating the point of this test.
	time.Sleep(200 * time.Millisecond)

	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "force quit aborted") {
		t.Errorf("Expected stop to show 'force quit aborted', got: %s", output)
	}
	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "failed to gracefully stop") {
		t.Errorf("Expected stderr to show 'failed to gracefully stop', got: %s", errOutput)
	}

	// The service ignored SIGTERM and is still alive; force stop it so the
	// test doesn't leak a real, indefinitely-looping process.
	cmd.SetArgs([]string{"stop", testFile.Name, "--force"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Force stop cleanup should not return an error, got : %v", err)
	}
	if !strings.Contains(outBuf.String(), "force stopped 1 process") {
		t.Errorf("Expected cleanup force stop to show 'force stopped 1 process', got: %s", outBuf.String())
	}
}

// stopCmdFakeManager implements manager.ServiceManager by embedding a nil
// interface and overriding only the methods stopCmd* helpers call, so the
// force-quit confirm/decline branches can be exercised without a real
// DB-backed manager.
type stopCmdFakeManager struct {
	forceStopResult manager.StopServiceResult
	manager.ServiceManager
	forceStopErr      error
	removeInstanceErr error
	setEnabledErr     error
	removedInstance   bool
	setEnabledCalled  bool
}

func (f *stopCmdFakeManager) ForceStopService(context.Context, string) (manager.StopServiceResult, error) {
	return f.forceStopResult, f.forceStopErr
}

func (f *stopCmdFakeManager) RemoveServiceInstance(context.Context, string) (bool, error) {
	return f.removedInstance, f.removeInstanceErr
}

func (f *stopCmdFakeManager) SetServiceEnabled(context.Context, string, bool) error {
	f.setEnabledCalled = true
	return f.setEnabledErr
}

func TestStopCmdConfirmForceQuitDeclined(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetIn(strings.NewReader("n\n"))

	err := stopCmdConfirmForceQuit(cmd, "svc", &stopCmdFakeManager{}, map[int]string{123: "boom"})

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(outBuf.String(), "force quit aborted") {
		t.Errorf("expected 'force quit aborted', got: %s", outBuf.String())
	}
}

func TestStopCmdConfirmForceQuitConfirmed(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetIn(strings.NewReader("y\n"))
	fake := &stopCmdFakeManager{
		forceStopResult: manager.StopServiceResult{Stopped: map[int]bool{123: true}},
		removedInstance: true,
	}

	err := stopCmdConfirmForceQuit(cmd, "svc", fake, map[int]string{123: "boom"})

	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	output := outBuf.String()
	if !strings.Contains(output, "force stopped 1 process") {
		t.Errorf("expected 'force stopped 1 process', got: %s", output)
	}
	if !strings.Contains(output, "service instance cleaned up") {
		t.Errorf("expected 'service instance cleaned up', got: %s", output)
	}
	if !fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled to be called once the force stop fully succeeded")
	}
}

// TestForceStopServiceManualInterventionSkipsPersist proves forceStopService
// does not persist the disabled boot flag when a PGID survives even the
// SIGKILL: it would otherwise mark a still-running process as "will not
// start at boot" with nothing left to ever reap it.
func TestForceStopServiceManualInterventionSkipsPersist(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	fake := &stopCmdFakeManager{
		forceStopResult: manager.StopServiceResult{Errored: map[int]string{123: "kill: operation not permitted"}},
		setEnabledErr:   errors.New("must not be called"),
	}

	forceStopService(cmd, "svc", fake)

	if fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled not to be called when a PGID survives force kill")
	}
	output := errBuf.String()
	if !strings.Contains(output, "manual action required") {
		t.Errorf("expected 'manual action required', got: %s", output)
	}
}

// TestForceStopServiceSetEnabledErrorLogged covers forceStopService's persist
// failure branch: the force kill fully succeeded, but recording the boot
// state fails and must be logged rather than silently swallowed.
func TestForceStopServiceSetEnabledErrorLogged(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	fake := &stopCmdFakeManager{
		forceStopResult: manager.StopServiceResult{Stopped: map[int]bool{123: true}},
		setEnabledErr:   errors.New("db closed"),
	}

	forceStopService(cmd, "svc", fake)

	if !fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled to be called once the force stop fully succeeded")
	}
	output := errBuf.String()
	if !strings.Contains(output, "persisting stopped state:") || !strings.Contains(output, "db closed") {
		t.Errorf("expected 'persisting stopped state: db closed', got: %s", output)
	}
}

// TestStopCmdHandleResultNoProcessesSetEnabledError covers stopCmdHandleResult's
// persist call in the "nothing was running" branch failing.
func TestStopCmdHandleResultNoProcessesSetEnabledError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	fake := &stopCmdFakeManager{setEnabledErr: errors.New("db closed")}

	err := stopCmdHandleResult(cmd, "svc", fake, manager.StopServiceResult{})

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	output := errBuf.String()
	if !strings.Contains(output, "persisting stopped state:") || !strings.Contains(output, "db closed") {
		t.Errorf("expected 'persisting stopped state: db closed', got: %s", output)
	}
}

// TestStopCmdHandleResultAllStoppedSetEnabledError covers stopCmdHandleResult's
// persist call in the "everything stopped cleanly" branch failing.
func TestStopCmdHandleResultAllStoppedSetEnabledError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	fake := &stopCmdFakeManager{setEnabledErr: errors.New("db closed")}

	err := stopCmdHandleResult(cmd, "svc", fake, manager.StopServiceResult{Stopped: map[int]bool{123: true}})

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	output := errBuf.String()
	if !strings.Contains(output, "persisting stopped state:") || !strings.Contains(output, "db closed") {
		t.Errorf("expected 'persisting stopped state: db closed', got: %s", output)
	}
}

func TestStopCmdPrintStaleDataWarning(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	stopCmdPrintStaleDataWarning(cmd, 0)
	if errBuf.String() != "" {
		t.Errorf("expected no warning for zero stale data, got: %s", errBuf.String())
	}

	stopCmdPrintStaleDataWarning(cmd, 2)
	if !strings.Contains(errBuf.String(), "failed to update history for 2 process(es)") {
		t.Errorf("expected stale data warning, got: %s", errBuf.String())
	}
}

// Simulates a second, already-dead process registered against the same
// service (e.g. leftover history from a previous run) alongside the one
// real running process, to exercise the plural "stopped N processes"
// aggregation path.
func TestStopCommandMultipleProcesses(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "1s")

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("error occurred during writing the yaml file, got: %v\n", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755)
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}

	var outBuf strings.Builder
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	// Register a second, unreachable PGID as a leftover "running" process
	// for the same service; StopService should find it already dead.
	const deadPGID = 999999
	_, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, testFile.Name, types.ProcessStateRunning)
	if err != nil {
		t.Fatalf("RegisterProcessHistoryEntry: %v", err)
	}

	cmd.SetArgs([]string{"stop", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Stop command should not return an error, got : %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "stopped 2 processes") {
		t.Errorf("Expected stop to show 'stopped 2 processes', got: %s", output)
	}
}
