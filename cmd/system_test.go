package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/buildinfo"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
	"github.com/spf13/cobra"
)

// slowReader wraps an io.Reader and returns one byte per Read call.
// Required because PromptConfirm creates a new bufio.Reader on each call —
// bufio pre-buffers the whole underlying reader on first fill, leaving nothing
// for the next prompt. One-byte reads prevent that lookahead.
type slowReader struct{ r io.Reader }

func (s *slowReader) Read(p []byte) (int, error) {
	return s.r.Read(p[:1])
}

func makeTestCmd(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c := &cobra.Command{}
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	return c, &out, &errOut
}

func setStdin(c *cobra.Command, input string) {
	c.SetIn(&slowReader{strings.NewReader(input)})
}

func fakeDetectRuntime(runtime string) func() (string, error) {
	return func() (string, error) { return runtime, nil }
}

func fakeDetectRuntimeErr(err error) func() (string, error) {
	return func() (string, error) { return "", err }
}

func recordingRunCmd(t *testing.T, calls *[]string) runCmdFn {
	t.Helper()
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		return []byte("ok"), nil
	}
}

// exitCodeRunCmd returns a runCmdFn that, on its first call, produces a real
// *exec.ExitError with the given exit code (by shelling out to `sh -c "exit N"`) —
// not a synthetic error — so tests exercise the same errors.As(*exec.ExitError)
// path production code hits. Subsequent calls succeed like recordingRunCmd.
func exitCodeRunCmd(t *testing.T, calls *[]string, code int) runCmdFn {
	t.Helper()
	first := true
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if first {
			first = false
			out, err := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("exit %d", code)).CombinedOutput()
			return out, err
		}
		return []byte("ok"), nil
	}
}

func noopRunCmd(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte("ok"), nil
}

func TestStartupCmdNonSystemdRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	_ = startupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos.service", false, false,
		fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected 'not supported' in stderr, got: %s", errBuf.String())
	}
}

func TestStartupCmdRuntimeDetectionError(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	_ = startupCmd(t.Context(), c, "/usr/local/bin", nil, "/tmp/", "eos.service", false, false,
		fakeDetectRuntimeErr(fmt.Errorf("no /proc")), noopRunCmd)

	if !strings.Contains(errBuf.String(), "getting system command") {
		t.Errorf("expected runtime error in stderr, got: %s", errBuf.String())
	}
}

func TestStartupCmdDeclineUnitFile(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	_ = startupCmd(t.Context(), c, "/usr/local/bin", &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false, false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls when user declines, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "unit file creation canceled") {
		t.Errorf("expected cancelation message, got: %s", outBuf.String())
	}
}

func TestStartupCmdWritesUnitFileAndEnablesWithoutRestart(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unit file creation, decline restart
	setStdin(c, "y\nn\n")

	var calls []string
	_ = startupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false, true,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if !strings.Contains(errBuf.String(), "debug") {
		t.Errorf("expected debug output in stderr with verbose=true, got: %s", errBuf.String())
	}

	unitFilePath := filepath.Join(tempDir, "eos.service")
	if _, err := os.Stat(unitFilePath); os.IsNotExist(err) {
		t.Error("expected unit file to be written")
	}

	want := []string{"systemctl daemon-reload", "systemctl enable eos"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected systemctl calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "system unit enabled, eos will start on boot") {
		t.Errorf("expected 'system unit enabled' message, got: %s", outBuf.String())
	}
}

func TestStartupCmdFullRestartPath(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	// confirm unit file, confirm restart
	setStdin(c, "y\ny\n")

	var calls []string
	_ = startupCmd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "eos.service", false, false,
		fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls))

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	want := []string{"systemctl daemon-reload", "systemctl enable eos", "systemctl start eos"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected systemctl calls %v, got %v", want, calls)
	}
}

func TestUnstartupCmdNonSystemdRuntime(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	_ = unstartupCmd(t.Context(), c, config.SystemdConfig{}, false, false, fakeDetectRuntime("openrc"), recordingRunCmd(t, &calls), identity)

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls, got: %v", calls)
	}
	if !strings.Contains(errBuf.String(), "not supported") {
		t.Errorf("expected 'not supported' in stderr, got: %s", errBuf.String())
	}
}

func TestUnstartupCmdDeclineConfirmation(t *testing.T) {
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	_ = unstartupCmd(t.Context(), c, config.SystemdConfig{}, false, false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls), identity)

	if len(calls) != 0 {
		t.Errorf("expected no systemctl calls when declined, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "canceled") {
		t.Errorf("expected 'canceled' message, got: %s", outBuf.String())
	}
}

