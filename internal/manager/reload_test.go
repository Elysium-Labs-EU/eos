package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// registerLongRunningService writes a service.yaml whose command is a plain
// long sleep (no port, no runtime binary check) and registers it, returning the
// manager. It is enough to exercise the reload orchestration — the launch,
// readiness gate, and single-instance drain — without needing a real network
// listener; the OS-level SO_REUSEPORT proof lives in internal/process.
func registerLongRunningService(t *testing.T, name string) (*LocalManager, string) {
	t.Helper()
	return registerServiceWithCommand(t, name, "sleep 300"), name
}

// registerServiceWithCommand registers a service whose command is command and
// returns the manager. It is the general form behind registerLongRunningService,
// letting a test choose a command that (say) traps SIGTERM to exercise the
// force-kill drain path.
func registerServiceWithCommand(t *testing.T, name, command string) *LocalManager {
	t.Helper()
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	// Keep every reload path anchored to the test's temp dir, never a real
	// ~/.eos: the base dir the manager writes under is tempDir, and setting
	// EOS_BASE_DIR guards any helper that would otherwise resolve HOME.
	t.Setenv("EOS_BASE_DIR", tempDir)

	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	registerServiceOnManager(t, mgr, tempDir, name, command)
	return mgr
}

// registerServiceOnManager writes name's service.yaml under baseDir and adds it
// to mgr's catalog. Split from registerServiceWithCommand so a test that needs
// its own manager (e.g. one with a cancelable context) can register against it.
func registerServiceOnManager(t *testing.T, mgr *LocalManager, baseDir, name, command string) {
	t.Helper()
	cfg := &types.ServiceConfig{Name: name, Command: command}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dir := filepath.Join(baseDir, name)
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		t.Fatalf("mkdir: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); writeErr != nil {
		t.Fatalf("write yaml: %v", writeErr)
	}
	entry, err := NewServiceCatalogEntry(name, dir, "service.yaml")
	if err != nil {
		t.Fatalf("catalog entry: %v", err)
	}
	if err := mgr.AddServiceCatalogEntry(entry); err != nil {
		t.Fatalf("add catalog entry: %v", err)
	}
}

// killGroup best-effort force-kills a process group during test cleanup.
func killGroup(pgid int) {
	if pgid > 1 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

// waitGone polls up to two seconds until the process group is no longer alive,
// so a test asserting "the old instance was drained" doesn't race the SIGTERM
// delivery.
func waitGone(pgid int) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !procutil.IsAlive(pgid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !procutil.IsAlive(pgid)
}

func alwaysReady(context.Context, int, int64, int) bool { return true }
func neverReady(context.Context, int, int64, int) bool  { return false }

// TestReloadServiceCutover proves the happy path: a new instance starts, the
// readiness gate passes, and only then is the old instance drained — old dead,
// new alive, and the reported PGIDs match.
func TestReloadServiceCutover(t *testing.T) {
	mgr, name := registerLongRunningService(t, "reload-cutover")

	oldPGID, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { killGroup(oldPGID) })

	result, err := mgr.ReloadService(name, alwaysReady, ReloadConfig{
		GracePeriod:      2 * time.Second,
		TickerPeriod:     20 * time.Millisecond,
		ReadinessTimeout: 2 * time.Second,
		ProbeInterval:    20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ReloadService: %v", err)
	}
	t.Cleanup(func() { killGroup(result.NewPGID) })

	if result.OldPGID != oldPGID {
		t.Errorf("OldPGID = %d, want %d", result.OldPGID, oldPGID)
	}
	if result.NewPGID == 0 || result.NewPGID == oldPGID {
		t.Fatalf("NewPGID = %d, want a fresh non-zero pgid", result.NewPGID)
	}
	if !waitGone(oldPGID) {
		t.Errorf("old instance pgid %d should have been drained", oldPGID)
	}
	if !procutil.IsAlive(result.NewPGID) {
		t.Errorf("new instance pgid %d should still be running", result.NewPGID)
	}
}

// TestReloadServiceAbortKeepsOld proves the safety property: when the new
// instance never passes the readiness gate, reload errors, kills the new
// instance, and leaves the old one serving.
func TestReloadServiceAbortKeepsOld(t *testing.T) {
	mgr, name := registerLongRunningService(t, "reload-abort")

	oldPGID, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { killGroup(oldPGID) })

	result, err := mgr.ReloadService(name, neverReady, ReloadConfig{
		GracePeriod:      2 * time.Second,
		TickerPeriod:     20 * time.Millisecond,
		ReadinessTimeout: 300 * time.Millisecond,
		ProbeInterval:    30 * time.Millisecond,
	})
	if !errors.Is(err, ErrReloadNotReady) {
		t.Fatalf("ReloadService err = %v, want ErrReloadNotReady", err)
	}
	if result.NewPGID != 0 {
		t.Cleanup(func() { killGroup(result.NewPGID) })
		if !waitGone(result.NewPGID) {
			t.Errorf("unready new instance pgid %d should have been killed", result.NewPGID)
		}
	}
	if !procutil.IsAlive(oldPGID) {
		t.Errorf("old instance pgid %d must keep serving after an aborted reload", oldPGID)
	}

	// The aborted instance's history row must be gone, not left Failed: a Failed
	// most-recent row would make the health monitor restart the service and kill
	// the old instance the abort just protected. Most-recent must be the old,
	// still-Running instance.
	recent, err := mgr.GetMostRecentProcessHistoryEntry(name)
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	if recent.PGID != oldPGID {
		t.Errorf("most-recent history pgid = %d, want the surviving old instance %d", recent.PGID, oldPGID)
	}
	if recent.State != types.ProcessStateRunning && recent.State != types.ProcessStateStarting {
		t.Errorf("old instance history state = %s, want Running/Starting (monitor must not see it as failed)", recent.State)
	}
}

