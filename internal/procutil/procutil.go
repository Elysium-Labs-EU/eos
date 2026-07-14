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
