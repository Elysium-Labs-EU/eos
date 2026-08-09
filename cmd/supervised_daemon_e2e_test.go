//go:build integration

package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file covers the one path the rest of the suite cannot fake: a daemon
// whose lifecycle systemd owns. Everywhere else a "live daemon" is a bare
// net.Listen that never speaks the IPC protocol and never parents anything, so
// it proves the CLI's manager choice but not the consequence of that choice.
// Here the daemon is a real transient systemd unit, and the assertions are read
// out of /proc rather than out of eos's own reporting.
//
// Isolation: ResolveSystemdScope checks the SYSTEM unit dir before the user one
// (see resolveScope in internal/config), and that dir comes from
// EOS_SYSTEMD_TARGET_DIR. Pointing it at a temp dir holding an eos.service
// marker is therefore enough to make the CLI resolve systemd mode without
// reading, writing or otherwise depending on a real install's unit file.

// supervisedScope returns the systemctl/systemd-run scope flags this process can
// actually drive, skipping when no systemd is reachable. Root drives the system
// manager; a normal user drives their own instance, which only exists when a
// session set XDG_RUNTIME_DIR.
func supervisedScope(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("systemd-managed daemon test requires Linux")
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("systemd-run not on PATH: no systemd to run the daemon under")
	}
	if os.Getuid() == 0 {
		return nil
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		t.Skip("no XDG_RUNTIME_DIR: no systemd user instance to run a unit in")
	}
	return []string{"--user"}
}

// systemctlValue reads one property off a unit, e.g. MainPID or ActiveState.
func systemctlValue(t *testing.T, scope []string, unit, property string) string {
	t.Helper()
	args := append(append([]string{}, scope...), "show", unit, "-p", property, "--value")
	out, err := exec.CommandContext(t.Context(), "systemctl", args...).Output()
	if err != nil {
		t.Fatalf("systemctl show %s -p %s: %v", unit, property, err)
	}
	return strings.TrimSpace(string(out))
}

// systemScopeUser returns the account a system-scope transient unit must run
// as. eos refuses to run as root, which is exactly why renderUnitFile's system
// unit template carries a User= line; sudo records the original account.
func systemScopeUser(t *testing.T) string {
	t.Helper()
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		t.Skip("running as root with no SUDO_USER: no target account for the unit's User= (eos refuses to run as root)")
	}
	return name
}

// chownForUnitUser hands dir to the account the system-scope unit runs as. The
// dirs come from os.MkdirTemp, so they are 0700 and owned by this root test
// process; the daemon underneath User= could not otherwise read its own base
// dir.
func chownForUnitUser(t *testing.T, dir, name string) {
	t.Helper()
	target, err := user.Lookup(name)
	if err != nil {
		t.Skipf("looking up SUDO_USER %q: %v", name, err)
	}
	uid, err := strconv.Atoi(target.Uid)
	if err != nil {
		t.Fatalf("parsing uid for %q: %v", name, err)
	}
	gid, err := strconv.Atoi(target.Gid)
	if err != nil {
		t.Fatalf("parsing gid for %q: %v", name, err)
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		t.Fatalf("chown %s to %s: %v", dir, name, err)
	}
}

