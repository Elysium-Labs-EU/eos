package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/process"
	"github.com/spf13/cobra"
)

func setupDaemonTestEnv(t *testing.T) (string, config.SystemConfig) {
	t.Helper()
	tempDir := t.TempDir()

	cfg := config.SystemConfig{
		Daemon: config.DaemonConfig{
			Standalone: &config.StandaloneDaemonConfig{
				PIDFile:    filepath.Join(tempDir, "eos.pid"),
				SocketPath: filepath.Join(tempDir, "eos.sock"),
				Log: config.DaemonLogConfig{
					LogFileName: "daemon.log",
				},
			},
			Systemd: nil,
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

	status, err := process.StatusStandaloneDaemon(cfg.Daemon.Standalone)
	if err != nil {
		t.Fatalf("StatusDaemon should not error when pid file is absent, got: %v", err)
	}
	if status.Running {
		t.Error("Daemon should not be reported as running when no pid file exists")
	}
}

func TestDaemonStatusWithStalePidFile(t *testing.T) {
	_, cfg := setupDaemonTestEnv(t)

	err := os.WriteFile(cfg.Daemon.Standalone.PIDFile, []byte("9999999"), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	status, err := process.StatusStandaloneDaemon(cfg.Daemon.Standalone)
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
	err := os.WriteFile(cfg.Daemon.Standalone.PIDFile, fmt.Appendf(nil, "%d", pid), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	status, err := process.StatusStandaloneDaemon(cfg.Daemon.Standalone)
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

	killed, err := process.StopStandaloneDaemon(cfg.Daemon.Standalone.PIDFile, cfg.Daemon.Standalone.SocketPath)
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

	logPath := filepath.Join(logDir, cfg.Daemon.Standalone.Log.LogFileName)
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

	logPath := filepath.Join(manager.CreateLogDirPath(tempDir), cfg.Daemon.Standalone.Log.LogFileName)
	_, err := os.Stat(logPath)
	if err == nil {
		t.Error("Log file should not exist before daemon has run")
	}
}

func TestDaemonInfoCommandOutput(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"daemon", "info"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("daemon status should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "not found") && !strings.Contains(output, "running") {
		t.Errorf("daemon status should report running state, got: %s", output)
	}
}

func TestDaemonInfoAllRequiresRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping: running as root, this test exercises the non-root rejection path")
	}

	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"daemon", "info", "--all"})

	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("daemon info --all should error when not run as root")
	}

	if !strings.Contains(errBuf.String(), "requires root") {
		t.Errorf("expected error output to mention 'requires root', got: %s", errBuf.String())
	}
}

func TestRenderDaemonSummaries(t *testing.T) {
	pidRunning := 111
	pidStale := 222

	daemons := []process.DaemonSummary{
		{Username: "alice", Err: errors.New("reading pid file: permission denied")},
		{Username: "bob", Status: &process.DaemonStatus{Running: false}},
		{Username: "carol", Status: &process.DaemonStatus{Running: true, Pid: &pidRunning}},
		{Username: "dave", Status: &process.DaemonStatus{Running: true, Pid: &pidStale}, StaleBinary: true},
	}

	cmd, outBuf, errBuf, _ := setupCmd(t)
	renderDaemonSummaries(cmd, daemons)
	output := outBuf.String() + errBuf.String()

	for _, want := range []string{
		"alice", "permission denied",
		"bob", "not running",
		"carol", "111",
		"dave", "222", "since-replaced binary",
		"1 daemon(s) still running the pre-update binary",
		"eos daemon stop",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got: %s", want, output)
		}
	}
}

func TestRenderDaemonSummariesEmpty(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	renderDaemonSummaries(cmd, nil)

	if !strings.Contains(outBuf.String(), "no standalone daemons found") {
		t.Errorf("expected empty-list message, got: %s", outBuf.String())
	}
}

