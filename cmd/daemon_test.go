package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/process"
	"eos/internal/testutil"
)

func setupDaemonTestEnv(t *testing.T) (string, config.SystemConfig) {
	t.Helper()
	tempDir := t.TempDir()

	cfg := config.SystemConfig{
		Daemon: config.DaemonConfig{
			PIDFile:     filepath.Join(tempDir, "eos.pid"),
			SocketPath:  filepath.Join(tempDir, "eos.sock"),
			LogFileName: "daemon.log",
		},
		Health: config.HealthConfig{
			MaxRestart: 10,
			Timeout: config.TimeOutConfig{
				Enable: true,
				Limit:  time.Second * 10,
			},
		},
	}

	return tempDir, cfg
}

func TestDaemonStatusNoPidFile(t *testing.T) {
	_, cfg := setupDaemonTestEnv(t)

	status, err := process.StatusDaemon(cfg.Daemon)
	if err != nil {
		t.Fatalf("StatusDaemon should not error when pid file is absent, got: %v", err)
	}
	if status.Running {
		t.Error("Daemon should not be reported as running when no pid file exists")
	}
}

func TestDaemonStatusWithStalePidFile(t *testing.T) {
	_, cfg := setupDaemonTestEnv(t)

	err := os.WriteFile(cfg.Daemon.PIDFile, []byte("9999999"), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	status, err := process.StatusDaemon(cfg.Daemon)
	if err != nil {
		t.Fatalf("StatusDaemon should not error for stale pid, got: %v", err)
	}

	if status.Running {
		t.Error("Daemon should not be reported as running for a dead PID")
	}
}

func TestDaemonStatusWithLiveProcess(t *testing.T) {
	_, cfg := setupDaemonTestEnv(t)

	pid := os.Getpid()
	err := os.WriteFile(cfg.Daemon.PIDFile, fmt.Appendf(nil, "%d", pid), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	status, err := process.StatusDaemon(cfg.Daemon)
	if err != nil {
		t.Fatalf("StatusDaemon should not error, got: %v", err)
	}
	if !status.Running {
		t.Error("Daemon should be reported as running for a live PID")
	}
	if status.Pid == nil || *status.Pid != pid {
		t.Errorf("Expected PID %d, got %v", pid, status.Pid)
	}
}

func TestDaemonStopNoPidFile(t *testing.T) {
	_, cfg := setupDaemonTestEnv(t)

	killed, err := process.StopDaemon(cfg.Daemon)
	if err != nil {
		t.Fatal("StopDaemon should not error when pid file doesn't exist")
	}
	if killed {
		t.Fatal("StopDaemon should not return killed 'true' when pid file doesn't exist")
	}
}

func TestDaemonLogsFileExists(t *testing.T) {
	tempDir, cfg := setupDaemonTestEnv(t)

	logDir := manager.CreateLogDirPath(tempDir)
	err := os.MkdirAll(logDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create log dir: %v", err)
	}

	logPath := filepath.Join(logDir, cfg.Daemon.LogFileName)
	err = os.WriteFile(logPath, []byte("test log line\n"), 0644)
	if err != nil {
		t.Fatalf("Failed to write log file: %v", err)
	}

	_, err = os.Stat(logPath)
	if err != nil {
		t.Errorf("Log file should be readable, got: %v", err)
	}
}

func TestDaemonLogsFileMissing(t *testing.T) {
	tempDir, cfg := setupDaemonTestEnv(t)

	logPath := filepath.Join(manager.CreateLogDirPath(tempDir), cfg.Daemon.LogFileName)
	_, err := os.Stat(logPath)
	if err == nil {
		t.Error("Log file should not exist before daemon has run")
	}
}

func TestDaemonStatusCommandOutput(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(mgr)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"daemon", "status"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("daemon status should not return an error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "not found") && !strings.Contains(output, "running") {
		t.Errorf("daemon status should report running state, got: %s", output)
	}
}

func TestDaemonStopCommandOutput(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	cmd := newTestRootCmd(mgr)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"daemon", "stop"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("daemon stop should not return a cobra error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "daemon was not running") && !strings.Contains(output, "stopping daemon...") {
		t.Errorf("daemon stop should report outcome, got: %s", output)
	}
}

func TestDaemonPidFilePermission_Bug(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping: running as root, bug only affects non-root users")
	}

	prodPidFile := "/var/run/eos.pid"

	// Try to write the PID file exactly as the daemon would.
	err := os.WriteFile(prodPidFile, []byte("12345"), 0644)

	if err == nil {
		removeErr := os.Remove(prodPidFile)
		if removeErr != nil {
			t.Fatalf("Removing of pid test file should not error, got: %v", removeErr)
		}
		t.Skip("Write to /var/run succeeded — unusual permissions on this system")
	}

	if !os.IsPermission(err) {
		t.Fatalf("Expected permission denied, got: %v", err)
	}

	_, statErr := os.Stat(prodPidFile)
	if statErr == nil {
		t.Error("PID file should not exist since write was denied")
	}

	t.Logf("BUG CONFIRMED: non-root cannot write to %s (%v). "+
		"PID/socket paths must move to a user-writable location.", prodPidFile, err)
}