func TestUnstartupCmdRemovesUnitAndReloads(t *testing.T) {
	tempDir := t.TempDir()
	unitFile := filepath.Join(tempDir, "eos.service")
	if err := os.WriteFile(unitFile, []byte("[Unit]"), 0644); err != nil {
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
	_ = unstartupCmd(t.Context(), c, config.SystemdConfig{
		SystemdTargetDir:      tempDir + "/",
		SystemdTargetFileName: "eos.service",
	}, false, false, fakeDetectRuntime("systemd"), recordingRunCmd(t, &calls), identity)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	if _, err := os.Stat(unitFile); !os.IsNotExist(err) {
		t.Error("expected unit file to be removed")
	}

	want := []string{"systemctl stop eos", "systemctl disable eos", "systemctl daemon-reload"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected systemctl calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "system unit startup removed") {
		t.Errorf("expected success message, got: %s", outBuf.String())
	}
}

func TestRenderPlistFile_SystemIncludesUserName(t *testing.T) {
	plist, err := renderPlistFile("/usr/local/bin", "alice", "org.elysiumlabs.eos", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"<string>org.elysiumlabs.eos</string>", "<string>/usr/local/bin/eos</string>", "<key>UserName</key>", "<string>alice</string>", "RunAtLoad", "KeepAlive"} {
		if !strings.Contains(plist, want) {
			t.Errorf("expected plist to contain %q, got:\n%s", want, plist)
		}
	}
}

func TestRenderPlistFile_UserAgentOmitsUserName(t *testing.T) {
	plist, err := renderPlistFile("/usr/local/bin", "alice", "org.elysiumlabs.eos", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(plist, "UserName") {
		t.Errorf("expected user agent plist to omit UserName, got:\n%s", plist)
	}
	if !strings.Contains(plist, "<string>org.elysiumlabs.eos</string>") {
		t.Errorf("expected label in plist, got:\n%s", plist)
	}
}

func TestLaunchdLabel(t *testing.T) {
	if got := launchdLabel("org.elysiumlabs.eos.plist"); got != "org.elysiumlabs.eos" {
		t.Errorf("expected %q, got %q", "org.elysiumlabs.eos", got)
	}
}

func TestLaunchdDomain(t *testing.T) {
	if got := launchdDomain(false, 501); got != "system" {
		t.Errorf("expected %q, got %q", "system", got)
	}
	if got := launchdDomain(true, 501); got != "gui/501" {
		t.Errorf("expected %q, got %q", "gui/501", got)
	}
}

func TestLaunchdScope(t *testing.T) {
	if got := launchdScope(false); got != "launch daemon" {
		t.Errorf("expected %q, got %q", "launch daemon", got)
	}
	if got := launchdScope(true); got != "launch agent" {
		t.Errorf("expected %q, got %q", "launch agent", got)
	}
}

func TestStartupCmdLaunchdDeclinePlist(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	_ = startupCmdLaunchd(t.Context(), c, "/usr/local/bin", &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "org.elysiumlabs.eos-test.plist", false, false, recordingRunCmd(t, &calls))

	if len(calls) != 0 {
		t.Errorf("expected no launchctl calls when user declines, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "launch daemon file creation canceled") {
		t.Errorf("expected cancelation message, got: %s", outBuf.String())
	}
}

func TestStartupCmdLaunchdWritesPlistAndEnablesWithoutRestart(t *testing.T) {
	tempDir := t.TempDir()
	c, outBuf, errBuf := makeTestCmd(t)
	// confirm plist creation, decline restart
	setStdin(c, "y\nn\n")

	var calls []string
	plistFileName := "org.elysiumlabs.eos-test.plist"
	_ = startupCmdLaunchd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", plistFileName, false, true, recordingRunCmd(t, &calls))

	if !strings.Contains(errBuf.String(), "debug") {
		t.Errorf("expected debug output in stderr with verbose=true, got: %s", errBuf.String())
	}

	plistPath := filepath.Join(tempDir, plistFileName)
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Error("expected plist file to be written")
	}

	if len(calls) != 3 {
		t.Fatalf("expected 3 launchctl calls (bootout, bootstrap, enable), got: %v", calls)
	}
	if !strings.HasPrefix(calls[0], "launchctl bootout system/org.elysiumlabs.eos-test") {
		t.Errorf("expected best-effort bootout first, got: %v", calls)
	}
	if !strings.HasPrefix(calls[1], "launchctl bootstrap system "+plistPath) {
		t.Errorf("expected bootstrap call, got: %v", calls)
	}
	if calls[2] != "launchctl enable system/org.elysiumlabs.eos-test" {
		t.Errorf("expected enable call, got: %v", calls)
	}

	if !strings.Contains(outBuf.String(), "launch daemon enabled, eos will start on boot") {
		t.Errorf("expected enabled message, got: %s", outBuf.String())
	}
}

func TestStartupCmdLaunchdFullRestartPath(t *testing.T) {
	tempDir := t.TempDir()
	c, _, errBuf := makeTestCmd(t)
	// confirm plist creation, confirm restart
	setStdin(c, "y\ny\n")

	var calls []string
	_ = startupCmdLaunchd(t.Context(), c, filepath.Join(tempDir, "eos"), &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}, tempDir+"/", "org.elysiumlabs.eos-test.plist", false, false, recordingRunCmd(t, &calls))

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	if len(calls) != 4 {
		t.Fatalf("expected 4 launchctl calls (bootout, bootstrap, enable, kickstart), got: %v", calls)
	}
	if !strings.HasPrefix(calls[3], "launchctl kickstart -k system/org.elysiumlabs.eos-test") {
		t.Errorf("expected kickstart call last, got: %v", calls)
	}
}

func TestUnstartupCmdLaunchdDeclineConfirmation(t *testing.T) {
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "n\n")

	var calls []string
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	_ = unstartupCmdLaunchd(t.Context(), c, config.LaunchdConfig{}, false, false, recordingRunCmd(t, &calls), identity)

	if len(calls) != 0 {
		t.Errorf("expected no launchctl calls when declined, got: %v", calls)
	}
	if !strings.Contains(outBuf.String(), "canceled") {
		t.Errorf("expected 'canceled' message, got: %s", outBuf.String())
	}
}

