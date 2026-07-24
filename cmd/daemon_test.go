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
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
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
func (f *fakeDaemonController) IsRunning(_ context.Context) bool     { return true }
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

// TestForkDaemonAlreadyRunning covers issue #156 bug 2: forkDaemon must refuse
// to fork (and report a clear error) when a live daemon already holds the PID
// file, instead of spawning a redundant child that fails to bind and reporting
// false success.
func TestForkDaemonAlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")

	if err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", os.Getpid()), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	cfg := &config.StandaloneDaemonConfig{PIDFile: pidFile, SocketPath: filepath.Join(tempDir, "eos.sock")}
	err = forkDaemon(t.Context(), cfg, false, identity)
	if err == nil {
		t.Fatal("expected error when daemon is already running")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

// TestEnsureDaemonNotRunning_NotRunningReturnsNil is the fast-path companion
// to TestEnsureDaemonNotRunning_AlreadyRunning and
// TestEnsureDaemonNotRunning_StatusError: no PID file at all must be a no-op.
func TestEnsureDaemonNotRunning_NotRunningReturnsNil(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.StandaloneDaemonConfig{PIDFile: filepath.Join(tempDir, "eos.pid")}

	if err := ensureDaemonNotRunning(cfg); err != nil {
		t.Errorf("expected nil when no daemon is running, got %v", err)
	}
}

// TestEnsureDaemonNotRunning_AlreadyRunning is the unit-level companion to
// TestForkDaemonAlreadyRunning.
func TestEnsureDaemonNotRunning_AlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	if err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", os.Getpid()), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	err := ensureDaemonNotRunning(&config.StandaloneDaemonConfig{PIDFile: pidFile})
	if err == nil {
		t.Fatal("expected error when daemon is already running")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

// TestEnsureDaemonNotRunning_StatusError covers the status-check-failed
// branch: a PID file path that isn't a readable regular file (here, a
// directory) must surface a wrapped error, not be treated as "not running".
func TestEnsureDaemonNotRunning_StatusError(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.StandaloneDaemonConfig{PIDFile: tempDir} // a directory, not a file

	err := ensureDaemonNotRunning(cfg)
	if err == nil {
		t.Fatal("expected error when the pid file path is unreadable")
	}
	if !strings.Contains(err.Error(), "checking daemon status") {
		t.Errorf("expected status-check error wrapper, got: %v", err)
	}
}

// TestSpawnForkedDaemon_BuildCommandError covers the buildForkCommand-failure
// branch: a PID file in a nonexistent directory makes OpenForkStderrLog fail.
func TestSpawnForkedDaemon_BuildCommandError(t *testing.T) {
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	pidFile := filepath.Join(t.TempDir(), "nonexistent-subdir", "eos.pid")
	err = spawnForkedDaemon(t.Context(), "/usr/bin/true", false, identity, pidFile)
	if err == nil {
		t.Fatal("expected error when the fork stderr log can't be opened")
	}
}

// TestSpawnForkedDaemon_StartError covers the cmd.Start()-failure branch: a
// nonexistent executable path can't be exec'd.
func TestSpawnForkedDaemon_StartError(t *testing.T) {
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	nonexistentExe := filepath.Join(tempDir, "nonexistent-eos-binary")

	err = spawnForkedDaemon(t.Context(), nonexistentExe, false, identity, pidFile)
	if err == nil {
		t.Fatal("expected error when exePath doesn't exist")
	}
	if !strings.Contains(err.Error(), "failed to start daemon process") {
		t.Errorf("expected start-failure error, got: %v", err)
	}
}

// spawnForkedDaemon's happy path (a real Start() that succeeds) is
// intentionally not unit-tested here: os/exec's ctx-watcher goroutine only
// exits once its context is done AND the process is Wait()'ed, which
// spawnForkedDaemon deliberately never does (the daemon must outlive this
// process) — exercising that path in-process leaks a goroutine that goleak
// (cmd/main_test.go) flags. It's covered instead by the real-process manual
// verification in this fix's PR description (build the binary, run
// `eos daemon start --detach` end to end).

// TestBuildForkCommandStderrIsRealFile covers issue #156 bug 1: the forked
// child's stderr must be a real *os.File beside the PID file, not an
// in-process io.Writer. A non-*os.File Stderr makes os/exec create a real OS
// pipe whose read end lives in this (short-lived) process; once this process
// exits, that pipe is orphaned and the detached child gets SIGPIPE'd on its
// next stderr write.
func TestBuildForkCommandStderrIsRealFile(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	cmd, stderrFile, err := buildForkCommand(t.Context(), "/usr/bin/true", false, identity, pidFile)
	if err != nil {
		t.Fatalf("buildForkCommand should not error: %v", err)
	}
	defer func() { _ = stderrFile.Close() }()

	if _, ok := cmd.Stderr.(*os.File); !ok {
		t.Errorf("expected cmd.Stderr to be a real *os.File, got %T", cmd.Stderr)
	}

	wantPath := filepath.Join(tempDir, "fork-stderr.log")
	if stderrFile.Name() != wantPath {
		t.Errorf("expected stderr capture file at %s, got %s", wantPath, stderrFile.Name())
	}
}

// TestWaitForForkPIDFileReadyReturnsNil is the fast-path companion to
// TestWaitForForkPIDFileTimesOutOnDeadPID: a PID file naming a live process
// must succeed immediately.
func TestWaitForForkPIDFileReadyReturnsNil(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")

	if err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", os.Getpid()), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	if err := waitForForkPIDFile(pidFile); err != nil {
		t.Errorf("waitForForkPIDFile should return nil when the daemon is running, got %v", err)
	}
}

// TestWaitForForkPIDFileTimesOutOnDeadPID covers issue #156 bug 1: a PID file
// can exist for an instant before the process that wrote it dies (e.g. killed
// by an orphaned stderr pipe). waitForForkPIDFile must re-verify the process is
// actually alive rather than declaring success from mere file existence.
func TestWaitForForkPIDFileTimesOutOnDeadPID(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")

	// A PID that almost certainly doesn't exist.
	if err := os.WriteFile(pidFile, []byte("9999999"), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	err := waitForForkPIDFile(pidFile)
	if err == nil {
		t.Fatal("expected waitForForkPIDFile to time out for a pid file naming a dead process, got nil")
	}
	if !strings.Contains(err.Error(), "timed out waiting for PID file") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// TestWaitForForkSocketTimesOutWhenNothingListens covers issue #156 bug 1's
// second race: a PID file can exist — and its process still be alive — for a
// brief window before an unrelated startup failure (e.g. a socket bind error)
// kills it moments later. waitForForkSocket must not return until the socket
// actually answers.
func TestWaitForForkSocketTimesOutWhenNothingListens(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "eos.sock")

	err := waitForForkSocket(t.Context(), socketPath)
	if err == nil {
		t.Fatal("expected waitForForkSocket to time out when nothing listens on the socket")
	}
	if !strings.Contains(err.Error(), "timed out waiting for daemon socket") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// TestForkStartupErrFoldsCapturedStderr covers the diagnostic path: a fork
// startup failure should fold its captured stderr into the returned error.
func TestForkStartupErrFoldsCapturedStderr(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")

	stderrFile, err := manager.OpenForkStderrLog(pidFile)
	if err != nil {
		t.Fatalf("OpenForkStderrLog: %v", err)
	}
	if _, writeErr := stderrFile.WriteString("child boom"); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}
	if closeErr := stderrFile.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}

	wrapped := forkStartupErr(errors.New("timed out waiting for PID file: "+pidFile), pidFile)
	if !strings.Contains(wrapped.Error(), "child boom") {
		t.Errorf("expected captured stderr folded into error, got: %v", wrapped)
	}

	emptyPidFile := filepath.Join(t.TempDir(), "empty.pid")
	if bare := forkStartupErr(errors.New("timed out"), emptyPidFile); strings.Contains(bare.Error(), "child stderr") {
		t.Errorf("expected no stderr section when capture file doesn't exist, got %v", bare)
	}
}