// TestReloadServiceReadinessTimeoutBelowProbeInterval proves awaitReady honors
// the readiness deadline independent of the probe tick: with ReadinessTimeout
// far below ProbeInterval it must give up at roughly ReadinessTimeout rather
// than blocking a full ProbeInterval for the first tick to arrive.
func TestReloadServiceReadinessTimeoutBelowProbeInterval(t *testing.T) {
	mgr, name := registerLongRunningService(t, "reload-fast-timeout")

	oldPGID, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { killGroup(oldPGID) })

	const probeInterval = 2 * time.Second
	start := time.Now()
	result, err := mgr.ReloadService(name, neverReady, ReloadConfig{
		GracePeriod:      2 * time.Second,
		TickerPeriod:     20 * time.Millisecond,
		ReadinessTimeout: 20 * time.Millisecond,
		ProbeInterval:    probeInterval,
	})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrReloadNotReady) {
		t.Fatalf("ReloadService err = %v, want ErrReloadNotReady", err)
	}
	if elapsed >= probeInterval {
		t.Errorf("reload took %s, want it to time out well before one ProbeInterval (%s)", elapsed, probeInterval)
	}
	if result.NewPGID != 0 {
		t.Cleanup(func() { killGroup(result.NewPGID) })
	}
	if !procutil.IsAlive(oldPGID) {
		t.Errorf("old instance pgid %d must keep serving after the timed-out reload", oldPGID)
	}
}

// sigtermIgnoringCommand builds a command that installs an ignore-SIGTERM trap,
// signals readiness by creating markerPath, then keeps a live process group
// across a group-wide SIGTERM (respawning the inner sleep the signal kills). The
// marker exists only after the trap is installed, so a caller that waits for it
// can drain without racing the trap setup; without that the SIGTERM would land
// on a default-disposition shell and just kill it.
func sigtermIgnoringCommand(markerPath string) string {
	return "trap '' TERM; : > " + markerPath + "; while :; do sleep 1; done"
}

// waitForFile polls until path exists or the deadline passes.
func waitForFile(path string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := os.Stat(path)
	return err == nil
}

// TestDrainInstanceAlreadyGone proves drainInstance records a process group that
// already exited as Stopped without erroring on the missing process.
func TestDrainInstanceAlreadyGone(t *testing.T) {
	mgr, name := registerLongRunningService(t, "drain-gone")

	pgid, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	killGroup(pgid)
	if !waitGone(pgid) {
		t.Fatalf("process group %d should be gone before draining", pgid)
	}

	if drainErr := mgr.drainInstance(name, pgid, time.Second, 20*time.Millisecond); drainErr != nil {
		t.Fatalf("drainInstance: %v", drainErr)
	}
	recent, err := mgr.GetMostRecentProcessHistoryEntry(name)
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	if recent.State != types.ProcessStateStopped {
		t.Errorf("state = %s, want Stopped", recent.State)
	}
}

