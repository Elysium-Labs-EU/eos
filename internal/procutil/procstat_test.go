package procutil

import (
	"testing"
	"time"
)

func TestCommEnd(t *testing.T) {
	if i, ok := commEnd("1234 (sleep) S 1 1234 1234 0 -1 4194304"); !ok || i != 11 {
		t.Errorf("commEnd(simple comm) = (%d, %v), want (11, true)", i, ok)
	}
	// The comm field itself can contain spaces and parentheses.
	if i, ok := commEnd("1234 (my (weird) proc) S 1 1234"); !ok || i != 21 {
		t.Errorf("commEnd(comm with parens) = (%d, %v), want (21, true)", i, ok)
	}
	if _, ok := commEnd("no closing paren here"); ok {
		t.Error("commEnd(no ')') = ok true, want false")
	}
	if _, ok := commEnd("1234 (sleep)"); ok {
		t.Error("commEnd(nothing after ')') = ok true, want false")
	}
}

func TestParseStartTimeField(t *testing.T) {
	// 20 fields after "pid (comm) ": state through starttime (index 19).
	const afterComm = "S 1 1234 1234 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 98765"
	ticks, ok := parseStartTimeField(afterComm)
	if !ok {
		t.Fatal("parseStartTimeField returned ok=false for a well-formed line")
	}
	if ticks != 98765 {
		t.Errorf("ticks = %d, want 98765", ticks)
	}

	if _, ok := parseStartTimeField("S 1 1234"); ok {
		t.Error("parseStartTimeField(too few fields) ok=true, want false")
	}
	if _, ok := parseStartTimeField("S 1 1234 1234 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 not-a-number"); ok {
		t.Error("parseStartTimeField(non-numeric starttime) ok=true, want false")
	}
}

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
		"non-numeric stime": "S 1 4242 4242 0 -1 0 0 0 0 0 100 bad 0",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := parseCPUFields(line); ok {
				t.Errorf("parseCPUFields(%q) ok=true, want false", line)
			}
		})
	}
}

func TestParseZombieField(t *testing.T) {
	pgrp, isZombie, ok := parseZombieField("R 1 4242 4242 0 -1 4194304")
	if !ok || pgrp != 4242 || isZombie {
		t.Errorf("running: got (%d, %v, %v), want (4242, false, true)", pgrp, isZombie, ok)
	}

	pgrp, isZombie, ok = parseZombieField("Z 1 4242 4242 0 -1 4194304")
	if !ok || pgrp != 4242 || !isZombie {
		t.Errorf("zombie: got (%d, %v, %v), want (4242, true, true)", pgrp, isZombie, ok)
	}

	if _, _, ok := parseZombieField("S 1"); ok {
		t.Error("parseZombieField(too few fields) ok=true, want false")
	}
	if _, _, ok := parseZombieField("S 1 x"); ok {
		t.Error("parseZombieField(non-numeric pgrp) ok=true, want false")
	}
}

// fakeProcReader is an in-memory procStatReader: each entry maps a pid to the
// post-"pid (comm) " portion of its /proc/<pid>/stat line, letting the
// group-scan logic in procstat.go be driven with fixture data instead of a
// real /proc filesystem.
type fakeProcReader map[int]string

func (f fakeProcReader) stat(pid int) (string, bool) {
	afterComm, ok := f[pid]
	if !ok {
		return "", false
	}
	// Reconstruct a full stat line: "pid (comm) " + the fixture's fields.
	return "(x) " + afterComm, true
}

func (f fakeProcReader) pids() []int {
	pids := make([]int, 0, len(f))
	for pid := range f {
		pids = append(pids, pid)
	}
	return pids
}

func TestNonZombieMemberFrom(t *testing.T) {
	r := fakeProcReader{
		100: "R 1 100 100 0 -1 4194304", // running, pgrp=100
		200: "Z 1 100 100 0 -1 4194304", // zombie, pgrp=100
	}

	if !nonZombieMemberFrom(r, 100, 100) {
		t.Error("running member of the group should match")
	}
	if nonZombieMemberFrom(r, 200, 100) {
		t.Error("zombie member of the group should not match")
	}
	if nonZombieMemberFrom(r, 300, 100) {
		t.Error("pid with no stat entry should not match")
	}
	if nonZombieMemberFrom(r, 100, 999) {
		t.Error("running member of a DIFFERENT group should not match")
	}
}

