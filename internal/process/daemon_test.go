package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// capturingLogger returns a *slog.Logger (with Debug enabled) plus the buffer
// its JSON output is written to, so tests can assert on both the level and
// message of what got logged.
func capturingLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
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

	t.Run("ECHILD stops the drain and logs at debug, not error", func(t *testing.T) {
		capturing, buf := capturingLogger()
		wrapped := fmt.Errorf("wait4: %w", syscall.ECHILD)

		if got := handleReapedChild(t.Context(), nil, capturing, -1, wrapped, status); got != reapStop {
			t.Errorf("expected reapStop on ECHILD, got %v", got)
		}

		out := buf.String()
		if strings.Contains(out, `"level":"ERROR"`) {
			t.Errorf("ECHILD must not log at ERROR, got: %s", out)
		}
		if !strings.Contains(out, `"level":"DEBUG"`) || !strings.Contains(out, "reap loop drained") {
			t.Errorf("expected a DEBUG 'reap loop drained' log line, got: %s", out)
		}
	})

	t.Run("genuine wait error still logs at error", func(t *testing.T) {
		capturing, buf := capturingLogger()

		if got := handleReapedChild(t.Context(), nil, capturing, -1, syscall.EINVAL, status); got != reapStop {
			t.Errorf("expected reapStop on wait error, got %v", got)
		}

		out := buf.String()
		if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, "cleaning up child process") {
			t.Errorf("expected an ERROR 'cleaning up child process' log line, got: %s", out)
		}
	})
}

func TestIsClientDisconnect(t *testing.T) {
	tests := []struct {
		err  error
		name string
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "EPIPE", err: syscall.EPIPE, want: true},
		{name: "wrapped EPIPE", err: fmt.Errorf("write: %w", syscall.EPIPE), want: true},
		{name: "ECONNRESET", err: syscall.ECONNRESET, want: true},
		{name: "wrapped ECONNRESET", err: fmt.Errorf("write: %w", syscall.ECONNRESET), want: true},
		{name: "net.ErrClosed", err: net.ErrClosed, want: true},
		{name: "wrapped net.ErrClosed", err: fmt.Errorf("write: %w", net.ErrClosed), want: true},
		{name: "unrelated error", err: errors.New("boom"), want: false},
		{name: "unrelated errno", err: syscall.EINVAL, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClientDisconnect(tt.err); got != tt.want {
				t.Errorf("isClientDisconnect(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestLogClientWriteError(t *testing.T) {
	t.Run("client disconnect logs at debug", func(t *testing.T) {
		logger, buf := capturingLogger()
		logClientWriteError(logger, "sending response", syscall.EPIPE)

		out := buf.String()
		if strings.Contains(out, `"level":"ERROR"`) {
			t.Errorf("client disconnect must not log at ERROR, got: %s", out)
		}
		if !strings.Contains(out, `"level":"DEBUG"`) || !strings.Contains(out, "sending response") {
			t.Errorf("expected a DEBUG 'sending response' log line, got: %s", out)
		}
	})

	t.Run("other errors log at error", func(t *testing.T) {
		logger, buf := capturingLogger()
		logClientWriteError(logger, "sending response", errors.New("boom"))

		out := buf.String()
		if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, "sending response") {
			t.Errorf("expected an ERROR 'sending response' log line, got: %s", out)
		}
	})
}

func TestAllMethodsHandled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	manager := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	for method := range types.ValidMethods {
		t.Run(string(method), func(t *testing.T) {
			req := types.DaemonRequest{Method: method, Args: nil}
			resp := executeRequest(t.Context(), manager, req)

			// Every method in ValidMethods must have an entry in requestHandlers;
			// a missing entry falls through to this error string.
			if !resp.Success && strings.Contains(resp.Error, "unknown method") {
				t.Errorf("Method %s not registered in requestHandlers", method)
			}
		})
	}
}

// TestExecuteRequest_UnknownMethod proves the reverse of TestAllMethodsHandled:
// a method with no entry in requestHandlers hits the "unknown method" branch
// rather than panicking on a nil map lookup.
func TestExecuteRequest_UnknownMethod(t *testing.T) {
	resp := executeRequest(t.Context(), nil, types.DaemonRequest{Method: "NotARealMethod"})
	if resp.Success {
		t.Fatal("expected failure for an unregistered method")
	}
	if !strings.Contains(resp.Error, "unknown method") {
		t.Errorf("expected an 'unknown method' error, got: %s", resp.Error)
	}
}

func TestExecuteRequest_GetVersion(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodGetVersion})
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var got types.GetVersionResponse
	if err := json.Unmarshal(resp.Data, &got); err != nil {
		t.Fatalf("unmarshaling response data: %v", err)
	}
	if got.Version == "" {
		t.Error("expected non-empty version")
	}
}

