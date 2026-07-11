package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestRemoveCommand(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

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

	cmd.SetArgs([]string{"add", fullPath})

	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"remove", "cms"})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Remove command should not return an error, got : %v", err)
	}
	output := outBuf.String()
	errOutput := errBuf.String()

	if !strings.Contains(output, "success cms unregistered") {
		t.Errorf("Expected remove to show 'success cms unregistered', got: %s", output)
	}
	if errOutput != "" {
		t.Errorf("Expected no errors', got: %s", errOutput)
	}
}

// TODO: func TestRemoveCommandServiceNotRegistered (no mock needed: just call remove without a prior add)
// TODO: func TestRemoveCommandIsRegisteredError (requires mock manager)
// TODO: func TestRemoveCommandWithActiveInstance (no mock needed: start a real instance, use
//       cmd.SetIn for the confirm/decline prompt, per the stdin-injection pattern in stop_test.go)
// TODO: func TestRemoveCommandRemoveInstanceError (requires mock manager)
// TODO: func TestRemoveCommandRemoveCatalogError (requires mock manager)
// TODO: func TestRemoveCommandMissingArgs (no mock needed: cobra.ExactArgs(1) rejects before RunE runs)