// TestAnyProcessRunningFrom_LeaderExitedChildAlive is the direct regression
// case for the bug anyProcessRunning fixes: the group leader (pgid) has
// already exited (no stat entry at all — e.g. the "/bin/sh -c" wrapper eos
// launches every command through, which dies almost instantly on a
// group-wide SIGTERM with no trap of its own) while a child sharing the same
// pgrp is still running (e.g. it installed its own SIGTERM handler and is
// mid graceful shutdown). The group must still read as alive.
func TestAnyProcessRunningFrom_LeaderExitedChildAlive(t *testing.T) {
	const pgid = 100
	r := fakeProcReader{
		// No entry for pgid itself: the leader is gone.
		101: "R 100 100 100 0 -1 4194304", // child, pgrp=100, running
	}
	if !anyProcessRunningFrom(r, pgid) {
		t.Error("want true: a live child remains even though the leader exited")
	}
}

func TestAnyProcessRunningFrom_LeaderAlive(t *testing.T) {
	const pgid = 500
	r := fakeProcReader{
		pgid: "R 1 500 500 0 -1 4194304", // leader itself running
	}
	if !anyProcessRunningFrom(r, pgid) {
		t.Error("want true: the leader is alive")
	}
}

func TestAnyProcessRunningFrom_LeaderZombieNoOtherMembers(t *testing.T) {
	const pgid = 777
	r := fakeProcReader{
		pgid: "Z 1 777 777 0 -1 4194304", // leader is a zombie, no children
	}
	if anyProcessRunningFrom(r, pgid) {
		t.Error("want false: the only group member is a zombie")
	}
}

func TestAnyProcessRunningFrom_LeaderZombieChildZombieToo(t *testing.T) {
	const pgid = 321
	r := fakeProcReader{
		pgid: "Z 1 321 321 0 -1 4194304",   // leader, zombie
		322:  "Z 321 321 321 0 -1 4194304", // child, also zombie
	}
	if anyProcessRunningFrom(r, pgid) {
		t.Error("want false: every group member is a zombie")
	}
}

func TestAnyProcessRunningFrom_EmptyGroup(t *testing.T) {
	if anyProcessRunningFrom(fakeProcReader{}, 42) {
		t.Error("want false: no processes at all")
	}
}

func TestCPUTicksFrom(t *testing.T) {
	r := fakeProcReader{
		10: "S 1 900 900 0 -1 4194304 0 0 0 0 100 25 0",  // pgrp=900, 125 ticks
		11: "S 10 900 900 0 -1 4194304 0 0 0 0 50 10 0",  // pgrp=900, 60 ticks
		12: "S 1 901 901 0 -1 4194304 0 0 0 0 999 999 0", // different group
	}
	got := cpuTicksFrom(r, []int{10, 11, 12}, 900)
	if want := int64(185); got != want {
		t.Errorf("cpuTicksFrom = %d, want %d", got, want)
	}
}

func TestCPUTicksFrom_MissingOrMalformedPidsSkipped(t *testing.T) {
	r := fakeProcReader{
		10: "S 1 900 900 0 -1 4194304 0 0 0 0 100 25 0", // pgrp=900, 125 ticks
	}
	// pid 999 has no fixture entry (process exited mid-scan); pid 10 counts.
	got := cpuTicksFrom(r, []int{10, 999}, 900)
	if want := int64(125); got != want {
		t.Errorf("cpuTicksFrom = %d, want %d", got, want)
	}
}

func TestCPUTicksFrom_EmptyPids(t *testing.T) {
	if got := cpuTicksFrom(fakeProcReader{}, nil, 900); got != 0 {
		t.Errorf("cpuTicksFrom(no pids) = %d, want 0", got)
	}
}

func TestTicksToDuration(t *testing.T) {
	// 250 jiffies at 100 ticks/sec (USER_HZ) is 2.5 seconds.
	if got, want := ticksToDuration(250, 100), 2500*time.Millisecond; got != want {
		t.Errorf("ticksToDuration(250, 100) = %v, want %v", got, want)
	}
	if got := ticksToDuration(0, 100); got != 0 {
		t.Errorf("ticksToDuration(0, 100) = %v, want 0", got)
	}
}

func TestAnyProcessRunningFrom_UnrelatedProcessesIgnored(t *testing.T) {
	const pgid = 654
	r := fakeProcReader{
		pgid: "Z 1 654 654 0 -1 4194304", // leader, zombie
		999:  "R 1 999 999 0 -1 4194304", // unrelated, running, different pgrp
	}
	if anyProcessRunningFrom(r, pgid) {
		t.Error("want false: the only live process belongs to a different group")
	}
}
