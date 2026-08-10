//go:build !linux && !darwin

package procutil

import (
	"fmt"
	"runtime"
	"time"
)

// anyProcessRunning always reports true outside Linux and macOS: kill(-pgid,
// 0) — the caller in IsAlive — is the only liveness signal available here.
func anyProcessRunning(pgid int) bool {
	return true
}

// platformStartTime has no implementation outside Linux and macOS, the two
// platforms eos supports (systemd and launchd persistence respectively).
// Callers must treat this error as a hard failure rather than silently
// skipping the start-time comparison — a stubbed match would reintroduce the
// PGID-reuse bug this mechanism exists to close.
func platformStartTime(pid int) (int64, error) {
	return 0, fmt.Errorf("process start time not supported on %s", runtime.GOOS)
}

// platformCPUTime has no implementation outside Linux and macOS. Callers treat
// the error as "not sampled" and simply omit CPU usage on this platform, the
// same way RSS sampling is Linux-only.
func platformCPUTime(pgid int) (time.Duration, error) {
	return 0, fmt.Errorf("process cpu time not supported on %s", runtime.GOOS)
}

// platformReadEnviron has no implementation outside Linux, the only platform
// with a /proc/<pid>/environ to read.
func platformReadEnviron(pid int) ([]string, error) {
	return nil, fmt.Errorf("process environment not supported on %s", runtime.GOOS)
}