// TestDrainInstanceForceKillsOnGraceTimeout proves the drain escalates to
// SIGKILL when the instance ignores SIGTERM past the grace period, so the
// cutover can't stall on an unresponsive service.
func TestDrainInstanceForceKillsOnGraceTimeout(t *testing.T) {
	name := "drain-timeout"
	marker := filepath.Join(t.TempDir(), "trap-ready")
	mgr := registerServiceWithCommand(t, name, sigtermIgnoringCommand(marker))

	pgid, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { killGroup(pgid) })
	if !waitForFile(marker, 2*time.Second) {
		t.Fatalf("service never installed its SIGTERM trap")
	}

	const gracePeriod = 150 * time.Millisecond
	start := time.Now()
	if drainErr := mgr.drainInstance(name, pgid, gracePeriod, 20*time.Millisecond); drainErr != nil {
		t.Fatalf("drainInstance: %v", drainErr)
	}
	if time.Since(start) < gracePeriod {
		t.Errorf("drain returned in %s, want it to wait at least the grace period %s", time.Since(start), gracePeriod)
	}
	if !waitGone(pgid) {
		t.Errorf("process group %d should have been force-killed after the grace period", pgid)
	}
	recent, err := mgr.GetMostRecentProcessHistoryEntry(name)
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	if recent.State != types.ProcessStateStopped {
		t.Errorf("state = %s, want Stopped", recent.State)
	}
}

// TestDrainInstanceCanceledLeavesRow proves that when the manager context is
// canceled mid-drain (daemon shutting down), drainInstance bails out without
// marking the row Stopped, leaving it for startup reconciliation.
func TestDrainInstanceCanceledLeavesRow(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	t.Setenv("EOS_BASE_DIR", tempDir)

	ctx, cancel := context.WithCancel(t.Context())
	mgr := NewLocalManager(db, tempDir, ctx, testutil.NewTestLogger(t))

	name := "drain-canceled"
	marker := filepath.Join(tempDir, name, "trap-ready")
	registerServiceOnManager(t, mgr, tempDir, name, sigtermIgnoringCommand(marker))

	pgid, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { killGroup(pgid) })
	if !waitForFile(marker, 2*time.Second) {
		t.Fatalf("service never installed its SIGTERM trap")
	}

	// Cancel while the drain is still waiting for the SIGTERM-ignoring process
	// to exit; the grace period is long enough that cancellation, not a timeout,
	// ends the wait.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	if drainErr := mgr.drainInstance(name, pgid, 5*time.Second, 20*time.Millisecond); drainErr != nil {
		t.Fatalf("drainInstance: %v", drainErr)
	}

	// Query with a live context; the manager's own context is now canceled.
	entry, err := db.GetProcessHistoryEntryByPGID(context.Background(), pgid)
	if err != nil {
		t.Fatalf("GetProcessHistoryEntryByPGID: %v", err)
	}
	if entry.State == types.ProcessStateStopped {
		t.Errorf("state = Stopped, want the row left intact for startup reconciliation")
	}
}

// TestReloadServiceNotRunning proves reload refuses a cold service instead of
// starting one — it swaps a live instance, it does not boot one.
func TestReloadServiceNotRunning(t *testing.T) {
	mgr, name := registerLongRunningService(t, "reload-cold")

	_, err := mgr.ReloadService(name, alwaysReady, ReloadConfig{
		GracePeriod:      time.Second,
		TickerPeriod:     20 * time.Millisecond,
		ReadinessTimeout: time.Second,
		ProbeInterval:    20 * time.Millisecond,
	})
	if !errors.Is(err, ErrServiceNotRunning) {
		t.Fatalf("ReloadService err = %v, want ErrServiceNotRunning", err)
	}
}

// TestReloadServiceNilProbe proves a missing readiness gate is fatal, not a
// silent blind cutover: without a probe there is no safe moment to drain the
// old instance, so reload must fail and leave it running.
func TestReloadServiceNilProbe(t *testing.T) {
	mgr, name := registerLongRunningService(t, "reload-nilprobe")

	oldPGID, err := mgr.StartService(name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { killGroup(oldPGID) })

	result, err := mgr.ReloadService(name, nil, ReloadConfig{
		GracePeriod:      time.Second,
		TickerPeriod:     20 * time.Millisecond,
		ReadinessTimeout: 200 * time.Millisecond,
		ProbeInterval:    20 * time.Millisecond,
	})
	if !errors.Is(err, ErrReloadNotReady) {
		t.Fatalf("ReloadService err = %v, want ErrReloadNotReady", err)
	}
	if result.NewPGID != 0 {
		t.Cleanup(func() { killGroup(result.NewPGID) })
	}
	if !procutil.IsAlive(oldPGID) {
		t.Errorf("old instance pgid %d must keep serving when no probe is configured", oldPGID)
	}
}
