package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
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

	cmd.SetArgs([]string{"start", testFile.Name})
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

	cmd.SetArgs([]string{"start", testFile.Name})
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

	cmd.SetArgs([]string{"start", testFile.Name})
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
	cmd.SetArgs([]string{"start", testFile.Name})
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

// TODO: Test force-quit decline path ("n" answer -> "force quit aborted" ->
// ErrCommandFailed). Requires a process that survives the graceful-stop grace
// period so countError > 0. The existing stubborn/SIGTERM-trap script pattern
// (see TestStopCommandGracePeriod) does not reliably survive when started via
// LocalManager.StartService in this environment - it dies well within the
// grace period regardless of the trap, so TestStopCommandGracePeriod's "y"
// answer is never actually exercised there either (its assertions happen to
// match the plain success path too, masking this). Needs a more reliable way
// to force a process into the errored/still-alive-past-grace-period state
// before this can be tested.

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
	cmd.SetArgs([]string{"start", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	// Register a second, unreachable PGID as a leftover "running" process
	// for the same service; StopService should find it already dead.
	const deadPGID = 999999
	_, err = db.RegisterProcessHistoryEntry(t.Context(), deadPGID, testFile.Name, types.ProcessStateRunning)
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
