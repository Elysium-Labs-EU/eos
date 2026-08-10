//go:build integration

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
)

// requireProcfs skips on platforms without /proc: livePGIDsForMarker below
// reads it directly for an OS-level view of what is actually running,
// independent of anything eos itself reports.
func requireProcfs(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc (linux)")
	}
}

// livePGIDsForMarker scans /proc/*/cmdline for live process-group leaders
// whose command line contains marker, returning the distinct set of
// process-group ids found. It never asks eos anything: this is the same
// information `ps` would report, read straight from the kernel. A launched
// service's leader has Setpgid set, so the leader's own pid equals its pgid.
func livePGIDsForMarker(t *testing.T, marker string) map[int]bool {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("reading /proc: %v", err)
	}
	found := make(map[int]bool)
	for _, e := range entries {
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if readErr != nil {
			continue // process exited between ReadDir and here, or is unreadable
		}
		if !strings.Contains(strings.ReplaceAll(string(raw), "\x00", " "), marker) {
			continue
		}
		if pgid, pgErr := syscall.Getpgid(pid); pgErr == nil {
			found[pgid] = true
		}
	}
	return found
}

// execLocalCmd runs "eos <args...>" in-process against mgr and returns the
// captured combined stdout+stderr and error. A fresh root command is built
// per call since cobra commands carry per-invocation flag state that does
// not reset cleanly across repeated ExecuteContext calls on the same
// instance.
func execLocalCmd(t *testing.T, mgr manager.ServiceManager, args ...string) (out string, err error) {
	t.Helper()
	c := newTestRootCmd(mgr)
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)
	err = c.ExecuteContext(t.Context())
	return buf.String(), err
}

// runLocalInBackground starts "eos run <name>" against mgr in a background
// goroutine and returns a channel carrying its eventual result. Local-mode
// "eos run" now blocks supervising the service in the foreground
// (runSuperviseIfLocal) once a start succeeds, so a synchronous call here
// would never return for this test's still-alive-after-stop scenario; a
// failed start (the duplicate-start attempt this test drives second) still
// returns promptly, since a failed runResolveAndStart never reaches
// runSuperviseIfLocal at all.
func runLocalInBackground(t *testing.T, mgr manager.ServiceManager, name string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		out, err := execLocalCmd(t, mgr, "run", name)
		if err != nil {
			err = fmt.Errorf("%w: %s", err, out)
		}
		done <- err
	}()
	return done
}

// TestAPIRunRefusesDuplicateStartAfterUnknownHistory is the integration
// regression test for the duplicate-instance bug: a graceful stop whose
// target ignores SIGTERM runs out its grace period and leaves the most
// recent process_history row Unknown while the process is still alive.
// Before the fix, lmReconcileHistoryEntry treated Unknown as terminal, so a
// subsequent start sailed past the already-running guard and launched a
// second live instance on top of the first. Every liveness assertion here
// reads /proc directly rather than trusting eos's own JSON output, since
// that reporting is exactly what the underlying defect could make
// misleading.
//
// Driven through the blocking local-mode "eos run" rather than "eos api
// run": apiRefuseLocalStart now refuses every "eos api run" against a local
// manager unconditionally, before it ever reaches history reconciliation, so
// the specific unknown-history scenario is no longer reachable through that
// command at all — its refusal is already covered generically by
// TestAPIRunWithServiceNameRefusesLocalStart and friends in
// api_run_test.go, which do not need this history setup to prove it. The
// duplicate-start guard itself lives one layer down, in
// LocalManager.StartService's call to reconcileStartHistory
// (lmReconcileHistoryEntry) — logic "eos run" reaches identically, so
// driving the regression through it (rather than through a real
// daemon-backed "eos api run", which needs a systemd user bus this
// environment does not reliably provide) keeps the real coverage without a
// daemon dependency.
func TestAPIRunRefusesDuplicateStartAfterUnknownHistory(t *testing.T) {
	requireProcfs(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	const svcName = "unknown-history-duplicate-guard"
	marker := "marker-" + svcName
	// The trap makes SIGTERM a no-op, so a graceful stop is guaranteed to run
	// out its grace period instead of exiting promptly; the trailing shell
	// comment is inert but keeps the exact command line greppable in /proc.
	command := fmt.Sprintf(`trap "" TERM; while true; do sleep 1; done # %s`, marker)
	svcDir := writeServiceDirWithCommand(t, svcName, command)

	if out, err := execLocalCmd(t, mgr, "add", svcDir); err != nil {
		t.Fatalf("eos add: %v\n%s", err, out)
	}

	firstRun := runLocalInBackground(t, mgr, svcName)
	firstPGID := waitForRunningPGID(t, mgr, svcName, 0)
	// Killing the process alone leaves firstRun's goroutine dangling until
	// its own poll notices (up to runSupervisePollInterval later); draining
	// the channel here too is what makes this cleanup actually synchronous
	// with the goroutine's exit, not just with the process's — required for
	// goleak (see cmd/main_test.go) to not flag it as still running.
	t.Cleanup(func() {
		_ = syscall.Kill(-firstPGID, syscall.SIGKILL)
		select {
		case <-firstRun:
		case <-time.After(3 * time.Second):
			t.Logf("first eos run did not return within 3s of the cleanup kill")
		}
	})

	if before := livePGIDsForMarker(t, marker); len(before) != 1 || !before[firstPGID] {
		t.Fatalf("expected exactly the started pgid %d alive after the first start, got %v", firstPGID, before)
	}

	stopStart := time.Now()
	stopOut, stopErr := execLocalCmd(t, mgr, "stop", svcName)
	if stopErr == nil {
		t.Fatalf("expected the stop to report a grace-period failure (target ignores SIGTERM), got success")
	}
	if elapsed := time.Since(stopStart); elapsed < 4*time.Second {
		t.Fatalf("expected the stop to run out its ~5s grace period before returning, took %s", elapsed)
	}
	if !errors.Is(stopErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed from the stop, got: %v", stopErr)
	}
	t.Logf("stop error output (grace period exceeded, history row should now be Unknown): %s", stopOut)

	if pgids := livePGIDsForMarker(t, marker); len(pgids) != 1 || !pgids[firstPGID] {
		t.Fatalf("expected the original process to still be the only live instance after the failed stop, got %v", pgids)
	}

	// The duplicate-start attempt: before the fix this succeeded and spawned
	// a second live process group on top of the still-alive original. A
	// failed start returns promptly (it never reaches the blocking
	// supervision loop), so this is a synchronous call, unlike the first.
	secondOut, secondErr := execLocalCmd(t, mgr, "run", svcName)
	if secondErr == nil {
		t.Fatalf("expected eos run to refuse while the original process is still alive, got success")
	}
	if !errors.Is(secondErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed from the refused run, got: %v", secondErr)
	}
	if !strings.Contains(secondOut, "already running") {
		t.Errorf("expected an already-running error, got: %s", secondOut)
	}

	// The first run's own background goroutine is still blocked supervising
	// firstPGID at this point; select on firstRun with no wait so a
	// surprise early return (e.g. it somehow lost track of the process) is
	// caught as a failure instead of silently ignored.
	select {
	case err := <-firstRun:
		t.Fatalf("the first eos run returned unexpectedly (should still be supervising): %v", err)
	default:
	}

	after := livePGIDsForMarker(t, marker)
	if len(after) != 1 {
		t.Fatalf("expected exactly one live process group for the service after the refused duplicate start, got %d: %v", len(after), after)
	}
	if !after[firstPGID] {
		t.Fatalf("expected the original pgid %d to still be the sole live process group, got %v", firstPGID, after)
	}
}
