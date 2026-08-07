package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// writeGateTestService writes a service.yaml for a given config directly (not
// via testutil.NewTestServiceConfigFile, which has no depends_on/max_wait
// knobs) and returns a ServiceCatalogEntry pointing at it. gateDependencies
// only needs the on-disk file plus this entry — it doesn't require the
// service to be registered in the catalog DB.
func writeGateTestService(t *testing.T, tempDir string, cfg *types.ServiceConfig) types.ServiceCatalogEntry {
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

func newGateTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetOut(&bytes.Buffer{})
	return c
}

func TestGateDependencies_NoDeps(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := writeGateTestService(t, tempDir, &types.ServiceConfig{Name: "solo", Command: "/bin/true"})

	if err := gateDependencies(t.Context(), newGateTestCmd(), mgr, &entry); err != nil {
		t.Fatalf("expected no error for a service with no depends_on, got %v", err)
	}
}

func TestGateDependencies_DependencyReady(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, err := db.RegisterProcessHistoryEntry(t.Context(), 999001, 1, "proxy", types.ProcessStateRunning); err != nil {
		t.Fatalf("seed dependency history: %v", err)
	}

	entry := writeGateTestService(t, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/true", DependsOn: []string{"proxy"}, MaxWait: "2s",
	})

	var outBuf bytes.Buffer
	cmd := newGateTestCmd()
	cmd.SetOut(&outBuf)

	if err := gateDependencies(t.Context(), cmd, mgr, &entry); err != nil {
		t.Fatalf("expected no error once dependency is ready, got %v", err)
	}
	if !strings.Contains(outBuf.String(), "proxy") {
		t.Errorf("expected the dependency name in the printed wait message, got: %s", outBuf.String())
	}

	// RecordDependencyWait must have cleared the mark once gateDependencies
	// returned — a stuck "waiting" would misreport eos status forever.
	if _, waiting, err := mgr.GetDependencyWaitStatus(t.Context(), "web"); err != nil || waiting {
		t.Errorf("expected the wait to be cleared after gateDependencies returns, waiting=%v err=%v", waiting, err)
	}
}

func TestGateDependencies_MaxWaitFailsLoud(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := writeGateTestService(t, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/true", DependsOn: []string{"never-started"}, MaxWait: "150ms",
	})

	err := gateDependencies(t.Context(), newGateTestCmd(), mgr, &entry)
	if err == nil {
		t.Fatal("expected an error once max_wait elapses with an unmet dependency")
	}
	if !strings.Contains(err.Error(), "never-started") {
		t.Errorf("expected the unmet dependency named in the error, got: %v", err)
	}

	// Even on the failure path, the wait mark must be cleared, not left stuck.
	if _, waiting, getErr := mgr.GetDependencyWaitStatus(t.Context(), "web"); getErr != nil || waiting {
		t.Errorf("expected the wait to be cleared after gateDependencies fails, waiting=%v err=%v", waiting, getErr)
	}
}

// TestGateDependencies_SelfDependencyFailsLoud documents that there is no cycle
// detection: a service listing itself in depends_on can never satisfy its own
// dependency, so the only guard is the finite max_wait. It must fail loud after
// max_wait rather than hang forever, and clear its wait mark afterwards.
func TestGateDependencies_SelfDependencyFailsLoud(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := writeGateTestService(t, tempDir, &types.ServiceConfig{
		Name: "loop", Command: "/bin/true", DependsOn: []string{"loop"}, MaxWait: "150ms",
	})

	// Run in a goroutine so a regression that reintroduces an unbounded wait
	// surfaces as a test timeout here instead of hanging the whole suite.
	done := make(chan error, 1)
	go func() {
		done <- gateDependencies(t.Context(), newGateTestCmd(), mgr, &entry)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error once max_wait elapses with a self-dependency")
		}
		if !strings.Contains(err.Error(), "loop") {
			t.Errorf("expected the unmet self-dependency named in the error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateDependencies did not return within the bound — a self-dependency must fail loud, not hang")
	}

	// The wait mark must be cleared even on the failure path.
	if _, waiting, getErr := mgr.GetDependencyWaitStatus(t.Context(), "loop"); getErr != nil || waiting {
		t.Errorf("expected the wait to be cleared after gateDependencies fails, waiting=%v err=%v", waiting, getErr)
	}
}

// TestGateDependencies_CycleFailsLoudNoHang documents the same for a 2-node
// cycle (web depends_on api, api depends_on web). Gating web, whose dependency
// api never starts, must fail loud at max_wait rather than deadlock.
func TestGateDependencies_CycleFailsLoudNoHang(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	writeGateTestService(t, tempDir, &types.ServiceConfig{
		Name: "api", Command: "/bin/true", DependsOn: []string{"web"}, MaxWait: "150ms",
	})
	web := writeGateTestService(t, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/true", DependsOn: []string{"api"}, MaxWait: "150ms",
	})

	done := make(chan error, 1)
	go func() {
		done <- gateDependencies(t.Context(), newGateTestCmd(), mgr, &web)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error once max_wait elapses with an unmet cyclic dependency")
		}
		if !strings.Contains(err.Error(), "api") {
			t.Errorf("expected the unmet dependency named in the error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateDependencies did not return within the bound — a dependency cycle must fail loud, not hang")
	}

	// The wait mark must be cleared even on the failure path.
	if _, waiting, getErr := mgr.GetDependencyWaitStatus(t.Context(), "web"); getErr != nil || waiting {
		t.Errorf("expected the wait to be cleared after gateDependencies fails, waiting=%v err=%v", waiting, getErr)
	}
}

func TestGateDependencies_ConfigLoadError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := types.ServiceCatalogEntry{Name: "missing", DirectoryPath: tempDir, ConfigFileName: "does-not-exist.yaml"}

	if err := gateDependencies(t.Context(), newGateTestCmd(), mgr, &entry); err == nil {
		t.Fatal("expected an error when the service config can't be loaded")
	}
}

func TestGateDependencies_MaxWaitParseError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	entry := writeGateTestService(t, tempDir, &types.ServiceConfig{
		Name: "web", Command: "/bin/true", DependsOn: []string{"proxy"}, MaxWait: "not-a-duration",
	})

	if err := gateDependencies(t.Context(), newGateTestCmd(), mgr, &entry); err == nil {
		t.Fatal("expected an error for a malformed max_wait")
	}
}