func TestUnstartupCmdLaunchdRemovesPlistAndBootsOut(t *testing.T) {
	tempDir := t.TempDir()
	plistFileName := "org.elysiumlabs.eos-test.plist"
	plistFile := filepath.Join(tempDir, plistFileName)
	if err := os.WriteFile(plistFile, []byte("<plist/>"), 0644); err != nil {
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
	_ = unstartupCmdLaunchd(t.Context(), c, config.LaunchdConfig{
		LaunchdTargetDir:     tempDir + "/",
		LaunchdPlistFileName: plistFileName,
	}, false, false, recordingRunCmd(t, &calls), identity)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}

	if _, err := os.Stat(plistFile); !os.IsNotExist(err) {
		t.Error("expected plist file to be removed")
	}

	want := []string{"launchctl bootout system/org.elysiumlabs.eos-test"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected launchctl calls %v, got %v", want, calls)
	}

	if !strings.Contains(outBuf.String(), "launch daemon startup removed") {
		t.Errorf("expected success message, got: %s", outBuf.String())
	}
}

func TestUnstartupCmdLaunchdToleratesJobNotLoaded(t *testing.T) {
	tempDir := t.TempDir()
	plistFileName := "org.elysiumlabs.eos-test.plist"
	plistFile := filepath.Join(tempDir, plistFileName)
	if err := os.WriteFile(plistFile, []byte("<plist/>"), 0644); err != nil {
		t.Fatal(err)
	}

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unstartup, decline restart standalone
	setStdin(c, "y\nn\n")

	var calls []string
	// exit code 3 ("No such process") is what launchctl bootout returns when the job
	// isn't currently loaded — must be tolerated, not treated as a fatal error, or the
	// plist would never be removed whenever the job happened to already be stopped.
	identity, identityErr := userutil.ResolveIdentity()
	if identityErr != nil {
		t.Fatalf("resolving identity: %v", identityErr)
	}
	err := unstartupCmdLaunchd(t.Context(), c, config.LaunchdConfig{
		LaunchdTargetDir:     tempDir + "/",
		LaunchdPlistFileName: plistFileName,
	}, false, false, exitCodeRunCmd(t, &calls, 3), identity)

	if err != nil {
		t.Errorf("expected exit code 3 to be tolerated, got error: %v", err)
	}
	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr: %s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "was not loaded") {
		t.Errorf("expected 'was not loaded' message, got: %s", outBuf.String())
	}
	if _, statErr := os.Stat(plistFile); !os.IsNotExist(statErr) {
		t.Error("expected plist file to still be removed when the job was already unloaded")
	}
	if !strings.Contains(outBuf.String(), "launch daemon startup removed") {
		t.Errorf("expected success message, got: %s", outBuf.String())
	}
}

func TestUnstartupCmdLaunchdOtherErrorsAreFatal(t *testing.T) {
	tempDir := t.TempDir()
	plistFileName := "org.elysiumlabs.eos-test.plist"
	plistFile := filepath.Join(tempDir, plistFileName)
	if err := os.WriteFile(plistFile, []byte("<plist/>"), 0644); err != nil {
		t.Fatal(err)
	}

	c, _, errBuf := makeTestCmd(t)
	setStdin(c, "y\n")

	var calls []string
	// A generic non-3 failure (e.g. permission denied) must remain fatal and must not
	// remove the plist file.
	identity, identityErr := userutil.ResolveIdentity()
	if identityErr != nil {
		t.Fatalf("resolving identity: %v", identityErr)
	}
	err := unstartupCmdLaunchd(t.Context(), c, config.LaunchdConfig{
		LaunchdTargetDir:     tempDir + "/",
		LaunchdPlistFileName: plistFileName,
	}, false, false, exitCodeRunCmd(t, &calls, 1), identity)

	if err == nil {
		t.Fatal("expected a non-3 exit code to be a fatal error")
	}
	if !strings.Contains(errBuf.String(), "stopping") {
		t.Errorf("expected stopping error message, got: %s", errBuf.String())
	}
	if _, statErr := os.Stat(plistFile); statErr != nil {
		t.Error("expected plist file to be left in place when bootout fails fatally")
	}
}

func TestIsAccessibleDir_AcceptsOwnDir(t *testing.T) {
	dir := t.TempDir()
	if !isAccessibleDir(dir, os.Getuid()) {
		t.Error("expected own directory to be accessible")
	}
}

func TestIsAccessibleDir_RejectsMissingDir(t *testing.T) {
	if isAccessibleDir(filepath.Join(t.TempDir(), "gone"), os.Getuid()) {
		t.Error("expected missing path to be inaccessible")
	}
}

func TestIsAccessibleDir_RejectsWrongOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown a directory to another uid")
	}
	dir := t.TempDir()
	otherUID := os.Getuid() + 1
	if err := os.Chown(dir, otherUID, os.Getgid()); err != nil {
		t.Fatalf("chown: %v", err)
	}
	if isAccessibleDir(dir, os.Getuid()) {
		t.Error("expected directory owned by another uid to be rejected, even though stat succeeds")
	}
}

func TestIsAccessibleDir_AcceptsDirOwnedByPassedUIDRegardlessOfProcessUID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown a directory to another uid")
	}
	dir := t.TempDir()
	targetUID := os.Getuid() + 1
	if err := os.Chown(dir, targetUID, os.Getgid()); err != nil {
		t.Fatalf("chown: %v", err)
	}
	if !isAccessibleDir(dir, targetUID) {
		t.Error("expected directory to be accessible when its owner matches the passed uid, even though the process's own uid differs")
	}
}