// TestExecuteRequest_DependencyWaitStatus proves the full server-side round
// trip for the 3 new methods against a real *manager.LocalManager: Set then
// Get returns the recorded wait, Clear then Get returns not-waiting.
func TestExecuteRequest_DependencyWaitStatus(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	getArgs, _ := json.Marshal(types.GetDependencyWaitStatusArgs{ServiceName: "web"})
	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodGetDependencyWaitStatus, Args: getArgs})
	if !resp.Success {
		t.Fatalf("initial Get: expected success, got error: %s", resp.Error)
	}
	var initial types.GetDependencyWaitStatusResponse
	if err := json.Unmarshal(resp.Data, &initial); err != nil {
		t.Fatalf("unmarshaling initial Get response: %v", err)
	}
	if initial.Waiting {
		t.Fatalf("expected not waiting initially, got %+v", initial)
	}

	setArgs, _ := json.Marshal(types.SetDependencyWaitStatusArgs{ServiceName: "web", Pending: []string{"db", "cache"}, Deadline: time.Now().Add(5 * time.Minute)})
	resp = executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodSetDependencyWaitStatus, Args: setArgs})
	if !resp.Success {
		t.Fatalf("Set: expected success, got error: %s", resp.Error)
	}

	resp = executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodGetDependencyWaitStatus, Args: getArgs})
	if !resp.Success {
		t.Fatalf("Get after Set: expected success, got error: %s", resp.Error)
	}
	var afterSet types.GetDependencyWaitStatusResponse
	if err := json.Unmarshal(resp.Data, &afterSet); err != nil {
		t.Fatalf("unmarshaling post-Set Get response: %v", err)
	}
	if !afterSet.Waiting || afterSet.Status == nil {
		t.Fatalf("expected waiting=true with a status after Set, got %+v", afterSet)
	}
	if afterSet.Status.ServiceName != "web" || len(afterSet.Status.Pending) != 2 {
		t.Errorf("unexpected status after Set: %+v", afterSet.Status)
	}

	clearArgs, _ := json.Marshal(types.ClearDependencyWaitStatusArgs{ServiceName: "web"})
	resp = executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodClearDependencyWaitStatus, Args: clearArgs})
	if !resp.Success {
		t.Fatalf("Clear: expected success, got error: %s", resp.Error)
	}

	resp = executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodGetDependencyWaitStatus, Args: getArgs})
	if !resp.Success {
		t.Fatalf("Get after Clear: expected success, got error: %s", resp.Error)
	}
	var afterClear types.GetDependencyWaitStatusResponse
	if err := json.Unmarshal(resp.Data, &afterClear); err != nil {
		t.Fatalf("unmarshaling post-Clear Get response: %v", err)
	}
	if afterClear.Waiting {
		t.Fatalf("expected not waiting after Clear, got %+v", afterClear)
	}
}

// TestExecuteRequest_SetServiceEnabled proves the daemon-side dispatch for
// MethodSetServiceEnabled against a real *manager.LocalManager: a registered
// service's Enabled flag flips, and a service that doesn't exist surfaces a
// sentinel error rather than succeeding silently.
func TestExecuteRequest_SetServiceEnabled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := db.RegisterService(t.Context(), "web", "/srv/web", "service.yaml"); err != nil {
		t.Fatalf("seeding catalog entry: %v", err)
	}

	disableArgs, _ := json.Marshal(types.SetServiceEnabledArgs{Name: "web", Enabled: false})
	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodSetServiceEnabled, Args: disableArgs})
	if !resp.Success {
		t.Fatalf("disable: expected success, got error: %s", resp.Error)
	}
	entry, err := mgr.GetServiceCatalogEntry(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if entry.Enabled {
		t.Error("expected Enabled=false after disabling via SetServiceEnabled")
	}

	enableArgs, _ := json.Marshal(types.SetServiceEnabledArgs{Name: "web", Enabled: true})
	resp = executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodSetServiceEnabled, Args: enableArgs})
	if !resp.Success {
		t.Fatalf("enable: expected success, got error: %s", resp.Error)
	}
	entry, err = mgr.GetServiceCatalogEntry(t.Context(), "web")
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if !entry.Enabled {
		t.Error("expected Enabled=true after re-enabling via SetServiceEnabled")
	}
}

// TestExecuteRequest_SetServiceEnabled_InvalidArgs proves malformed args are
// rejected with an "invalid args" error rather than panicking on Unmarshal.
func TestExecuteRequest_SetServiceEnabled_InvalidArgs(t *testing.T) {
	resp := executeRequest(t.Context(), nil, types.DaemonRequest{Method: types.MethodSetServiceEnabled, Args: json.RawMessage(`not-json`)})
	if resp.Success {
		t.Fatal("expected failure for malformed args")
	}
	if !strings.Contains(resp.Error, "invalid MethodSetServiceEnabled args") {
		t.Errorf("expected invalid args error, got: %s", resp.Error)
	}
}

// TestExecuteRequest_SetServiceEnabled_UnknownService proves a nonexistent
// service surfaces the manager's error instead of succeeding silently.
func TestExecuteRequest_SetServiceEnabled_UnknownService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	args, _ := json.Marshal(types.SetServiceEnabledArgs{Name: "ghost", Enabled: false})
	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodSetServiceEnabled, Args: args})
	if resp.Success {
		t.Fatal("expected failure for an unregistered service")
	}
}

// TestExecuteRequest_DependencyWaitStatus_unsupportedManager proves the 3
// handlers degrade to a clean error (not a panic) against a manager.ServiceManager
// that doesn't implement dependencyWaitStatusStore.
func TestExecuteRequest_DependencyWaitStatus_unsupportedManager(t *testing.T) {
	mgr := &fakeServiceManager{}

	for _, method := range []types.MethodName{
		types.MethodSetDependencyWaitStatus,
		types.MethodClearDependencyWaitStatus,
		types.MethodGetDependencyWaitStatus,
	} {
		t.Run(string(method), func(t *testing.T) {
			resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: method, Args: []byte(`{}`)})
			if resp.Success {
				t.Fatalf("expected failure for unsupported manager, got success")
			}
			if !strings.Contains(resp.Error, "not supported") {
				t.Errorf("expected a 'not supported' error, got: %s", resp.Error)
			}
		})
	}
}

// fakeDependencyWaitStore implements dependencyWaitStatusStore over a
// manager.ServiceManager stand-in, so the 3 dependency-wait handlers'
// sentinelErrorResponse branches can be tested independently of what a real
// *manager.LocalManager can be made to fail on.
type fakeDependencyWaitStore struct {
	manager.ServiceManager
	setErr    error
	clearErr  error
	getErr    error
	getStatus types.DependencyWaitStatus
	getWait   bool
}

func (f *fakeDependencyWaitStore) SetDependencyWaitStatus(context.Context, string, []string, time.Time) error {
	return f.setErr
}

func (f *fakeDependencyWaitStore) ClearDependencyWaitStatus(context.Context, string) error {
	return f.clearErr
}

func (f *fakeDependencyWaitStore) GetDependencyWaitStatus(context.Context, string) (types.DependencyWaitStatus, bool, error) {
	return f.getStatus, f.getWait, f.getErr
}

