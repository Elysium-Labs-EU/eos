package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestRestartCommand(t *testing.T) {
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
		t.Fatalf("add should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"start", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	output := outBuf.String()

	if !strings.Contains(output, "started with PGID:") {
		t.Errorf("The start command didn't complete successfully, no PGID was returned")
	}

	outBuf.Reset()
	cmd.SetArgs([]string{"restart", testFile.Name})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Restart command execution error, got: %v", err)
	}

	output = outBuf.String()

	if !strings.Contains(output, "restarted with PGID:") {
		t.Fatalf("The restart command didn't complete successfully, no PGID was returned")
	}
}
