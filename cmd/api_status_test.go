package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
)

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
