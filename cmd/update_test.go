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

func TestUpdateCommand(t *testing.T) {
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
		t.Fatalf("preparing update test - add should not return an error, got: %v\n", err)
	}

	fullDirPath = filepath.Join(tempDir, "test-project-2")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	anotherTestFile := testutil.CreateTestServiceConfigFile(t)
	yamlData, err = yaml.Marshal(anotherTestFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullPath = filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"update", "cms", fullPath})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("status should not return an error, got: %v\n", err)
	}
	output := buf.String()

	if !strings.Contains(output, "Successfully updated the service") {
		t.Errorf("Expected update to show 'Successfully updated the service', got: %v\n", output)
	}
}
