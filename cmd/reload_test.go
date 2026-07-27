package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

// TestReloadCommandUnregistered checks reload rejects a service that was never
// registered with a clear, actionable message instead of attempting a cutover.
func TestReloadCommandUnregistered(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"reload", "does-not-exist"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("reload of unregistered service should fail, got: %v", err)
	}
	if out := errBuf.String(); !strings.Contains(out, "is not registered") {
		t.Errorf("expected 'is not registered' message, got: %s", out)
	}
}

// TestReloadCommandRequiresDaemon checks that reload against an in-process
// (non-daemon) manager is refused: the cutover launches and health-gates a
// second instance inside the supervising daemon, so it can't run against a
// short-lived CLI manager. The test root wires a LocalManager, which does not
// implement the daemon-backed reload capability.
func TestReloadCommandRequiresDaemon(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	cfgFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(cfgFile)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dir := filepath.Join(tempDir, "reload-proj")
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		t.Fatalf("mkdir: %v", mkErr)
	}
	yamlPath := filepath.Join(dir, "service.yaml")
	if writeErr := os.WriteFile(yamlPath, yamlData, 0644); writeErr != nil {
		t.Fatalf("write yaml: %v", writeErr)
	}

	cmd.SetArgs([]string{"add", yamlPath})
	if addErr := cmd.ExecuteContext(t.Context()); addErr != nil {
		t.Fatalf("add should not error: %v", addErr)
	}

	cmd.SetArgs([]string{"reload", cfgFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("reload without a daemon should fail, got: %v", err)
	}
	if out := errBuf.String(); !strings.Contains(out, "requires the standalone eos daemon") {
		t.Errorf("expected daemon-required message, got: %s", out)
	}
}