func TestHandleSetDependencyWaitStatus_storeError(t *testing.T) {
	mgr := &fakeDependencyWaitStore{setErr: errors.New("disk full")}
	args, _ := json.Marshal(types.SetDependencyWaitStatusArgs{ServiceName: "web"})
	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodSetDependencyWaitStatus, Args: args})
	if resp.Success {
		t.Fatal("expected failure when the store errors")
	}
}

func TestHandleClearDependencyWaitStatus_storeError(t *testing.T) {
	mgr := &fakeDependencyWaitStore{clearErr: errors.New("disk full")}
	args, _ := json.Marshal(types.ClearDependencyWaitStatusArgs{ServiceName: "web"})
	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodClearDependencyWaitStatus, Args: args})
	if resp.Success {
		t.Fatal("expected failure when the store errors")
	}
}

func TestHandleGetDependencyWaitStatus_storeError(t *testing.T) {
	mgr := &fakeDependencyWaitStore{getErr: errors.New("disk full")}
	args, _ := json.Marshal(types.GetDependencyWaitStatusArgs{ServiceName: "web"})
	resp := executeRequest(t.Context(), mgr, types.DaemonRequest{Method: types.MethodGetDependencyWaitStatus, Args: args})
	if resp.Success {
		t.Fatal("expected failure when the store errors")
	}
}

func TestReconcileOrphans_Empty(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))
}

// TestReconcileOrphans_ClearsDependencyWaits proves a fresh daemon boot wipes
// any dependency_waits row left over from a previous daemon process — it
// belonged to a goroutine that no longer exists, so trusting it (even with a
// still-future deadline) would misreport a service as gated on a dependency
// that nothing is actually still waiting for.
func TestReconcileOrphans_ClearsDependencyWaits(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	if err := db.SetDependencyWaitStatus(t.Context(), "web", []string{"proxy"}, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("SetDependencyWaitStatus: %v", err)
	}

	reconcileOrphans(t.Context(), db, testutil.NewTestLogger(t))

	if _, waiting, err := db.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Errorf("expected reconcileOrphans to clear the stale wait, waiting=%v err=%v", waiting, err)
	}
}