// startSupervisedDaemon runs the eos daemon as a transient systemd unit and
// returns the unit name once its socket answers. The daemon detects it is under
// systemd (INVOCATION_ID) and stays in the foreground, exactly as the installed
// unit's ExecStart does.
func startSupervisedDaemon(t *testing.T, scope []string, bin, baseDir, unitDir string) string {
	t.Helper()
	unit := fmt.Sprintf("eos-e2e-%d.service", os.Getpid())

	args := append(append([]string{}, scope...),
		"--unit="+unit,
		"--collect",
		"--setenv=EOS_BASE_DIR="+baseDir,
		"--setenv=EOS_SYSTEMD_TARGET_DIR="+unitDir,
	)
	if len(scope) == 0 {
		// System scope: mirror the installed unit, which runs the daemon as a
		// real account rather than root.
		unitUser := systemScopeUser(t)
		chownForUnitUser(t, baseDir, unitUser)
		chownForUnitUser(t, unitDir, unitUser)
		args = append(args, "--property=User="+unitUser)
	}
	args = append(args, bin, "daemon", "start", "--foreground")
	if out, err := exec.CommandContext(t.Context(), "systemd-run", args...).CombinedOutput(); err != nil {
		t.Skipf("systemd-run could not start a transient unit (%v):\n%s", err, out)
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

// supervisedEosCmd runs the CLI against the isolated base dir and the fake unit
// dir, so it resolves the systemd-managed daemon config under test.
func supervisedEosCmd(t *testing.T, bin, baseDir, unitDir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Env = append(os.Environ(),
		"EOS_BASE_DIR="+baseDir,
		"EOS_SYSTEMD_TARGET_DIR="+unitDir,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// procPPIDAndPGID reads a process's parent and process-group ids out of
// /proc/<pid>/stat. The comm field is parenthesised and may itself contain
// spaces, so fields are counted from after the closing paren: stat field 4
// (ppid) and 5 (pgrp) are the first two entries there after the state char.
func procPPIDAndPGID(t *testing.T, pid int) (ppid, pgid int, alive bool) {
	t.Helper()
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, false
	}
	closing := strings.LastIndex(string(raw), ")")
	if closing < 0 {
		t.Fatalf("unparseable /proc/%d/stat: %q", pid, raw)
	}
	fields := strings.Fields(string(raw)[closing+1:])
	if len(fields) < 3 {
		t.Fatalf("unparseable /proc/%d/stat tail: %q", pid, raw)
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("parsing ppid from /proc/%d/stat: %v", pid, err)
	}
	pgid, err = strconv.Atoi(fields[2])
	if err != nil {
		t.Fatalf("parsing pgid from /proc/%d/stat: %v", pid, err)
	}
	return ppid, pgid, true
}

// unixSocketInode returns the inode /proc/net/unix records for a listening
// socket path, which is what ties the socket file on disk to the process
// holding it open.
func unixSocketInode(t *testing.T, socketPath string) string {
	t.Helper()
	raw, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		t.Fatalf("reading /proc/net/unix: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		// Num RefCount Protocol Flags Type St Inode Path
		if len(fields) >= 8 && fields[7] == socketPath {
			t.Logf("/proc/net/unix: %s", strings.TrimSpace(line))
			return fields[6]
		}
	}
	t.Fatalf("no /proc/net/unix entry for %s", socketPath)
	return ""
}

// assertHoldsSocket fails unless pid has the given socket inode open, i.e. the
// daemon process — not the CLI, and not a leftover file — is the listener.
func assertHoldsSocket(t *testing.T, pid int, inode string) {
	t.Helper()
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatalf("reading %s: %v", fdDir, err)
	}
	want := fmt.Sprintf("socket:[%s]", inode)
	for _, entry := range entries {
		target, linkErr := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if linkErr != nil {
			continue
		}
		if target == want {
			t.Logf("daemon pid %d holds the listening socket: fd %s -> %s", pid, entry.Name(), target)
			return
		}
	}
	t.Errorf("daemon pid %d does not hold socket inode %s", pid, inode)
}

// waitForGroupGone polls until no process remains in pgid (up to 5s).
func waitForGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, alive := procPPIDAndPGID(t, pgid); !alive {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("process group %d still present 5s after stop", pgid)
}

// logProcessTree records the human-readable process tree for pgid, so a failed
// run (and a -v run) carries the same evidence a manual ps would.
func logProcessTree(t *testing.T, pgid int) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "ps", "-eo", "pid,ppid,pgid,args").Output()
	if err != nil {
		return
	}
	var kept []string
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 || strings.Contains(line, fmt.Sprintf(" %d ", pgid)) {
			kept = append(kept, strings.TrimRight(line, " "))
		}
	}
	t.Logf("process tree for pgid %d:\n%s", pgid, strings.Join(kept, "\n"))
}

type apiRunOut struct {
	Name string `json:"name"`
	PGID int    `json:"pgid"`
}

