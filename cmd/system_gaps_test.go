package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

// runCmdErrorOnNth returns a runCmdFn that succeeds on every call except the
// nth (1-indexed), which fails with a generic error — for isolating exactly
// which step in a multi-command sequence (daemon-reload, enable, start, ...)
// a test wants to fail, without disturbing the steps before it.
func runCmdErrorOnNth(t *testing.T, calls *[]string, failOn int) runCmdFn {
	t.Helper()
	n := 0
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		n++
		if n == failOn {
			return []byte("boom"), errors.New("command failed")
		}
		return []byte("ok"), nil
	}
}

// badEOSBaseDir returns a path whose parent is a regular file, so
// config.CreateBaseDir's os.MkdirAll fails without needing root — the same
// trick TestNewSystemConfigHelper_CreateBaseDirError uses in root_test.go.
func badEOSBaseDir(t *testing.T) string {
	t.Helper()
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte(""), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return filepath.Join(notADir, "eos")
}

// spawnSleepChild starts a real disposable child process and reaps it in the
// background, mirroring internal/process/daemon_lifecycle_test.go's
// spawnDisposableChild — needed to exercise stopStandaloneForRestart's actual
// "signal a live process and confirm it exits" success path for real, rather
// than mocking process.StopStandaloneDaemon.
func spawnSleepChild(t *testing.T) *exec.Cmd {
	t.Helper()
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to spawn disposable child: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		<-reaped
	})
	return child
}

// --- loadSystemConfigAndFlags -------------------------------------------------

func TestLoadSystemConfigAndFlags_NewSystemConfigError(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", badEOSBaseDir(t))
	cmd, _, _ := makeTestCmd(t)
	cmd.Flags().Bool("verbose", false, "")
	cmd.Flags().BoolP("yes", "y", false, "")
	printCmd, _, printErrBuf := makeTestCmd(t)

	_, _, _, _, _, err := loadSystemConfigAndFlags(cmd, printCmd)
	if err == nil {
		t.Fatal("expected error when the base dir cannot be created")
	}
	if !strings.Contains(printErrBuf.String(), "getting system configuration") {
		t.Errorf("expected config error, got: %s", printErrBuf.String())
	}
}

func TestLoadSystemConfigAndFlags_FlagYesError(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", t.TempDir())
	cmd, _, _ := makeTestCmd(t) // deliberately no "yes" flag registered
	printCmd, _, printErrBuf := makeTestCmd(t)

	_, _, _, _, _, err := loadSystemConfigAndFlags(cmd, printCmd)
	if err == nil {
		t.Fatal("expected error when the 'yes' flag isn't registered")
	}
	if !strings.Contains(printErrBuf.String(), "parsing flag") {
		t.Errorf("expected flag-parsing error, got: %s", printErrBuf.String())
	}
}

func TestLoadSystemConfigAndFlags_Success(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", t.TempDir())
	cmd, _, _ := makeTestCmd(t)
	cmd.Flags().Bool("verbose", true, "")
	cmd.Flags().BoolP("yes", "y", true, "")
	printCmd, _, _ := makeTestCmd(t)

	installDir, systemConfig, _, verbose, flagYes, err := loadSystemConfigAndFlags(cmd, printCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installDir == "" || systemConfig == nil {
		t.Error("expected populated installDir and systemConfig")
	}
	if !verbose || !flagYes {
		t.Errorf("expected verbose=true, flagYes=true, got verbose=%v flagYes=%v", verbose, flagYes)
	}
}

// --- darwin-specific full-CLI branches (safe: neither test breaks the shared
// PersistentPreRun's own newSystemConfig() call, which os.Exit(1)s on failure) ---

// TestSystemStartupCommand_DarwinUserLaunchAgentsDirError overrides
// EOS_LAUNCHD_TARGET_DIR to a fake, already-managed system dir so
// newSystemConfig's own launchd-scope resolution (also run once already by
// systemCmd's PersistentPreRun) never itself calls the real, HOME-dependent
// config.UserLaunchAgentsDir() — isolating the failure to the startup RunE's
// own separate, unconditional call to it.
func TestSystemStartupCommand_DarwinUserLaunchAgentsDirError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only startup path")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping: userAgent branch requires a non-root invocation")
	}
	cmd, _, errBuf, _ := setupCmd(t)

	fakeSystemDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeSystemDir, config.LaunchdPlistFileName), []byte("<plist/>"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EOS_LAUNCHD_TARGET_DIR", fakeSystemDir+"/")
	t.Setenv("HOME", "")

	cmd.SetArgs([]string{"system", "startup", "--yes"})
	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("expected error when the user LaunchAgents dir cannot be resolved")
	}
	if !strings.Contains(errBuf.String(), "resolving user launch agents dir") {
		t.Errorf("expected launch-agents-dir error, got: %s", errBuf.String())
	}
}

