//go:build integration

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This file extends the real-process integration harness in daemon_e2e_test.go
// (buildEosBinary, e2eTempDir, eosCmd, startDaemon/stopDaemon, killDaemonPID,
// writeTestService, readJSONLog) to cover the degraded-mode behaviors added by
// the recent fixes: the daemon-down warning (#142), stale process_history
// flagging (#143), and the unwritable-log restart backoff (#145). Every test
// drives the actual eos binary against an isolated EOS_BASE_DIR — no mocks.

// writeServiceDirWithCommand creates a service dir with a service.yaml whose
// command is caller-supplied, mirroring writeTestService but for services that
// must exit or fail rather than sleep forever. The command is written as a
// single-quoted YAML scalar so it may contain double quotes.
func writeServiceDirWithCommand(t *testing.T, name, command string) string {
	t.Helper()
	dir := e2eTempDir(t)
	if err := os.Chmod(dir, 0755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("chmod service dir: %v", err)
	}
	yaml := fmt.Sprintf("name: %q\ncommand: '%s'\n", name, command)
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), []byte(yaml), 0644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write service.yaml: %v", err)
	}
	return dir
}

// serviceLine returns the first status-table line mentioning serviceName, or ""
// if none. The status table prints one row per service; matching the whole line
// keeps a per-service assertion from being fooled by another service's cell.
func serviceLine(out, serviceName string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, serviceName) {
			return line
		}
	}
	return ""
}

// TestDaemonE2E_Degraded_DaemonDownWarning exercises issue #142: a read command
// run while the daemon is down must print the daemon-down banner before it
// papers the outage over by auto-starting a standalone daemon.
//
// It drives `eos status` because that is the reachable path in the standalone
// harness: warnIfDaemonDown probes liveness BEFORE getManager, so the banner
// fires on a fresh base dir. The sibling warning on `eos run`/start
// (warnDaemonDownBeforeStart) is called AFTER getManager, which in standalone
// mode has already auto-started the daemon — so that variant only fires under a
// separately-managed systemd unit and is covered by the unit tests in
// cmd/daemon_liveness_test.go, not reachable here.
func TestDaemonE2E_Degraded_DaemonDownWarning(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	// status auto-starts a standalone daemon after warning; make sure it dies.
	t.Cleanup(func() { killDaemonPID(baseDir) })

	out, err := eosCmd(t, bin, baseDir, "status")
	if err != nil {
		t.Fatalf("eos status: %v\n%s", err, out)
	}

	const wantWarn = "eos daemon is not running"
	if !strings.Contains(out, wantWarn) {
		t.Errorf("expected daemon-down warning %q on a fresh base dir, got:\n%s", wantWarn, out)
	}

	stopDaemon(t, bin, baseDir)
}

// TestDaemonE2E_Degraded_StaleProcessHistory exercises issue #143: once a
// service's process_history row stops being refreshed for longer than
// StaleThresholdMultiplier (3) times the health-check interval, `eos status`
// flags it "(stale)". A stopped service is the clean trigger — the monitor's
// ProcessStateStopped branch is a no-op, so updated_at freezes at stop time
// while the daemon (and its monitor) keep running to serve the status query.
//
// A second, still-running service acts as the negative control, but only on
// Linux: the monitor only rewrites a running row's updated_at when it samples
// RSS/CPU, and that sampling reads /proc, so on non-Linux hosts even a healthy
// running service legitimately stops being refreshed. The integration suite
// targets Linux (make test-integration); the negative assertion is gated to it.
func TestDaemonE2E_Degraded_StaleProcessHistory(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	t.Cleanup(func() { killDaemonPID(baseDir) })

	startDaemon(t, bin, baseDir, false)

	// stalesvc will be stopped and left to go stale; livesvc stays running.
	for _, name := range []string{"stalesvc", "livesvc"} {
		svcDir := writeTestService(t, name)
		if out, err := eosCmd(t, bin, baseDir, "add", svcDir); err != nil {
			t.Fatalf("eos add %s: %v\n%s", name, err, out)
		}
		if out, err := eosCmd(t, bin, baseDir, "run", name); err != nil {
			t.Fatalf("eos run %s: %v\n%s", name, err, out)
		}
	}

	if out, err := eosCmd(t, bin, baseDir, "stop", "stalesvc"); err != nil {
		t.Fatalf("eos stop stalesvc: %v\n%s", err, out)
	}

	// Default health-check interval is 2s → stale threshold is 6s. Wait past it
	// with margin so the frozen row is unambiguously stale while livesvc keeps
	// being refreshed.
	time.Sleep(8 * time.Second)

	out, err := eosCmd(t, bin, baseDir, "status")
	if err != nil {
		t.Fatalf("eos status: %v\n%s", err, out)
	}

	staleLine := serviceLine(out, "stalesvc")
	if staleLine == "" {
		t.Fatalf("stalesvc not present in status output:\n%s", out)
	}
	if !strings.Contains(staleLine, "(stale)") {
		t.Errorf("expected stopped, un-refreshed stalesvc to be flagged (stale), got row:\n%s", staleLine)
	}

	liveLine := serviceLine(out, "livesvc")
	if liveLine == "" {
		t.Fatalf("livesvc not present in status output:\n%s", out)
	}
	if runtime.GOOS == "linux" {
		if strings.Contains(liveLine, "(stale)") {
			t.Errorf("running livesvc should not be flagged stale (monitor refreshes it via /proc sampling), got row:\n%s", liveLine)
		}
	} else {
		t.Logf("skipping running-service negative control on %s: monitor cannot sample /proc, so a running row is not refreshed here. Row was:\n%s", runtime.GOOS, liveLine)
	}

	stopDaemon(t, bin, baseDir)
}