func TestEnsureUserBusAvailable_CorrectsWhenCurrentOwnedByOtherUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown a directory to another uid")
	}
	c, _, errBuf := makeTestCmd(t)

	stale := t.TempDir()
	if err := os.Chown(stale, os.Getuid()+1, os.Getgid()); err != nil {
		t.Fatalf("chown: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", stale)
	expected := t.TempDir()

	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run should not be called when the expected runtime dir already exists")
		return nil, nil
	}

	if err := ensureUserBusAvailable(t.Context(), c, true, "testuser", os.Getuid(), expected, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != expected {
		t.Errorf("expected XDG_RUNTIME_DIR corrected to %q, got %q", expected, got)
	}
	if !strings.Contains(errBuf.String(), "correcting XDG_RUNTIME_DIR") {
		t.Errorf("expected debug output about correcting XDG_RUNTIME_DIR, got: %s", errBuf.String())
	}
}

// TestEnsureUserBusAvailable_AcceptsExpectedOwnedByTargetUIDUnderSudo guards against regressing to
// checking os.Getuid() internally: under `sudo`, os.Getuid() is 0 (root) while the systemd --user
// session being managed belongs to a different, non-root target uid (resolved by the caller via
// userutil.EffectiveUser()). ensureUserBusAvailable must trust the passed-in uid, not the process's
// own uid, or it wrongly rejects the target user's legitimately-owned runtime dir.
func TestEnsureUserBusAvailable_AcceptsExpectedOwnedByTargetUIDUnderSudo(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown a directory to another uid")
	}
	c, _, errBuf := makeTestCmd(t)
	t.Setenv("XDG_RUNTIME_DIR", "")

	targetUID := os.Getuid() + 1 // simulates the sudo-invoking user, distinct from the process's own (root) uid
	expected := t.TempDir()
	if err := os.Chown(expected, targetUID, os.Getgid()); err != nil {
		t.Fatalf("chown: %v", err)
	}

	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run should not be called when the expected runtime dir already exists")
		return nil, nil
	}

	if err := ensureUserBusAvailable(t.Context(), c, true, "testuser", targetUID, expected, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != expected {
		t.Errorf("expected XDG_RUNTIME_DIR corrected to %q, got %q", expected, got)
	}
	if !strings.Contains(errBuf.String(), "correcting XDG_RUNTIME_DIR") {
		t.Errorf("expected debug output about correcting XDG_RUNTIME_DIR, got: %s", errBuf.String())
	}
}

func TestEnsureUserBusAvailable_CorrectsStaleEnvVar(t *testing.T) {
	c, _, errBuf := makeTestCmd(t)
	expected := t.TempDir()
	stale := filepath.Join(t.TempDir(), "gone")
	t.Setenv("XDG_RUNTIME_DIR", stale)

	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run should not be called when the expected runtime dir already exists")
		return nil, nil
	}

	if err := ensureUserBusAvailable(t.Context(), c, true, "testuser", os.Getuid(), expected, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != expected {
		t.Errorf("expected XDG_RUNTIME_DIR corrected to %q, got %q", expected, got)
	}
	if !strings.Contains(errBuf.String(), "correcting XDG_RUNTIME_DIR") {
		t.Errorf("expected debug output about correcting XDG_RUNTIME_DIR, got: %s", errBuf.String())
	}
}

func TestEnsureUserBusAvailable_DeclinePrompt(t *testing.T) {
	c, _, _ := makeTestCmd(t)
	setStdin(c, "n\n")
	t.Setenv("XDG_RUNTIME_DIR", "")
	expected := filepath.Join(t.TempDir(), "missing")

	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run should not be called when the user declines enabling linger")
		return nil, nil
	}

	err := ensureUserBusAvailable(t.Context(), c, false, "testuser", os.Getuid(), expected, run)
	if err == nil {
		t.Fatal("expected error when user declines enabling linger")
	}
}

func TestEnsureUserBusAvailable_EnablesLingerAndRecovers(t *testing.T) {
	c, outBuf, _ := makeTestCmd(t)
	setStdin(c, "y\n")
	t.Setenv("XDG_RUNTIME_DIR", "")
	expected := filepath.Join(t.TempDir(), "runtime")

	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if err := os.MkdirAll(expected, 0700); err != nil {
			t.Fatal(err)
		}
		return []byte("ok"), nil
	}

	if err := ensureUserBusAvailable(t.Context(), c, false, "testuser", os.Getuid(), expected, run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != expected {
		t.Errorf("expected XDG_RUNTIME_DIR set to %q, got %q", expected, got)
	}
	want := []string{"loginctl enable-linger testuser"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("expected calls %v, got %v", want, calls)
	}
	if !strings.Contains(outBuf.String(), "user bus is now available") {
		t.Errorf("expected success message, got: %s", outBuf.String())
	}
}

func TestSystemInfoCommand(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"system", "info"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing info test - add should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "System Config") {
		t.Errorf("expected the output to contain 'System Config' header, got: %s", output)
	}
}

func TestSystemUpdateCommand(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"system", "update"})

	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v\n", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "dev") {
		t.Errorf("expected the output to contain 'dev', got: %s", output)
	}
}

// func TestCopyFile_TextFileBusy(t *testing.T) {
// 	if runtime.GOOS != "linux" {
// 		t.Skip("text file busy error is Linux-specific")
// 	}

// 	tempDir := t.TempDir()

// 	// Create a minimal executable binary (a simple sleep script won't work;
// 	// we need an actual ELF binary). We'll compile a tiny Go program.
// 	srcPath := filepath.Join(tempDir, "main.go")
// 	err := os.WriteFile(srcPath, []byte(`package main
// import "time"
// func main() { time.Sleep(30 * time.Second) }
// `), 0644)
// 	if err != nil {
// 		t.Fatalf("failed to write source file: %v", err)
// 	}

