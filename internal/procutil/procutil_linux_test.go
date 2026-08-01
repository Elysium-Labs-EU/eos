//go:build linux

package procutil

import "testing"

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
