package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestAddCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())

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
	if !strings.Contains(output, "eos run cms") {
		t.Errorf("Expected add to hint 'eos run cms', got: %s", output)
	}
	if strings.Contains(output, "eos start") {
		t.Errorf("Expected add not to reference removed 'eos start' command, got: %s", output)
	}
	isRegistered, err := db.IsServiceRegistered(t.Context(), "cms")
	if err != nil {
		t.Errorf("An error occurred during service registration check %s\n", err)
	}
	if !isRegistered {
		t.Error("The service was checked but not found to be registered")
	}
}

func TestAddNonexistentPathCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", "not-a-yaml"})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
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

func TestAddInvalidConfigRejected(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	testFile.Command = ""

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", fullPath})

	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "invalid service config") {
		t.Errorf("expected 'invalid service config' in output, got: %s", output)
	}

	isRegistered, err := db.IsServiceRegistered(t.Context(), "cms")
	if err != nil {
		t.Errorf("An error occurred during service registration check %s\n", err)
	}
	if isRegistered {
		t.Error("The service should not be registered for a config with an empty command")
	}
}

func TestAddInvalidYamlCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(manager)

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatal("add should return an error")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Errorf("expected error to mention 'accepts 1 arg(s), received 0', got: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for a cobra-level arg validation error, got: %s", buf.String())
	}
	isRegistered, err := db.IsServiceRegistered(t.Context(), "cms")
	if err != nil {
		t.Errorf("An error occurred during service registration check %s\n", err)
	}
	if isRegistered {
		t.Error("The service should not be registered")
	}
}
