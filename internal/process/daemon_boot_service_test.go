package process

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// writeBootTestService writes a service.yaml for cfg under tempDir/cfg.Name
// and returns a ServiceCatalogEntry pointing at it, mirroring
// newTestDaemonWithService's file layout but without starting a daemon —
// bootService is a plain function over *manager.LocalManager, callable
// directly.
func writeBootTestService(t *testing.T, tempDir string, cfg *types.ServiceConfig) types.ServiceCatalogEntry {
	t.Helper()
	dir := filepath.Join(tempDir, cfg.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), data, 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}
	return types.ServiceCatalogEntry{Name: cfg.Name, DirectoryPath: dir, ConfigFileName: "service.yaml"}
}

// bootTestService writes cfg to disk and registers it in mgr's catalog, since
// StartService (which bootService calls once its gate clears) requires the
// service to already be a registered catalog entry, not merely a file on disk.
func bootTestService(t *testing.T, mgr *manager.LocalManager, tempDir string, cfg *types.ServiceConfig) types.ServiceCatalogEntry {
	t.Helper()
	entry := writeBootTestService(t, tempDir, cfg)
	catalogEntry, err := manager.NewServiceCatalogEntry(entry.Name, entry.DirectoryPath, entry.ConfigFileName)
	if err != nil {
		t.Fatalf("NewServiceCatalogEntry: %v", err)
	}
	if err := mgr.AddServiceCatalogEntry(t.Context(), catalogEntry); err != nil {
		t.Fatalf("AddServiceCatalogEntry: %v", err)
	}
	return entry
}

// TestBootService_NoDependencies proves the baseline (pre-depends_on) boot
// path: a service with no depends_on starts immediately, exactly as before
// ordering existed.
func TestBootService_NoDependencies(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := bootTestService(t, mgr, tempDir, &types.ServiceConfig{Name: "solo", Command: "/bin/sleep 5"})

	bootService(t.Context(), mgr, testutil.NewTestLogger(t), &entry)

	if _, err := mgr.GetServiceInstance(t.Context(), "solo"); err != nil {
		t.Errorf("expected 'solo' to have started, got: %v", err)
	}
}

// TestBootService_WaitsThenStarts proves bootService gates on depends_on
// before starting: the dependent only starts once its dependency's process
// history reports Running.
func TestBootService_WaitsThenStarts(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 999002, 1, "proxy", types.ProcessStateRunning); err != nil {
		t.Fatalf("seed dependency history: %v", err)
	}

	entry := bootTestService(t, mgr, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/sleep 5", DependsOn: []string{"proxy"}, MaxWait: "2s",
	})

	bootService(t.Context(), mgr, testutil.NewTestLogger(t), &entry)

	if _, err := mgr.GetServiceInstance(t.Context(), "web"); err != nil {
		t.Errorf("expected 'web' to have started once its dependency was ready, got: %v", err)
	}
	if _, waiting, err := mgr.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Errorf("expected the wait to be cleared once bootService returns, waiting=%v err=%v", waiting, err)
	}
}

// TestBootService_UnmetDependencyNeverStarts proves the "log and continue"
// contract: an unmet dependency stops this one service from starting (after
// its max_wait ceiling), without bootService itself erroring or panicking —
// bootPersistedServices relies on this so one bad service can't abort boot
// for the rest.
func TestBootService_UnmetDependencyNeverStarts(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := writeBootTestService(t, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/sleep 5", DependsOn: []string{"never-started"}, MaxWait: "150ms",
	})

	bootService(t.Context(), mgr, testutil.NewTestLogger(t), &entry)

	if _, err := mgr.GetServiceInstance(t.Context(), "web"); !errors.Is(err, manager.ErrServiceNotRunning) {
		t.Errorf("expected 'web' to never have started, got err: %v", err)
	}
	if _, waiting, err := mgr.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Errorf("expected the wait to be cleared even on max_wait failure, waiting=%v err=%v", waiting, err)
	}
}

// TestBootService_MaxWaitParseError proves a malformed max_wait is logged and
// skipped rather than starting the service anyway.
func TestBootService_MaxWaitParseError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := writeBootTestService(t, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/sleep 5", DependsOn: []string{"proxy"}, MaxWait: "not-a-duration",
	})

	bootService(t.Context(), mgr, testutil.NewTestLogger(t), &entry)

	if _, err := mgr.GetServiceInstance(t.Context(), "web"); !errors.Is(err, manager.ErrServiceNotRunning) {
		t.Errorf("expected 'web' to never have started with a malformed max_wait, got err: %v", err)
	}
}

// TestBootService_ConfigLoadError proves a missing/unreadable service.yaml is
// logged and skipped rather than panicking.
func TestBootService_ConfigLoadError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := types.ServiceCatalogEntry{Name: "missing", DirectoryPath: tempDir, ConfigFileName: "does-not-exist.yaml"}

	bootService(t.Context(), mgr, testutil.NewTestLogger(t), &entry)
}

// TestBootPersistedServices_SkipsDisabled proves the issue #172 fix: a
// service stopped by hand (Enabled=false) is left alone on daemon boot, while
// a sibling that was never stopped starts normally in the same pass.
func TestBootPersistedServices_SkipsDisabled(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	bootTestService(t, mgr, tempDir, &types.ServiceConfig{Name: "kept-running", Command: "/bin/sleep 5"})
	bootTestService(t, mgr, tempDir, &types.ServiceConfig{Name: "stopped-by-hand", Command: "/bin/sleep 5"})

	if err := db.SetServiceCatalogEnabled(t.Context(), "stopped-by-hand", false); err != nil {
		t.Fatalf("SetServiceCatalogEnabled: %v", err)
	}

	if err := bootPersistedServices(t.Context(), mgr, testutil.NewTestLogger(t)); err != nil {
		t.Fatalf("bootPersistedServices: %v", err)
	}

	if _, err := mgr.GetServiceInstance(t.Context(), "kept-running"); err != nil {
		t.Errorf("expected 'kept-running' to have started, got: %v", err)
	}
	if _, err := mgr.GetServiceInstance(t.Context(), "stopped-by-hand"); !errors.Is(err, manager.ErrServiceNotRunning) {
		t.Errorf("expected 'stopped-by-hand' to stay stopped, got err: %v", err)
	}
}
