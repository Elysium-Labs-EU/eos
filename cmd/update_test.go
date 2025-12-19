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

func TestUpdateCommand(t *testing.T) {
	db, tempDir := testutil.SetupTestDB(t)
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
		t.Fatalf("preparing status test - add should not return an error, got: %v\n", err)
	} else {
		fullDirPath := filepath.Join(tempDir, "test-project-2")
		err := os.MkdirAll(fullDirPath, 0755)

		if err != nil {
			t.Fatalf("could not create test-project directory: %v\n", err)
			return
		}

		anotherTestFile := &types.ServiceConfig{
			Name:    "cms",
			Command: "/home/user/start-script.sh",
			Port:    1337,
		}

		yamlData, err := yaml.Marshal(anotherTestFile)
		if err != nil {
			t.Fatalf("Failed to marshal test config: %v", err)
		}

		fullPath := filepath.Join(fullDirPath, "service.yaml")
		os.WriteFile(fullPath, yamlData, 0644)

		cmd.SetArgs([]string{"update", "cms", fullPath})

		err = cmd.Execute()

		if err != nil {
			t.Fatalf("status should not return an error, got: %v\n", err)
		} else {
			output := buf.String()

			if !strings.Contains(output, "Successfully updated the service") {
				t.Errorf("Expected update to show 'Successfully updated the service', got: %v\n", output)
			}
		}
	}

}
