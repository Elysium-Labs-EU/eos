//go:build !linux && !darwin

package procutil

import (
	"fmt"
	"runtime"
	"time"
)

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
