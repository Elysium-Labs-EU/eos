package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
)

func TestStopCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	testFile := testutil.CreateTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithRuntimePath(""))

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

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

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

	output := buf.String()

	if !strings.Contains(output, "stopped (1 processes)") {
		t.Errorf("Expected remove to show 'stopped (1 processes)', got: %s", output)
	}
	if !strings.Contains(output, "service instance cleaned up") {
		t.Errorf("Expected remove to show 'service instance cleaned up', got: %s", output)
	}
}

// TODO: Test misbehaving processes
