package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/config"
)

func TestOpenrcStartupCmdNonOpenRCRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	_ = openrcStartupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos", false,
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
	_ = openrcStartupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos", false,
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
	}, tempDir+"/", "eos", false,
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
	}, tempDir+"/", "eos", true,
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
	}, tempDir+"/", "eos", false,
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
	_ = openrcUnstartupCmd(t.Context(), c, "/tmp/", "eos", false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

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
	_ = openrcUnstartupCmd(t.Context(), c, "/tmp/", "eos", false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

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
	_ = openrcUnstartupCmd(t.Context(), c, tempDir+"/", "eos", false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

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