// TestReconcileOrphans_ClearAllDependencyWaitsErrorIsLogged proves a failure
// clearing stale dependency waits is logged and swallowed, not a reason to
// abort the rest of reconcileOrphans's orphan-checking work.
func TestReconcileOrphans_ClearAllDependencyWaitsErrorIsLogged(t *testing.T) {
	db, _, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	if err := db.CloseDBConnection(); err != nil {
		t.Fatalf("CloseDBConnection: %v", err)
	}

	logger, buf := capturingLogger()
	reconcileOrphans(t.Context(), db, logger)

	if !strings.Contains(buf.String(), "clearing stale dependency waits") {
		t.Errorf("expected the clear failure to be logged, got: %s", buf.String())
	}
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
			// SIGKILL delivery is asynchronous: reconcileOrphans returning only
			// means the signal was sent, not that the kernel has finished tearing
			// the process down yet. Poll briefly instead of checking once
			// immediately, so a slow-scheduled kill under CPU contention doesn't
			// flake the test.
			deadline := time.Now().Add(time.Second)
			for procutil.IsAlive(pgid) && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
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

// fakeServiceManager is a manager.ServiceManager test double: it embeds the
// nil interface (any unoverridden method panics if called) and only
// implements RestartService/StopService/GetVersion, the methods
// handleRestartService, handleStopService, and handleGetVersion invoke.
type fakeServiceManager struct {
	manager.ServiceManager
	restartFunc                    func(name string, gracePeriod, tickerPeriod time.Duration) (int, error)
	stopFunc                       func(name string, gracePeriod, tickerPeriod time.Duration) (manager.StopServiceResult, error)
	getVersionFunc                 func() (types.GetVersionResponse, error)
	getServiceInstanceFunc         func(name string) (*types.ServiceInstance, error)
	removeServiceInstanceFunc      func(name string) (bool, error)
	startServiceFunc               func(name string) (int, error)
	forceStopServiceFunc           func(name string) (manager.StopServiceResult, error)
	addServiceCatalogEntryFunc     func(service *types.ServiceCatalogEntry) error
	getServiceCatalogEntryFunc     func(name string) (types.ServiceCatalogEntry, error)
	isServiceRegisteredFunc        func(name string) (bool, error)
	removeServiceCatalogEntryFunc  func(name string) (bool, error)
	updateServiceCatalogEntryFunc  func(name, newDirectoryPath, newConfigFileName string) error
	getMostRecentProcessHistFunc   func(name string) (*types.ProcessHistory, error)
	getLiveOrphanProcessGroupsFunc func(name string) ([]types.ProcessHistory, error)
	newServiceLogFilesFunc         func(serviceName string) (string, string, error)
	getServiceLogFilePathFunc      func(serviceName string, errorLog bool) (*string, error)
}

func (f *fakeServiceManager) RestartService(_ context.Context, name string, gracePeriod, tickerPeriod time.Duration) (int, error) {
	return f.restartFunc(name, gracePeriod, tickerPeriod)
}

func (f *fakeServiceManager) StopService(_ context.Context, name string, gracePeriod, tickerPeriod time.Duration) (manager.StopServiceResult, error) {
	return f.stopFunc(name, gracePeriod, tickerPeriod)
}

func (f *fakeServiceManager) GetVersion(_ context.Context) (types.GetVersionResponse, error) {
	return f.getVersionFunc()
}

func (f *fakeServiceManager) GetServiceInstance(_ context.Context, name string) (*types.ServiceInstance, error) {
	return f.getServiceInstanceFunc(name)
}

func (f *fakeServiceManager) RemoveServiceInstance(_ context.Context, name string) (bool, error) {
	return f.removeServiceInstanceFunc(name)
}

func (f *fakeServiceManager) StartService(_ context.Context, name string) (int, error) {
	return f.startServiceFunc(name)
}

func (f *fakeServiceManager) ForceStopService(_ context.Context, name string) (manager.StopServiceResult, error) {
	return f.forceStopServiceFunc(name)
}

func (f *fakeServiceManager) AddServiceCatalogEntry(_ context.Context, service *types.ServiceCatalogEntry) error {
	return f.addServiceCatalogEntryFunc(service)
}

func (f *fakeServiceManager) GetServiceCatalogEntry(_ context.Context, name string) (types.ServiceCatalogEntry, error) {
	return f.getServiceCatalogEntryFunc(name)
}

func (f *fakeServiceManager) IsServiceRegistered(_ context.Context, name string) (bool, error) {
	return f.isServiceRegisteredFunc(name)
}

func (f *fakeServiceManager) RemoveServiceCatalogEntry(_ context.Context, name string) (bool, error) {
	return f.removeServiceCatalogEntryFunc(name)
}

func (f *fakeServiceManager) UpdateServiceCatalogEntry(_ context.Context, name, newDirectoryPath, newConfigFileName string) error {
	return f.updateServiceCatalogEntryFunc(name, newDirectoryPath, newConfigFileName)
}

func (f *fakeServiceManager) GetMostRecentProcessHistoryEntry(_ context.Context, name string) (*types.ProcessHistory, error) {
	return f.getMostRecentProcessHistFunc(name)
}

func (f *fakeServiceManager) GetLiveOrphanProcessGroups(_ context.Context, name string) ([]types.ProcessHistory, error) {
	return f.getLiveOrphanProcessGroupsFunc(name)
}

func (f *fakeServiceManager) NewServiceLogFiles(_ context.Context, serviceName string) (string, string, error) {
	return f.newServiceLogFilesFunc(serviceName)
}

func (f *fakeServiceManager) GetServiceLogFilePath(_ context.Context, serviceName string, errorLog bool) (*string, error) {
	return f.getServiceLogFilePathFunc(serviceName, errorLog)
}

func TestHandleRestartService(t *testing.T) {
	t.Run("invalid JSON args", func(t *testing.T) {
		resp := handleRestartService(t.Context(), &fakeServiceManager{}, json.RawMessage(`{invalid`))
		if resp.Success {
			t.Fatal("expected failure for invalid JSON")
		}
		if !strings.Contains(resp.Error, "invalid MethodRestartService args") {
			t.Errorf("unexpected error message: %s", resp.Error)
		}
	})

	t.Run("invalid grace period", func(t *testing.T) {
		raw, _ := json.Marshal(types.RestartServiceArgs{Name: "svc", GracePeriod: "not-a-duration", TickerPeriod: "1s"})
		resp := handleRestartService(t.Context(), &fakeServiceManager{}, raw)
		if resp.Success {
			t.Fatal("expected failure for invalid grace period")
		}
		if !strings.Contains(resp.Error, "invalid grace period") {
			t.Errorf("unexpected error message: %s", resp.Error)
		}
	})

	t.Run("invalid ticker period", func(t *testing.T) {
		raw, _ := json.Marshal(types.RestartServiceArgs{Name: "svc", GracePeriod: "1s", TickerPeriod: "not-a-duration"})
		resp := handleRestartService(t.Context(), &fakeServiceManager{}, raw)
		if resp.Success {
			t.Fatal("expected failure for invalid ticker period")
		}
		if !strings.Contains(resp.Error, "invalid ticker period") {
			t.Errorf("unexpected error message: %s", resp.Error)
		}
	})

	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("restart failed")
		mgr := &fakeServiceManager{
			restartFunc: func(name string, gracePeriod, tickerPeriod time.Duration) (int, error) {
				return 0, wantErr
			},
		}
		raw, _ := json.Marshal(types.RestartServiceArgs{Name: "svc", GracePeriod: "1s", TickerPeriod: "1s"})
		resp := handleRestartService(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.RestartService errors")
		}
		if resp.Error != wantErr.Error() {
			t.Errorf("expected error %q, got %q", wantErr.Error(), resp.Error)
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{
			restartFunc: func(name string, gracePeriod, tickerPeriod time.Duration) (int, error) {
				if name != "svc" {
					t.Errorf("expected name svc, got %s", name)
				}
				if gracePeriod != 2*time.Second {
					t.Errorf("expected grace period 2s, got %s", gracePeriod)
				}
				if tickerPeriod != 500*time.Millisecond {
					t.Errorf("expected ticker period 500ms, got %s", tickerPeriod)
				}
				return 4242, nil
			},
		}
		raw, _ := json.Marshal(types.RestartServiceArgs{Name: "svc", GracePeriod: "2s", TickerPeriod: "500ms"})
		resp := handleRestartService(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]int
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got["pid"] != 4242 {
			t.Errorf("expected pid 4242, got %d", got["pid"])
		}
	})
}

func TestHandleStopService(t *testing.T) {
	t.Run("invalid JSON args", func(t *testing.T) {
		resp := handleStopService(t.Context(), &fakeServiceManager{}, json.RawMessage(`{invalid`))
		if resp.Success {
			t.Fatal("expected failure for invalid JSON")
		}
		if !strings.Contains(resp.Error, "invalid MethodStopService args") {
			t.Errorf("unexpected error message: %s", resp.Error)
		}
	})

	t.Run("invalid grace period", func(t *testing.T) {
		raw, _ := json.Marshal(types.StopServiceArgs{Name: "svc", GracePeriod: "not-a-duration", TickerPeriod: "1s"})
		resp := handleStopService(t.Context(), &fakeServiceManager{}, raw)
		if resp.Success {
			t.Fatal("expected failure for invalid grace period")
		}
		if !strings.Contains(resp.Error, "invalid grace period") {
			t.Errorf("unexpected error message: %s", resp.Error)
		}
	})

	t.Run("invalid ticker period", func(t *testing.T) {
		raw, _ := json.Marshal(types.StopServiceArgs{Name: "svc", GracePeriod: "1s", TickerPeriod: "not-a-duration"})
		resp := handleStopService(t.Context(), &fakeServiceManager{}, raw)
		if resp.Success {
			t.Fatal("expected failure for invalid ticker period")
		}
		if !strings.Contains(resp.Error, "invalid ticker period") {
			t.Errorf("unexpected error message: %s", resp.Error)
		}
	})

	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("stop failed")
		mgr := &fakeServiceManager{
			stopFunc: func(name string, gracePeriod, tickerPeriod time.Duration) (manager.StopServiceResult, error) {
				return manager.StopServiceResult{}, wantErr
			},
		}
		raw, _ := json.Marshal(types.StopServiceArgs{Name: "svc", GracePeriod: "1s", TickerPeriod: "1s"})
		resp := handleStopService(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.StopService errors")
		}
		if resp.Error != wantErr.Error() {
			t.Errorf("expected error %q, got %q", wantErr.Error(), resp.Error)
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{
			stopFunc: func(name string, gracePeriod, tickerPeriod time.Duration) (manager.StopServiceResult, error) {
				if name != "svc" {
					t.Errorf("expected name svc, got %s", name)
				}
				if gracePeriod != 2*time.Second {
					t.Errorf("expected grace period 2s, got %s", gracePeriod)
				}
				if tickerPeriod != 500*time.Millisecond {
					t.Errorf("expected ticker period 500ms, got %s", tickerPeriod)
				}
				return manager.StopServiceResult{Stopped: map[int]bool{4242: true}}, nil
			},
		}
		raw, _ := json.Marshal(types.StopServiceArgs{Name: "svc", GracePeriod: "2s", TickerPeriod: "500ms"})
		resp := handleStopService(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got manager.StopServiceResult
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if !got.Stopped[4242] {
			t.Errorf("expected pid 4242 marked stopped, got %+v", got.Stopped)
		}
	})
}

