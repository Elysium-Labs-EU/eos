package process

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestPgroupStillAlive(t *testing.T) {
	logger := discardLogger()

	t.Run("pid<=1 is never checked", func(t *testing.T) {
		if got := pgroupStillAlive(1, logger); got {
			t.Error("expected false for pid=1")
		}
		if got := pgroupStillAlive(0, logger); got {
			t.Error("expected false for pid=0")
		}
	})

	t.Run("dead pgroup returns false", func(t *testing.T) {
		if got := pgroupStillAlive(deadPID(t), logger); got {
			t.Error("expected false for a dead process group")
		}
	})

	t.Run("live pgroup returns true and logs", func(t *testing.T) {
		cmd := exec.Command("sleep", "30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatalf("starting live test process: %v", err)
		}
		pgid := cmd.Process.Pid
		t.Cleanup(func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			_ = cmd.Wait()
		})

		capturing, buf := capturingLogger()
		if got := pgroupStillAlive(pgid, capturing); !got {
			t.Error("expected true for a live process group")
		}
		if !strings.Contains(buf.String(), "process group still alive") {
			t.Errorf("expected an info log about the live group, got: %s", buf.String())
		}
	})
}

// reapedStatus starts a shell child that exits with exitCode, waits for it to
// actually exit, and reaps it directly via syscall.Wait4 so the returned
// syscall.WaitStatus is genuine kernel state rather than a hand-built value -
// WaitStatus's bit layout is platform-specific and has no public constructor.
func reapedStatus(t *testing.T, exitCode int) (int, syscall.WaitStatus) {
	t.Helper()
	cmd := exec.Command("sh", "-c", "exit "+strconv.Itoa(exitCode))
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting child: %v", err)
	}
	pid := cmd.Process.Pid

	var status syscall.WaitStatus
	for {
		got, err := syscall.Wait4(pid, &status, 0, nil)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			t.Fatalf("wait4: %v", err)
		}
		if got == pid {
			break
		}
	}
	return pid, status
}

func TestRecordReapedExit(t *testing.T) {
	t.Run("clean exit records Stopped", func(t *testing.T) {
		db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
		pid, status := reapedStatus(t, 0)
		if _, err := db.RegisterProcessHistoryEntry(t.Context(), pid, 0, "svc", types.ProcessStateRunning); err != nil {
			t.Fatalf("RegisterProcessHistoryEntry: %v", err)
		}

		recordReapedExit(t.Context(), db, testutil.NewTestLogger(t), pid, status)

		hist, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc")
		if err != nil {
			t.Fatalf("GetMostRecentProcessHistoryEntryByName: %v", err)
		}
		if hist.State != types.ProcessStateStopped {
			t.Errorf("want Stopped, got %s", hist.State)
		}
		if hist.StoppedAt == nil {
			t.Error("want StoppedAt set")
		}
	})

	t.Run("nonzero exit records Failed", func(t *testing.T) {
		db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
		pid, status := reapedStatus(t, 7)
		if _, err := db.RegisterProcessHistoryEntry(t.Context(), pid, 0, "svc", types.ProcessStateRunning); err != nil {
			t.Fatalf("RegisterProcessHistoryEntry: %v", err)
		}

		recordReapedExit(t.Context(), db, testutil.NewTestLogger(t), pid, status)

		hist, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc")
		if err != nil {
			t.Fatalf("GetMostRecentProcessHistoryEntryByName: %v", err)
		}
		if hist.State != types.ProcessStateFailed {
			t.Errorf("want Failed, got %s", hist.State)
		}
	})

	t.Run("db update error is logged and swallowed", func(t *testing.T) {
		db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
		if err := db.CloseDBConnection(); err != nil {
			t.Fatalf("CloseDBConnection: %v", err)
		}
		logger, buf := capturingLogger()
		pid, status := reapedStatus(t, 0)

		recordReapedExit(t.Context(), db, logger, pid, status)

		if !strings.Contains(buf.String(), "updating reaped process in database") {
			t.Errorf("expected the update failure to be logged, got: %s", buf.String())
		}
	})
}

// TestHandleSIGCHLDRequest_DrainsMultipleZombies proves the loop in
// handleSIGCHLDRequest keeps calling Wait4(-1, WNOHANG) until every exited
// child has been reaped, not just the first - two real children are left
// unreaped (zombies) and a single call must clear both.
func TestHandleSIGCHLDRequest_DrainsMultipleZombies(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	cmdOK := exec.Command("sh", "-c", "exit 0")
	if err := cmdOK.Start(); err != nil {
		t.Fatalf("starting child (clean exit): %v", err)
	}
	cmdFail := exec.Command("sh", "-c", "exit 3")
	if err := cmdFail.Start(); err != nil {
		t.Fatalf("starting child (failing exit): %v", err)
	}
	// Safety net: if the drain loop under test doesn't reap these, the test
	// binary would otherwise leave zombies behind. Wait() on an
	// already-reaped process just returns ECHILD, which is fine to ignore.
	t.Cleanup(func() {
		_, _ = cmdOK.Process.Wait()
		_, _ = cmdFail.Process.Wait()
	})

	if _, err := db.RegisterProcessHistoryEntry(t.Context(), cmdOK.Process.Pid, 0, "svc-ok", types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry svc-ok: %v", err)
	}
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), cmdFail.Process.Pid, 0, "svc-fail", types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry svc-fail: %v", err)
	}

	// Give both children time to actually exit before draining, so both are
	// zombies (not still running) when handleSIGCHLDRequest runs.
	time.Sleep(150 * time.Millisecond)

	handleSIGCHLDRequest(t.Context(), db, testutil.NewTestLogger(t))

	histOK, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc-ok")
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntryByName svc-ok: %v", err)
	}
	if histOK.State != types.ProcessStateStopped {
		t.Errorf("svc-ok: want Stopped, got %s", histOK.State)
	}

	histFail, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc-fail")
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntryByName svc-fail: %v", err)
	}
	if histFail.State != types.ProcessStateFailed {
		t.Errorf("svc-fail: want Failed, got %s", histFail.State)
	}
}
