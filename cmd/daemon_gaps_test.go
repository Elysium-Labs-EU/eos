package cmd

// This file closes coverage gaps on cmd/daemon.go's "new code" lines flagged by
// the SonarCloud quality gate for this branch's diff. Every test here drives
// real code paths (real subprocesses, real cobra/pflag error handling, real
// filesystem state) rather than mocking interfaces, matching this repo's
// existing testing style in daemon_test.go / daemon_controller_test.go.
//
// A few branches are documented as impractical to cover here and are skipped
// with an explanation rather than forced:
//   - tailDaemonLogFile's StdoutPipe() error (cmd/daemon.go ~line 156): exec.Cmd's
//     StdoutPipe only errors if Stdout is already set or the command has already
//     started; tailDaemonLogFile's local, freshly built *exec.Cmd never satisfies
//     either condition, so this branch is unreachable without changing production
//     code to inject a pipe failure.
//   - buildDaemonSubcmds' startCmd.Flags().MarkHidden error (~line 535): the
//     immediately preceding line unconditionally registers the same flag name, so
//     MarkHidden can never fail in practice.
//   - resolveDaemonControllerPreRun's two os.Exit(1) branches (~lines 626-628,
//     631-633): calling this in-process would exit the test binary; there is no
//     subprocess-relaunch harness in this repo to test os.Exit paths safely.
//   - restartDaemonStandaloneIfConfirmed's success branch (~lines 776-777): the
//     error branch (771-775) is covered below via a real forkDaemon "already
//     running" failure, but the success branch requires forkDaemon's real child
//     to come up (os.Executable() launched with "daemon start --foreground") —
//     in-process that resolves to the *test* binary, not a real eos binary, so
//     it can only be exercised by the build-a-real-binary harness in
//     daemon_e2e_test.go (integration-tagged, excluded from the coverage run
//     that gates this PR).
//   - printAllDaemons' DiscoverDaemons() error branch (~line 844) and
//     openrcDaemonController.Start/Stop's rc-service delegation (~lines 406,
//     419): all three sit behind an `os.Getuid() != 0` guard that returns before
//     ever reaching them; neither this sandbox nor the non-integration `make ci`
//     job runs as root, so they are structurally unreachable in the graded run
//     (the separate root+systemd integration-test CI job is -tags integration
//     and isn't part of this coverage measurement either).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// --- standaloneDaemonController.Info: status-check error (line 93) ---

// TestStandaloneDaemonController_Info_StatusError covers the branch where
// process.StatusStandaloneDaemon itself fails (as opposed to simply reporting
// "not running"): a PID file path that is actually a directory makes
// os.ReadFile fail with something other than os.IsNotExist.
func TestStandaloneDaemonController_Info_StatusError(t *testing.T) {
	tempDir := t.TempDir()
	c := newStandaloneController(t, tempDir)
	c.cfg.PIDFile = tempDir // a directory, not a file

	var out, errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)

	c.Info(cmd)

	if !strings.Contains(errBuf.String(), "getting daemon info") {
		t.Errorf("expected 'getting daemon info' error, got: %s", errBuf.String())
	}
}

// --- tailDaemonLogFile: follow branch, Start() error, non-zero exit (lines 139, 160, 167) ---

