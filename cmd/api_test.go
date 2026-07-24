package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// setupAPICmd is a shared fixture for api_*_test.go: it wires a fresh test
// DB and manager into the root cobra command and returns it with captured
// stdout/stderr buffers and the manager's temp working dir.
func setupAPICmd(t *testing.T) (cmd *cobra.Command, outBuf *bytes.Buffer, errBuf *bytes.Buffer, tempDir string) {
	t.Helper()
	db, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, td, t.Context(), testutil.NewTestLogger(t))
	c := newTestRootCmd(mgr)

	var ob, eb bytes.Buffer
	c.SetOut(&ob)
	c.SetErr(&eb)

	return c, &ob, &eb, td
}

// writeServiceFiles is a shared fixture for api_*_test.go: it marshals cfg
// into a service.yaml under dir/test-project and returns the yaml path.
func writeServiceFiles(t *testing.T, dir string, cfg any) (yamlPath string) {
	t.Helper()

	fullDir := filepath.Join(dir, "test-project")
	if err := os.MkdirAll(fullDir, 0755); err != nil {
		t.Fatalf("could not create test dir: %v", err)
	}

	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("could not marshal config: %v", err)
	}

	yamlPath = filepath.Join(fullDir, "service.yaml")
	if err = os.WriteFile(yamlPath, yamlData, 0644); err != nil {
		t.Fatalf("could not write yaml: %v", err)
	}

	return yamlPath
}

func TestAPICommand(t *testing.T) {
	cmd, outBuf, _, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("API root command should not return an error, got: %v", err)
	}
	output := outBuf.String()

	if !strings.Contains(output, "Machine-readable JSON interface") {
		t.Errorf("Expected output to contain 'Machine-readable JSON interface', got %s", output)
	}
	if !strings.Contains(output, "eos api") {
		t.Errorf("Expected output to contain api text, got: %s", output)
	}
}
