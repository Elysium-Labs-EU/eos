package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/spf13/cobra"
)

func TestNewDaemonConfigOpenRC(t *testing.T) {
	logCfg := config.EosLogConfig{}

	t.Run("managed and not under supervisor delegates to OpenRC", func(t *testing.T) {
		cfg := newDaemonConfigOpenRC(t.TempDir(), true, false, "/etc/init.d/", logCfg)
		if cfg.OpenRC == nil {
			t.Fatal("expected OpenRC config when managed and not under supervisor")
		}
		if cfg.Standalone != nil {
			t.Error("expected Standalone to be nil when delegating to OpenRC")
		}
		if cfg.OpenRC.InitDir != "/etc/init.d/" || cfg.OpenRC.InitFileName != "eos" {
			t.Errorf("unexpected OpenRC config: %+v", cfg.OpenRC)
		}
	})

	t.Run("under supervisor runs standalone", func(t *testing.T) {
		// We ARE the supervised daemon: must run standalone in-process, not loop
		// back into rc-service.
		cfg := newDaemonConfigOpenRC(t.TempDir(), true, true, "/etc/init.d/", logCfg)
		if cfg.OpenRC != nil {
			t.Error("expected OpenRC to be nil when running under supervise-daemon")
		}
		if cfg.Standalone == nil {
			t.Fatal("expected Standalone config when under supervise-daemon")
		}
	})

	t.Run("not managed runs standalone", func(t *testing.T) {
		cfg := newDaemonConfigOpenRC(t.TempDir(), false, false, "/etc/init.d/", logCfg)
		if cfg.OpenRC != nil {
			t.Error("expected OpenRC to be nil when no init script is installed")
		}
		if cfg.Standalone == nil {
			t.Fatal("expected Standalone config when not OpenRC-managed")
		}
	})
}

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

func TestNewSystemConfigHelper_CreateBaseDirError(t *testing.T) {
	// A path whose parent is a regular file makes os.MkdirAll fail inside
	// CreateBaseDir, without needing root privileges to reach that branch.
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte(""), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EOS_BASE_DIR", filepath.Join(notADir, "eos"))

	_, _, _, _, err := newSystemConfig()
	if err == nil {
		t.Fatal("expected error when base dir cannot be created, got nil")
	}
}

func TestNewSystemConfigHelper_LoadEosConfigError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.EosConfigFileName), []byte("health: [not: valid"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EOS_BASE_DIR", dir)

	_, _, _, _, err := newSystemConfig()
	if err == nil {
		t.Fatal("expected error for invalid eos config, got nil")
	}
	if !strings.Contains(err.Error(), "loading eos config") {
		t.Errorf("expected 'loading eos config' in error, got: %v", err)
	}
}

func TestNewSystemConfigHelper_TelemetryEnabledWithoutEndpoint(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", t.TempDir())
	t.Setenv("EOS_OTEL_ENABLE", "true")
	t.Setenv("EOS_OTEL_ENDPOINT", "")

	_, _, _, _, err := newSystemConfig()
	if err == nil {
		t.Fatal("expected error when telemetry is enabled without an endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "telemetry") {
		t.Errorf("expected 'telemetry' in error, got: %v", err)
	}
}

// func TestNewRootCmd(t *testing.T) {}
// func TestGetManager(t *testing.T) {}
// func TestExecute(t *testing.T) {}