type apiStatusOut struct {
	Services []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		PGID   int    `json:"pgid"`
	} `json:"services"`
}

// waitForAPIStatusRunning polls "eos api status" until the daemon's health
// monitor has promoted name out of 'starting', and returns the raw JSON it
// settled on. Polling rather than sleeping keeps the assertion about what the
// daemon converges to, not about how fast its first tick lands.
func waitForAPIStatusRunning(t *testing.T, bin, baseDir, unitDir, name string) string {
	t.Helper()
	var last string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, err := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "status")
		if err != nil {
			t.Fatalf("eos api status: %v\n%s", err, out)
		}
		last = out
		var status apiStatusOut
		if json.Unmarshal([]byte(out), &status) == nil {
			for _, svc := range status.Services {
				if svc.Name == name && svc.Status == "running" {
					return out
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last
}

// TestSupervisedDaemonE2E_ServiceIsParentedByTheDaemon is the end-to-end
// counterpart to TestNewManagerSupervisedLive: with a genuinely systemd-managed
// daemon on this base dir, "eos api run" must hand the work to that daemon, so
// the service comes out as the DAEMON's child in the real process table, in the
// process group the API reported, with the daemon holding the listening socket.
// Before this change the same command spawned the service under the CLI, which
// exited milliseconds later and left it reparented to init.
func TestSupervisedDaemonE2E_ServiceIsParentedByTheDaemon(t *testing.T) {
	scope := supervisedScope(t)
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	// A marker unit file is all IsSystemdManaged looks for; the daemon really
	// runs under the transient unit started below, not under this one.
	unitDir := e2eTempDir(t)
	if err := os.WriteFile(filepath.Join(unitDir, "eos.service"), []byte("# e2e marker\n"), 0644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("writing unit marker: %v", err)
	}

	unit := startSupervisedDaemon(t, scope, bin, baseDir, unitDir)

	mainPID, err := strconv.Atoi(systemctlValue(t, scope, unit, "MainPID"))
	if err != nil || mainPID <= 0 {
		t.Fatalf("unit %s has no MainPID (%v)", unit, err)
	}
	t.Logf("supervised daemon: unit=%s ActiveState=%s MainPID=%d",
		unit, systemctlValue(t, scope, unit, "ActiveState"), mainPID)

	// The CLI must agree it is in systemd mode; otherwise the rest of this test
	// would be proving something about standalone.
	infoOut, err := supervisedEosCmd(t, bin, baseDir, unitDir, "system", "info")
	if err != nil {
		t.Fatalf("eos system info: %v\n%s", err, infoOut)
	}
	if !strings.Contains(infoOut, "systemd managed: true") {
		t.Fatalf("CLI did not resolve systemd mode:\n%s", infoOut)
	}

	sockPath := filepath.Join(baseDir, "eos.sock")
	assertHoldsSocket(t, mainPID, unixSocketInode(t, sockPath))

	svcDir := writeTestService(t, "supervisedsvc")
	if out, addErr := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "add", svcDir); addErr != nil {
		t.Fatalf("eos api add: %v\n%s", addErr, out)
	}

	runOut, err := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "run", "supervisedsvc")
	if err != nil {
		t.Fatalf("eos api run: %v\n%s", err, runOut)
	}
	t.Logf("eos api run -> %s", strings.TrimSpace(runOut))
	var runResult apiRunOut
	if unmarshalErr := json.Unmarshal([]byte(runOut), &runResult); unmarshalErr != nil {
		t.Fatalf("eos api run did not emit the documented JSON (%v): %q", unmarshalErr, runOut)
	}
	if runResult.PGID <= 0 {
		t.Fatalf("eos api run reported no pgid: %q", runOut)
	}

	statusOut := waitForAPIStatusRunning(t, bin, baseDir, unitDir, "supervisedsvc")
	logProcessTree(t, runResult.PGID)

	// The pgid the API returned is a real, live process group...
	ppid, pgid, alive := procPPIDAndPGID(t, runResult.PGID)
	if !alive {
		t.Fatalf("pgid %d reported by eos api run is not a live process", runResult.PGID)
	}
	if pgid != runResult.PGID {
		t.Errorf("pid %d is in process group %d, not the %d eos reported", runResult.PGID, pgid, runResult.PGID)
	}
	// ...and it belongs to the daemon, not to the CLI that has already exited.
	if ppid != mainPID {
		t.Errorf("service pid %d has ppid %d, want the unit MainPID %d (ppid 1 means it was orphaned by the CLI)",
			runResult.PGID, ppid, mainPID)
	}

	t.Logf("eos api status -> %s", strings.TrimSpace(statusOut))
	var status apiStatusOut
	if unmarshalErr := json.Unmarshal([]byte(statusOut), &status); unmarshalErr != nil {
		t.Fatalf("eos api status did not emit the documented JSON (%v): %q", unmarshalErr, statusOut)
	}
	var found bool
	for _, svc := range status.Services {
		if svc.Name != "supervisedsvc" {
			continue
		}
		found = true
		if svc.Status != "running" {
			t.Errorf("eos api status reports %q, want running", svc.Status)
		}
		if svc.PGID != runResult.PGID {
			t.Errorf("eos api status reports pgid %d, want %d", svc.PGID, runResult.PGID)
		}
	}
	if !found {
		t.Errorf("eos api status did not list supervisedsvc: %s", statusOut)
	}

	stopOut, err := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "stop", "supervisedsvc")
	if err != nil {
		t.Fatalf("eos api stop: %v\n%s", err, stopOut)
	}
	t.Logf("eos api stop -> %s", strings.TrimSpace(stopOut))
	waitForGroupGone(t, runResult.PGID)
	logProcessTree(t, runResult.PGID)
}

