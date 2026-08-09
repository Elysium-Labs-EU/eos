//go:build integration

package cmd

import (
	"bytes"
	"encoding/json"
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

// execAPICmd runs "eos api <args...>" in-process against mgr and returns the
// captured stdout, stderr, and error. A fresh root command is built per call
// (matching the rest of this package's api_*_test.go files) since cobra
// commands carry per-invocation flag state that does not reset cleanly across
// repeated ExecuteContext calls on the same instance.
func execAPICmd(t *testing.T, mgr manager.ServiceManager, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs(append([]string{"api"}, args...))
	err = c.ExecuteContext(t.Context())
	return outBuf.String(), errBuf.String(), err
}

// TestAPIRunRefusesDuplicateStartAfterUnknownHistory is the integration
// regression test for the duplicate-instance bug: a graceful stop whose
// target ignores SIGTERM runs out its grace period and leaves the most
// recent process_history row Unknown while the process is still alive.
// Before the fix, lmReconcileHistoryEntry treated Unknown as terminal, so a
// subsequent "eos api run" sailed past the already-running guard and
// launched a second live instance on top of the first. Every liveness
// assertion here reads /proc directly rather than trusting eos's own JSON
// output, since that reporting is exactly what the underlying defect could
// make misleading.
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

	if _, stderr, err := execAPICmd(t, mgr, "add", svcDir); err != nil {
		t.Fatalf("eos api add: %v\n%s", err, stderr)
	}

	firstOut, firstErr, err := execAPICmd(t, mgr, "run", svcName)
	if err != nil {
		t.Fatalf("eos api run (first start): %v\n%s", err, firstErr)
	}
	var first struct {
		PGID int `json:"pgid"`
	}
	if jsonErr := json.Unmarshal([]byte(firstOut), &first); jsonErr != nil {
		t.Fatalf("parsing run output %q: %v", firstOut, jsonErr)
	}
	t.Cleanup(func() { _ = syscall.Kill(-first.PGID, syscall.SIGKILL) })

	if before := livePGIDsForMarker(t, marker); len(before) != 1 || !before[first.PGID] {
		t.Fatalf("expected exactly the started pgid %d alive after the first start, got %v", first.PGID, before)
	}

	stopStart := time.Now()
	_, stopErrOut, stopErr := execAPICmd(t, mgr, "stop", svcName)
	if stopErr == nil {
		t.Fatalf("expected the stop to report a grace-period failure (target ignores SIGTERM), got success")
	}
	if elapsed := time.Since(stopStart); elapsed < 4*time.Second {
		t.Fatalf("expected the stop to run out its ~5s grace period before returning, took %s", elapsed)
	}
	if !errors.Is(stopErr, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed from the stop, got: %v", stopErr)
	}
	t.Logf("stop error output (grace period exceeded, history row should now be Unknown): %s", stopErrOut)

	if pgids := livePGIDsForMarker(t, marker); len(pgids) != 1 || !pgids[first.PGID] {
		t.Fatalf("expected the original process to still be the only live instance after the failed stop, got %v", pgids)
	}

	// The duplicate-start attempt: before the fix this succeeded and spawned
	// a second live process group on top of the still-alive original.
	_, secondErrOut, secondErr := execAPICmd(t, mgr, "run", svcName)
	if secondErr == nil {
		t.Fatalf("expected eos api run to refuse while the original process is still alive, got success")
	}
	if !errors.Is(secondErr, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed from the refused run, got: %v", secondErr)
	}
	if !strings.Contains(secondErrOut, "already running") {
		t.Errorf("expected an already-running error, got: %s", secondErrOut)
	}

	after := livePGIDsForMarker(t, marker)
	if len(after) != 1 {
		t.Fatalf("expected exactly one live process group for the service after the refused duplicate start, got %d: %v", len(after), after)
	}
	if !after[first.PGID] {
		t.Fatalf("expected the original pgid %d to still be the sole live process group, got %v", first.PGID, after)
	}
}
