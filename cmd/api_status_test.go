package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// apiStatusFakeManager implements manager.ServiceManager by embedding a nil
// interface and overriding only the methods newAPIStatusCmd's helpers call,
// so error branches can be exercised without a real DB-backed manager.
type apiStatusFakeManager struct {
	manager.ServiceManager
	catalogErr   error
	processErr   error
	instanceErr  error
	processEntry *types.ProcessHistory
	instance     *types.ServiceInstance
	catalog      []types.ServiceCatalogEntry
}

func (f *apiStatusFakeManager) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) {
	return f.catalog, f.catalogErr
}

func (f *apiStatusFakeManager) GetMostRecentProcessHistoryEntry(_ string) (*types.ProcessHistory, error) {
	return f.processEntry, f.processErr
}

func (f *apiStatusFakeManager) GetServiceInstance(_ string) (*types.ServiceInstance, error) {
	return f.instance, f.instanceErr
}

func TestAPIStatusCollectServicesCatalogError(t *testing.T) {
	wantErr := errors.New("boom")
	if _, err := apiStatusCollectServices(&apiStatusFakeManager{catalogErr: wantErr}); err == nil || !strings.Contains(err.Error(), "getting services") {
		t.Errorf("expected wrapped 'getting services' error, got: %v", err)
	}
}

func TestAPIStatusBuildServiceEntryProcessError(t *testing.T) {
	wantErr := errors.New("boom")
	reg := types.ServiceCatalogEntry{Name: "svc"}
	if _, err := apiStatusBuildServiceEntry(&apiStatusFakeManager{processErr: wantErr}, reg); err == nil || !strings.Contains(err.Error(), `getting process for "svc"`) {
		t.Errorf("expected wrapped 'getting process for' error, got: %v", err)
	}
}

func TestAPIStatusBuildServiceEntryInstanceError(t *testing.T) {
	wantErr := errors.New("boom")
	reg := types.ServiceCatalogEntry{Name: "svc"}
	if _, err := apiStatusBuildServiceEntry(&apiStatusFakeManager{instanceErr: wantErr}, reg); err == nil || !strings.Contains(err.Error(), `getting instance for "svc"`) {
		t.Errorf("expected wrapped 'getting instance for' error, got: %v", err)
	}
}

func TestAPIStatusCollectServicesPropagatesEntryError(t *testing.T) {
	wantErr := errors.New("boom")
	catalog := []types.ServiceCatalogEntry{{Name: "svc"}}
	if _, err := apiStatusCollectServices(&apiStatusFakeManager{catalog: catalog, processErr: wantErr}); err == nil || !strings.Contains(err.Error(), `getting process for "svc"`) {
		t.Errorf("expected propagated 'getting process for' error, got: %v", err)
	}
}

func TestAPIStatusEmptyRegistry(t *testing.T) {
	cmd, outBuf, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "status"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStatusResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Services == nil {
		t.Errorf("expected non-nil services slice, got nil")
	}
	if len(result.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(result.Services))
	}
}

func TestAPIStatusWithOneRegisteredService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	// api add only, not api run: service stays registered but never starts.
	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to register: %v\n%s", err, errBuf.String())
	}

	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "status"})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStatusResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	if result.Services[0].Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, result.Services[0].Name)
	}
}

func TestAPIStatusWithRunningService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", "-f", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to start: %v\n%s", err, errBuf.String())
	}

	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "status"})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStatusResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services[0]
	if svc.Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, svc.Name)
	}
	if svc.PGID <= 0 {
		t.Errorf("expected pgid > 0, got %d", svc.PGID)
	}
}

func TestAPIStatusMultipleServices(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	// Hand-rolled YAML instead of testutil.NewTestServiceConfigFile: that helper
	// pairs with writeServiceFiles, which always writes to a fixed
	// tempDir/test-project/service.yaml, so 3 services would collide on one path.
	names := []string{"svc-alpha", "svc-beta", "svc-gamma"}
	for _, name := range names {
		dir := filepath.Join(tempDir, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		yaml := fmt.Sprintf("name: %q\ncommand: \"./run.sh\"\n", name)
		yamlPath := filepath.Join(dir, "service.yaml")
		if err := os.WriteFile(yamlPath, []byte(yaml), 0644); err != nil {
			t.Fatalf("write yaml %s: %v", name, err)
		}

		c := newTestRootCmd(mgr)
		var outBuf, errBuf bytes.Buffer
		c.SetOut(&outBuf)
		c.SetErr(&errBuf)
		c.SetArgs([]string{"api", "add", yamlPath})
		if err := c.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("failed to register %s: %v\n%s", name, err, errBuf.String())
		}
	}

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "status"})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStatusResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if len(result.Services) != 3 {
		t.Errorf("expected 3 services, got %d", len(result.Services))
	}
}

// TestAPIStatusWithDependencyWait proves the JSON status output surfaces a
// service currently gated on depends_on as status "waiting" with its still-
// pending dependency names in waiting_for, instead of looking identical to a
// service that was simply never started.
func TestAPIStatusWithDependencyWait(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to register: %v\n%s", err, errBuf.String())
	}

	if err := mgr.SetDependencyWaitStatus(testFile.Name, []string{"proxy", "cache"}, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("SetDependencyWaitStatus: %v", err)
	}

	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "status"})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStatusResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	svc := result.Services[0]
	if svc.Status != "waiting" {
		t.Errorf("expected status %q, got %q", "waiting", svc.Status)
	}
	if len(svc.WaitingFor) != 2 || svc.WaitingFor[0] != "proxy" || svc.WaitingFor[1] != "cache" {
		t.Errorf("expected waiting_for [proxy cache], got %v", svc.WaitingFor)
	}
}
