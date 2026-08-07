package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

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

// TestReloadService_LiveMonitorCrashOnStart proves the abort-safety guarantee
// holds against a concurrently-running HealthMonitor — the primary failure
// scenario the reload path exists for. The incoming instance genuinely crashes
// on start (exits non-zero the instant it launches, not via a faked probe). A
// live monitor ticks fast enough to act several times inside the reload window;
// without the reload suspension it would mark the crashed incoming instance
// Failed and queue a RestartService that stop-then-starts the service, killing
// the surviving old instance the moment reload releases the lock. With the
// suspension, reload aborts and the original process group keeps serving,
// untouched, with no socket gap.
func TestReloadService_LiveMonitorCrashOnStart(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	// Anchor every reload/launch path to the test temp dir, never a real
	// ~/.eos: EOS_BASE_DIR guards any helper that would otherwise resolve HOME.
	t.Setenv("EOS_BASE_DIR", tempDir)

	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	name := "reload-live-monitor"
	dir := filepath.Join(tempDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// The command runs via /bin/sh -c. The first launch (the old instance, via
	// StartService) finds no marker, drops one, and execs a long sleep — staying
	// alive. The reload's incoming launch finds the marker and exits 1
	// immediately: a real crash-on-start. The marker path is absolute so the
	// check does not depend on the launch cwd.
	marker := filepath.Join(dir, "started.marker")
	command := "if [ -e " + marker + " ]; then exit 1; fi; touch " + marker + "; exec sleep 300"
	cfg := &types.ServiceConfig{Name: name, Command: command}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); writeErr != nil {
		t.Fatalf("write yaml: %v", writeErr)
	}
	entry, err := manager.NewServiceCatalogEntry(name, dir, "service.yaml")
	if err != nil {
		t.Fatalf("catalog entry: %v", err)
	}
	if addErr := mgr.AddServiceCatalogEntry(t.Context(), entry); addErr != nil {
		t.Fatalf("add catalog entry: %v", addErr)
	}

	oldPGID, err := mgr.StartService(t.Context(), name)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-oldPGID, syscall.SIGKILL) })

	// StartService returns the instant the process forks, before its shell has
	// necessarily run `touch`. Wait until the marker actually exists so the
	// reload's incoming launch is guaranteed to see it and crash; otherwise it
	// races the old instance's touch and sleeps alongside it.
	if !waitForFile(marker, 2*time.Second) {
		t.Fatalf("old instance never created marker %s", marker)
	}

	// A live HealthMonitor ticking well inside the reload window, so it gets many
	// chances to (wrongly) act on the crashed incoming instance during the
	// cutover.
	healthConfig := newTestHealthConfig(t, WithCheckInterval(15*time.Millisecond))
	shutdownConfig := newTestShutdownConfig(t, WithGracePeriod(time.Second))
	hm := NewHealthMonitor(mgr, db, testutil.NewTestLogger(t), healthConfig, *shutdownConfig, otelx.NoopHandles())

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { hm.Start(ctx) })
	t.Cleanup(func() { cancel(); wg.Wait() })

	// The service declares no port, so ProbeReady gates on liveness alone: the
	// crashed incoming instance never passes, so reload times out and aborts.
	result, err := mgr.ReloadService(name, ProbeReady, manager.ReloadConfig{
		GracePeriod:      time.Second,
		TickerPeriod:     20 * time.Millisecond,
		ReadinessTimeout: 800 * time.Millisecond,
		ProbeInterval:    30 * time.Millisecond,
	})
	if !errors.Is(err, manager.ErrReloadNotReady) {
		t.Fatalf("ReloadService err = %v, want ErrReloadNotReady", err)
	}
	if result.NewPGID != 0 {
		t.Cleanup(func() { _ = syscall.Kill(-result.NewPGID, syscall.SIGKILL) })
	}

	// The whole point: the surviving old instance was never bounced. Had the
	// monitor restarted the service, RestartService would have stopped oldPGID
	// and started a fresh group, so oldPGID would be dead here.
	if !procutil.IsAlive(oldPGID) {
		t.Fatalf("old instance pgid %d was bounced during/after the reload abort; it must keep serving", oldPGID)
	}

	// Let the monitor run several more ticks now that reload has released the
	// lock: with the incoming row deleted, the old, non-failed instance is
	// most-recent again, so no restart should fire.
	time.Sleep(150 * time.Millisecond)
	if !procutil.IsAlive(oldPGID) {
		t.Fatalf("old instance pgid %d was bounced by the monitor after the reload returned", oldPGID)
	}

	recent, err := mgr.GetMostRecentProcessHistoryEntry(t.Context(), name)
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	if recent.PGID != oldPGID {
		t.Errorf("most-recent history pgid = %d, want surviving old instance %d", recent.PGID, oldPGID)
	}
	if recent.State != types.ProcessStateRunning && recent.State != types.ProcessStateStarting {
		t.Errorf("old instance state = %s, want Running/Starting (monitor must not see it as failed)", recent.State)
	}
}
