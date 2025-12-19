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

func TestRemoveCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
	manager := manager.NewLocalManager(db, tempDir)
	cmd := newTestRootCmd(manager)
	var buf bytes.Buffer

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

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", fullPath})

	err = cmd.Execute()

	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"remove", "cms"})
	err = cmd.Execute()

	if err != nil {
		t.Fatalf("Remove command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "Successfully removed service") {
		t.Errorf("Expected remove to show 'Successfully removed service', got: %s", output)
	}
}