func TestDaemonStopCommandOutput(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"daemon", "stop"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("daemon stop should not return a cobra error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "daemon was not running") && !strings.Contains(output, "stopping daemon...") {
		t.Errorf("daemon stop should report outcome, got: %s", output)
	}
}

func TestDaemonPidFilePermission_Bug(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping: running as root, bug only affects non-root users")
	}

	prodPidFile := "/var/run/eos.pid"

	err := os.WriteFile(prodPidFile, []byte("12345"), 0644)

	if err == nil {
		removeErr := os.Remove(prodPidFile)
		if removeErr != nil {
			t.Fatalf("Removing of pid test file should not error, got: %v", removeErr)
		}
		t.Skip("Write to /var/run succeeded - unusual permissions on this system")
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

// fakeDaemonController records Start calls and returns a configured error.
type fakeDaemonController struct {
	startErr    error
	startCalled bool
	detachArg   bool
}

func (f *fakeDaemonController) Start(_ context.Context, detach bool, _ bool, _ bool) error {
	f.startCalled = true
	f.detachArg = detach
	return f.startErr
}

func (f *fakeDaemonController) Stop(_ context.Context, _ *cobra.Command, _ bool) (bool, error) {
	return false, nil
}
func (f *fakeDaemonController) Remove() error                        { return nil }
func (f *fakeDaemonController) Info(_ *cobra.Command)                {}
func (f *fakeDaemonController) Logs(_ *cobra.Command, _ int, _ bool) {}
func (f *fakeDaemonController) LogsHint() string                     { return "" }

func newTestDaemonCmd(ctrl DaemonController) *cobra.Command {
	parent := &cobra.Command{Use: "daemon"}
	buildDaemonSubcmds(parent, func() DaemonController { return ctrl })
	return parent
}

func TestDaemonStartDefaultsToBackground(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.startCalled {
		t.Fatal("expected Start to be called")
	}
	if !fake.detachArg {
		t.Fatal("expected detach=true by default")
	}
	if !strings.Contains(out.String(), "background") {
		t.Errorf("expected 'background' in output, got: %s", out.String())
	}
}

func TestDaemonStartForegroundFlag(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start", "--foreground"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.startCalled {
		t.Fatal("expected Start to be called")
	}
	if fake.detachArg {
		t.Fatal("expected detach=false for --foreground flag")
	}
	if !strings.Contains(out.String(), "foreground") {
		t.Errorf("expected 'foreground' in output, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Ctrl-C") {
		t.Errorf("expected Ctrl-C warning in output, got: %s", out.String())
	}
}

func TestDaemonStartForegroundShortFlag(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start", "-f"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.detachArg {
		t.Fatal("expected detach=false for -f flag")
	}
}

func TestDaemonStartDetachLongFlagIsNoOp(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start", "--detach"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.startCalled {
		t.Fatal("expected Start to be called")
	}
	if !fake.detachArg {
		t.Fatal("expected detach=true for --detach flag (kept for backward compatibility)")
	}
	if !strings.Contains(out.String(), "background") {
		t.Errorf("expected 'background' in output, got: %s", out.String())
	}
}

func TestDaemonStartDetachShortFlagIsNoOp(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start", "-d"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.detachArg {
		t.Fatal("expected detach=true for -d flag")
	}
}

func TestDaemonStartConflictingFlags(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start", "--foreground", "--detach"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if fake.startCalled {
		t.Fatal("expected Start not to be called on conflicting flags")
	}
	if !strings.Contains(errOut.String(), "cannot use --foreground and --detach together") {
		t.Errorf("expected conflicting flags error, got: %s", errOut.String())
	}
}

func TestDaemonStartError(t *testing.T) {
	fake := &fakeDaemonController{startErr: errors.New("boom")}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Errorf("expected error message in stderr, got: %s", errOut.String())
	}
}

func TestDaemonStartDetachSuccessOutput(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"start", "--detach"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "daemon started in background") {
		t.Errorf("expected success message, got: %s", out.String())
	}
}