// 	binaryPath := filepath.Join(tempDir, "eos")
// 	buildCmd := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, srcPath)
// 	if out, outputErr := buildCmd.CombinedOutput(); outputErr != nil {
// 		t.Fatalf("failed to compile test binary: %v\n%s", outputErr, out)
// 	}

// 	// Start the binary so the OS holds it as "text busy"
// 	ctx, cancel := context.WithCancel(t.Context())
// 	defer cancel()

// 	proc := exec.CommandContext(ctx, binaryPath)
// 	if startErr := proc.Start(); startErr != nil {
// 		t.Fatalf("failed to start test binary: %v", startErr)
// 	}
// 	defer func() {
// 		cancel()
// 		_ = proc.Wait()
// 	}()

// 	time.Sleep(100 * time.Millisecond)

// 	replacementPath := filepath.Join(tempDir, "eos_new")
// 	err = os.WriteFile(replacementPath, []byte("new binary content"), 0755)
// 	if err != nil {
// 		t.Fatalf("failed to create replacement file: %v", err)
// 	}

// 	err = copyFile(replacementPath, binaryPath)
// 	if err == nil {
// 		t.Fatal("expected 'text file busy' error, got nil")
// 	}

// 	if !strings.Contains(err.Error(), "text file busy") {
// 		t.Errorf("expected 'text file busy' in error message, got: %v", err)
// 	}
// }

func TestSystemUpdateWithInvalidVersionCommand(t *testing.T) {
	buildinfo.Version = "invalid-version"
	defer func() { buildinfo.Version = "dev" }()

	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	cmd.SetArgs([]string{"system", "update"})

	err = cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v\n", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "invalid version tag, must start with 'v") {
		t.Errorf("expected the output to contain 'invalid version tag, must start with 'v', got: %s", output)
	}
}

func TestSystemUpdateWithInvalidOSArchCombinationCommand(t *testing.T) {
	buildinfo.Version = "v0.0.1"
	defer func() { buildinfo.Version = "dev" }()

	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error, got: %v\n", err)
	}

	installDir, baseDir, systemConfig, identity, err := newSystemConfig()
	if err != nil {
		t.Fatalf("preparing update test - newSystemConfig should not return an error: %v\n", err)
	}

	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd, identity)
	if err != nil {
		t.Fatalf("preparing update test - newDaemonController should not return an error: %v\n", err)
	}

	fakeFetchRelease := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{
			TagName: "v99.0.0",
			Assets:  []Asset{{Name: "eos-linux-arm64"}},
		}, nil
	}

	_ = updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, ctrl, "arm64", "darwin", false, fakeFetchRelease, handleDownloadBinary, fetchChecksumForBinary)

	output := errBuf.String()

	if !strings.Contains(output, "no compatible asset found") {
		t.Errorf("expected the output to contain 'no compatible asset found', got: %s", output)
	}
}

func TestSystemUpdateWithLowerVersionCommand(t *testing.T) {
	buildinfo.Version = "v0.0.1"
	defer func() { buildinfo.Version = "dev" }()

	cmd, outBuf, _, tempDir := setupCmd(t)

	t.Setenv("EOS_INSTALL_DIR", tempDir)

	err := os.Mkdir(filepath.Join(tempDir, "eos"), 0755)
	if err != nil {
		t.Fatalf("preparing update test - mkdir should not return an error: %v\n", err)
	}

	installDir, baseDir, systemConfig, identity, err := newSystemConfig()
	if err != nil {
		t.Fatalf("preparing update test - newSystemConfig should not return an error: %v\n", err)
	}

	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd, identity)
	if err != nil {
		t.Fatalf("preparing update test - newDaemonController should not return an error: %v\n", err)
	}

	binaryContent := []byte("fake binary")
	sum := sha256.Sum256(binaryContent)
	expectedDigest := hex.EncodeToString(sum[:])

	fakeFetchRelease := func(_ context.Context, _ bool) (*Release, error) {
		return &Release{
			TagName: "v99.0.0",
			Assets: []Asset{
				{
					Name:               "eos-linux-arm64",
					BrowserDownloadURL: "https://codeberg.org/fake/download",
				},
			},
		}, nil
	}

	fakeGetChecksum := func(_ context.Context, _ *Asset, _ string) (string, error) {
		return expectedDigest, nil
	}

	fakeDownloadBinary := func(_ context.Context, asset *Asset) (*os.File, string, error) {
		dir := t.TempDir()
		f, createErr := os.CreateTemp(dir, asset.Name)
		if createErr != nil {
			return nil, "", createErr
		}
		if _, writeErr := f.Write(binaryContent); writeErr != nil {
			_ = f.Close()
			return nil, "", writeErr
		}
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			_ = f.Close()
			return nil, "", seekErr
		}
		return f, dir, nil
	}

	setStdin(cmd, "y\ny\n")

	_ = updateCmd(t.Context(), cmd, buildinfo.GetVersionOnly(), installDir, ctrl, "arm64", "linux", false, fakeFetchRelease, fakeDownloadBinary, fakeGetChecksum)

	output := outBuf.String()

	if !strings.Contains(output, "info checksums match") {
		t.Errorf("expected the output to contain 'info checksums match', got: %s", output)
	}
}

func TestSystemUpdateCheckWritableFailed(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test directory permission restrictions as root")
	}
	dir := t.TempDir()
	err := os.Chmod(dir, 0555)
	if err != nil {
		t.Fatalf("errored during test setup")
	}

	testCmd := &cobra.Command{}

	err = checkWritable(testCmd, dir)
	if err == nil {
		t.Fatalf("expected permission issues, got: %v", err)
	}
}

