//go:build linux

package procutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAnyProcessRunning_RealProcess exercises anyProcessRunning end-to-end
// against a real, live /proc filesystem — the OS-specific glue (realProcReader)
// that the portable, fixture-driven tests in procstat_test.go can't reach.
// The group-scan logic itself (leader-exited-but-child-alive included) is
// covered there; this just proves realProcReader wires it to actual /proc
// correctly.
func TestAnyProcessRunning_RealProcess(t *testing.T) {
	cmd, pgid := launchGroupLeader(t)

	if !anyProcessRunning(pgid) {
		t.Errorf("anyProcessRunning(%d) = false, want true for a live process", pgid)
	}

	killAndReap(t, cmd)

	if anyProcessRunning(pgid) {
		t.Errorf("anyProcessRunning(%d) = true, want false for a reaped process", pgid)
	}
}

// TestReadEnviron_NonexistentPid exercises platformReadEnviron's error path:
// a pid with no /proc entry must return an error, not an empty-but-ok
// environment (that would silently misreport "no vars" as an actual
// finding).
func TestReadEnviron_NonexistentPid(t *testing.T) {
	if _, err := ReadEnviron(999999999); err == nil {
		t.Error("expected an error reading environ for a nonexistent pid")
	}
}

// TestReadEnviron_PermissionDenied reproduces the exact case a diagnose
// bundle hits whenever the daemon runs as root and the CLI does not: the
// kernel restricts /proc/<pid>/environ to a process owned by the same user
// (or root), so reading another user's environ must fail outright rather
// than return a partial or empty-but-ok result that would silently misreport
// "no vars" as an actual finding. Spawns a real root-owned child via
// passwordless sudo — a synthetic fixture can't reproduce this, since the
// permission check happens in the kernel against the target's real
// credentials, not anything this package's own logic can fake.
func TestReadEnviron_PermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — cannot exercise a cross-user permission denial")
	}
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		t.Skipf("passwordless sudo unavailable, cannot spawn a root-owned process: %v", err)
	}

	cmd := exec.Command("sudo", "-n", "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sudo sleep: %v", err)
	}
	sudoPid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = exec.Command("sudo", "-n", "pkill", "-9", "-P", strconv.Itoa(sudoPid)).Run()
		_ = exec.Command("sudo", "-n", "kill", "-9", strconv.Itoa(sudoPid)).Run()
		_ = cmd.Wait()
	})

	// sudo forks a child that execs into the real target command, so the
	// root-owned pid to test against is that child, not sudo's own pid (whose
	// real uid is still the caller's — see the childrenFile doc comment).
	rootPid, err := waitForRootChild(sudoPid)
	if err != nil {
		t.Fatalf("discovering sudo's root-owned child: %v", err)
	}

	if _, err := ReadEnviron(rootPid); err == nil {
		t.Fatal("expected a permission error reading another user's environ, got a successful read")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected a permission-denied error (errors.Is os.ErrPermission), got: %v", err)
	}
}

// waitForRootChild polls /proc/<pid>/task/<pid>/children (the kernel's own
// list of pid's direct children) for sudoPid's forked-and-exec'd target
// process, since sudo's own pid keeps the caller's real uid — only its child
// is genuinely root-owned end to end.
func waitForRootChild(sudoPid int) (int, error) {
	childrenFile := fmt.Sprintf("/proc/%d/task/%d/children", sudoPid, sudoPid)
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childrenFile)
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 {
			lastErr = fmt.Errorf("no children listed yet in %s", childrenFile)
			time.Sleep(20 * time.Millisecond)
			continue
		}
		return strconv.Atoi(fields[0])
	}
	return 0, fmt.Errorf("timed out waiting for a child pid: %w", lastErr)
}
