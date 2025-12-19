package cmd

import (
	"bytes"
	"deploy-cli/internal/manager"
	"deploy-cli/internal/testutil"
	"deploy-cli/internal/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLogsCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	runtime := types.Runtime{
		Type: "nodejs",
	}
	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "./start-script.sh",
		Port:    1337,
		Runtime: runtime,
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
	os.WriteFile(fullPath, yamlData, 0644)

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"start", testFile.Name})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Start command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"logs", testFile.Name, "--follow=false"})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "Checking the logs for cms") {
		t.Errorf("Expected status to show 'Checking the logs for cms', got: %s", output)
	}
	if strings.Contains(output, "An error occured during getting the log file, got") {
		t.Errorf("Log file should be found")
	}
	if !strings.HasSuffix(output, "Checking the logs for cms \n") {
		t.Errorf("Log file should be empty")
	}
}

func TestLogsNeverRanServiceCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
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
	os.WriteFile(fullPath, yamlData, 0644)

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"logs", testFile.Name, "--follow=false"})
	err = cmd.Execute()
	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "Checking the logs for cms") {
		t.Errorf("Expected status to show 'Checking the logs for cms', got: %s", output)
	}
	if strings.Contains(output, "An error occured during getting the log file, got") {
		t.Errorf("Log file should be found")
	}
}

func TestLogsNonExistingServiceCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"logs", "cms", "--follow=false"})

	err := cmd.Execute()

	if err != nil {
		t.Fatalf("Logs command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "Checking the logs for cms") {
		t.Errorf("Expected status to show 'Checking the logs for cms', got: %s", output)
	}
	if !strings.Contains(output, "Service 'cms' is not registered") {
		t.Errorf("Expected status to show 'Service 'cms' is not registered', got: %s", output)
	}
}