// TestSystemUnstartupCommand_DarwinNoLaunchdConfigured exercises the normal,
// unmodified default state (no plist installed anywhere) so
// systemConfig.Daemon.Launchd resolves to nil, hitting the "nothing to
// remove" branch.
func TestSystemUnstartupCommand_DarwinNoLaunchdConfigured(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only unstartup path")
	}
	cmd, _, errBuf, _ := setupCmd(t)

	cmd.SetArgs([]string{"system", "unstartup", "--yes"})
	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("expected error when no launchd startup is configured")
	}
	if !strings.Contains(errBuf.String(), "no launchd startup configured") {
		t.Errorf("expected 'no launchd startup configured' error, got: %s", errBuf.String())
	}
}

func TestSystemUninstallCommand_DeclineConfirm(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"system", "uninstall"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "uninstall canceled") {
		t.Errorf("expected cancel message, got: %s", outBuf.String())
	}
}

// --- infoCmd ------------------------------------------------------------------

func TestInfoCmd_SystemdManagedNoTimeoutTelemetryEnabled(t *testing.T) {
	cmd, outBuf, _ := makeTestCmd(t)
	cfg := &config.SystemConfig{
		Daemon: config.DaemonConfig{
			Systemd: &config.SystemdConfig{
				SystemdTargetDir:      "/etc/systemd/system/",
				SystemdTargetFileName: "eos.service",
			},
		},
		Health: config.HealthConfig{
			Timeout: config.TimeOutConfig{Enable: false, Limit: 5 * time.Second},
		},
		Shutdown: config.ShutdownConfig{GracePeriod: 3 * time.Second},
		Telemetry: config.TelemetryConfig{
			Enable:   true,
			Endpoint: "http://collector:4318",
			Insecure: true,
		},
	}

	infoCmd(cmd, "/usr/local/bin", "/home/eos/.eos", cfg)

	out := outBuf.String()
	for _, want := range []string{
		"systemd managed:", "true",
		"/etc/systemd/system/", "eos.service",
		"(not active)",
		"http://collector:4318",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

// --- ensureUserBusAvailable -----------------------------------------------

func TestEnsureUserBusAvailable_EnableLingerRunError(t *testing.T) {
	c, outBuf, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")
	t.Setenv("XDG_RUNTIME_DIR", "")
	expected := filepath.Join(t.TempDir(), "missing")

	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("enable-linger failed")
	}

	err := ensureUserBusAvailable(t.Context(), c, false, "testuser", os.Getuid(), expected, run)
	if err == nil {
		t.Fatal("expected error when enabling linger fails")
	}
	if !strings.Contains(errBuf.String(), "enable-linger") {
		t.Errorf("expected enable-linger error, got: %s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "sudo") {
		t.Errorf("expected sudo hint, got: %s", errBuf.String())
	}
	// The "run manually" fallback hint is printed to stdout, not stderr.
	if !strings.Contains(outBuf.String(), "run manually") {
		t.Errorf("expected manual-run hint, got: %s", outBuf.String())
	}
}

// --- prepareUserBus / ensureSystemdUnitDir --------------------------------

func TestEnsureSystemdUnitDir_UserUnitMkdirAllError(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	systemdDir := filepath.Join(notADir, "sub") + "/"

	c, _, errBuf := makeTestCmd(t)
	err := ensureSystemdUnitDir(c, false, true, systemdDir, "unused")
	if err == nil {
		t.Fatal("expected error when the user systemd directory cannot be created")
	}
	if !strings.Contains(errBuf.String(), "creating user systemd directory") {
		t.Errorf("expected mkdir error, got: %s", errBuf.String())
	}
}

func TestPrepareUserBus_UserCredentialsError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	badUser := &user.User{Uid: "not-a-number", Gid: "0", Username: "testuser"}
	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run should not be called when resolving user credentials fails")
		return nil, nil
	}

	err := prepareUserBus(t.Context(), c, false, badUser, run)
	if err == nil {
		t.Fatal("expected error for a malformed uid")
	}
	if !strings.Contains(errBuf.String(), "getting current user credentials") {
		t.Errorf("expected credentials error, got: %s", errBuf.String())
	}
}

