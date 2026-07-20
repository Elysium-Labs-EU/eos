package process

import (
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHandleReapedChild(t *testing.T) {
	logger := discardLogger()
	var status syscall.WaitStatus

	t.Run("wait error stops the drain", func(t *testing.T) {
		if got := handleReapedChild(t.Context(), nil, logger, 0, errors.New("ECHILD"), status); got != reapStop {
			t.Errorf("expected reapStop on wait error, got %v", got)
		}
	})

	t.Run("no more children stops the drain", func(t *testing.T) {
		if got := handleReapedChild(t.Context(), nil, logger, 0, nil, status); got != reapStop {
			t.Errorf("expected reapStop when pid is 0, got %v", got)
		}
	})

	t.Run("negative pid continues without db work", func(t *testing.T) {
		if got := handleReapedChild(t.Context(), nil, logger, -1, nil, status); got != reapContinue {
			t.Errorf("expected reapContinue on negative pid, got %v", got)
		}
	})
}

func TestAllMethodsHandled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	for method := range types.ValidMethods {
		t.Run(string(method), func(t *testing.T) {
			req := types.DaemonRequest{Method: method, Args: nil}
			resp := executeRequest(manager, req)

			// Every method in ValidMethods must be a case in executeRequest's
			// switch; falling through to the default returns this error string.
			if !resp.Success && strings.Contains(resp.Error, "unknown method") {
				t.Errorf("Method %s not handled in switch", method)
			}
		})
	}
}

func TestReconcileOrphans_Empty(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))
}

func TestReconcileOrphans_NoHistory(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	if err := db.RegisterService(t.Context(), "website", "/opt/website", "service.yaml"); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

	_, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "website")
	if !errors.Is(err, database.ErrProcessHistoryNotFound) {
		t.Errorf("expected ErrProcessHistoryNotFound, got %v", err)
	}
}

func TestReconcileOrphans_TerminalStates(t *testing.T) {
	for _, state := range []types.ProcessState{types.ProcessStateStopped, types.ProcessStateFailed} {
		t.Run(string(state), func(t *testing.T) {
			db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

			if err := db.RegisterService(t.Context(), "svc", "/opt/svc", "service.yaml"); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}
			if _, err := db.RegisterProcessHistoryEntry(t.Context(), 12345, 0, "svc", state); err != nil {
				t.Fatalf("RegisterProcessHistoryEntry: %v", err)
			}

			reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

			hist, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc")
			if err != nil {
				t.Fatalf("GetMostRecentProcessHistoryEntryByName: %v", err)
			}
			if hist.State != state {
				t.Errorf("state should be unchanged: want %s, got %s", state, hist.State)
			}
		})
	}
}

// TestReconcileOrphans_TerminalStateButAlive is the direct regression test
// for #96: a history row recorded Stopped/Failed (e.g. a lost SIGCHLD race)
// must not be trusted blindly — if the PGID it points at is still alive,
// reconcileOrphans must kill it and correct the row, not skip it.
func TestReconcileOrphans_TerminalStateButAlive(t *testing.T) {
	for _, state := range []types.ProcessState{types.ProcessStateStopped, types.ProcessStateFailed} {
		t.Run(string(state), func(t *testing.T) {
			db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

			if err := db.RegisterService(t.Context(), "svc", "/opt/svc", "service.yaml"); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}

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

			// Record the process's real start time so it passes the
			// IsAliveMatching guard — this row genuinely points at our
			// process, so reconcileOrphans is expected to kill it.
			startTicks, err := procutil.StartTime(pgid)
			if err != nil {
				t.Fatalf("StartTime: %v", err)
			}
			if _, err = db.RegisterProcessHistoryEntry(t.Context(), pgid, startTicks, "svc", state); err != nil {
				t.Fatalf("RegisterProcessHistoryEntry: %v", err)
			}

			reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

			hist, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc")
			if err != nil {
				t.Fatalf("GetMostRecentProcessHistoryEntryByName: %v", err)
			}
			if hist.State != types.ProcessStateStopped {
				t.Errorf("want Stopped after killing live orphan, got %s", hist.State)
			}
			if hist.StoppedAt == nil {
				t.Error("want StoppedAt set")
			}
			if procutil.IsAlive(pgid) {
				t.Error("process should have been killed")
			}
		})
	}
}

