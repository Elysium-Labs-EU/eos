package process

import (
	"errors"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
)

func TestAllMethodsHandled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	for method := range types.ValidMethods {
		t.Run(string(method), func(t *testing.T) {
			req := types.DaemonRequest{Method: method, Args: nil}
			resp := executeRequest(manager, req)

			// Should NOT get "unknown method" error
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
			if _, err := db.RegisterProcessHistoryEntry(t.Context(), 12345, "svc", state); err != nil {
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

func TestReconcileOrphans_ActiveStates(t *testing.T) {
	for _, state := range []types.ProcessState{types.ProcessStateRunning, types.ProcessStateStarting, types.ProcessStateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

			if err := db.RegisterService(t.Context(), "svc", "/opt/svc", "service.yaml"); err != nil {
				t.Fatalf("RegisterService: %v", err)
			}
			if _, err := db.RegisterProcessHistoryEntry(t.Context(), 2000001, "svc", state); err != nil {
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
		if _, err := db.RegisterProcessHistoryEntry(t.Context(), svc.pgid, svc.name, svc.state); err != nil {
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

// func TestStartDaemon(t *testing.T) {}
// func TestStopDaemon(t *testing.T) {}
// func TestStatusDaemon(t *testing.T) {}
// func TestHandleIncomingCommands(t *testing.T) {}
// func TestHandleConnection(t *testing.T) {}
// func TestSendErrorResponse(t *testing.T) {}
