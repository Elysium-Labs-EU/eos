//go:build linux

package procutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

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
