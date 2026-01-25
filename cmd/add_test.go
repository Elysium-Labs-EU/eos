package cmd

import (
	"bytes"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/testutil"
	"eos/internal/types"
	"strings"

	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAddCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
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

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", fullPath})

	err = cmd.Execute()

	if err != nil {
		t.Fatalf("add should not return an error, got: %v\n", err)
	}
	output := buf.String()

	if !strings.Contains(output, "Successfully registered service") {
		t.Errorf("Expected add to show 'Successfully registered service', got: %s", output)
	}
	isRegistered, err := db.IsServiceRegistered("cms")
	if err != nil {
		t.Errorf("An error occured during service registration check %s\n", err)
	}
	if !isRegistered {
		t.Error("The service was checked but not found to be registered")
	}
}
