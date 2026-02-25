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

func TestAddCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

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

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", fullPath})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("add should not return an error, got: %v\n", err)
	}
	output := buf.String()

	if !strings.Contains(output, "success") {
		t.Errorf("Expected add to show 'success', got: %s", output)
	}
	isRegistered, err := db.IsServiceRegistered(t.Context(), "cms")
	if err != nil {
		t.Errorf("An error occurred during service registration check %s\n", err)
	}
	if !isRegistered {
		t.Error("The service was checked but not found to be registered")
	}
}

func TestAddIncompleteCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", "not-a-yaml"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("add should not return an error, got: %v\n", err)
	}
	output := buf.String()

	if !strings.Contains(output, "directory or file on path not-a-yaml does not exist") {
		t.Errorf("Expected add to show 'directory or file on path not-a-yaml does not exist', got: %s", output)
	}
	isRegistered, err := db.IsServiceRegistered(t.Context(), "cms")
	if err != nil {
		t.Errorf("An error occurred during service registration check %s\n", err)
	}
	if isRegistered {
		t.Error("The service should not be registered")
	}
}

func TestAddInvalidYamlCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatal("add should return an error")
	}
	output := buf.String()

	if !strings.Contains(output, "Error: accepts 1 arg(s), received 0") {
		t.Errorf("Expected add to show 'Error: accepts 1 arg(s), received 0', got: %s", output)
	}
	isRegistered, err := db.IsServiceRegistered(t.Context(), "cms")
	if err != nil {
		t.Errorf("An error occurred during service registration check %s\n", err)
	}
	if isRegistered {
		t.Error("The service should not be registered")
	}
}