func TestSystemUpdateCopyFile(t *testing.T) {
	dir := t.TempDir()

	src, err := os.CreateTemp(dir, "srcTest")
	if err != nil {
		t.Fatal("creating file srcTest")
	}

	testContent := "Hello World"
	err = os.WriteFile(src.Name(), []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("writing file srcTest, got: %v", err)
	}

	dst, err := os.CreateTemp(dir, "dstTest")
	if err != nil {
		t.Fatalf("creating file dstTest, got: %v", err)
	}

	err = copyFile(src.Name(), dst.Name())
	if err != nil {
		t.Fatalf("copyFile errored, got: %v", err)
	}

	content, err := os.ReadFile(dst.Name())
	if err != nil {
		t.Fatalf("read destination file, got: %v", err)
	}
	if string(content) != testContent {
		t.Fatalf("expected to read same content on destination as source, got: %s", string(content))
	}
}

func TestSystemUpdateCreateDestinationFile(t *testing.T) {
	dir := t.TempDir()

	testFile := filepath.Join(dir, "dstTest")
	err := createDestinationFile(testFile)
	if err != nil {
		t.Fatalf("expected create destination file to not error, got: %v", err)
	}
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatalf("expected file to exist, but it does not")
	}
}

func TestSystemVersionCommand(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"system", "version"})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("preparing version test - add should not return an error, got: %v\n", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "dev") {
		t.Errorf("expected the output to contain 'dev', got: %s", output)
	}
}

// hostRedirectTransport rewrites any request to hit addr over plain HTTP.
// Lets tests intercept hardcoded codeberg.org URLs.
type hostRedirectTransport struct{ addr string }

func (h *hostRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.URL.Host = h.addr
	r2.URL.Scheme = "http"
	return http.DefaultTransport.RoundTrip(r2)
}

// useHTTPTestServer starts an httptest.Server and wires httpClient to route
// all requests to it. Restores the original client on test cleanup.
func useHTTPTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	orig := httpClient
	httpClient = &http.Client{Transport: &hostRedirectTransport{addr: srv.Listener.Addr().String()}}
	t.Cleanup(func() {
		httpClient = orig
		srv.Close()
	})
}

func TestFetchLatestRelease_success(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Release{TagName: "v2.0.0"})
	})
	rel, err := fetchLatestRelease(t.Context(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.TagName != "v2.0.0" {
		t.Errorf("got tag %q", rel.TagName)
	}
}

func TestFetchLatestRelease_includePre(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Release{{TagName: "v3.0.0-rc.1"}})
	})
	rel, err := fetchLatestRelease(t.Context(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel.TagName != "v3.0.0-rc.1" {
		t.Errorf("got tag %q", rel.TagName)
	}
}

func TestFetchLatestRelease_nonOKStatus(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := fetchLatestRelease(t.Context(), false)
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
}

func TestFetchLatestRelease_emptyPreList(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Release{})
	})
	_, err := fetchLatestRelease(t.Context(), true)
	if err == nil {
		t.Fatal("expected error for empty pre-release list")
	}
}

func TestFetchLatestRelease_badJSON(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	_, err := fetchLatestRelease(t.Context(), false)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestHandleDownloadBinary_success(t *testing.T) {
	content := []byte("fake binary content")
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	})
	asset := &Asset{
		Name:               "eos-linux-amd64",
		BrowserDownloadURL: "https://codeberg.org/fake/download",
	}
	f, tempDir, err := handleDownloadBinary(t.Context(), asset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = f.Close() }()

	got, readErr := io.ReadAll(f)
	if readErr != nil {
		t.Fatalf("reading downloaded file: %v", readErr)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestHandleDownloadBinary_invalidURL(t *testing.T) {
	asset := &Asset{
		Name:               "eos-linux-amd64",
		BrowserDownloadURL: "https://github.com/fake/download",
	}
	_, _, err := handleDownloadBinary(t.Context(), asset)
	if err == nil {
		t.Fatal("expected error for non-codeberg URL")
	}
}

func TestHandleDownloadBinary_nonOK(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	asset := &Asset{
		Name:               "eos-linux-amd64",
		BrowserDownloadURL: "https://codeberg.org/fake/download",
	}
	_, _, err := handleDownloadBinary(t.Context(), asset)
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
}

func TestFetchChecksumForBinary_success(t *testing.T) {
	binaryName := "eos-linux-amd64"
	checksum := "abc123def456"
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", checksum, binaryName)
	})
	asset := &Asset{
		Name:               "sha256sums.txt",
		BrowserDownloadURL: "https://codeberg.org/fake/sha256sums.txt",
	}
	got, err := fetchChecksumForBinary(t.Context(), asset, binaryName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != checksum {
		t.Errorf("got checksum %q, want %q", got, checksum)
	}
}

func TestFetchChecksumForBinary_nilAsset(t *testing.T) {
	_, err := fetchChecksumForBinary(t.Context(), nil, "eos-linux-amd64")
	if err == nil {
		t.Fatal("expected error for nil asset")
	}
}

func TestFetchChecksumForBinary_notFound(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "abc123  eos-linux-arm64\n")
	})
	asset := &Asset{BrowserDownloadURL: "https://codeberg.org/fake/sha256sums.txt"}
	_, err := fetchChecksumForBinary(t.Context(), asset, "eos-linux-amd64")
	if err == nil {
		t.Fatal("expected error when binary not found in checksums")
	}
}