func TestHandleGetServiceInstance(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		mgr := &fakeServiceManager{getServiceInstanceFunc: func(name string) (*types.ServiceInstance, error) {
			return nil, wantErr
		}}
		raw, _ := json.Marshal(types.GetServiceInstanceArgs{Name: "svc"})
		resp := handleGetServiceInstance(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.GetServiceInstance errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{getServiceInstanceFunc: func(name string) (*types.ServiceInstance, error) {
			return &types.ServiceInstance{Name: name}, nil
		}}
		raw, _ := json.Marshal(types.GetServiceInstanceArgs{Name: "svc"})
		resp := handleGetServiceInstance(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got types.GetServiceInstanceResponse
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got.Instance.Name != "svc" {
			t.Errorf("expected instance name svc, got %s", got.Instance.Name)
		}
	})
}

func TestHandleRemoveServiceInstance(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("remove failed")
		mgr := &fakeServiceManager{removeServiceInstanceFunc: func(name string) (bool, error) { return false, wantErr }}
		raw, _ := json.Marshal(types.RemoveServiceInstanceArgs{Name: "svc"})
		resp := handleRemoveServiceInstance(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.RemoveServiceInstance errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{removeServiceInstanceFunc: func(name string) (bool, error) { return true, nil }}
		raw, _ := json.Marshal(types.RemoveServiceInstanceArgs{Name: "svc"})
		resp := handleRemoveServiceInstance(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]bool
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if !got["removed"] {
			t.Errorf("expected removed=true, got %+v", got)
		}
	})
}

func TestHandleStartService(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("start failed")
		mgr := &fakeServiceManager{startServiceFunc: func(name string) (int, error) { return 0, wantErr }}
		raw, _ := json.Marshal(types.StartServiceArgs{Name: "svc"})
		resp := handleStartService(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.StartService errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{startServiceFunc: func(name string) (int, error) { return 4242, nil }}
		raw, _ := json.Marshal(types.StartServiceArgs{Name: "svc"})
		resp := handleStartService(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]int
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got["pid"] != 4242 {
			t.Errorf("expected pid 4242, got %d", got["pid"])
		}
	})
}

func TestHandleForceStopService(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("force stop failed")
		mgr := &fakeServiceManager{forceStopServiceFunc: func(name string) (manager.StopServiceResult, error) {
			return manager.StopServiceResult{}, wantErr
		}}
		raw, _ := json.Marshal(types.ForceStopServiceArgs{Name: "svc"})
		resp := handleForceStopService(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.ForceStopService errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{forceStopServiceFunc: func(name string) (manager.StopServiceResult, error) {
			return manager.StopServiceResult{Stopped: map[int]bool{4242: true}}, nil
		}}
		raw, _ := json.Marshal(types.ForceStopServiceArgs{Name: "svc"})
		resp := handleForceStopService(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got manager.StopServiceResult
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if !got.Stopped[4242] {
			t.Errorf("expected pid 4242 marked stopped, got %+v", got.Stopped)
		}
	})
}

func TestHandleAddServiceCatalogEntry(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("add failed")
		mgr := &fakeServiceManager{addServiceCatalogEntryFunc: func(service *types.ServiceCatalogEntry) error { return wantErr }}
		raw, _ := json.Marshal(types.AddServiceCatalogEntryArgs{Service: &types.ServiceCatalogEntry{Name: "svc"}})
		resp := handleAddServiceCatalogEntry(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.AddServiceCatalogEntry errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{addServiceCatalogEntryFunc: func(service *types.ServiceCatalogEntry) error { return nil }}
		raw, _ := json.Marshal(types.AddServiceCatalogEntryArgs{Service: &types.ServiceCatalogEntry{Name: "svc"}})
		resp := handleAddServiceCatalogEntry(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
	})
}

func TestHandleGetServiceCatalogEntry(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		mgr := &fakeServiceManager{getServiceCatalogEntryFunc: func(name string) (types.ServiceCatalogEntry, error) {
			return types.ServiceCatalogEntry{}, wantErr
		}}
		raw, _ := json.Marshal(types.GetServiceCatalogEntryArgs{Name: "svc"})
		resp := handleGetServiceCatalogEntry(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.GetServiceCatalogEntry errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{getServiceCatalogEntryFunc: func(name string) (types.ServiceCatalogEntry, error) {
			return types.ServiceCatalogEntry{Name: name}, nil
		}}
		raw, _ := json.Marshal(types.GetServiceCatalogEntryArgs{Name: "svc"})
		resp := handleGetServiceCatalogEntry(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got types.ServiceCatalogEntry
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got.Name != "svc" {
			t.Errorf("expected name svc, got %s", got.Name)
		}
	})
}