// TestStandaloneDaemonController_Logs_Follow covers the follow=true info line
// ("streaming daemon logs"): a real `tail -f` is started against a real log
// file and left to run until the command's context is canceled, which
// terminates the child and unblocks tailCmd.Wait().
func TestStandaloneDaemonController_Logs_Follow(t *testing.T) {
	tempDir := t.TempDir()
	c := newStandaloneController(t, tempDir)
	logDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("creating log dir: %v", err)
	}
	logPath := filepath.Join(logDir, c.cfg.Log.LogFileName)
	if err := os.WriteFile(logPath, []byte("followed line\n"), 0644); err != nil {
		t.Fatalf("writing log file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(ctx)

	// Cancel shortly after tail -f has had time to emit the existing line, so
	// Logs (which blocks on tailCmd.Wait()) returns instead of hanging forever.
	go func() {
		<-time.After(300 * time.Millisecond)
		cancel()
	}()

	c.Logs(cmd, 10, true)

	if !strings.Contains(out.String(), "streaming daemon logs") {
		t.Errorf("expected 'streaming daemon logs' info line, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "followed line") {
		t.Errorf("expected tailed content, got: %s", out.String())
	}
}

// TestStandaloneDaemonController_Logs_TailStartError covers the branch where
// resolving "tail" on PATH fails: with PATH pointed at an empty directory,
// the binary can't be found before tailCmd is ever started.
func TestStandaloneDaemonController_Logs_TailStartError(t *testing.T) {
	tempDir := t.TempDir()
	c := newStandaloneController(t, tempDir)
	logDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("creating log dir: %v", err)
	}
	logPath := filepath.Join(logDir, c.cfg.Log.LogFileName)
	if err := os.WriteFile(logPath, []byte("line\n"), 0644); err != nil {
		t.Fatalf("writing log file: %v", err)
	}

	t.Setenv("PATH", t.TempDir()) // empty dir: "tail" cannot be found

	var errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	c.Logs(cmd, 10, false)

	if !strings.Contains(errBuf.String(), "resolving tail") {
		t.Errorf("expected 'resolving tail' error, got: %s", errBuf.String())
	}
}

// TestStandaloneDaemonController_Logs_TailNonZeroExit covers the branch where
// the tail process exits with a real (non-Ctrl-C) error after streaming
// output: a fake "tail" script on PATH prints the log file's content, then
// exits 3.
func TestStandaloneDaemonController_Logs_TailNonZeroExit(t *testing.T) {
	tempDir := t.TempDir()
	c := newStandaloneController(t, tempDir)
	logDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("creating log dir: %v", err)
	}
	logPath := filepath.Join(logDir, c.cfg.Log.LogFileName)
	if err := os.WriteFile(logPath, []byte("boomable content\n"), 0644); err != nil {
		t.Fatalf("writing log file: %v", err)
	}

	installFakeTailThatFails(t)

	var out, errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	c.Logs(cmd, 10, false)

	if !strings.Contains(out.String(), "boomable content") {
		t.Errorf("expected tailed content before the failure, got: %s", out.String())
	}
	if !strings.Contains(errBuf.String(), "log command failed") {
		t.Errorf("expected 'log command failed' error, got: %s", errBuf.String())
	}
}

// installFakeTailThatFails puts a fake "tail" on PATH that cats whatever path
// it was given (its last argument) and then exits non-zero, non-130.
func installFakeTailThatFails(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nfile=\"\"\nfor a in \"$@\"; do file=\"$a\"; done\n/bin/cat \"$file\"\nexit 3\n"
	if err := os.WriteFile(filepath.Join(dir, "tail"), []byte(script), 0755); err != nil {
		t.Fatalf("writing fake tail: %v", err)
	}
	t.Setenv("PATH", dir)
}

// --- systemdDaemonController.Logs / runJournalStream / reportJournalExit (lines 256, 268, 278, 283, 285) ---

func TestSystemdDaemonController_Logs_InvalidLineCount(t *testing.T) {
	c := systemdDaemonController{}
	var errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	c.Logs(cmd, 20000, false)

	if !strings.Contains(errBuf.String(), "invalid line count") {
		t.Errorf("expected 'invalid line count' error, got: %s", errBuf.String())
	}
}

// installFakeJournalctl puts a fake "journalctl" on PATH that echoes a fixed
// line and exits 0, so tests can exercise runJournalStream's success path
// without a real systemd journal.
func installFakeJournalctl(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho fake journal line\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0755); err != nil {
		t.Fatalf("writing fake journalctl: %v", err)
	}
	t.Setenv("PATH", dir)
}

func TestSystemdDaemonController_Logs_StreamingAndShowing(t *testing.T) {
	installFakeJournalctl(t)

	for _, tc := range []struct {
		name   string
		want   string
		follow bool
	}{
		{name: "showing", follow: false, want: "showing daemon logs"},
		{name: "streaming", follow: true, want: "streaming daemon logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := systemdDaemonController{}
			var out bytes.Buffer
			cmd := newTestRootCmd(nil)
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetContext(context.Background())

			c.Logs(cmd, 10, tc.follow)

			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("expected %q, got: %s", tc.want, out.String())
			}
			if !strings.Contains(out.String(), "fake journal line") {
				t.Errorf("expected journalctl output forwarded, got: %s", out.String())
			}
		})
	}
}

// TestRunJournalStream_StartError covers the branch where resolving
// journalctl on PATH fails (not found).
func TestRunJournalStream_StartError(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: "journalctl" cannot be found

	var errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	runJournalStream(cmd, []string{"-u", "eos", "-n", "10"})

	if !strings.Contains(errBuf.String(), "resolving journalctl") {
		t.Errorf("expected 'resolving journalctl' error, got: %s", errBuf.String())
	}
}

// TestReportJournalExit_RealNonZeroExit covers reportJournalExit's print
// branch using a real *exec.ExitError from a real failing process, rather
// than a hand-built error value.
func TestReportJournalExit_RealNonZeroExit(t *testing.T) {
	realErr := exitErrorWithCode(t, 7)

	var errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetErr(&errBuf)

	reportJournalExit(cmd, realErr)

	if !strings.Contains(errBuf.String(), "journalctl failed") {
		t.Errorf("expected 'journalctl failed' error, got: %s", errBuf.String())
	}
}

