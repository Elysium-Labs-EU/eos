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

// TestCleanupTestDaemonTempDirs proves testDaemonConfig's os.MkdirTemp dirs
// actually get swept: it drives a real "daemon info" through newTestRootCmd
// (the only path that calls testDaemonConfig), confirms the dir it registers
// exists, then calls cleanupTestDaemonTempDirs directly — the same function
// TestMain invokes via goleak.Cleanup — and confirms both the directory and
// the registry entry are gone. Calling this via TestMain's post-m.Run() hook
// can't be measured by go test -cover (coverage is flushed before that hook
// runs), so this direct call is what makes the cleanup path count as tested.
func TestCleanupTestDaemonTempDirs(t *testing.T) {
	testDaemonTempDirsMu.Lock()
	before := len(testDaemonTempDirs)
	testDaemonTempDirsMu.Unlock()

	cmd := newTestRootCmd(nil)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"daemon", "info"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("daemon info: %v", err)
	}

	testDaemonTempDirsMu.Lock()
	dirs := append([]string(nil), testDaemonTempDirs...)
	testDaemonTempDirsMu.Unlock()
	if len(dirs) <= before {
		t.Fatalf("expected testDaemonConfig to register a new temp dir, had %d now have %d", before, len(dirs))
	}
	newDir := dirs[len(dirs)-1]
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("expected temp dir %s to exist before cleanup: %v", newDir, err)
	}

	cleanupTestDaemonTempDirs()

	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Errorf("expected temp dir %s to be removed after cleanup, stat err=%v", newDir, err)
	}
	testDaemonTempDirsMu.Lock()
	remaining := len(testDaemonTempDirs)
	testDaemonTempDirsMu.Unlock()
	if remaining != 0 {
		t.Errorf("expected testDaemonTempDirs to be cleared after cleanup, got %d entries", remaining)
	}
}

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

func TestResolveLinuxDaemonConfig(t *testing.T) {
	logCfg := config.EosLogConfig{}

	t.Run("systemd managed delegates to systemd", func(t *testing.T) {
		systemdDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(systemdDir, config.SystemdTargetFileName), []byte("[Unit]"), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())
		t.Setenv("EOS_SYSTEMD_TARGET_DIR", systemdDir+"/")
		t.Setenv("EOS_OPENRC_INIT_DIR", t.TempDir()+"/")
		// Clear a real INVOCATION_ID the test process may have inherited (e.g.
		// GitHub Actions Linux runners are themselves launched under systemd) —
		// otherwise config.IsUnderSystemd() sees "under systemd" and
		// newDaemonConfig's isSystemdManaged && !underSystemd branch never
		// fires, leaving cfg.Systemd nil.
		t.Setenv("INVOCATION_ID", "")

		cfg, err := resolveLinuxDaemonConfig(t.TempDir(), logCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Systemd == nil {
			t.Fatal("expected Systemd config when a systemd unit is installed")
		}
		if cfg.Standalone != nil {
			t.Error("expected Standalone to be nil when delegating to systemd")
		}
		if cfg.OpenRC != nil {
			t.Error("expected OpenRC to be nil when systemd wins")
		}
	})

	t.Run("openrc managed when systemd is not", func(t *testing.T) {
		initDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(initDir, config.OpenRCTargetFileName), []byte("#!/sbin/openrc-run"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())
		t.Setenv("EOS_SYSTEMD_TARGET_DIR", t.TempDir()+"/")
		t.Setenv("EOS_OPENRC_INIT_DIR", initDir+"/")

		cfg, err := resolveLinuxDaemonConfig(t.TempDir(), logCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.OpenRC == nil {
			t.Fatal("expected OpenRC config when an OpenRC init script is installed and systemd is not managed")
		}
		if cfg.Standalone != nil {
			t.Error("expected Standalone to be nil when delegating to OpenRC")
		}
		if cfg.Systemd != nil {
			t.Error("expected Systemd to be nil when OpenRC wins")
		}
	})

	t.Run("neither managed runs standalone", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("EOS_SYSTEMD_TARGET_DIR", t.TempDir()+"/")
		t.Setenv("EOS_OPENRC_INIT_DIR", t.TempDir()+"/")

		cfg, err := resolveLinuxDaemonConfig(t.TempDir(), logCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Standalone == nil {
			t.Fatal("expected Standalone config when nothing is installed")
		}
		if cfg.Systemd != nil || cfg.OpenRC != nil {
			t.Error("expected Systemd and OpenRC to both be nil when nothing is installed")
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

	mgr, cleanup, mode, err := newManager(rootCmd, td, config.DaemonConfig{Standalone: nil}, nil)
	if err != nil {
		t.Fatalf("newManager should not error in local mode: %v", err)
	}
	if mode != (localMode{}) {
		t.Errorf("expected a clean localMode with no daemon configured, got %+v", mode)
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
	c, ob, eb, td, _ := setupCmdWithManager(t)
	return c, ob, eb, td
}

// setupCmdWithManager is setupCmd plus the underlying *manager.LocalManager,
// for tests that need to start a service directly (via startServiceForTest)
// rather than through the now-blocking "eos run" command.
func setupCmdWithManager(t *testing.T) (cmd *cobra.Command, outBuf *bytes.Buffer, errBuf *bytes.Buffer, tempDir string, mgr *manager.LocalManager) {
	t.Helper()
	db, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	t.Setenv("EOS_BASE_DIR", td)
	mgr = manager.NewLocalManager(db, td, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	c := newTestRootCmd(mgr)

	var ob, eb bytes.Buffer
	c.SetOut(&ob)
	c.SetErr(&eb)

	return c, &ob, &eb, td, mgr
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
