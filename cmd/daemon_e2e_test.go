//go:build integration

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// e2eTempDir creates a short-path temp dir under /tmp.
// Required on macOS: t.TempDir() paths under /var/folders are too long for Unix sockets
// (kernel limit: 104 bytes including null terminator) and are noexec (can't run binaries).
func e2eTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "eos-e2e-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// buildEosBinary compiles the eos binary into /tmp and returns its path.
func buildEosBinary(t *testing.T) string {
	t.Helper()
	dir := e2eTempDir(t)
	binPath := filepath.Join(dir, "eos")
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, "codeberg.org/Elysium_Labs/eos").CombinedOutput()
	if err != nil {
		t.Fatalf("build eos binary: %v\n%s", err, out)
	}
	// os.MkdirTemp defaults to 0700. When this test runs as root (integration
	// tests on Linux), the daemon child drops to a non-root uid before exec —
	// it needs traversal into dir to run the binary, same as a real install
	// path (e.g. /usr/local/bin) that's world-executable.
	if err := os.Chmod(dir, 0755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("chmod bin dir: %v", err)
	}
	return binPath
}

// eosCmd runs the eos binary with EOS_BASE_DIR and EOS_SYSTEMD_TARGET_DIR isolated.
// EOS_SYSTEMD_TARGET_DIR points to a nonexistent path so eos always uses standalone mode.
func eosCmd(t *testing.T, bin, baseDir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Env = append(os.Environ(),
		"EOS_BASE_DIR="+baseDir,
		"EOS_SYSTEMD_TARGET_DIR=/nonexistent-eos-e2e",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// startDaemon starts the daemon detached and polls until the Unix socket is ready (up to 8s).
// The socket appears after the PID file, so it's the correct readiness signal.
func startDaemon(t *testing.T, bin, baseDir string, verbose bool) {
	t.Helper()
	args := []string{"daemon", "start", "--detach"}
	if verbose {
		args = append(args, "--verbose")
	}
	if out, err := eosCmd(t, bin, baseDir, args...); err != nil {
		t.Fatalf("daemon start: %v\n%s", err, out)
	}

	sockFile := filepath.Join(baseDir, "eos.sock")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockFile); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Dump daemon log to help diagnose startup failures.
	logPath := filepath.Join(baseDir, "logs", "daemon.log")
	if raw, err := os.ReadFile(logPath); err == nil {
		t.Logf("daemon log at timeout:\n%s", raw)
	} else {
		t.Logf("daemon log not found (%v)", err)
	}
	t.Fatal("daemon did not start within 8s (socket never appeared)")
}

// stopDaemon stops the daemon and polls until the PID file is removed (up to 5s).
func stopDaemon(t *testing.T, bin, baseDir string) {
	t.Helper()
	if out, err := eosCmd(t, bin, baseDir, "daemon", "stop"); err != nil {
		t.Logf("daemon stop output: %s", out)
	}

	pidFile := filepath.Join(baseDir, "eos.pid")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("daemon did not stop within 5s, sending SIGTERM directly")
	killDaemonPID(baseDir)
}

// killDaemonPID sends SIGTERM to the PID recorded in the PID file (best-effort).
func killDaemonPID(baseDir string) {
	data, err := os.ReadFile(filepath.Join(baseDir, "eos.pid"))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
}

// writeTestService creates a service dir with a minimal service.yaml and returns the dir path.
// The dir is made world-traversable: the daemon child (started as root here) drops to a
// non-root uid before reading service.yaml, same as it would reading a real user's service dir.
func writeTestService(t *testing.T, name string) string {
	t.Helper()
	// e2eTempDir (not t.TempDir()) so chmod below only needs to cover one
	// level: t.TempDir() nests under an also-0700 per-test parent dir.
	dir := e2eTempDir(t)
	if err := os.Chmod(dir, 0755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("chmod service dir: %v", err)
	}
	yaml := fmt.Sprintf("name: %q\ncommand: \"/bin/sleep 3600\"\n", name)
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), []byte(yaml), 0644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write service.yaml: %v", err)
	}
	return dir
}

// readJSONLog parses the daemon log file and returns all entries.
func readJSONLog(t *testing.T, baseDir string) []map[string]any {
	t.Helper()
	logPath := filepath.Join(baseDir, "logs", "daemon.log")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading daemon log %q: %v", logPath, err)
	}
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &entry); jsonErr != nil {
			t.Errorf("non-JSON log line: %q", line)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// assertDebugMsg asserts that a DEBUG log entry with the given msg exists.
func assertDebugMsg(t *testing.T, entries []map[string]any, msg string) {
	t.Helper()
	for _, e := range entries {
		if e["level"] == "DEBUG" && e["msg"] == msg {
			return
		}
	}
	var got []string
	for _, e := range entries {
		if e["level"] == "DEBUG" {
			got = append(got, fmt.Sprintf("%q", e["msg"]))
		}
	}
	t.Errorf("expected DEBUG log %q not found; debug msgs: [%s]", msg, strings.Join(got, ", "))
}

// TestDaemonE2E_VerboseOn_FullLifecycle drives the full service lifecycle with --verbose
// and asserts that debug-level lifecycle entries appear in the daemon log file.
func TestDaemonE2E_VerboseOn_FullLifecycle(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	t.Cleanup(func() { killDaemonPID(baseDir) })

	// Start daemon with verbose logging.
	startDaemon(t, bin, baseDir, true)

	// Add and run a test service.
	svcDir := writeTestService(t, "testsvc")
	if out, err := eosCmd(t, bin, baseDir, "add", svcDir); err != nil {
		t.Fatalf("eos add: %v\n%s", err, out)
	}
	if out, err := eosCmd(t, bin, baseDir, "run", "testsvc"); err != nil {
		t.Fatalf("eos run: %v\n%s", err, out)
	}

	// Give the health monitor one tick to record the Running state.
	time.Sleep(500 * time.Millisecond)

	// Stop the service.
	if out, err := eosCmd(t, bin, baseDir, "stop", "testsvc"); err != nil {
		t.Fatalf("eos stop: %v\n%s", err, out)
	}

	// Restart (run again).
	if out, err := eosCmd(t, bin, baseDir, "run", "testsvc"); err != nil {
		t.Fatalf("eos run (restart): %v\n%s", err, out)
	}

	// Stop daemon cleanly.
	stopDaemon(t, bin, baseDir)

	entries := readJSONLog(t, baseDir)

	// Daemon init lifecycle.
	assertDebugMsg(t, entries, "PID written")
	assertDebugMsg(t, entries, "socket listening")
	assertDebugMsg(t, entries, "database connected")

	// Service start lifecycle.
	assertDebugMsg(t, entries, "launching service")
	assertDebugMsg(t, entries, "process started")

	// Service stop lifecycle.
	assertDebugMsg(t, entries, "sending SIGTERM")
}

// TestDaemonE2E_VerboseOff_NoDebug starts the daemon without --verbose and asserts
// that no DEBUG entries appear in the log file.
func TestDaemonE2E_VerboseOff_NoDebug(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	t.Cleanup(func() { killDaemonPID(baseDir) })

	startDaemon(t, bin, baseDir, false)
	stopDaemon(t, bin, baseDir)

	for _, e := range readJSONLog(t, baseDir) {
		if e["level"] == "DEBUG" {
			raw, _ := json.Marshal(e)
			t.Errorf("unexpected DEBUG entry with verbose=false: %s", raw)
		}
	}
}
