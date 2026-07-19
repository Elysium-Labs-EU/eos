package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/spf13/cobra"
)

func TestNewManagerLocalMode(t *testing.T) {
	_, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	rootCmd := &cobra.Command{Use: "eos"}
	rootCmd.SetContext(t.Context())
	rootCmd.Flags().Bool("no-daemon", false, "")
	rootCmd.Flags().Bool("verbose", false, "")
	if err := rootCmd.Flags().Set("no-daemon", "true"); err != nil {
		t.Fatalf("setting no-daemon flag: %v", err)
	}

	mgr, cleanup, err := newManager(rootCmd, td, config.DaemonConfig{Standalone: nil}, nil)
	if err != nil {
		t.Fatalf("newManager should not error in local mode: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected a manager in local mode")
	}
	if cleanup == nil {
		t.Fatal("expected a cleanup func in local mode")
	}
	t.Cleanup(cleanup)
}

func setupCmd(t *testing.T) (cmd *cobra.Command, outBuf *bytes.Buffer, errBuf *bytes.Buffer, tempDir string) {
	t.Helper()
	db, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	t.Setenv("EOS_BASE_DIR", td)
	mgr := manager.NewLocalManager(db, td, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	c := newTestRootCmd(mgr)

	var ob, eb bytes.Buffer
	c.SetOut(&ob)
	c.SetErr(&eb)

	return c, &ob, &eb, td
}

func TestRootCommand(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Root command should not return an error, got: %v", err)
	}
	output := outBuf.String()

	if !strings.Contains(output, "eos - Test version") {
		t.Errorf("Expected output to contain 'eos - Test version', got %s", output)
	}
	if !strings.Contains(output, "Available Commands") {
		t.Errorf("Expected bare invocation to fall back to full help output, got: %s", output)
	}
}

func TestHelpCommand(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"--help"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("Help command should not return an error, go: %v", err)
	}
	output := outBuf.String()

	if !strings.Contains(output, "eos is a service supervisor") {
		t.Errorf("Expected help to contain description, got: '%s'", output)
	}
}

func TestNewSystemConfigHelper(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", t.TempDir())
	_, baseDir, _, _, err := newSystemConfig()

	if err != nil {
		t.Fatalf("Creating the system config should not throw an error")
	}
	if baseDir == "" {
		t.Fatalf("Basedir variable cannot be an empty string")
	}
}

// func TestNewRootCmd(t *testing.T) {}
// func TestGetManager(t *testing.T) {}
// func TestExecute(t *testing.T) {}
