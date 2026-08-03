package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

// selectiveFailRunCmd returns a runCmdFn that records every call like
// recordingRunCmd, but fails with failErr whenever the invoked command name
// equals failName — letting a test target one specific rc-update/rc-service
// invocation among several without disturbing the others.
func selectiveFailRunCmd(t *testing.T, calls *[]string, failName string, failErr error) runCmdFn {
	t.Helper()
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if name == failName {
			return []byte("boom"), failErr
		}
		return []byte("ok"), nil
	}
}

func TestOpenrcStartupCmdNonOpenRCRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	_ = openrcStartupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos", false, false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no rc-* calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected 'not supported' in stderr, got: %s", errBuf.String())
	}
}

func TestOpenrcStartupCmdRuntimeDetectionError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	_ = openrcStartupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos", false, false,
		fakeDetectRuntimeErr(errors.New("no /run")), noopRunCmd)

	if !strings.Contains(errBuf.String(), "getting system command") {
		t.Errorf("expected runtime error in stderr, got: %s", errBuf.String())
	}
}

func TestOpenrcStartupCmdDeclineInitScript(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	_ = openrcStartupCmd(t.Context(), c, "/usr/local/bin", &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos", false, false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no rc-* calls when user declines, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "init script creation canceled") {
		t.Errorf("expected cancelation message, got: %s", outBuf.String())
	}
}

func TestOpenrcStartupCmdWritesScriptAndEnablesWithoutRestart(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, errBuf := makeTestCmd(t)
	// confirm init script creation, decline restart
	setStdin(c, "y\nn\n")

	var calls []string
	_ = openrcStartupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos", true, false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if !strings.Contains(errBuf.String(), "debug") {
		t.Errorf("expected debug output in stderr with verbose=true, got: %s", errBuf.String())
	}

	scriptPath := filepath.Join(tempDir, "eos")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("expected init script to be written: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("expected init script to be executable")
	}

	want := []string{"rc-update add eos default"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "service enabled, eos will start on boot") {
		t.Errorf("expected 'service enabled' message, got: %s", outBuf.String())
	}
}

func TestOpenrcStartupCmdFullRestartPath(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	// confirm init script, confirm restart
	setStdin(c, "y\ny\n")

	var calls []string
	_ = openrcStartupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos", false, false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	want := []string{"rc-update add eos default", "rc-service eos start"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}
}

func TestOpenrcUnstartupCmdNonOpenRCRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	_ = openrcUnstartupCmd(t.Context(), c, "/tmp/", "eos", false, false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls), identity)

	if len(calls) != 0 {
		t.Errorf("expected no rc-* calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected 'not supported' in stderr, got: %s", errBuf.String())
	}
}

func TestOpenrcUnstartupCmdDeclineConfirmation(t *testing.T) {
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	_ = openrcUnstartupCmd(t.Context(), c, "/tmp/", "eos", false, false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls), identity)

	if len(calls) != 0 {
		t.Errorf("expected no rc-* calls when declined, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "canceled") {
		t.Errorf("expected 'canceled' message, got: %s", outBuf.String())
	}
}

func TestOpenrcUnstartupCmdRemovesScriptAndDisables(t *testing.T) {
	tempDir := t.TempDir()
	initFile := filepath.Join(tempDir, "eos")
	if err := os.WriteFile(initFile, []byte("#!/sbin/openrc-run"), 0755); err != nil {
		t.Fatal(err)
	}

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unstartup, decline restart standalone
	setStdin(c, "y\nn\n")

	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	_ = openrcUnstartupCmd(t.Context(), c, tempDir+"/", "eos", false, false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls), identity)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Error("expected init script to be removed")
	}

	want := []string{"rc-service eos stop", "rc-update del eos default"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "init script removed, startup disabled") {
		t.Errorf("expected success message, got: %s", outBuf.String())
	}
}

// TestOpenrcStartupCmdCheckWritableFails covers the checkWritable error
// branch: initDir doesn't exist, so the writability probe fails before any
// rc-* command is ever attempted.
func TestOpenrcStartupCmdCheckWritableFails(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	err := openrcStartupCmd(t.Context(), c, "/usr/local/bin", nil, "/nonexistent-eos-openrc-dir/", "eos", false, false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected no rc-* calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "checking destination file") {
		t.Errorf("expected 'checking destination file' error, got: %s", errBuf.String())
	}
}

// TestOpenrcStartupCmdWriteInitScriptFails covers the os.WriteFile error
// branch: the init file's target path is itself a directory, so writing the
// script to it fails even though the containing directory is writable.
func TestOpenrcStartupCmdWriteInitScriptFails(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755); err != nil {
		t.Fatal(err)
	}
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	var calls []string
	err := openrcStartupCmd(t.Context(), c, filepath.Join(tempDir, "install"), nil, tempDir+"/", "eos", false, false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected no rc-* calls when the init script write fails, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "writing init script") {
		t.Errorf("expected 'writing init script' error, got: %s", errBuf.String())
	}
}