func TestFetchChecksumForBinary_nonOK(t *testing.T) {
	useHTTPTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	asset := &Asset{BrowserDownloadURL: "https://codeberg.org/fake/sha256sums.txt"}
	_, err := fetchChecksumForBinary(t.Context(), asset, "eos-linux-amd64")
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
}

func TestReplaceBinary_success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "eos-new")
	dst := filepath.Join(dir, "eos")

	if err := os.WriteFile(src, []byte("new binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Errorf("expected 'new binary', got %q", got)
	}
}

func TestValidateDigest_mismatch(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "binary")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err = f.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}

	if err = validateDigest("wrongchecksum", f); err == nil {
		t.Fatal("expected error for checksum mismatch")
	}
}

func TestExtractServiceInstancesFromErrors(t *testing.T) {
	errs := ErrorServices{
		"svc1": {Service: types.ServiceInstance{Name: "svc1"}, Error: fmt.Errorf("err1")},
		"svc2": {Service: types.ServiceInstance{Name: "svc2"}, Error: fmt.Errorf("err2")},
	}
	got := extractServiceInstancesFromErrors(errs)
	if len(got) != 2 {
		t.Errorf("expected 2 instances, got %d", len(got))
	}
}

// mockMgr is a test-only manager.ServiceManager that delegates to optional func fields.
type mockMgr struct {
	getAllInstances func() ([]types.ServiceInstance, error)
	stopSvc         func(string, time.Duration, time.Duration) (manager.StopServiceResult, error)
	forceStop       func(string) (manager.StopServiceResult, error)
	removeInstance  func(string) (bool, error)
}

func (m *mockMgr) GetAllServiceInstances() ([]types.ServiceInstance, error) {
	if m.getAllInstances != nil {
		return m.getAllInstances()
	}
	return nil, nil
}
func (m *mockMgr) StopService(name string, gp, tp time.Duration) (manager.StopServiceResult, error) {
	if m.stopSvc != nil {
		return m.stopSvc(name, gp, tp)
	}
	return manager.StopServiceResult{}, nil
}
func (m *mockMgr) ForceStopService(name string) (manager.StopServiceResult, error) {
	if m.forceStop != nil {
		return m.forceStop(name)
	}
	return manager.StopServiceResult{}, nil
}
func (m *mockMgr) RemoveServiceInstance(name string) (bool, error) {
	if m.removeInstance != nil {
		return m.removeInstance(name)
	}
	return true, nil
}
func (m *mockMgr) GetServiceInstance(string) (*types.ServiceInstance, error) {
	return &types.ServiceInstance{}, nil
}
func (m *mockMgr) AddServiceCatalogEntry(*types.ServiceCatalogEntry) error           { return nil }
func (m *mockMgr) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) { return nil, nil }
func (m *mockMgr) GetServiceCatalogEntry(string) (types.ServiceCatalogEntry, error) {
	return types.ServiceCatalogEntry{}, nil
}
func (m *mockMgr) IsServiceRegistered(string) (bool, error)               { return false, nil }
func (m *mockMgr) RemoveServiceCatalogEntry(string) (bool, error)         { return false, nil }
func (m *mockMgr) UpdateServiceCatalogEntry(string, string, string) error { return nil }
func (m *mockMgr) GetMostRecentProcessHistoryEntry(string) (*types.ProcessHistory, error) {
	return &types.ProcessHistory{}, nil
}
func (m *mockMgr) NewServiceLogFiles(string) (string, string, error) { return "", "", nil }
func (m *mockMgr) GetServiceLogFilePath(string, bool) (*string, error) {
	s := ""
	return &s, nil
}
func (m *mockMgr) RestartService(string, time.Duration, time.Duration) (int, error) { return 0, nil }
func (m *mockMgr) StartService(string) (int, error)                                 { return 0, nil }

func TestStopServices_allSuccess(t *testing.T) {
	mgr := &mockMgr{
		stopSvc: func(_ string, _, _ time.Duration) (manager.StopServiceResult, error) {
			return manager.StopServiceResult{Stopped: map[int]bool{1: true}}, nil
		},
	}
	instances := []types.ServiceInstance{{Name: "svc1"}, {Name: "svc2"}}
	cfg := &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Second}}

	stopped, errored := stopServices(mgr, cfg, instances)
	if len(stopped) != 2 {
		t.Errorf("expected 2 stopped, got %d", len(stopped))
	}
	if len(errored) != 0 {
		t.Errorf("expected 0 errored, got %d: %v", len(errored), errored)
	}
}

func TestStopServices_withError(t *testing.T) {
	mgr := &mockMgr{
		stopSvc: func(name string, _, _ time.Duration) (manager.StopServiceResult, error) {
			if name == "svc2" {
				return manager.StopServiceResult{}, fmt.Errorf("stop failed")
			}
			return manager.StopServiceResult{Stopped: map[int]bool{1: true}}, nil
		},
	}
	instances := []types.ServiceInstance{{Name: "svc1"}, {Name: "svc2"}}
	cfg := &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Second}}

	stopped, errored := stopServices(mgr, cfg, instances)
	if len(stopped) != 1 {
		t.Errorf("expected 1 stopped, got %d", len(stopped))
	}
	if len(errored) != 1 {
		t.Errorf("expected 1 errored, got %d", len(errored))
	}
}

func TestForceStopServices_allSuccess(t *testing.T) {
	mgr := &mockMgr{
		forceStop: func(_ string) (manager.StopServiceResult, error) {
			return manager.StopServiceResult{Stopped: map[int]bool{2: true}}, nil
		},
	}
	instances := []types.ServiceInstance{{Name: "svc1"}, {Name: "svc2"}}

	errored := forceStopServices(mgr, instances)
	if len(errored) != 0 {
		t.Errorf("expected 0 errored, got %d: %v", len(errored), errored)
	}
}

