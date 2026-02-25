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
	"eos/internal/types"
)

func TestLogsCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	testFile := testutil.CreateTestServiceConfigFile(t)

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

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"start", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"logs", testFile.Name, "--follow=false"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}

	output := buf.String()

	if strings.Contains(output, "An error occurred during getting the log file, got") {
		t.Errorf("Log file should be found")
	}
	if !strings.Contains(output, "run: eos start cms to start it") {
		t.Errorf("Service should not have started")
	}
}

func TestLogsNeverRanServiceCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "./start-script.sh",
		Port:    1337,
	}

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

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"logs", testFile.Name, "--follow=false"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "has never been started") {
		t.Errorf("Expected status to show 'has never been started', got: %s", output)
	}
	if strings.Contains(output, "An error occurred during getting the log file, got") {
		t.Errorf("Log file should be found")
	}
}

func TestLogsNonExistingServiceCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"logs", "cms", "--follow=false"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Logs command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "error cms is not registered") {
		t.Errorf("Expected status to show 'error cms is not registered', got: %s", output)
	}
}
