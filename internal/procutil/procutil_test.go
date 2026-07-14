package procutil

import (
	"os"
	"syscall"
	"testing"
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