// TestOpenrcStartupCmdRcUpdateAddFails covers the "rc-update add" error
// branch.
func TestOpenrcStartupCmdRcUpdateAddFails(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n") // confirm init script creation; the rc-update failure ends the flow before the restart prompt

	var calls []string
	run := selectiveFailRunCmd(t, &calls, "rc-update", errors.New("rc-update: openrc is not running"))
	err := openrcStartupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), nil, tempDir+"/", "eos", false, false,
		fakeDetectRuntime("openrc"), run)

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	want := []string{"rc-update add eos default"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}
	if !strings.Contains(errBuf.String(), "enabling service") {
		t.Errorf("expected 'enabling service' error, got: %s", errBuf.String())
	}
}

// TestOpenrcStartupCmdRcServiceStartFails covers the "rc-service start" error
// branch, reached only after rc-update add succeeds and the restart is
// confirmed.
func TestOpenrcStartupCmdRcServiceStartFails(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\ny\n") // confirm init script creation, confirm restart

	var calls []string
	run := selectiveFailRunCmd(t, &calls, rcServiceCmdName, errors.New("rc-service: eos: does not exist"))
	err := openrcStartupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos", false, false,
		fakeDetectRuntime("openrc"), run)

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	want := []string{"rc-update add eos default", "rc-service eos start"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}
	if !strings.Contains(errBuf.String(), "starting service") {
		t.Errorf("expected 'starting service' error, got: %s", errBuf.String())
	}
}

// TestOpenrcUnstartupCmdRcServiceStopFails covers the "rc-service stop" error
// branch: the init script must survive since removal never runs.
func TestOpenrcUnstartupCmdRcServiceStopFails(t *testing.T) {
	tempDir := t.TempDir()
	initFile := filepath.Join(tempDir, "eos")
	if err := os.WriteFile(initFile, []byte("#!/sbin/openrc-run"), 0755); err != nil {
		t.Fatal(err)
	}
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	var calls []string
	run := selectiveFailRunCmd(t, &calls, rcServiceCmdName, errors.New("rc-service: eos: does not exist"))
	unstartupErr := openrcUnstartupCmd(t.Context(), c, tempDir+"/", "eos", false, false, fakeDetectRuntime("openrc"), run, identity)

	if !errors.Is(unstartupErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", unstartupErr)
	}
	want := []string{"rc-service eos stop"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}
	if !strings.Contains(errBuf.String(), "stopping service") {
		t.Errorf("expected 'stopping service' error, got: %s", errBuf.String())
	}
	if _, statErr := os.Stat(initFile); statErr != nil {
		t.Errorf("expected init script to remain since the command failed before removal, got stat err: %v", statErr)
	}
}

// TestOpenrcUnstartupCmdRcUpdateDelFails covers the "rc-update del" error
// branch: rc-service stop must have already succeeded, and the init script
// must survive since removal never runs.
func TestOpenrcUnstartupCmdRcUpdateDelFails(t *testing.T) {
	tempDir := t.TempDir()
	initFile := filepath.Join(tempDir, "eos")
	if err := os.WriteFile(initFile, []byte("#!/sbin/openrc-run"), 0755); err != nil {
		t.Fatal(err)
	}
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	var calls []string
	run := selectiveFailRunCmd(t, &calls, "rc-update", errors.New("rc-update: openrc is not running"))
	unstartupErr := openrcUnstartupCmd(t.Context(), c, tempDir+"/", "eos", false, false, fakeDetectRuntime("openrc"), run, identity)

	if !errors.Is(unstartupErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", unstartupErr)
	}
	want := []string{"rc-service eos stop", "rc-update del eos default"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}
	if !strings.Contains(errBuf.String(), "disabling service") {
		t.Errorf("expected 'disabling service' error, got: %s", errBuf.String())
	}
	if _, statErr := os.Stat(initFile); statErr != nil {
		t.Errorf("expected init script to remain since rc-update del failed before removal, got stat err: %v", statErr)
	}
}

// TestOpenrcUnstartupCmdRemoveInitScriptFails covers the os.Remove error
// branch: the init script never existed at fullTargetName, so both rc-*
// commands succeed but the final removal fails.
func TestOpenrcUnstartupCmdRemoveInitScriptFails(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	var calls []string
	unstartupErr := openrcUnstartupCmd(t.Context(), c, tempDir+"/", "eos", false, false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls), identity)

	if !errors.Is(unstartupErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", unstartupErr)
	}
	want := []string{"rc-service eos stop", "rc-update del eos default"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected rc-* calls %v, got %v", want, calls)
	}
	if !strings.Contains(errBuf.String(), "removing init script") {
		t.Errorf("expected 'removing init script' error, got: %s", errBuf.String())
	}
}

func TestRenderOpenRCScript(t *testing.T) {
	script, err := renderOpenRCScript("/usr/local/bin", "eosuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, `command="/usr/local/bin/eos"`) {
		t.Errorf("expected command line in script, got: %s", script)
	}
	if !strings.Contains(script, `command_user="eosuser"`) {
		t.Errorf("expected command_user line in script, got: %s", script)
	}
	if !strings.Contains(script, `supervisor="supervise-daemon"`) {
		t.Errorf("expected supervisor line in script, got: %s", script)
	}
}