func TestHandleIsServiceRegistered(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		mgr := &fakeServiceManager{isServiceRegisteredFunc: func(name string) (bool, error) { return false, wantErr }}
		raw, _ := json.Marshal(types.IsServiceRegisteredArgs{Name: "svc"})
		resp := handleIsServiceRegistered(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.IsServiceRegistered errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{isServiceRegisteredFunc: func(name string) (bool, error) { return true, nil }}
		raw, _ := json.Marshal(types.IsServiceRegisteredArgs{Name: "svc"})
		resp := handleIsServiceRegistered(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]bool
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if !got["exists"] {
			t.Errorf("expected exists=true, got %+v", got)
		}
	})
}

func TestHandleRemoveServiceCatalogEntry(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("remove failed")
		mgr := &fakeServiceManager{removeServiceCatalogEntryFunc: func(name string) (bool, error) { return false, wantErr }}
		raw, _ := json.Marshal(types.RemoveServiceCatalogEntryArgs{Name: "svc"})
		resp := handleRemoveServiceCatalogEntry(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.RemoveServiceCatalogEntry errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{removeServiceCatalogEntryFunc: func(name string) (bool, error) { return true, nil }}
		raw, _ := json.Marshal(types.RemoveServiceCatalogEntryArgs{Name: "svc"})
		resp := handleRemoveServiceCatalogEntry(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]bool
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if !got["removed"] {
			t.Errorf("expected removed=true, got %+v", got)
		}
	})
}

func TestHandleUpdateServiceCatalogEntry(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("update failed")
		mgr := &fakeServiceManager{updateServiceCatalogEntryFunc: func(name, newDirectoryPath, newConfigFileName string) error {
			return wantErr
		}}
		raw, _ := json.Marshal(types.UpdateServiceCatalogEntryArgs{Name: "svc", NewDirectoryPath: "/new", NewConfigFileName: "service.yaml"})
		resp := handleUpdateServiceCatalogEntry(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.UpdateServiceCatalogEntry errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{updateServiceCatalogEntryFunc: func(name, newDirectoryPath, newConfigFileName string) error {
			if name != "svc" || newDirectoryPath != "/new" || newConfigFileName != "service.yaml" {
				t.Errorf("unexpected args: %s %s %s", name, newDirectoryPath, newConfigFileName)
			}
			return nil
		}}
		raw, _ := json.Marshal(types.UpdateServiceCatalogEntryArgs{Name: "svc", NewDirectoryPath: "/new", NewConfigFileName: "service.yaml"})
		resp := handleUpdateServiceCatalogEntry(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
	})
}

func TestHandleGetMostRecentProcessHistoryEntry(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		mgr := &fakeServiceManager{getMostRecentProcessHistFunc: func(name string) (*types.ProcessHistory, error) {
			return nil, wantErr
		}}
		raw, _ := json.Marshal(types.GetMostRecentProcessHistoryEntryArgs{Name: "svc"})
		resp := handleGetMostRecentProcessHistoryEntry(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.GetMostRecentProcessHistoryEntry errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{getMostRecentProcessHistFunc: func(name string) (*types.ProcessHistory, error) {
			return &types.ProcessHistory{ServiceName: name}, nil
		}}
		raw, _ := json.Marshal(types.GetMostRecentProcessHistoryEntryArgs{Name: "svc"})
		resp := handleGetMostRecentProcessHistoryEntry(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got types.GetMostRecentProcessHistoryEntryResponse
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got.ProcessEntry.ServiceName != "svc" {
			t.Errorf("expected service name svc, got %s", got.ProcessEntry.ServiceName)
		}
	})
}

func TestHandleGetLiveOrphanProcessGroups(t *testing.T) {
	t.Run("invalid args", func(t *testing.T) {
		mgr := &fakeServiceManager{}
		resp := handleGetLiveOrphanProcessGroups(t.Context(), mgr, json.RawMessage(`{`))
		if resp.Success {
			t.Fatal("expected failure for invalid args")
		}
	})

	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		mgr := &fakeServiceManager{getLiveOrphanProcessGroupsFunc: func(name string) ([]types.ProcessHistory, error) {
			return nil, wantErr
		}}
		raw, _ := json.Marshal(types.GetLiveOrphanProcessGroupsArgs{Name: "svc"})
		resp := handleGetLiveOrphanProcessGroups(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.GetLiveOrphanProcessGroups errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{getLiveOrphanProcessGroupsFunc: func(name string) ([]types.ProcessHistory, error) {
			return []types.ProcessHistory{{ServiceName: name, PGID: 1763}}, nil
		}}
		raw, _ := json.Marshal(types.GetLiveOrphanProcessGroupsArgs{Name: "svc"})
		resp := handleGetLiveOrphanProcessGroups(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got types.GetLiveOrphanProcessGroupsResponse
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if len(got.Entries) != 1 || got.Entries[0].PGID != 1763 {
			t.Errorf("expected [{PGID:1763}], got %+v", got.Entries)
		}
	})
}

func TestHandleNewServiceLogFiles(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("create failed")
		mgr := &fakeServiceManager{newServiceLogFilesFunc: func(serviceName string) (string, string, error) {
			return "", "", wantErr
		}}
		raw, _ := json.Marshal(types.NewServiceLogFilesArgs{ServiceName: "svc"})
		resp := handleNewServiceLogFiles(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.NewServiceLogFiles errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		mgr := &fakeServiceManager{newServiceLogFilesFunc: func(serviceName string) (string, string, error) {
			return "/log/out.log", "/log/err.log", nil
		}}
		raw, _ := json.Marshal(types.NewServiceLogFilesArgs{ServiceName: "svc"})
		resp := handleNewServiceLogFiles(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]string
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got["logPath"] != "/log/out.log" || got["errorLogPath"] != "/log/err.log" {
			t.Errorf("unexpected paths: %+v", got)
		}
	})
}