func TestPrepareUserBus_EnsureUserBusAvailableError(t *testing.T) {
	effectiveUser, err := userutil.EffectiveUser()
	if err != nil {
		t.Fatalf("resolving effective user: %v", err)
	}
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "n\n") // decline enabling linger
	t.Setenv("XDG_RUNTIME_DIR", "")

	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run should not be called when the user declines enabling linger")
		return nil, nil
	}

	busErr := prepareUserBus(t.Context(), c, false, effectiveUser, run)
	if busErr == nil {
		t.Fatal("expected error when no user bus is available and linger is declined")
	}
	if !strings.Contains(errBuf.String(), "preparing user bus") {
		t.Errorf("expected 'preparing user bus' error, got: %s", errBuf.String())
	}
}

// --- enableSystemdUnit ------------------------------------------------------

func TestEnableSystemdUnit_DaemonReloadError(t *testing.T) {
	cmd, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 1)

	if err := enableSystemdUnit(t.Context(), cmd, false, false, "eos", run); err == nil {
		t.Fatal("expected error when daemon-reload fails")
	}
	if !strings.Contains(errBuf.String(), "daemon-reload") {
		t.Errorf("expected daemon-reload error, got: %s", errBuf.String())
	}
}

func TestEnableSystemdUnit_EnableError(t *testing.T) {
	cmd, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 2)

	if err := enableSystemdUnit(t.Context(), cmd, false, false, "eos", run); err == nil {
		t.Fatal("expected error when enable fails")
	}
	if !strings.Contains(errBuf.String(), "enabling service") {
		t.Errorf("expected enable error, got: %s", errBuf.String())
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 systemctl calls (reload succeeded, enable failed), got: %v", calls)
	}
}

// --- stopStandaloneForRestart -----------------------------------------------

func TestStopStandaloneForRestart_NilConfig(t *testing.T) {
	cmd, outBuf, _ := makeTestCmd(t)
	if err := stopStandaloneForRestart(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "daemon was not running") {
		t.Errorf("expected not-running message, got: %s", outBuf.String())
	}
}

func TestStopStandaloneForRestart_StopError(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	socketPath := filepath.Join(tempDir, "eos.sock")
	if err := os.WriteFile(pidFile, []byte("not-a-pid"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	cmd, _, errBuf := makeTestCmd(t)
	err := stopStandaloneForRestart(cmd, &config.StandaloneDaemonConfig{PIDFile: pidFile, SocketPath: socketPath})
	if err == nil {
		t.Fatal("expected error for a malformed pid file")
	}
	if !strings.Contains(errBuf.String(), "stopping daemon") {
		t.Errorf("expected stop error, got: %s", errBuf.String())
	}
}

func TestStopStandaloneForRestart_Success(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	socketPath := filepath.Join(tempDir, "eos.sock")

	child := spawnSleepChild(t)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	cmd, outBuf, _ := makeTestCmd(t)
	err := stopStandaloneForRestart(cmd, &config.StandaloneDaemonConfig{PIDFile: pidFile, SocketPath: socketPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "daemon stopped") {
		t.Errorf("expected 'daemon stopped' message, got: %s", outBuf.String())
	}
}

// --- startupCmd extra branches ----------------------------------------------

func TestStartupCmd_WriteUnitFileError(t *testing.T) {
	tempDir := t.TempDir()
	unitPath := filepath.Join(tempDir, "eos.service")
	// Pre-create the target path AS A DIRECTORY so os.WriteFile fails (EISDIR)
	// even though prepareSystemUnitDir's own checks (on the containing dir) pass.
	if err := os.MkdirAll(unitPath, 0755); err != nil {
		t.Fatal(err)
	}

	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	var calls []string
	err := startupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false, false, false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if err == nil {
		t.Fatal("expected error when the unit file target path is a directory")
	}
	if !strings.Contains(errBuf.String(), "writing unit file") {
		t.Errorf("expected write error, got: %s", errBuf.String())
	}
	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls, got: %v", calls)
	}
}

func TestStartupCmd_SystemctlStartError(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\ny\n") // confirm create, confirm restart

	var calls []string
	run := runCmdErrorOnNth(t, &calls, 3) // 1: daemon-reload, 2: enable, 3: start -> fails
	err := startupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false, false, false,
		fakeDetectRuntime("systemd"), run)

	if err == nil {
		t.Fatal("expected error when systemctl start fails")
	}
	if !strings.Contains(errBuf.String(), "starting systemd daemon") {
		t.Errorf("expected start error, got: %s", errBuf.String())
	}
	if len(calls) != 3 {
		t.Errorf("expected 3 systemctl calls, got: %v", calls)
	}
}

// --- disableAndRemoveSystemdUnit ---------------------------------------------

func TestDisableAndRemoveSystemdUnit_StopError(t *testing.T) {
	cmd, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 1)

	err := disableAndRemoveSystemdUnit(t.Context(), cmd, false, false, "system unit", "eos", "/nonexistent/eos.service", run)
	if err == nil {
		t.Fatal("expected error when stop fails")
	}
	if !strings.Contains(errBuf.String(), "stopping system unit") {
		t.Errorf("expected stop error, got: %s", errBuf.String())
	}
}

