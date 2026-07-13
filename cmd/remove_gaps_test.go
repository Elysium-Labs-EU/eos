package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestRemoveCommandServiceNotRegistered(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"remove", "does-not-exist"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for an unregistered service")
	}
	if !strings.Contains(errBuf.String(), "is not registered") {
		t.Errorf("expected 'is not registered' error, got: %s", errBuf.String())
	}
}

func TestRemoveCommandMissingArgs(t *testing.T) {
	cmd, _, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"remove"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for missing arguments")
	}
}

// addAndRunService writes a service.yaml + a long-lived start script under tempDir, registers
// it, and starts it via "run" (not the deprecated "start" command), returning its name.
func addAndRunService(t *testing.T, cmd *cobra.Command, tempDir string) string {
	t.Helper()
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal service config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}
	script := "#!/bin/bash\nexec sleep 3600"
	if err := os.WriteFile(filepath.Join(fullDirPath, "start-script.sh"), []byte(script), 0755); err != nil {
		t.Fatalf("failed to write start script: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: unexpected error: %v", err)
	}
	return testFile.Name
}

func TestRemoveCommandWithActiveInstance_Decline(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	name := addAndRunService(t, cmd, tempDir)

	outBuf.Reset()
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"remove", name})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when the remove is aborted")
	}
	if !strings.Contains(outBuf.String(), "remove aborted") {
		t.Errorf("expected 'remove aborted', got: %s", outBuf.String())
	}
}

func TestRemoveCommandWithActiveInstance_Confirm(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	name := addAndRunService(t, cmd, tempDir)

	outBuf.Reset()
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"remove", name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "unregistered") {
		t.Errorf("expected 'unregistered', got: %s", outBuf.String())
	}
}