// TestDaemonE2E_Degraded_UnwritableLogHaltsRestart exercises issue #145: when a
// service's log files become unwritable, its restart fails with a permission
// error; that real cause must surface in `eos status`, and the restart loop
// must halt instead of spinning every ~2s.
//
// The trigger is real: permsvc runs briefly then exits non-zero, so the monitor
// tries to restart it; by then its already-created log files have been chmod'd
// read-only, so prepareLaunchIO's log open returns EACCES on the restart path.
//
// This cannot be exercised as root (root bypasses DAC file permissions, so the
// log open would succeed and the service would just keep flapping) or on a
// filesystem that ignores mode bits. The test probes writability after chmod
// and skips visibly in those cases rather than silently passing.
func TestDaemonE2E_Degraded_UnwritableLogHaltsRestart(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	t.Cleanup(func() { killDaemonPID(baseDir) })

	startDaemon(t, bin, baseDir, false)

	// Runs long enough to reach Running and give us a window to revoke write
	// access to the log files, then exits non-zero so the monitor restarts it.
	svcDir := writeServiceDirWithCommand(t, "permsvc", `/bin/sh -c "sleep 4; exit 1"`)
	if out, err := eosCmd(t, bin, baseDir, "add", svcDir); err != nil {
		t.Fatalf("eos add permsvc: %v\n%s", err, out)
	}
	if out, err := eosCmd(t, bin, baseDir, "run", "permsvc"); err != nil {
		t.Fatalf("eos run permsvc: %v\n%s", err, out)
	}

	// The first launch creates the log files; give it a beat, then revoke write.
	time.Sleep(1 * time.Second)
	logsDir := filepath.Join(baseDir, "logs")
	outLog := filepath.Join(logsDir, "permsvc-out.log")
	errLog := filepath.Join(logsDir, "permsvc-error.log")
	for _, p := range []string{outLog, errLog} {
		if err := os.Chmod(p, 0444); err != nil {
			t.Fatalf("chmod %s read-only: %v", p, err)
		}
	}
	requireUnwritable(t, outLog)

	// Service exits at ~t+4s; wait past that plus several 2s monitor ticks so a
	// non-halting loop would log many restart failures.
	time.Sleep(8 * time.Second)

	entries := readJSONLog(t, baseDir)
	var restartFailures []string
	for _, e := range entries {
		if e["level"] == "ERROR" && e["msg"] == "failed to restart" {
			errStr, _ := e["error"].(string)
			restartFailures = append(restartFailures, errStr)
		}
	}

	if len(restartFailures) == 0 {
		t.Fatalf("expected a 'failed to restart' entry after the log became unwritable; daemon log had none:\n%s", dumpLog(t, baseDir))
	}
	if !strings.Contains(restartFailures[0], "permission denied") {
		t.Errorf("expected restart failure to carry the permission cause, got: %q", restartFailures[0])
	}
	// The permission branch caps the restart counter on the first failure, so
	// canRestart stops firing: a healthy fix logs one failure, not one every
	// ~2s. Allow a small margin for tick timing but reject a spinning loop.
	if len(restartFailures) > 2 {
		t.Errorf("expected the restart loop to back off (<=2 failures), got %d — looks like it is spinning:\n%v", len(restartFailures), restartFailures)
	}

	// The real permission cause must also surface to the operator via status.
	out, err := eosCmd(t, bin, baseDir, "status")
	if err != nil {
		t.Fatalf("eos status: %v\n%s", err, out)
	}
	for _, want := range []string{"permission denied", "needs intervention"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected eos status to surface %q, got:\n%s", want, out)
		}
	}

	stopDaemon(t, bin, baseDir)
}

// requireUnwritable skips the test (visibly) unless the current process is
// actually denied write access to path. This makes the root / permission-
// ignoring-filesystem gap explicit instead of letting the test pass without
// exercising the EACCES restart path.
func requireUnwritable(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_WRONLY, 0)
	if err == nil {
		_ = f.Close()
		t.Skipf("log file %s is still writable after chmod 0444 (running as root, or a filesystem that ignores mode bits) — cannot exercise the unwritable-log restart path", path)
	}
	if !os.IsPermission(err) {
		t.Skipf("expected a permission error probing %s, got %v — skipping unwritable-log test", path, err)
	}
}

// dumpLog returns the raw daemon log for failure diagnostics.
func dumpLog(t *testing.T, baseDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(baseDir, "logs", "daemon.log"))
	if err != nil {
		return fmt.Sprintf("(could not read daemon log: %v)", err)
	}
	return string(raw)
}