func TestDisableAndRemoveSystemdUnit_DisableError(t *testing.T) {
	cmd, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 2)

	err := disableAndRemoveSystemdUnit(t.Context(), cmd, false, false, "system unit", "eos", "/nonexistent/eos.service", run)
	if err == nil {
		t.Fatal("expected error when disable fails")
	}
	if !strings.Contains(errBuf.String(), "disabling system unit") {
		t.Errorf("expected disable error, got: %s", errBuf.String())
	}
}

func TestDisableAndRemoveSystemdUnit_RemoveError(t *testing.T) {
	cmd, _, errBuf := makeTestCmd(t)
	var calls []string
	run := recordingRunCmd(t, &calls)
	unitPath := filepath.Join(t.TempDir(), "does-not-exist.service")

	err := disableAndRemoveSystemdUnit(t.Context(), cmd, false, false, "system unit", "eos", unitPath, run)
	if err == nil {
		t.Fatal("expected error removing a nonexistent unit file")
	}
	if !strings.Contains(errBuf.String(), "removing unit file") {
		t.Errorf("expected remove error, got: %s", errBuf.String())
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 systemctl calls (stop, disable) before the failed remove, got: %v", calls)
	}
}

func TestDisableAndRemoveSystemdUnit_DaemonReloadError(t *testing.T) {
	tempDir := t.TempDir()
	unitPath := filepath.Join(tempDir, "eos.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 3) // 1: stop, 2: disable succeed; 3: daemon-reload -> fails

	err := disableAndRemoveSystemdUnit(t.Context(), cmd, false, false, "system unit", "eos", unitPath, run)
	if err == nil {
		t.Fatal("expected error when the final daemon-reload fails")
	}
	if !strings.Contains(errBuf.String(), "daemon-reload") {
		t.Errorf("expected daemon-reload error, got: %s", errBuf.String())
	}
	if _, statErr := os.Stat(unitPath); !os.IsNotExist(statErr) {
		t.Error("expected unit file to already be removed even though the final reload failed")
	}
}

// --- unstartupCmd -------------------------------------------------------------

func TestUnstartupCmd_UserUnitLingerHint(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // own dir -> ensureUserBusAvailable succeeds without prompting
	tempDir := t.TempDir()
	unitFile := filepath.Join(tempDir, "eos.service")
	if err := os.WriteFile(unitFile, []byte("[Unit]"), 0644); err != nil {
		t.Fatal(err)
	}

	c, outBuf, errBuf := makeTestCmd(t)
	setStdin(c, "y\nn\n") // confirm unstartup, decline restart standalone

	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	_ = unstartupCmd(t.Context(), c, config.SystemdConfig{
		SystemdTargetDir:      tempDir + "/",
		SystemdTargetFileName: "eos.service",
	}, true, false, false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls), identity)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "if you enabled linger, also run") {
		t.Errorf("expected linger hint, got: %s", outBuf.String())
	}
}

// --- launchd target dir helpers -----------------------------------------------

func TestPrepareLaunchdTargetDir_MissingDir(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	if prepareLaunchdTargetDir(c, "/nonexistent/eos-test-launchd-dir") {
		t.Error("expected false for an inaccessible directory")
	}
	if !strings.Contains(errBuf.String(), "not accessible") {
		t.Errorf("expected 'not accessible' in stderr, got: %s", errBuf.String())
	}
}

func TestPrepareLaunchdTargetDir_NotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	c, _, errBuf := makeTestCmd(t)
	if prepareLaunchdTargetDir(c, dir) {
		t.Error("expected false for a non-writable directory")
	}
	if !strings.Contains(errBuf.String(), "checking destination file") {
		t.Errorf("expected writability error in stderr, got: %s", errBuf.String())
	}
}

func TestEnsureLaunchdDir_UserAgentMkdirAllError(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	launchdDir := filepath.Join(notADir, "sub") + "/"

	c, _, errBuf := makeTestCmd(t)
	err := ensureLaunchdDir(c, false, true, launchdDir)
	if err == nil {
		t.Fatal("expected error when LaunchAgents dir cannot be created")
	}
	if !strings.Contains(errBuf.String(), "creating LaunchAgents directory") {
		t.Errorf("expected mkdir error, got: %s", errBuf.String())
	}
}

// --- resolveLaunchdUID ---------------------------------------------------------

