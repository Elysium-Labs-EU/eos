//go:build integration

package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startSupervisedEosUnit is startSupervisedDaemon with one difference: the
// transient unit is literally named "eos.service" rather than a per-pid
// name. systemdMainPID (cmd/daemon.go) hardcodes "eos" as the unit it queries
// for MainPID — daemon info's version/stale-binary lines are unreachable
// under any other unit name, so this is required to exercise them at all
// through the real CLI rather than a stub. systemd-run refuses to shadow an
// already-loaded unit of the same name, so this cannot clobber a real "eos"
// service if one happens to be installed in the same scope: it fails to
// start (and the test skips) instead of colliding with it.
func startSupervisedEosUnit(t *testing.T, scope []string, bin, baseDir, unitDir string) string {
	t.Helper()
	const unit = "eos.service"

	args := append(append([]string{}, scope...),
		"--unit="+unit,
		"--collect",
		"--setenv=EOS_BASE_DIR="+baseDir,
		"--setenv=EOS_SYSTEMD_TARGET_DIR="+unitDir,
	)
	if len(scope) == 0 {
		unitUser := systemScopeUser(t)
		chownForUnitUser(t, baseDir, unitUser)
		chownForUnitUser(t, unitDir, unitUser)
		args = append(args, "--property=User="+unitUser)
	}
	args = append(args, bin, "daemon", "start", "--foreground")
	if out, err := exec.CommandContext(t.Context(), "systemd-run", args...).CombinedOutput(); err != nil {
		t.Skipf("systemd-run could not start transient unit %q (%v) -- likely shadows an existing \"eos\" unit in this scope:\n%s", unit, err, out)
	}
	t.Cleanup(func() {
		stopArgs := append(append([]string{}, scope...), "stop", unit)
		_ = exec.Command("systemctl", stopArgs...).Run() //nolint:gosec // fixed argv, test cleanup
		resetArgs := append(append([]string{}, scope...), "reset-failed", unit)
		_ = exec.Command("systemctl", resetArgs...).Run() //nolint:gosec // fixed argv, test cleanup
	})

	sockPath := filepath.Join(baseDir, "eos.sock")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return unit
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("supervised daemon never answered on %s; unit state=%s",
		sockPath, systemctlValue(t, scope, unit, "ActiveState"))
	return ""
}

// buildEosBinaryVersioned is buildEosBinary with a caller-chosen --version
// string baked in via the same ldflags the release build uses, so a test can
// tell two builds of the same binary apart by their reported version.
func buildEosBinaryVersioned(t *testing.T, version string) string {
	t.Helper()
	dir := e2eTempDir(t)
	binPath := filepath.Join(dir, "eos")
	ldflags := fmt.Sprintf("-X github.com/Elysium-Labs-EU/eos/internal/buildinfo.Version=%s", version)
	out, err := exec.CommandContext(t.Context(), "go", "build", "-ldflags", ldflags, "-o", binPath, "github.com/Elysium-Labs-EU/eos").CombinedOutput()
	if err != nil {
		t.Fatalf("build eos binary: %v\n%s", err, out)
	}
	if err := os.Chmod(dir, 0755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("chmod bin dir: %v", err)
	}
	return binPath
}

// TestSupervisedDaemonE2E_StaleBinaryAfterReplace is the regression test for
// the bug this change fixes: install.sh replaces the on-disk binary with
// `mv -f` while the systemd-managed daemon keeps running the old, now-unlinked
// inode. Before this change, `eos daemon info` silently dropped its "running
// version:" line in exactly this situation (os.Readlink of /proc/<pid>/exe
// returns a path suffixed " (deleted)", which fails to exec), and gave no
// warning that the running daemon was on stale code.
func TestSupervisedDaemonE2E_StaleBinaryAfterReplace(t *testing.T) {
	scope := supervisedScope(t)
	oldVersion := "v0.0.1-old"
	newVersion := "v0.0.2-new"
	oldBin := buildEosBinaryVersioned(t, oldVersion)
	baseDir := e2eTempDir(t)

	unitDir := e2eTempDir(t)
	if err := os.WriteFile(filepath.Join(unitDir, "eos.service"), []byte("# e2e marker\n"), 0644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("writing unit marker: %v", err)
	}

	unit := startSupervisedEosUnit(t, scope, oldBin, baseDir, unitDir)
	t.Logf("supervised daemon started from %s (version %s) as unit %s", oldBin, oldVersion, unit)

	// Simulate install.sh: build a second binary and rename it over the path the
	// daemon is still running from. The daemon process keeps its own (now
	// unlinked) inode; only the path on disk points at the new content.
	newBin := buildEosBinaryVersioned(t, newVersion)
	if err := os.Rename(newBin, oldBin); err != nil {
		t.Fatalf("simulating install.sh's binary replace (rename %s -> %s): %v", newBin, oldBin, err)
	}
	t.Logf("replaced %s in place with a %s build (mv -f semantics)", oldBin, newVersion)

	infoOut, err := supervisedEosCmd(t, oldBin, baseDir, unitDir, "daemon", "info")
	if err != nil {
		t.Fatalf("eos daemon info: %v\n%s", err, infoOut)
	}
	t.Logf("eos daemon info after replace:\n%s", infoOut)

	if !strings.Contains(infoOut, "running version:") {
		t.Errorf("expected a 'running version:' line even after the binary was replaced out from under the daemon, got:\n%s", infoOut)
	}
	if strings.Contains(infoOut, "running version: "+newVersion) {
		t.Errorf("running version reported the newly installed binary's version instead of the still-running daemon's, got:\n%s", infoOut)
	}
	if !strings.Contains(infoOut, "running version: "+oldVersion) {
		t.Errorf("expected the daemon's actual running version %q, got:\n%s", oldVersion, infoOut)
	}
	if !strings.Contains(infoOut, "since-replaced binary, restart needed") {
		t.Errorf("expected a stale-binary warning once the on-disk binary no longer matches the running daemon, got:\n%s", infoOut)
	}

	// Sanity: json-shaped commands still function against the running daemon
	// through the replaced CLI binary, proving the daemon itself is unaffected.
	statusOut, err := supervisedEosCmd(t, oldBin, baseDir, unitDir, "api", "status")
	if err != nil {
		t.Fatalf("eos api status after replace: %v\n%s", err, statusOut)
	}
	var status apiStatusOut
	if unmarshalErr := json.Unmarshal([]byte(statusOut), &status); unmarshalErr != nil {
		t.Fatalf("eos api status did not emit the documented JSON (%v): %q", unmarshalErr, statusOut)
	}
}

