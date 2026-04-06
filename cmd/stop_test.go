package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/testutil"
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

	cmd.SetIn(strings.NewReader("n\n"))

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

// TODO: Test force quit flag
// TODO: Test grace period stopping
// TODO: Test misbehaving processes

// func TestForceStopService(t *testing.T) {}
// func TestCleanupServiceInstance(t *testing.T) {}