func TestResolveLaunchdUID_UserCredentialsError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	badUser := &user.User{Uid: "not-a-number", Gid: "0", Username: "testuser"}

	if _, err := resolveLaunchdUID(c, true, badUser); err == nil {
		t.Fatal("expected error for a malformed uid")
	}
	if !strings.Contains(errBuf.String(), "getting current user credentials") {
		t.Errorf("expected credentials error, got: %s", errBuf.String())
	}
}

// --- bootstrapLaunchdJob --------------------------------------------------------

func TestBootstrapLaunchdJob_BootstrapError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 2) // 1: bootout (best-effort, ignored), 2: bootstrap -> fails

	err := bootstrapLaunchdJob(t.Context(), c, false, "system", "system/org.elysiumlabs.eos-test", "/tmp/eos-test.plist", run)
	if err == nil {
		t.Fatal("expected error when bootstrap fails")
	}
	if !strings.Contains(errBuf.String(), "bootstrap:") {
		t.Errorf("expected bootstrap error, got: %s", errBuf.String())
	}
}

func TestBootstrapLaunchdJob_EnableError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	run := runCmdErrorOnNth(t, &calls, 3) // 1: bootout, 2: bootstrap succeed; 3: enable -> fails

	err := bootstrapLaunchdJob(t.Context(), c, false, "system", "system/org.elysiumlabs.eos-test", "/tmp/eos-test.plist", run)
	if err == nil {
		t.Fatal("expected error when enable fails")
	}
	if !strings.Contains(errBuf.String(), "enabling service") {
		t.Errorf("expected enable error, got: %s", errBuf.String())
	}
	if len(calls) != 3 {
		t.Errorf("expected 3 launchctl calls, got: %v", calls)
	}
}

// --- startupCmdLaunchd extra branches --------------------------------------------

func TestStartupCmdLaunchd_WritePlistFileError(t *testing.T) {
	tempDir := t.TempDir()
	plistFileName := "org.elysiumlabs.eos-test.plist"
	plistPath := filepath.Join(tempDir, plistFileName)
	if err := os.MkdirAll(plistPath, 0755); err != nil {
		t.Fatal(err)
	}

	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	var calls []string
	err := startupCmdLaunchd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", plistFileName, false, false, false, recordingRunCmd(t, &calls))

	if err == nil {
		t.Fatal("expected error when the plist target path is a directory")
	}
	if !strings.Contains(errBuf.String(), "writing plist file") {
		t.Errorf("expected write error, got: %s", errBuf.String())
	}
}

// TestStartupCmdLaunchd_UserAgentFullPath exercises the userAgent=true branch
// end to end — existing tests in system_test.go only cover userAgent=false.
func TestStartupCmdLaunchd_UserAgentFullPath(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "y\nn\n") // confirm plist creation, decline restart

	var calls []string
	plistFileName := "org.elysiumlabs.eos-test.plist"
	err := startupCmdLaunchd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", plistFileName, true, false, false, recordingRunCmd(t, &calls))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "launch agent enabled, eos will start on login") {
		t.Errorf("expected 'launch agent enabled' message, got: %s", outBuf.String())
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 launchctl calls (bootout, bootstrap, enable), got: %v", calls)
	}
	if !strings.Contains(calls[0], fmt.Sprintf("gui/%d", os.Getuid())) {
		t.Errorf("expected gui/<uid> domain in launchctl calls, got: %v", calls)
	}
}

func TestStartupCmdLaunchd_KickstartError(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\ny\n") // confirm plist, confirm restart

	var calls []string
	run := runCmdErrorOnNth(t, &calls, 4) // bootout, bootstrap, enable succeed; kickstart fails
	err := startupCmdLaunchd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "org.elysiumlabs.eos-test.plist", false, false, false, run)

	if err == nil {
		t.Fatal("expected error when kickstart fails")
	}
	if !strings.Contains(errBuf.String(), "starting launchd daemon") {
		t.Errorf("expected kickstart error, got: %s", errBuf.String())
	}
}

// --- unstartupCmdLaunchd -----------------------------------------------------

func TestUnstartupCmdLaunchd_RemovePlistError(t *testing.T) {
	tempDir := t.TempDir()
	plistFileName := "org.elysiumlabs.eos-test.plist"
	// Deliberately do NOT create the plist file, so os.Remove fails.

	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	unstartErr := unstartupCmdLaunchd(t.Context(), c, config.LaunchdConfig{
		LaunchdTargetDir:     tempDir + "/",
		LaunchdPlistFileName: plistFileName,
	}, false, false, false, recordingRunCmd(t, &calls), identity)

	if unstartErr == nil {
		t.Fatal("expected error removing a nonexistent plist file")
	}
	if !strings.Contains(errBuf.String(), "removing plist file") {
		t.Errorf("expected remove error, got: %s", errBuf.String())
	}
}

