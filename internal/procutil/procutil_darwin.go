//go:build darwin

package procutil

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// platformStartTime reads p_starttime from the kernel's kinfo_proc for pid
// via sysctl (kern.proc.pid.<pid>) — macOS has no procfs equivalent, so this
// is the cheap, non-procfs mechanism the kernel exposes for a process's start
// time. Returned as microseconds since the epoch; only ever compared for
// equality against another value obtained the same way.
func platformStartTime(pid int) (int64, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, fmt.Errorf("sysctl kern.proc.pid.%d: %w", pid, err)
	}
	starttime := kp.Proc.P_starttime
	return int64(starttime.Sec)*1_000_000 + int64(starttime.Usec), nil
}