// isolatedHomeEosCmd is eosCmd plus one addition: HOME points at a throwaway
// directory too, not just EOS_BASE_DIR/EOS_SYSTEMD_TARGET_DIR.
// config.ResolveSystemdScope falls back to the real per-user systemd dir
// (~/.config/systemd/user) whenever the EOS_SYSTEMD_TARGET_DIR override isn't
// itself managed — correct standalone-vs-systemd detection in general, but it
// means on a host that already has a real "eos.service" user unit installed
// (left over from prior manual testing, say), eosCmd's isolation silently
// stops working and this test would run against systemd instead of
// standalone. Redirecting HOME sidesteps that fallback entirely.
func isolatedHomeEosCmd(t *testing.T, bin, baseDir, homeDir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
		"EOS_BASE_DIR="+baseDir,
		"EOS_SYSTEMD_TARGET_DIR=/nonexistent-eos-e2e",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// startIsolatedDaemon is startDaemon, using isolatedHomeEosCmd instead of
// eosCmd so the daemon it starts can't collide with a real systemd user unit
// already installed on the host (see isolatedHomeEosCmd).
func startIsolatedDaemon(t *testing.T, bin, baseDir, homeDir string) {
	t.Helper()
	if out, err := isolatedHomeEosCmd(t, bin, baseDir, homeDir, "daemon", "start", "--detach"); err != nil {
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
	t.Fatal("daemon did not start within 8s (socket never appeared)")
}

// stopIsolatedDaemon is stopDaemon's isolatedHomeEosCmd counterpart.
func stopIsolatedDaemon(t *testing.T, bin, baseDir, homeDir string) {
	t.Helper()
	if out, err := isolatedHomeEosCmd(t, bin, baseDir, homeDir, "daemon", "stop"); err != nil {
		t.Logf("daemon stop output: %s", out)
	}
}

// TestStandaloneDaemonE2E_StaleBinaryAfterReplace is the standalone analog of
// TestSupervisedDaemonE2E_StaleBinaryAfterReplace: this is the regression test
// for the actual bug reported — a standalone (non-systemd) daemon kept running
// after install.sh's `mv -f` replaced the on-disk binary out from under it, and
// unlike the systemd path, neither "eos daemon info" nor "eos system info" said
// anything was wrong. The drift itself was always caught (discoverDaemonsIn's
// "--all" scan already flagged it); this was the one place an operator running
// the single-daemon default lookup would see a clean bill of health regardless.
func TestStandaloneDaemonE2E_StaleBinaryAfterReplace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/exe resolution is Linux-only")
	}
	oldVersion := "v0.0.1-old"
	newVersion := "v0.0.2-new"
	oldBin := buildEosBinaryVersioned(t, oldVersion)
	baseDir := e2eTempDir(t)
	homeDir := e2eTempDir(t)

	startIsolatedDaemon(t, oldBin, baseDir, homeDir)
	t.Cleanup(func() { stopIsolatedDaemon(t, oldBin, baseDir, homeDir) })

	// Simulate install.sh: build a second binary and rename it over the path the
	// daemon is still running from. The daemon process keeps its own (now
	// unlinked) inode; only the path on disk points at the new content.
	newBin := buildEosBinaryVersioned(t, newVersion)
	if err := os.Rename(newBin, oldBin); err != nil {
		t.Fatalf("simulating install.sh's binary replace (rename %s -> %s): %v", newBin, oldBin, err)
	}
	t.Logf("replaced %s in place with a %s build (mv -f semantics)", oldBin, newVersion)

	infoOut, err := isolatedHomeEosCmd(t, oldBin, baseDir, homeDir, "daemon", "info")
	if err != nil {
		t.Fatalf("eos daemon info: %v\n%s", err, infoOut)
	}
	t.Logf("eos daemon info after replace:\n%s", infoOut)
	if !strings.Contains(infoOut, "since-replaced binary, restart needed") {
		t.Errorf("expected 'eos daemon info' to warn about the stale binary, got:\n%s", infoOut)
	}

	sysInfoOut, err := isolatedHomeEosCmd(t, oldBin, baseDir, homeDir, "system", "info")
	if err != nil {
		t.Fatalf("eos system info: %v\n%s", err, sysInfoOut)
	}
	t.Logf("eos system info after replace:\n%s", sysInfoOut)
	if !strings.Contains(sysInfoOut, "since-replaced binary, restart needed") {
		t.Errorf("expected 'eos system info' to warn about the stale binary, got:\n%s", sysInfoOut)
	}
}