// TestReportJournalExit_IgnoresCtrlCExit covers the Ctrl-C exemption (exit
// code 130) using a real *exec.ExitError.
func TestReportJournalExit_IgnoresCtrlCExit(t *testing.T) {
	realErr := exitErrorWithCode(t, 130)

	var errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetErr(&errBuf)

	reportJournalExit(cmd, realErr)

	if errBuf.Len() != 0 {
		t.Errorf("expected no output for exit code 130, got: %s", errBuf.String())
	}
}

// exitErrorWithCode runs a real short-lived shell process that exits with the
// given code and returns the resulting *exec.ExitError.
func exitErrorWithCode(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run() // #nosec G204 -- fixed test-only args
	if err == nil {
		t.Fatalf("expected process to exit with code %d, got nil error", code)
	}
	return err
}

// --- openrcDaemonController.IsRunning (line 433; no root guard, unlike Start/Stop) ---

func TestOpenRCDaemonController_IsRunning(t *testing.T) {
	t.Run("rc-service reports running", func(t *testing.T) {
		rec := &recordingRun{}
		c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}, run: rec.run}

		if !c.IsRunning(context.Background()) {
			t.Error("expected IsRunning=true when rc-service status succeeds")
		}
		if len(rec.calls) != 1 || strings.Join(rec.calls[0], " ") != "rc-service eos status" {
			t.Errorf("expected 'rc-service eos status', got %v", rec.calls)
		}
	})

	t.Run("rc-service reports not running", func(t *testing.T) {
		rec := &recordingRun{err: errors.New("exit 3")}
		c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}, run: rec.run}

		if c.IsRunning(context.Background()) {
			t.Error("expected IsRunning=false when rc-service status fails")
		}
	})
}

// --- startCmd RunE: flag-parsing error branches (lines 496, 501) ---

// findDaemonSubcommand locates a direct subcommand of a daemon command tree
// built by newTestDaemonCmd/buildDaemonSubcmds.
func findDaemonSubcommand(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

// TestDaemonStart_ForegroundFlagParseError covers the defensive
// cmd.Flags().GetBool("foreground") error branch: calling the RunE closure
// directly with a bare *cobra.Command that never registered that flag
// reproduces pflag's real "flag accessed but not defined" error.
func TestDaemonStart_ForegroundFlagParseError(t *testing.T) {
	parent := newTestDaemonCmd(&fakeDaemonController{})
	startCmd := findDaemonSubcommand(t, parent, "start")

	bare := &cobra.Command{}
	var out, errBuf bytes.Buffer
	bare.SetOut(&out)
	bare.SetErr(&errBuf)

	err := startCmd.RunE(bare, nil)
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "parsing flag") {
		t.Errorf("expected 'parsing flag' error, got: %s", errBuf.String())
	}
}

// TestDaemonStart_DetachFlagParseError covers the second cmd.Flags().GetBool
// error branch (for "detach"): the bare command registers "foreground" (so
// the first GetBool succeeds) but not "detach".
func TestDaemonStart_DetachFlagParseError(t *testing.T) {
	parent := newTestDaemonCmd(&fakeDaemonController{})
	startCmd := findDaemonSubcommand(t, parent, "start")

	bare := &cobra.Command{}
	bare.Flags().BoolP("foreground", "f", false, "")
	var out, errBuf bytes.Buffer
	bare.SetOut(&out)
	bare.SetErr(&errBuf)

	err := startCmd.RunE(bare, nil)
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "parsing flag") {
		t.Errorf("expected 'parsing flag' error, got: %s", errBuf.String())
	}
}

// --- stopCmd RunE: ctrl.Stop error and killed=true success (lines 550, 557) ---

func TestDaemonStopCmd_StopError(t *testing.T) {
	fake := &fakeDaemonController{stopErr: errors.New("stop boom")}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"stop"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errOut.String(), "stop boom") {
		t.Errorf("expected error message in stderr, got: %s", errOut.String())
	}
}

func TestDaemonStopCmd_KilledSuccess(t *testing.T) {
	fake := &fakeDaemonController{stopKilled: true}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"stop"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "daemon stopped") {
		t.Errorf("expected 'daemon stopped' message, got: %s", out.String())
	}
}

// --- removeCmd RunE: info line, error branch, success branch (lines 570, 572, 575-576) ---