// TestSupervisedDaemonE2E_UnitDownRefusesToStart is the other half of the
// invariant: with the same systemd-managed config but the unit stopped, the CLI
// must refuse to start a service in-process rather than spawn one it cannot
// supervise. Reads still work, so status keeps serving last-known state.
func TestSupervisedDaemonE2E_UnitDownRefusesToStart(t *testing.T) {
	scope := supervisedScope(t)
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	unitDir := e2eTempDir(t)
	if err := os.WriteFile(filepath.Join(unitDir, "eos.service"), []byte("# e2e marker\n"), 0644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("writing unit marker: %v", err)
	}

	unit := startSupervisedDaemon(t, scope, bin, baseDir, unitDir)

	svcDir := writeTestService(t, "downsvc")
	if out, err := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "add", svcDir); err != nil {
		t.Fatalf("eos api add: %v\n%s", err, out)
	}

	stopArgs := append(append([]string{}, scope...), "stop", unit)
	if out, err := exec.CommandContext(t.Context(), "systemctl", stopArgs...).CombinedOutput(); err != nil {
		t.Fatalf("systemctl stop %s: %v\n%s", unit, err, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", filepath.Join(baseDir, "eos.sock"), 200*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(100 * time.Millisecond)
	}

	runOut, err := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "run", "downsvc")
	t.Logf("eos api run with the unit stopped -> %s", strings.TrimSpace(runOut))
	if err == nil {
		t.Fatalf("expected a non-zero exit refusing the start, got success: %s", runOut)
	}
	if !strings.Contains(runOut, "refusing to start in-process") {
		t.Errorf("expected the start refusal, got: %s", runOut)
	}

	// Nothing was spawned: no downsvc process exists to be orphaned.
	psOut, psErr := exec.CommandContext(t.Context(), "ps", "-eo", "pid,ppid,pgid,args").Output()
	if psErr == nil && strings.Contains(string(psOut), filepath.Join(svcDir, "service.yaml")) {
		t.Errorf("a service process was spawned despite the refusal:\n%s", psOut)
	}

	// Reads still serve last-known state through the local manager.
	statusOut, _ := supervisedEosCmd(t, bin, baseDir, unitDir, "api", "status")
	if !strings.Contains(statusOut, "downsvc") {
		t.Errorf("eos api status did not serve last-known state with the unit down: %s", statusOut)
	}
}