func TestHandleGetServiceLogFilePath(t *testing.T) {
	t.Run("mgr returns error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		mgr := &fakeServiceManager{getServiceLogFilePathFunc: func(serviceName string, errorLog bool) (*string, error) {
			return nil, wantErr
		}}
		raw, _ := json.Marshal(types.GetServiceLogFilePathArgs{ServiceName: "svc"})
		resp := handleGetServiceLogFilePath(t.Context(), mgr, raw)
		if resp.Success {
			t.Fatal("expected failure when mgr.GetServiceLogFilePath errors")
		}
	})

	t.Run("success", func(t *testing.T) {
		path := "/log/out.log"
		mgr := &fakeServiceManager{getServiceLogFilePathFunc: func(serviceName string, errorLog bool) (*string, error) {
			return &path, nil
		}}
		raw, _ := json.Marshal(types.GetServiceLogFilePathArgs{ServiceName: "svc"})
		resp := handleGetServiceLogFilePath(t.Context(), mgr, raw)
		if !resp.Success {
			t.Fatalf("expected success, got error: %s", resp.Error)
		}
		var got map[string]*string
		if err := json.Unmarshal(resp.Data, &got); err != nil {
			t.Fatalf("unmarshaling response data: %v", err)
		}
		if got["filepath"] == nil || *got["filepath"] != path {
			t.Errorf("unexpected filepath: %+v", got)
		}
	})
}

func TestIsAuthorizedPeer(t *testing.T) {
	tests := []struct {
		name       string
		gotUID     uint32
		allowedUID uint32
		want       bool
	}{
		{"matching uid authorized", 1000, 1000, true},
		{"mismatched uid rejected", 1000, 1001, false},
		{"root allowed uid zero", 0, 0, true},
		{"root authorized regardless of allowedUID", 0, 1000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAuthorizedPeer(tt.gotUID, tt.allowedUID); got != tt.want {
				t.Errorf("isAuthorizedPeer(%d, %d) = %v, want %v", tt.gotUID, tt.allowedUID, got, tt.want)
			}
		})
	}
}

// newUnixSocketPair binds a real Unix domain socket in t.TempDir(), dials it,
// and hands back both ends already connected — a real *net.UnixConn pair, so
// tests exercise the actual SO_PEERCRED/LOCAL_PEERCRED syscall path in
// peerUID rather than a mocked one.
func newUnixSocketPair(t *testing.T) (serverConn, clientConn net.Conn) {
	t.Helper()

	sockPath := filepath.Join(shortTempDir(t), "t.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listening on unix socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	acceptCh := make(chan net.Conn, 1)
	acceptErrCh := make(chan error, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			acceptErrCh <- acceptErr
			return
		}
		acceptCh <- conn
	}()

	clientConn, err = net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dialing unix socket: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	select {
	case serverConn = <-acceptCh:
	case acceptErr := <-acceptErrCh:
		t.Fatalf("accepting connection: %v", acceptErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the server side of the socket pair to accept")
	}
	t.Cleanup(func() { _ = serverConn.Close() })

	return serverConn, clientConn
}

// TestHandleConnection_SameUIDAccepted proves the daemon's own CLI commands
// keep working: a connection from this same process (necessarily the same
// uid) passes the peer-credential check and gets dispatched to the manager
// as before. This exercises the real peerUID syscall, not a mock.
func TestHandleConnection_SameUIDAccepted(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", t.TempDir())

	serverConn, clientConn := newUnixSocketPair(t)
	logger := discardLogger()
	mgr := &fakeServiceManager{
		getVersionFunc: func() (types.GetVersionResponse, error) {
			return types.GetVersionResponse{Version: "test-version"}, nil
		},
	}

	if err := json.NewEncoder(clientConn).Encode(types.DaemonRequest{Method: types.MethodGetVersion}); err != nil {
		t.Fatalf("client encode: %v", err)
	}

	done := make(chan struct{})
	go func() {
		handleConnection(t.Context(), serverConn, mgr, logger, uint32(os.Getuid()))
		close(done)
	}()

	var resp types.DaemonResponse
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("client decode: %v", err)
	}
	<-done

	if !resp.Success {
		t.Fatalf("expected success for a same-uid connection, got error: %s", resp.Error)
	}
	var version types.GetVersionResponse
	if err := json.Unmarshal(resp.Data, &version); err != nil {
		t.Fatalf("unmarshaling response data: %v", err)
	}
	if version.Version != "test-version" {
		t.Errorf("expected version %q, got %q", "test-version", version.Version)
	}
}

// TestHandleConnection_MismatchedUIDRejected proves a non-root peer whose uid
// doesn't match the daemon's own uid is rejected before its request is even
// decoded — root gets its own carve-out (TestHandleConnection_RootUIDAccepted)
// so this only holds when the real process uid isn't 0, hence the skip under
// root. A real second-uid caller isn't available in CI, so this connects as
// the real process uid (a genuine SO_PEERCRED/LOCAL_PEERCRED read, not a
// mock) but tells handleConnection to only accept a different uid — the same
// boundary check production wires up via os.Getuid(). fakeServiceManager's
// getVersionFunc is left nil, so a panic would surface if the request were
// ever dispatched.
func TestHandleConnection_MismatchedUIDRejected(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root is always an authorized peer; see TestHandleConnection_RootUIDAccepted")
	}
	t.Setenv("EOS_BASE_DIR", t.TempDir())

	serverConn, clientConn := newUnixSocketPair(t)
	logger, logBuf := capturingLogger()
	mgr := &fakeServiceManager{}
	wrongUID := uint32(os.Getuid()) + 1

	if err := json.NewEncoder(clientConn).Encode(types.DaemonRequest{Method: types.MethodGetVersion}); err != nil {
		t.Fatalf("client encode: %v", err)
	}

	done := make(chan struct{})
	go func() {
		handleConnection(t.Context(), serverConn, mgr, logger, wrongUID)
		close(done)
	}()

	var resp types.DaemonResponse
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("client decode: %v", err)
	}
	<-done

	if resp.Success {
		t.Fatal("expected the connection to be rejected, got success")
	}
	if resp.Error != "unauthorized" {
		t.Errorf("expected error %q, got %q", "unauthorized", resp.Error)
	}
	if !strings.Contains(logBuf.String(), "rejecting connection from unauthorized peer") {
		t.Errorf("expected a rejection log entry, got: %s", logBuf.String())
	}
}

