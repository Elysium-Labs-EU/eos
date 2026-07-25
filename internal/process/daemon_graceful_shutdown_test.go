package process

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// newTestDaemonWithService builds a real standalone daemon with the given
// shutdown grace period, registers and starts a service running command, and
// returns the daemon plus its own t.Context(). Callers get a live process
// group actually running under the daemon's LocalManager (real osExecutor,
// not a fake), matching how the daemon behaves in production.
func newTestDaemonWithService(t *testing.T, gracePeriod time.Duration, serviceName, command string) *daemon {
	t.Helper()
	sockDir := shortTempDir(t)
	_, _, dbDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	standalone := daemonInitCfg(sockDir)
	shutdownConfig := config.ShutdownConfig{GracePeriod: gracePeriod}

	d, err := newStandaloneDaemon(t.Context(), false /* logToFileAndConsole */, false /* verbose */, dbDir, standalone, shutdownConfig, config.TelemetryConfig{})
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}

	serviceDir := t.TempDir()
	serviceConfig := &types.ServiceConfig{Name: serviceName, Command: command}
	yamlData, err := yaml.Marshal(serviceConfig)
	if err != nil {
		t.Fatalf("marshal service config: %v", err)
	}
	if writeErr := os.WriteFile(filepath.Join(serviceDir, "service.yaml"), yamlData, 0644); writeErr != nil {
		t.Fatalf("writing service.yaml: %v", writeErr)
	}

	catalogEntry, err := manager.NewServiceCatalogEntry(serviceName, serviceDir, "service.yaml")
	if err != nil {
		t.Fatalf("NewServiceCatalogEntry: %v", err)
	}
	if addErr := d.mgr.AddServiceCatalogEntry(catalogEntry); addErr != nil {
		t.Fatalf("AddServiceCatalogEntry: %v", addErr)
	}
	if _, startErr := d.mgr.StartService(serviceName); startErr != nil {
		t.Fatalf("StartService: %v", startErr)
	}

	// Give the shell a moment to install its TERM trap and reach the wait
	// loop before a test signals it.
	time.Sleep(200 * time.Millisecond)
	return d
}

// TestDaemonShutdown_GracefullyStopsRunningServices reproduces issue #93: a
// standalone daemon's shutdown used to cancel its top-level context before
// ever signaling registered services, and exec.CommandContext's default
// cancellation policy is to SIGKILL immediately — so a service never got the
// chance to catch SIGTERM and clean up, no matter how large
// ShutdownGracePeriod was configured. This starts a real service whose
// command traps SIGTERM and writes a marker file, calls (*daemon).shutdown,
// and asserts the marker was written — proving SIGTERM was delivered and
// waited on rather than the process being hard-killed out from under it.
func TestDaemonShutdown_GracefullyStopsRunningServices(t *testing.T) {
	serviceDir := t.TempDir()
	markerPath := filepath.Join(serviceDir, "trapfile.txt")
	gracePeriod := 3 * time.Second

	d := newTestDaemonWithService(t, gracePeriod, "trapper", fmt.Sprintf(
		`trap 'echo GOT_TERM >> %q; exit 0' TERM; echo READY; while true; do sleep 0.1; done`,
		markerPath,
	))

	shutdownStart := time.Now()
	d.shutdown(t.Context())
	elapsed := time.Since(shutdownStart)

	marker, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatalf("reading marker file: %v (service was never signaled before shutdown killed it)", readErr)
	}
	if string(marker) != "GOT_TERM\n" {
		t.Errorf("expected marker file to contain %q, got %q", "GOT_TERM\n", marker)
	}

	if elapsed >= gracePeriod {
		t.Errorf("shutdown took %s, expected well under the %s grace period since the service exits promptly on SIGTERM", elapsed, gracePeriod)
	}
}

// TestDaemonShutdown_ForceKillsServiceThatIgnoresSIGTERM proves the other
// half of the contract: a service that traps and ignores TERM must still be
// force-killed once ShutdownGracePeriod elapses, not left running forever —
// shutdown() must actually return and the process must actually be gone.
func TestDaemonShutdown_ForceKillsServiceThatIgnoresSIGTERM(t *testing.T) {
	gracePeriod := 500 * time.Millisecond
	d := newTestDaemonWithService(t, gracePeriod, "stubborn", `trap '' TERM; echo READY; while true; do sleep 0.1; done`)

	history, err := d.mgr.GetMostRecentProcessHistoryEntry("stubborn")
	if err != nil {
		t.Fatalf("GetMostRecentProcessHistoryEntry: %v", err)
	}
	pgid := history.PGID

	shutdownStart := time.Now()
	d.shutdown(t.Context())
	elapsed := time.Since(shutdownStart)

	if elapsed < gracePeriod {
		t.Errorf("shutdown returned after %s, expected at least the %s grace period since the service ignores SIGTERM", elapsed, gracePeriod)
	}

	// Checks the leader PID specifically, not the whole process group
	// (syscall.Kill(-pgid, 0)): WaitDelay's own force-kill fallback
	// (os/exec's internal c.Process.Kill()) only kills the tracked leader
	// process, not its process group, so a short-lived grandchild the shell
	// happened to have spawned (here, the loop's own "sleep 0.1") can still
	// be transiently alive for its own remaining, independent lifetime.
	if err := syscall.Kill(pgid, 0); err == nil {
		t.Errorf("expected leader pid %d to be force-killed after the grace period elapsed, but it is still alive", pgid)
	}
}
