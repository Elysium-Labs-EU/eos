package procutil

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestIsAlive(t *testing.T) {
	if IsAlive(0) {
		t.Error("pgid=0 should be dead")
	}
	if IsAlive(-1) {
		t.Error("pgid=-1 should be dead")
	}
	if IsAlive(1) {
		t.Error("pgid=1 should be dead (short-circuits at <=1)")
	}

	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	if !IsAlive(pgid) {
		t.Errorf("own pgid %d should be alive", pgid)
	}

	// Large, arbitrary PGID stand-in for a dead process group: real PGIDs on a
	// typical dev/CI machine stay well below this value, so it's unlikely to
	// collide with a live process. Not guaranteed, so guard with a skip.
	const deadPGID = 999991
	if IsAlive(deadPGID) {
		t.Skipf("pgid %d is actually alive — skipping dead check", deadPGID)
	}
}

// launchGroupLeader starts a real, short-lived process as the leader of a new
// process group (mirroring how LocalManager.StartService launches services),
// so its pid doubles as its pgid — exactly the shape procutil.IsAliveMatching
// expects.
func launchGroupLeader(t *testing.T) (cmd *exec.Cmd, pgid int) {
	t.Helper()
	cmd = exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("Getpgid: %v", err)
	}
	return cmd, pgid
}

func killAndReap(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	_ = cmd.Wait()
}

// TestCPUTime_IdleProcess checks that a sleeping group leader accumulates
// essentially no CPU time between two readings.
func TestCPUTime_IdleProcess(t *testing.T) {
	cmd, pgid := launchGroupLeader(t) // sleep 30 — idle
	defer killAndReap(t, cmd)

	first, err := CPUTime(pgid)
	if err != nil {
		t.Fatalf("CPUTime(%d): %v", pgid, err)
	}
	time.Sleep(300 * time.Millisecond)
	second, err := CPUTime(pgid)
	if err != nil {
		t.Fatalf("CPUTime(%d) second read: %v", pgid, err)
	}

	// An idle process should burn well under 50ms of CPU over 300ms of sleep.
	if delta := second - first; delta > 50*time.Millisecond {
		t.Errorf("idle CPUTime delta = %v, want ~0", delta)
	}
}

// TestCPUTime_BusyProcess checks that a process spinning in a tight loop
// accumulates a growing amount of CPU time.
func TestCPUTime_BusyProcess(t *testing.T) {
	// Group leader spinning a busy loop; drives real utime/stime.
	cmd := exec.Command("sh", "-c", "while :; do :; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start busy loop: %v", err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("Getpgid: %v", err)
	}
	defer killAndReap(t, cmd)

	first, err := CPUTime(pgid)
	if err != nil {
		t.Fatalf("CPUTime(%d): %v", pgid, err)
	}
	time.Sleep(400 * time.Millisecond)
	second, err := CPUTime(pgid)
	if err != nil {
		t.Fatalf("CPUTime(%d) second read: %v", pgid, err)
	}

	// A fully busy single process should accrue a large fraction of the wall
	// interval as CPU time; require at least 100ms over the 400ms window.
	if delta := second - first; delta < 100*time.Millisecond {
		t.Errorf("busy CPUTime delta = %v, want a substantial fraction of 400ms", delta)
	}
}

func TestStartTime(t *testing.T) {
	cmd, pgid := launchGroupLeader(t)
	defer killAndReap(t, cmd)

	start, err := StartTime(pgid)
	if err != nil {
		t.Fatalf("StartTime(%d): %v", pgid, err)
	}
	if start <= 0 {
		t.Errorf("StartTime(%d) = %d, want > 0", pgid, start)
	}

	// Re-reading immediately should return the same value: it's a fixed
	// kernel-recorded fact about the process, not a live clock.
	again, err := StartTime(pgid)
	if err != nil {
		t.Fatalf("StartTime(%d) second read: %v", pgid, err)
	}
	if again != start {
		t.Errorf("StartTime(%d) not stable: %d then %d", pgid, start, again)
	}
}

// TestIsAliveMatching_RejectsPGIDReuse simulates the kernel recycling a PGID:
// forcing an actual PGID collision between two sequential real processes
// isn't practical to trigger deterministically, so instead this launches two
// real, sequential processes and checks the comparison logic directly —
// process B's live PGID paired with process A's (older, no-longer-current)
// start time must NOT be treated as a liveness match, the same way a stored
// process_history row for a dead process A must not falsely match an
// unrelated process B that later reused A's PGID.
func TestIsAliveMatching_RejectsPGIDReuse(t *testing.T) {
	cmdA, pgidA := launchGroupLeader(t)
	startA, err := StartTime(pgidA)
	if err != nil {
		killAndReap(t, cmdA)
		t.Fatalf("StartTime(%d) for process A: %v", pgidA, err)
	}
	killAndReap(t, cmdA)

	// Ensure the kernel clock has moved on so B's start time can't coincide
	// with A's by chance.
	time.Sleep(1100 * time.Millisecond)

	cmdB, pgidB := launchGroupLeader(t)
	defer killAndReap(t, cmdB)
	startB, err := StartTime(pgidB)
	if err != nil {
		t.Fatalf("StartTime(%d) for process B: %v", pgidB, err)
	}
	if startB == startA {
		t.Skip("process B's start time coincided with process A's — cannot exercise the mismatch path")
	}

	// The scenario this guards against: a process_history row recorded
	// (pgidA, startA) for A. A is now dead. If pgidB happened to be recycled
	// as pgidA, checking liveness with the PGID alone would wrongly report
	// A as still running/starting. Pairing the check with the recorded start
	// time must reject it.
	if IsAliveMatching(pgidB, startA) {
		t.Errorf("IsAliveMatching(%d, %d) = true, want false: B's start time (%d) differs from A's stored start time", pgidB, startA, startB)
	}

	// Sanity check the positive case still holds: B matched against its own
	// current start time is a live match.
	if !IsAliveMatching(pgidB, startB) {
		t.Errorf("IsAliveMatching(%d, %d) = false, want true: B is alive with this exact start time", pgidB, startB)
	}

	// And A, now dead, should not match its own recorded start time either.
	if IsAliveMatching(pgidA, startA) {
		t.Errorf("IsAliveMatching(%d, %d) = true, want false: A is dead", pgidA, startA)
	}
}