func TestForceStopServices_withError(t *testing.T) {
	mgr := &mockMgr{
		forceStop: func(name string) (manager.StopServiceResult, error) {
			if name == "svc1" {
				return manager.StopServiceResult{}, fmt.Errorf("force stop failed")
			}
			return manager.StopServiceResult{Stopped: map[int]bool{3: true}}, nil
		},
	}
	instances := []types.ServiceInstance{{Name: "svc1"}, {Name: "svc2"}}

	errored := forceStopServices(mgr, instances)
	if len(errored) != 1 {
		t.Errorf("expected 1 errored, got %d", len(errored))
	}
}

func TestSupportedPlatformsMatchCheckForUpdates(t *testing.T) {
	for _, platform := range supportedPlatforms {
		parts := strings.SplitN(platform, "-", 2)
		if len(parts) != 2 {
			t.Errorf("supportedPlatforms entry %q is not in os-arch format", platform)
			continue
		}
		goos, goarch := parts[0], parts[1]

		release := &Release{
			TagName: "v99.0.0",
			Assets:  []Asset{{Name: "eos-" + goos + "-" + goarch}},
		}
		result, err := checkForUpdates(release, "v0.0.1", goarch, goos)
		if err != nil {
			t.Errorf("supportedPlatforms entry %q not matched by checkForUpdates: %v", platform, err)
		}
		if result.Asset == nil {
			t.Errorf("supportedPlatforms entry %q: checkForUpdates returned nil asset", platform)
		}
	}
}

type stubUpdateController struct {
	stopErr    error
	startErr   error
	startCount int
	killed     bool
}

func (s *stubUpdateController) Start(_ context.Context, _, _, _ bool) error {
	s.startCount++
	return s.startErr
}
func (s *stubUpdateController) Stop(_ context.Context, _ *cobra.Command, _ bool) (bool, error) {
	return s.killed, s.stopErr
}
func (s *stubUpdateController) Remove() error                        { return nil }
func (s *stubUpdateController) Info(_ *cobra.Command)                {}
func (s *stubUpdateController) Logs(_ *cobra.Command, _ int, _ bool) {}
func (s *stubUpdateController) LogsHint() string                     { return "eos daemon logs" }

func TestRestartDaemonAfterUpdate(t *testing.T) {
	t.Run("declined leaves manual restart hint", func(t *testing.T) {
		cmd, outBuf, _, _ := setupCmd(t)
		setStdin(cmd, "n\n")
		ctrl := &stubUpdateController{}

		if err := restartDaemonAfterUpdate(t.Context(), cmd, ctrl, t.TempDir(), "v9.9.9"); err != nil {
			t.Fatalf("expected nil error when declined, got %v", err)
		}
		if ctrl.startCount != 0 {
			t.Errorf("Start should not be called when restart declined")
		}
		if !strings.Contains(outBuf.String(), "manual daemon restart required") {
			t.Errorf("expected manual restart hint, got %s", outBuf.String())
		}
	})

	t.Run("not running short-circuits before start", func(t *testing.T) {
		cmd, outBuf, _, _ := setupCmd(t)
		setStdin(cmd, "y\n")
		ctrl := &stubUpdateController{killed: false}

		if err := restartDaemonAfterUpdate(t.Context(), cmd, ctrl, t.TempDir(), "v9.9.9"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if ctrl.startCount != 0 {
			t.Errorf("Start should not be called when daemon was not running")
		}
		if !strings.Contains(outBuf.String(), "daemon was not running") {
			t.Errorf("expected not-running message, got %s", outBuf.String())
		}
	})

	t.Run("stop error fails the command", func(t *testing.T) {
		cmd, _, _, _ := setupCmd(t)
		setStdin(cmd, "y\n")
		ctrl := &stubUpdateController{stopErr: errors.New("boom")}

		if err := restartDaemonAfterUpdate(t.Context(), cmd, ctrl, t.TempDir(), "v9.9.9"); err == nil {
			t.Fatal("expected error when stop fails")
		}
	})

	t.Run("start error surfaces logs hint", func(t *testing.T) {
		cmd, _, errBuf, _ := setupCmd(t)
		setStdin(cmd, "y\n")
		ctrl := &stubUpdateController{killed: true, startErr: errors.New("nope")}

		if err := restartDaemonAfterUpdate(t.Context(), cmd, ctrl, t.TempDir(), "v9.9.9"); err == nil {
			t.Fatal("expected error when start fails")
		}
		if !strings.Contains(errBuf.String(), "eos daemon logs") {
			t.Errorf("expected logs hint in output, got %s", errBuf.String())
		}
	})

	t.Run("successful restart cleans up and reports success", func(t *testing.T) {
		cmd, outBuf, _, _ := setupCmd(t)
		setStdin(cmd, "y\n")
		ctrl := &stubUpdateController{killed: true}
		tempDir := t.TempDir()

		if err := restartDaemonAfterUpdate(t.Context(), cmd, ctrl, tempDir, "v9.9.9"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if ctrl.startCount != 1 {
			t.Errorf("expected Start called once, got %d", ctrl.startCount)
		}
		if !strings.Contains(outBuf.String(), "eos updated to") {
			t.Errorf("expected success message, got %s", outBuf.String())
		}
		if _, statErr := os.Stat(tempDir); !os.IsNotExist(statErr) {
			t.Errorf("expected temp dir removed, stat err = %v", statErr)
		}
	})
}
