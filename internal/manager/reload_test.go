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
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	// Keep every reload path anchored to the test's temp dir, never a real
	// ~/.eos: the base dir the manager writes under is tempDir, and setting
	// EOS_BASE_DIR guards any helper that would otherwise resolve HOME.
	t.Setenv("EOS_BASE_DIR", tempDir)

	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	cfg := &types.ServiceConfig{Name: name, Command: "sleep 300"}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dir := filepath.Join(tempDir, name)
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
	return mgr, name
}

// killGroup best-effort force-kills a process group during test cleanup.
func killGroup(pgid int) {
	if pgid > 1 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

// waitGone polls until the process group is no longer alive or the deadline
// passes, so a test asserting "the old instance was drained" doesn't race the
// SIGTERM delivery.
func waitGone(pgid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
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
	if !waitGone(oldPGID, 2*time.Second) {
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
		if !waitGone(result.NewPGID, 2*time.Second) {
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
