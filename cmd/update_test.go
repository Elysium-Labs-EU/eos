package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

// newYamlServiceFile writes a valid service.yaml into dir and returns its path.
func newYamlServiceFile(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("could not create dir %s: %v", dir, err)
	}
	data, err := yaml.Marshal(testutil.NewTestServiceConfigFile(t))
	if err != nil {
		t.Fatalf("failed to marshal service config: %v", err)
	}
	path := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}
	return path
}

func TestUpdateCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	firstPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v1"))
	cmd.SetArgs([]string{"add", firstPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}

	secondPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v2"))
	outBuf.Reset()
	cmd.SetArgs([]string{"update", "cms", secondPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("update: unexpected error: %v", err)
	}

	if !strings.Contains(outBuf.String(), "updated") {
		t.Errorf("expected 'updated' in output, got: %s", outBuf.String())
	}
}

// TODO: func TestUpdateCommandServiceNotRegistered
// TODO: func TestUpdateCommandIsRegisteredError (requires mock manager)
// TODO: func TestUpdateCommandInvalidPath
// TODO: func TestUpdateCommandUpdateCatalogError (requires mock manager)
// TODO: func TestUpdateCommandMissingArgs
// TODO: func TestUpdateCommandTooManyArgs
