//go:build linux

package procutil

import "testing"

func TestParseCPUFields(t *testing.T) {
	// Fields after "pid (comm) ": state ppid pgrp session tty_nr tpgid flags
	// minflt cminflt majflt cmajflt utime stime ...
	// Here pgrp=4242, utime=100, stime=25.
	const afterComm = "S 1 4242 4242 0 -1 4194304 100 0 0 0 100 25 0 0 20 0 1 0 12345"

	pgrp, cpuTicks, ok := parseCPUFields(afterComm)
	if !ok {
		t.Fatal("parseCPUFields returned ok=false for a well-formed line")
	}
	if pgrp != 4242 {
		t.Errorf("pgrp = %d, want 4242", pgrp)
	}
	if cpuTicks != 125 {
		t.Errorf("cpuTicks = %d, want 125 (utime 100 + stime 25)", cpuTicks)
	}
}

func TestParseCPUFields_Malformed(t *testing.T) {
	cases := map[string]string{
		"too few fields":    "S 1 4242",
		"non-numeric pgrp":  "S 1 x 4242 0 -1 0 0 0 0 0 100 25 0",
		"non-numeric utime": "S 1 4242 4242 0 -1 0 0 0 0 0 bad 25 0",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := parseCPUFields(line); ok {
				t.Errorf("parseCPUFields(%q) ok=true, want false", line)
			}
		})
	}
}