// --- cleanupTempDir -------------------------------------------------------------

func TestCleanupTempDir_RemoveAllError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "totarget")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0755) })

	c, _, errBuf := makeTestCmd(t)
	cleanupTempDir(c, target)
	if !strings.Contains(errBuf.String(), "cleanup of") {
		t.Errorf("expected cleanup-failure warning, got: %s", errBuf.String())
	}
}

// --- validateUpdatePreconditions -------------------------------------------------

func TestValidateUpdatePreconditions_DirNotAccessible(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	err := validateUpdatePreconditions(c, filepath.Join(t.TempDir(), "does-not-exist"), "v1.0.0")
	if err == nil {
		t.Fatal("expected error for a nonexistent install dir")
	}
	if !strings.Contains(errBuf.String(), "not accessible") {
		t.Errorf("expected 'not accessible' error, got: %s", errBuf.String())
	}
}

func TestValidateUpdatePreconditions_NotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	c, _, errBuf := makeTestCmd(t)
	err := validateUpdatePreconditions(c, dir, "v1.0.0")
	if err == nil {
		t.Fatal("expected error for a non-writable install dir")
	}
	if !strings.Contains(errBuf.String(), "checking destination file") {
		t.Errorf("expected writability error, got: %s", errBuf.String())
	}
}

func TestValidateUpdatePreconditions_InvalidSemver(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	err := validateUpdatePreconditions(c, t.TempDir(), "vnotasemver")
	if err == nil {
		t.Fatal("expected error for an invalid semver version")
	}
	if !strings.Contains(errBuf.String(), "invalid semantic version") {
		t.Errorf("expected semver error, got: %s", errBuf.String())
	}
}

// --- resolveUpdateTarget --------------------------------------------------------

func TestResolveUpdateTarget_FetchReleaseError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	fakeFetch := func(_ context.Context, _ bool) (*Release, error) {
		return nil, errors.New("network down")
	}
	_, proceed, err := resolveUpdateTarget(t.Context(), c, fakeFetch, "v1.0.0", "amd64", "linux", false)
	if err == nil {
		t.Fatal("expected error")
	}
	if proceed {
		t.Error("expected proceed=false on fetch error")
	}
	if !strings.Contains(errBuf.String(), "fetching latest release") {
		t.Errorf("expected fetch error, got: %s", errBuf.String())
	}
}

func TestResolveUpdateTarget_AlreadyLatest(t *testing.T) {
	c, outBuf, _ := makeTestCmd(t)
	fakeFetch := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{TagName: "v1.0.0"}, nil
	}
	result, proceed, err := resolveUpdateTarget(t.Context(), c, fakeFetch, "v1.0.0", "amd64", "linux", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proceed {
		t.Error("expected proceed=false when already on latest version")
	}
	if result.LatestVersion != "" {
		t.Errorf("expected empty LatestVersion, got: %q", result.LatestVersion)
	}
	if !strings.Contains(outBuf.String(), "already on the latest version") {
		t.Errorf("expected already-latest message, got: %s", outBuf.String())
	}
}

// --- downloadAndVerifyBinary -----------------------------------------------------

func TestDownloadAndVerifyBinary_DownloadError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	fakeDownload := func(_ context.Context, _ *Asset) (*os.File, string, error) {
		return nil, "", errors.New("network down")
	}
	_, _, err := downloadAndVerifyBinary(t.Context(), c, UpdateResult{Asset: &Asset{Name: "eos"}}, fakeDownload, fetchChecksumForBinary)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "downloading binary") {
		t.Errorf("expected download error, got: %s", errBuf.String())
	}
}

func TestDownloadAndVerifyBinary_ChecksumFetchError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	tempDir := t.TempDir()
	fakeDownload := func(_ context.Context, asset *Asset) (*os.File, string, error) {
		f, err := os.CreateTemp(tempDir, asset.Name)
		if err != nil {
			return nil, "", err
		}
		return f, tempDir, nil
	}
	fakeChecksum := func(_ context.Context, _ *Asset, _ string) (string, error) {
		return "", errors.New("checksums unavailable")
	}
	_, _, err := downloadAndVerifyBinary(t.Context(), c, UpdateResult{Asset: &Asset{Name: "eos"}}, fakeDownload, fakeChecksum)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "fetching checksums") {
		t.Errorf("expected checksum-fetch error, got: %s", errBuf.String())
	}
}

