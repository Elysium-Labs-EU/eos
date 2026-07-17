// Package procutil provides OS-level process liveness checks shared across
// the manager and process packages.
package procutil

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

// IsAlive reports whether any live process exists in the given process group.
//
// On Linux, kill(-pgid, 0) returns nil even when the only remaining process is
// a zombie — a process that has exited but has not yet been reaped by its
// parent's Wait call. A zombie is not running, so we read /proc/<pgid>/stat and
// treat state 'Z' as dead.
//
// On macOS, kill(-pgid, 0) returns EPERM for zombies (caught by the err != nil
// check below), so the /proc path is not needed there.
func IsAlive(pgid int) bool {
	if pgid <= 1 {
		return false
	}
	if err := syscall.Kill(-pgid, 0); err != nil {
		return false
	}
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pgid))
		if err != nil {
			return false
		}
		statStr := string(data)
		// Format: pid (comm) state ... — find state char after the last ')' in comm
		if i := strings.LastIndex(statStr, ")"); i >= 0 && i+2 < len(statStr) {
			return statStr[i+2] != 'Z'
		}
	}
	return true
}

// StartTime returns an opaque, platform-specific integer identifying when the
// kernel started pid. It is only meaningful compared for equality against
// another value obtained the same way on the same host — never persisted
// across platforms and never converted to wall-clock time.
//
// This exists to detect PGID reuse: kill(-pgid, 0) only proves some process
// group with that PGID is alive, not that it's the same process a stored
// record was made for, since PGIDs get recycled by the kernel. Comparing the
// recorded start time alongside the PGID rules out a collision with an
// unrelated, later process that was assigned the same PGID.
func StartTime(pid int) (int64, error) {
	return platformStartTime(pid)
}

// IsAliveMatching reports whether pgid is alive and its current start time
// matches startedAtTicks (as previously returned by StartTime for the same
// pgid). Every process this package tracks is launched with Setpgid: true,
// making it the leader of a new process group, so pgid also doubles as the
// leader's own pid — that's what lets us read its /proc or sysctl start time
// directly from the stored pgid.
func IsAliveMatching(pgid int, startedAtTicks int64) bool {
	if !IsAlive(pgid) {
		return false
	}
	current, err := StartTime(pgid)
	if err != nil {
		return false
	}
	return current == startedAtTicks
}
