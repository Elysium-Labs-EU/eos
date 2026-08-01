//go:build linux

package procutil

import (
	"fmt"
	"os"
	"time"
)

// linuxClockTicks is the kernel's USER_HZ (sysconf(_SC_CLK_TCK)): the number of
// utime/stime jiffies per second in /proc/<pid>/stat. It is 100 on every
// standard Linux build eos targets; there is no cgo-free sysconf, so it is
// fixed here the same way gopsutil and others do.
const linuxClockTicks = 100

// realProcReader reads process state from the live /proc filesystem — the
// only implementation of procStatReader (see procstat.go) that actually
// touches the OS, so the group-scan logic it feeds stays pure and portable.
type realProcReader struct{}

func (realProcReader) stat(pid int) (string, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// pids lists every PID currently visible under /proc, silently reading as
// empty on failure — the procStatReader interface has no error return, and
// callers through it (anyProcessRunningFrom) already treat "nothing found"
// and "couldn't look" the same way. platformCPUTime needs to tell those two
// apart, so it goes through listPids instead.
func (r realProcReader) pids() []int {
	pids, err := r.listPids()
	if err != nil {
		return nil
	}
	return pids
}

// listPids is the same /proc scan as pids, but surfaces an inaccessible
// /proc as an error rather than swallowing it — platformCPUTime needs to
// distinguish "read zero CPU time" from "couldn't read at all" so a
// transient failure doesn't get recorded as a real, misleading sample. See
// listPidsIn (procstat.go) for the actual scan-and-filter logic.
func (realProcReader) listPids() ([]int, error) {
	return listPidsIn("/proc")
}

// anyProcessRunning reports whether at least one process whose process group
// is pgid is running (not a zombie), reading live process state from /proc.
// See anyProcessRunningFrom for the actual scan logic and IsAlive's doc
// comment on procutil.go for why every group member needs checking, not just
// pgid itself.
func anyProcessRunning(pgid int) bool {
	return anyProcessRunningFrom(realProcReader{}, pgid)
}

// platformCPUTime sums utime+stime across every live process whose process
// group (pgrp) is pgid — the same scope as the RSS sampler — and converts the
// jiffies to a Duration via the fixed USER_HZ. eos launches each service as
// its own group leader in the daemon's namespace, so a process's pgrp equals
// the PGID eos stored for it. The per-pid parsing, summation, and tick
// conversion are the pure, portably-tested cpuTicksFrom/ticksToDuration.
func platformCPUTime(pgid int) (time.Duration, error) {
	pids, err := (realProcReader{}).listPids()
	if err != nil {
		return 0, err
	}
	return ticksToDuration(cpuTicksFrom(realProcReader{}, pids, pgid), linuxClockTicks), nil
}

func platformStartTime(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}

	statStr := string(data)
	i, ok := commEnd(statStr)
	if !ok {
		return 0, fmt.Errorf("unexpected /proc/%d/stat format", pid)
	}

	ticks, ok := parseStartTimeField(statStr[i+2:])
	if !ok {
		return 0, fmt.Errorf("unexpected /proc/%d/stat starttime field", pid)
	}
	return ticks, nil
}
