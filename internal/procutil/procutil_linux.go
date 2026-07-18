//go:build linux

package procutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// linuxClockTicks is the kernel's USER_HZ (sysconf(_SC_CLK_TCK)): the number of
// utime/stime jiffies per second in /proc/<pid>/stat. It is 100 on every
// standard Linux build eos targets; there is no cgo-free sysconf, so it is
// fixed here the same way gopsutil and others do.
const linuxClockTicks = 100

// platformStartTime reads field 22 (starttime, in clock ticks since boot) of
// /proc/<pid>/stat. The comm field can itself contain spaces or parentheses,
// so the field list is located from the last ')' rather than by naive
// whitespace splitting from the start of the line.
// commEnd locates the closing ')' of the comm field in a /proc/<pid>/stat line,
// returning its index and ok=false if the line is malformed. The comm field can
// contain spaces or parentheses, so the field list is found from the last ')'.
func commEnd(statStr string) (int, bool) {
	i := strings.LastIndex(statStr, ")")
	if i < 0 || i+2 >= len(statStr) {
		return 0, false
	}
	return i, true
}

// parseStartTimeField extracts field 22 (starttime, in clock ticks since boot)
// from the post-comm portion of a /proc/<pid>/stat line.
func parseStartTimeField(afterComm string) (int64, bool) {
	// Fields after "pid (comm) " are: state(1) ppid(2) pgrp(3) session(4)
	// tty_nr(5) tpgid(6) flags(7) minflt(8) cminflt(9) majflt(10) cmajflt(11)
	// utime(12) stime(13) cutime(14) cstime(15) priority(16) nice(17)
	// num_threads(18) itrealvalue(19) starttime(20) — 0-indexed that's 19.
	const starttimeFieldIndex = 19
	fields := strings.Fields(afterComm)
	if len(fields) <= starttimeFieldIndex {
		return 0, false
	}
	ticks, err := strconv.ParseInt(fields[starttimeFieldIndex], 10, 64)
	if err != nil {
		return 0, false
	}
	return ticks, true
}

// parseCPUFields extracts the process group (field 5, "pgrp") and the total CPU
// jiffies (utime field 14 + stime field 15) from the post-comm portion of a
// /proc/<pid>/stat line. Indices are 0-based into the whitespace-split fields
// after "pid (comm) ": state(0) ppid(1) pgrp(2) session(3) tty_nr(4) tpgid(5)
// flags(6) minflt(7) cminflt(8) majflt(9) cmajflt(10) utime(11) stime(12).
func parseCPUFields(afterComm string) (pgrp int, cpuTicks int64, ok bool) {
	const (
		pgrpFieldIndex  = 2
		utimeFieldIndex = 11
		stimeFieldIndex = 12
	)
	fields := strings.Fields(afterComm)
	if len(fields) <= stimeFieldIndex {
		return 0, 0, false
	}
	pgrp, err := strconv.Atoi(fields[pgrpFieldIndex])
	if err != nil {
		return 0, 0, false
	}
	utime, err := strconv.ParseInt(fields[utimeFieldIndex], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	stime, err := strconv.ParseInt(fields[stimeFieldIndex], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return pgrp, utime + stime, true
}

// procCPUTicksForPGID returns the utime+stime jiffies of pid if it belongs to
// pgid, else 0. A read failure (the process exited mid-scan) is treated as 0.
func procCPUTicksForPGID(pid, pgid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	statStr := string(data)
	i, ok := commEnd(statStr)
	if !ok {
		return 0
	}
	pgrp, cpuTicks, ok := parseCPUFields(statStr[i+2:])
	if !ok || pgrp != pgid {
		return 0
	}
	return cpuTicks
}

// platformCPUTime sums utime+stime across every live process whose process
// group (pgrp) is pgid — the same scope as the RSS sampler — and converts the
// jiffies to a Duration via the fixed USER_HZ. eos launches each service as its
// own group leader in the daemon's namespace, so a process's pgrp equals the
// PGID eos stored for it.
func platformCPUTime(pgid int) (time.Duration, error) {
	procDir, err := os.Open("/proc")
	if err != nil {
		return 0, fmt.Errorf("open /proc: %w", err)
	}
	names, err := procDir.Readdirnames(-1)
	_ = procDir.Close()
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}

	var totalTicks int64
	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		totalTicks += procCPUTicksForPGID(pid, pgid)
	}
	return time.Duration(totalTicks) * time.Second / linuxClockTicks, nil
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
