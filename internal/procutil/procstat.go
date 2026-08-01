package procutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// commEnd locates the closing ')' of the comm field in a /proc/<pid>/stat
// line, returning its index and ok=false if the line is malformed. The comm
// field can contain spaces or parentheses, so the field list is found from
// the last ')' rather than by naive whitespace splitting from the start of
// the line.
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

// parseZombieField extracts the process group (field 5, "pgrp") and whether
// the process is a zombie (state field 3) from the post-comm portion of a
// /proc/<pid>/stat line.
func parseZombieField(afterComm string) (pgrp int, isZombie bool, ok bool) {
	const (
		stateFieldIndex = 0
		pgrpFieldIndex  = 2
	)
	fields := strings.Fields(afterComm)
	if len(fields) <= pgrpFieldIndex {
		return 0, false, false
	}
	pgrp, err := strconv.Atoi(fields[pgrpFieldIndex])
	if err != nil {
		return 0, false, false
	}
	return pgrp, fields[stateFieldIndex] == "Z", true
}

// procStatReader abstracts reading /proc/<pid>/stat content and listing the
// PIDs currently visible, so the group-liveness scan below is pure logic
// testable with fixture data on any platform — the real, Linux-only /proc
// access lives behind this interface (see procutil_linux.go's
// realProcReader), not inside the scan itself.
type procStatReader interface {
	stat(pid int) (string, bool)
	pids() []int
}

// nonZombieMemberFrom reports whether pid belongs to process group pgid and
// is running (not a zombie), reading process state through r. A read failure
// (the process exited mid-check, or r has no entry for pid) counts as no
// match.
func nonZombieMemberFrom(r procStatReader, pid, pgid int) bool {
	statStr, ok := r.stat(pid)
	if !ok {
		return false
	}
	i, ok := commEnd(statStr)
	if !ok {
		return false
	}
	memberPgrp, isZombie, ok := parseZombieField(statStr[i+2:])
	return ok && memberPgrp == pgid && !isZombie
}

// anyProcessRunningFrom reports whether at least one process whose process
// group is pgid is running (not a zombie), reading process state through r.
// The fast path checks pgid's own stat entry (true whenever the group leader
// is still alive); the fallback scan covers the case where the leader has
// already exited but another member of the group has not (see IsAlive's doc
// comment on procutil.go).
func anyProcessRunningFrom(r procStatReader, pgid int) bool {
	if nonZombieMemberFrom(r, pgid, pgid) {
		return true
	}
	for _, pid := range r.pids() {
		if pid == pgid {
			continue
		}
		if nonZombieMemberFrom(r, pid, pgid) {
			return true
		}
	}
	return false
}

// cpuTicksFrom sums utime+stime jiffies across every pid in pids whose
// process group is pgid, reading each one's stat content through r. Pure
// logic testable with fixture data (see procstat_test.go); the real
// directory listing and its error handling live in platformCPUTime, which
// needs to surface an inaccessible /proc as an error rather than silently
// report a zero-CPU sample.
func cpuTicksFrom(r procStatReader, pids []int, pgid int) int64 {
	var total int64
	for _, pid := range pids {
		statStr, ok := r.stat(pid)
		if !ok {
			continue
		}
		i, ok := commEnd(statStr)
		if !ok {
			continue
		}
		pgrp, cpuTicks, ok := parseCPUFields(statStr[i+2:])
		if !ok || pgrp != pgid {
			continue
		}
		total += cpuTicks
	}
	return total
}

// listPidsIn lists the numeric-named entries of dir as PIDs, the same way a
// /proc listing enumerates live processes by their directory names. Split
// out from realProcReader.listPids (procutil_linux.go) so this — the actual
// scan-and-filter logic — is testable against a fixture directory (see
// procstat_test.go) instead of requiring a real /proc, which only exists on
// Linux. os.Open/Readdirnames themselves are not Linux-specific; only the
// hardcoded "/proc" path in the caller is.
func listPidsIn(dir string) ([]int, error) {
	d, err := os.Open(dir) // #nosec G304 -- dir is "/proc" from the caller or a test fixture, never user input
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}
	names, err := d.Readdirnames(-1)
	_ = d.Close()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	pids := make([]int, 0, len(names))
	for _, name := range names {
		if pid, convErr := strconv.Atoi(name); convErr == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// ticksToDuration converts a jiffy count to a Duration given the platform's
// clock ticks per second (USER_HZ on Linux).
func ticksToDuration(ticks, clockTicksPerSec int64) time.Duration {
	return time.Duration(ticks) * time.Second / time.Duration(clockTicksPerSec)
}