func TestDownloadAndVerifyBinary_ChecksumMismatch(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	tempDir := t.TempDir()
	fakeDownload := func(_ context.Context, asset *Asset) (*os.File, string, error) {
		f, err := os.CreateTemp(tempDir, asset.Name)
		if err != nil {
			return nil, "", err
		}
		if _, writeErr := f.WriteString("actual content"); writeErr != nil {
			return nil, "", writeErr
		}
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			return nil, "", seekErr
		}
		return f, tempDir, nil
	}
	fakeChecksum := func(_ context.Context, _ *Asset, _ string) (string, error) {
		return "0000000000000000000000000000000000000000000000000000000000000000", nil
	}
	_, _, err := downloadAndVerifyBinary(t.Context(), c, UpdateResult{Asset: &Asset{Name: "eos"}}, fakeDownload, fakeChecksum)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "checksum validation failed") {
		t.Errorf("expected checksum mismatch error, got: %s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "eos system update") {
		t.Errorf("expected retry hint, got: %s", errBuf.String())
	}
}

// --- installUpdatedBinary --------------------------------------------------------

func TestInstallUpdatedBinary_BackupCreateAndCopyError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "eos")
	if err := os.WriteFile(binaryPath, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	cmd, _, errBuf := makeTestCmd(t)
	tempDir := t.TempDir()
	newBinary, err := os.CreateTemp(t.TempDir(), "newbin")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = newBinary.Close() }()

	err = installUpdatedBinary(cmd, newBinary, binaryPath, tempDir)
	if err == nil {
		t.Fatal("expected error when the backup directory isn't writable")
	}
	if !strings.Contains(errBuf.String(), "creating destination file") {
		t.Errorf("expected 'creating destination file' error, got: %s", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "backing up current binary") {
		t.Errorf("expected 'backing up current binary' error, got: %s", errBuf.String())
	}
}

func TestInstallUpdatedBinary_ReplaceBinaryError(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "eos")
	if err := os.WriteFile(binaryPath, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	newBinDir := t.TempDir()
	newBinary, err := os.CreateTemp(newBinDir, "newbin")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = newBinary.Close() }()
	if _, writeErr := newBinary.WriteString("new binary content"); writeErr != nil {
		t.Fatal(writeErr)
	}
	// Remove the source file out from under the *os.File handle: copyFile's
	// earlier backup step (using binaryPath, unrelated) still succeeds, but
	// replaceBinary's os.Open(binary.Name()) now fails.
	if removeErr := os.Remove(newBinary.Name()); removeErr != nil {
		t.Fatal(removeErr)
	}

	cmd, _, errBuf := makeTestCmd(t)
	tempDir := t.TempDir()

	err = installUpdatedBinary(cmd, newBinary, binaryPath, tempDir)
	if err == nil {
		t.Fatal("expected error when the source binary file is gone")
	}
	if !strings.Contains(errBuf.String(), "installing new binary") {
		t.Errorf("expected 'installing new binary' error, got: %s", errBuf.String())
	}
}

// --- restartDaemonAfterUpdate root-only branch --------------------------------

func TestRestartDaemonAfterUpdate_RootHint(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root to exercise the root-only 'other users may be stale' hint")
	}
	cmd, outBuf, errBuf, _ := setupCmd(t)
	setStdin(cmd, "y\n")
	ctrl := &stubUpdateController{running: true, killed: true}

	if err := restartDaemonAfterUpdate(t.Context(), cmd, ctrl, t.TempDir(), "v9.9.9"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "other users on this host") {
		t.Errorf("expected root-only hint, got: %s", outBuf.String())
	}
	if !strings.Contains(errBuf.String(), "eos daemon info --all") {
		t.Errorf("expected root-only hint command, got: %s", errBuf.String())
	}
}

// --- updateCmd decline path -------------------------------------------------------

func TestUpdateCmd_DeclineUpgrade(t *testing.T) {
	buildinfo.Version = "v0.0.1"
	defer func() { buildinfo.Version = "dev" }()

	cmd, outBuf, _, tempDir := setupCmd(t)
	t.Setenv("EOS_INSTALL_DIR", tempDir)
	if err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	installDir, baseDir, systemConfig, identity, err := newSystemConfig()
	if err != nil {
		t.Fatalf("newSystemConfig: %v", err)
	}
	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.Telemetry, systemConfig.UnderSystemd, identity)
	if err != nil {
		t.Fatalf("newDaemonController: %v", err)
	}

	fakeFetchRelease := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{TagName: "v99.0.0", Assets: []Asset{{Name: "eos-linux-arm64"}}}, nil
	}
	downloadCalled := false
	fakeDownloadBinary := func(_ context.Context, _ *Asset) (*os.File, string, error) {
		downloadCalled = true
		return nil, "", fmt.Errorf("should not be called")
	}

	setStdin(cmd, "n\n")

	err = updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, ctrl, "arm64", "linux", false, fakeFetchRelease, fakeDownloadBinary, fetchChecksumForBinary)
	if err != nil {
		t.Fatalf("unexpected error on decline: %v", err)
	}
	if downloadCalled {
		t.Error("expected download to be skipped when the upgrade is declined")
	}
	if !strings.Contains(outBuf.String(), "update canceled") {
		t.Errorf("expected 'update canceled', got: %s", outBuf.String())
	}
}