// TestReconcileOrphans_PGIDReuse is the direct regression test for #2
// (Critical): a history row whose PGID is currently alive but whose recorded
// started_at_ticks does NOT match the live process — i.e. the kernel recycled
// that PGID number for an unrelated, innocent process — must never be killed.
// reconcileOrphans must leave that process running (our recorded process is
// long gone) and still reconcile a live-looking row to Stopped without signal.
// Before the fix this SIGKILLed the innocent process, because it gated the
// kill on procutil.IsAlive alone, which only proves *some* group with that
// PGID is alive, not that it's the one eos recorded.
func TestReconcileOrphans_PGIDReuse(t *testing.T) {
	cases := []struct {
		name        string
		ticks       func(real int64) int64
		state       types.ProcessState
		wantStopped bool
	}{
		{"running/mismatched-ticks", func(real int64) int64 { return real + 1 }, types.ProcessStateRunning, true},
		{"starting/mismatched-ticks", func(real int64) int64 { return real + 1 }, types.ProcessStateStarting, true},
		{"unknown/mismatched-ticks", func(real int64) int64 { return real + 1 }, types.ProcessStateUnknown, true},
		{"stopped/mismatched-ticks", func(real int64) int64 { return real + 1 }, types.ProcessStateStopped, false},
		{"failed/mismatched-ticks", func(real int64) int64 { return real + 1 }, types.ProcessStateFailed, false},
		// StartedAtTicks <= 0 is an unverifiable match: also treated as not-ours.
		{"running/zero-ticks", func(int64) int64 { return 0 }, types.ProcessStateRunning, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

			if err := db.RegisterService(t.Context(), "svc", "/opt/svc", "service.yaml"); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}

			// A real, innocent process that happens to hold the PGID our stale
			// row points at. It is NOT one of eos's services.
			cmd := exec.Command("sleep", "30")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatalf("starting innocent test process: %v", err)
			}
			pgid := cmd.Process.Pid
			t.Cleanup(func() {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				_ = cmd.Wait()
			})

			realTicks, err := procutil.StartTime(pgid)
			if err != nil {
				t.Fatalf("StartTime: %v", err)
			}
			if _, err = db.RegisterProcessHistoryEntry(t.Context(), pgid, tc.ticks(realTicks), "svc", tc.state); err != nil {
				t.Fatalf("RegisterProcessHistoryEntry: %v", err)
			}

			reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

			// The core security assertion: the innocent process survives.
			if !procutil.IsAlive(pgid) {
				t.Fatalf("innocent process (recycled PGID %d) was killed; reconcileOrphans must not SIGKILL a non-matching PGID", pgid)
			}

			hist, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), "svc")
			if err != nil {
				t.Fatalf("GetMostRecentProcessHistoryEntryByName: %v", err)
			}
			if tc.wantStopped {
				if hist.State != types.ProcessStateStopped {
					t.Errorf("want Stopped (row reconciled without kill), got %s", hist.State)
				}
				if hist.StoppedAt == nil {
					t.Error("want StoppedAt set")
				}
			} else if hist.State != tc.state {
				t.Errorf("terminal row should be unchanged: want %s, got %s", tc.state, hist.State)
			}
		})
	}
}

func TestReconcileOrphans_ActiveStates(t *testing.T) {
	for _, state := range []types.ProcessState{types.ProcessStateRunning, types.ProcessStateStarting, types.ProcessStateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

			if err := db.RegisterService(t.Context(), "svc", "/opt/svc", "service.yaml"); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}
			if _, err := db.RegisterProcessHistoryEntry(t.Context(), 2000001, 0, "svc", state); err != nil {
				t.Fatalf("RegisterProcessHistoryEntry: %v", err)
			}

			reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

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
	}
}

// TestReconcileOrphans_Mixed runs terminal and active states side by side to
// confirm reconcileOrphans only touches the active ones, not just each state
// in isolation.
func TestReconcileOrphans_Mixed(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	services := []struct {
		name        string
		state       types.ProcessState
		pgid        int
		wantStopped bool
	}{
		{"running-svc", types.ProcessStateRunning, 2000001, true},
		{"stopped-svc", types.ProcessStateStopped, 2000002, false},
		{"failed-svc", types.ProcessStateFailed, 2000003, false},
		{"starting-svc", types.ProcessStateStarting, 2000004, true},
		{"unknown-svc", types.ProcessStateUnknown, 2000005, true},
	}

	for _, svc := range services {
		if err := db.RegisterService(t.Context(), svc.name, "/opt/"+svc.name, "service.yaml"); err != nil {
			t.Fatalf("RegisterService %s: %v", svc.name, err)
		}
		if _, err := db.RegisterProcessHistoryEntry(t.Context(), svc.pgid, 0, svc.name, svc.state); err != nil {
			t.Fatalf("RegisterProcessHistoryEntry %s: %v", svc.name, err)
		}
	}

	reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

	for _, svc := range services {
		hist, err := db.GetMostRecentProcessHistoryEntryByName(t.Context(), svc.name)
		if err != nil {
			t.Fatalf("%s: GetMostRecentProcessHistoryEntryByName: %v", svc.name, err)
		}
		if svc.wantStopped {
			if hist.State != types.ProcessStateStopped {
				t.Errorf("%s: want Stopped, got %s", svc.name, hist.State)
			}
		} else if hist.State != svc.state {
			t.Errorf("%s: want %s unchanged, got %s", svc.name, svc.state, hist.State)
		}
	}
}

// TODO: no test coverage for handleIncomingCommands, handleConnection, or
// sendErrorResponse (all in daemon.go) — they need a real net.Listener/net.Conn
// and aren't exercised elsewhere.