// TestHandleConnection_RootUIDAccepted proves the case this round's fix
// exists for: a peer connecting as raw root (uid 0) is authorized even
// though it doesn't match the daemon's own (privilege-dropped) uid — the
// real-world shape of `sudo eos <command>` against an already-running
// daemon that was started under sudo and dropped to a lower uid (see
// cmd/daemon.go's SysProcAttr.Credential). Requires root to actually
// present uid 0 over SO_PEERCRED/LOCAL_PEERCRED, so it skips otherwise.
func TestHandleConnection_RootUIDAccepted(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root to exercise a real uid-0 peer credential")
	}
	t.Setenv("EOS_BASE_DIR", t.TempDir())

	serverConn, clientConn := newUnixSocketPair(t)
	logger := discardLogger()
	mgr := &fakeServiceManager{
		getVersionFunc: func() (types.GetVersionResponse, error) {
			return types.GetVersionResponse{Version: "test-version"}, nil
		},
	}
	daemonUID := uint32(os.Getuid()) + 1 // simulates a daemon dropped to a non-root uid

	if err := json.NewEncoder(clientConn).Encode(types.DaemonRequest{Method: types.MethodGetVersion}); err != nil {
		t.Fatalf("client encode: %v", err)
	}

	done := make(chan struct{})
	go func() {
		handleConnection(t.Context(), serverConn, mgr, logger, daemonUID)
		close(done)
	}()

	var resp types.DaemonResponse
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("client decode: %v", err)
	}
	<-done

	if !resp.Success {
		t.Fatalf("expected root to be authorized against a non-root daemon uid, got error: %s", resp.Error)
	}
}

// TestBootPersistedServices_ListError proves a failure listing the service
// catalog is wrapped with context and returned rather than silently
// swallowed — the daemon can't recover persisted services it couldn't list.
func TestBootPersistedServices_ListError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	if err := db.CloseDBConnection(); err != nil {
		t.Fatalf("CloseDBConnection: %v", err)
	}

	logger, buf := capturingLogger()
	if err := bootPersistedServices(t.Context(), mgr, logger); err == nil {
		t.Fatal("expected an error when listing the service catalog fails")
	}
	if !strings.Contains(buf.String(), "getting all service catalog entries") {
		t.Errorf("expected the list failure to be logged, got: %s", buf.String())
	}
}

// TestCatalogEntriesOrEmpty and TestServiceInstancesOrEmpty prove the daemon
// telemetry gauge callbacks degrade to an empty slice (rather than panicking
// or propagating an error the OTel SDK has no way to surface) when their
// underlying query fails.
func TestCatalogEntriesOrEmpty(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if got := catalogEntriesOrEmpty(t.Context(), mgr, testutil.NewTestLogger(t)); got != nil {
		t.Errorf("expected no entries in an empty catalog, got: %+v", got)
	}

	if err := db.CloseDBConnection(); err != nil {
		t.Fatalf("CloseDBConnection: %v", err)
	}
	if got := catalogEntriesOrEmpty(t.Context(), mgr, testutil.NewTestLogger(t)); got != nil {
		t.Errorf("expected nil on query failure, got: %+v", got)
	}
}

func TestServiceInstancesOrEmpty(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if got := serviceInstancesOrEmpty(t.Context(), mgr, testutil.NewTestLogger(t)); got != nil {
		t.Errorf("expected no instances with none registered, got: %+v", got)
	}

	if err := db.CloseDBConnection(); err != nil {
		t.Fatalf("CloseDBConnection: %v", err)
	}
	if got := serviceInstancesOrEmpty(t.Context(), mgr, testutil.NewTestLogger(t)); got != nil {
		t.Errorf("expected nil on query failure, got: %+v", got)
	}
}

// TestDaemonServe proves serve launches both the IPC command listener and the
// health monitor as background goroutines rather than blocking the caller.
func TestDaemonServe(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	d := &daemon{
		listener:    ln,
		ctx:         ctx,
		logger:      testutil.NewTestLogger(t),
		db:          db,
		mgr:         mgr,
		otelHandles: otelx.NoopHandles(),
	}

	d.serve(&config.HealthConfig{}, config.ShutdownConfig{})

	// Dial the listener so handleIncomingCommands's Accept loop actually
	// hands a connection off to handleConnection, not just starts up idle.
	conn, dialErr := net.Dial("tcp", ln.Addr().String())
	if dialErr != nil {
		t.Fatalf("dial: %v", dialErr)
	}
	// handleConnection reads a request off conn and would block on an empty
	// dial; a JSON-decode error response is enough to let the accept-side
	// goroutine finish rather than lingering past the test.
	_ = json.NewEncoder(conn).Encode(types.DaemonRequest{Method: types.MethodGetVersion})
	var resp types.DaemonResponse
	_ = json.NewDecoder(conn).Decode(&resp)
	_ = conn.Close()

	// No further assertion beyond "did not block/panic": serve's job is
	// only to launch the two goroutines and return immediately.
	cancel()
}

// TestStartStandaloneDaemon_UnderSystemd proves the UnderSystemd branch
// recovers persisted services (an empty catalog here, so recover is a
// no-op) before returning, and that the daemon returns cleanly once its
// context is canceled rather than blocking forever.
func TestStartStandaloneDaemon_UnderSystemd(t *testing.T) {
	// A short os.MkdirTemp root, not t.TempDir(): the latter nests under
	// this test's (long) name, and a unix socket path is capped at
	// ~104 bytes — nesting under the test name alone can blow that budget.
	tempDir, err := os.MkdirTemp("", "eos-startdaemon-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	daemonCfg := testutil.NewTestStandaloneDaemonConfig(t, tempDir)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	opts := StandaloneDaemonStartOptions{
		BaseDir:      tempDir,
		UnderSystemd: true,
	}
	if err := StartStandaloneDaemon(ctx, opts, daemonCfg.Standalone, &config.HealthConfig{}, config.ShutdownConfig{}, config.TelemetryConfig{}); err != nil {
		t.Errorf("expected StartStandaloneDaemon to return nil once the context is done, got: %v", err)
	}
}