// --- uninstallCmd internals -----------------------------------------------------

func TestUninstallCmd_BinaryRemoveError(t *testing.T) {
	mgr := &mockMgr{}
	fake := &fakeDaemonController{}
	installDir := t.TempDir()
	eosDir := filepath.Join(installDir, "eos")
	if err := os.MkdirAll(eosDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(eosDir, "file.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetContext(t.Context())
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.Flags().Bool("verbose", false, "")

	err := uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, fake, installDir, t.TempDir(), true)
	if err == nil {
		t.Fatal("expected error removing a non-empty 'eos' directory")
	}
	if !strings.Contains(errBuf.String(), "removing eos binary") {
		t.Errorf("expected binary-removal error, got: %s", errBuf.String())
	}
}

func TestUninstallCmd_SystemDataRemoveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	mgr := &mockMgr{}
	fake := &fakeDaemonController{}
	installDir := t.TempDir() // no "eos" binary present -> removal is a no-op (IsNotExist)

	parent := t.TempDir()
	baseDir := filepath.Join(parent, "basedir")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0755) })

	var out, errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetContext(t.Context())
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.Flags().Bool("verbose", false, "")

	err := uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, fake, installDir, baseDir, true)
	if err == nil {
		t.Fatal("expected error removing system data from an unwritable parent")
	}
	if !strings.Contains(errBuf.String(), "removing eos system data") {
		t.Errorf("expected system-data-removal error, got: %s", errBuf.String())
	}
}

func TestUninstallCmd_DeclineSystemDataRemoval(t *testing.T) {
	mgr := &mockMgr{}
	fake := &fakeDaemonController{}
	installDir := t.TempDir()
	baseDir := t.TempDir()

	var out bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetContext(t.Context())
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.Flags().Bool("verbose", false, "")
	cmd.SetIn(strings.NewReader("n\n"))

	err := uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, fake, installDir, baseDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "skipped removal eos system data") {
		t.Errorf("expected skip message, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "rm -rf") {
		t.Errorf("expected manual-removal hint, got: %s", out.String())
	}
	if _, statErr := os.Stat(baseDir); statErr != nil {
		t.Errorf("expected baseDir to remain, got stat err: %v", statErr)
	}
}

// --- handleStoppingServices ------------------------------------------------------

func TestHandleStoppingServices_ForceStopAlsoErrors(t *testing.T) {
	svc := types.ServiceInstance{Name: "svc-a"}
	cfg := &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Second}}
	mgr := &mockMgr{
		stopSvc: func(_ string, _, _ time.Duration) (manager.StopServiceResult, error) {
			return manager.StopServiceResult{}, errors.New("stop failed")
		},
		forceStop: func(_ string) (manager.StopServiceResult, error) {
			return manager.StopServiceResult{}, errors.New("force stop also failed")
		},
	}
	var out bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("y\ny\n"))

	ok := handleStoppingServices(cmd, mgr, cfg, []types.ServiceInstance{svc}, false)
	if !ok {
		t.Fatal("expected handleStoppingServices to still return true after a failed (best-effort) force-stop")
	}
	if !strings.Contains(out.String(), "force stopping services") {
		t.Errorf("expected force-stop-error warning, got: %s", out.String())
	}
}

func TestHandleStoppingServices_RemoveInstanceError(t *testing.T) {
	svc := types.ServiceInstance{Name: "svc-a"}
	cfg := &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Second}}
	mgr := &mockMgr{
		removeInstance: func(_ string) (bool, error) {
			return false, errors.New("remove failed")
		},
	}
	var out, errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader("y\n"))

	ok := handleStoppingServices(cmd, mgr, cfg, []types.ServiceInstance{svc}, false)
	if !ok {
		t.Fatal("expected handleStoppingServices to return true")
	}
	if !strings.Contains(errBuf.String(), "cleaning up service instance") {
		t.Errorf("expected cleanup error, got: %s", errBuf.String())
	}
}