func TestDaemonRemoveCmd_Error(t *testing.T) {
	fake := &fakeDaemonController{removeErr: errors.New("remove boom")}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"remove"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(out.String(), "removing daemon...") {
		t.Errorf("expected 'removing daemon...' info line, got: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "remove boom") {
		t.Errorf("expected error message in stderr, got: %s", errOut.String())
	}
}

func TestDaemonRemoveCmd_Success(t *testing.T) {
	fake := &fakeDaemonController{}
	cmd := newTestDaemonCmd(fake)
	var out, errOut strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"remove"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "removing daemon...") {
		t.Errorf("expected 'removing daemon...' info line, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "daemon removed") {
		t.Errorf("expected 'daemon removed' success message, got: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "eos system unstartup") {
		t.Errorf("expected 'eos system unstartup' hint, got: %s", errOut.String())
	}
}

// --- restartDaemonStandaloneIfConfirmed: confirmed-but-fork-fails branch (lines 771-775) ---

// TestRestartDaemonStandaloneIfConfirmed_ForkFails covers the error branch:
// confirmOrDecline is short-circuited via flagYes=true (no prompt needed),
// then the real forkDaemon refuses because a "daemon" is already running,
// simulated by a live PID file at the relative path
// (config.DaemonPIDFile/config.DaemonSocketPath) that
// restartDaemonStandaloneIfConfirmed hardcodes relative to the process's
// working directory. The working directory is temporarily redirected to an
// isolated tempdir so no real files are touched.
func TestRestartDaemonStandaloneIfConfirmed_ForkFails(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tempDir := t.TempDir()
	if err = os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() {
		if cerr := os.Chdir(origWD); cerr != nil {
			t.Fatalf("restoring working directory: %v", cerr)
		}
	})

	if err = os.WriteFile(config.DaemonPIDFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("writing pid file: %v", err)
	}

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	var out, errOut bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	resultErr := restartDaemonStandaloneIfConfirmed(context.Background(), cmd, true, identity)
	if !errors.Is(resultErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", resultErr)
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "starting daemon") {
		t.Errorf("expected 'starting daemon' error prefix, got: %s", combined)
	}
	if !strings.Contains(combined, "already running") {
		t.Errorf("expected the underlying 'already running' cause, got: %s", combined)
	}
	if !strings.Contains(combined, eosDaemonLogsCmdName) {
		t.Errorf("expected the daemon-logs hint, got: %s", combined)
	}
}

// --- printSystemdDaemonDetails: pid-unknown-but-running, and resolved version (lines 908, 914) ---

// TestPrintSystemdDaemonDetails_RunningPIDUnknown covers the fallback branch
// where the socket answers (daemon is up) but systemdMainPID can't resolve a
// PID (e.g. transient systemctl hiccup): a fake systemctl on PATH prints
// nothing parseable.
func TestPrintSystemdDaemonDetails_RunningPIDUnknown(t *testing.T) {
	dir := shortTempSocketDir(t)
	sockPath := filepath.Join(dir, "eos.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	installFakeSystemctlEmptyOutput(t)

	var out, errOut bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	printSystemdDaemonDetails(cmd, config.SystemdConfig{SocketPath: sockPath})

	var runningLine string
	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.Contains(line, "running") && !strings.Contains(line, "is systemd managed") {
			runningLine = line
			break
		}
	}
	if runningLine == "" {
		t.Fatalf("expected a 'running' status line, got: %s", out.String())
	}
	if strings.Contains(runningLine, "(pid") {
		t.Errorf("expected the pid-unknown fallback (no pid rendered), got line: %s", runningLine)
	}
}

// installFakeSystemctlEmptyOutput puts a fake "systemctl" on PATH that prints
// nothing, making systemdMainPID's strconv.Atoi fail.
func installFakeSystemctlEmptyOutput(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0755); err != nil {
		t.Fatalf("writing fake systemctl: %v", err)
	}
	t.Setenv("PATH", dir)
}

// TestPrintSystemdDaemonDetails_RunningVersionResolved covers the branch where
// systemdDaemonRunningVersion actually resolves a version string: this
// requires following /proc/<pid>/exe, which is Linux-only (there is no /proc
// on macOS), so it's skipped on other platforms and instead exercised by CI's
// ubuntu-latest runner. A real "sleep" child is spawned so /proc/<pid>/exe
// resolves to a real, executable binary that understands --version (GNU
// coreutils sleep).
func TestPrintSystemdDaemonDetails_RunningVersionResolved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/exe resolution is Linux-only")
	}

	child := spawnDisposableChild(t)
	stubSystemctl(t, child.Process.Pid)

	var out, errOut bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	printSystemdDaemonDetails(cmd, config.SystemdConfig{})

	if !strings.Contains(out.String(), "running version:") {
		t.Errorf("expected a resolved 'running version:' line, got: %s", out.String())
	}
}
