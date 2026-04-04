package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
)

func TestAPIRunWithServiceFile(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	cmd.SetArgs([]string{"api", "run", "-f", yamlPath})

	err := cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("expected no error, got: %v\nerr output: %s", err, errBuf.String())
	}

	var result apiRunResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}

	if result.Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, result.Name)
	}
	if result.PGID <= 0 {
		t.Errorf("expected pgid > 0, got %d", result.PGID)
	}
	if result.Restarted {
		t.Errorf("expected restarted=false for a fresh start, got: %+v", result)
	}
	if result.Skipped {
		t.Errorf("expected skipped=false, got: %+v", result)
	}
}

func TestAPIRunWithServiceName(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	// First run: register and start via file
	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", "-f", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("first run failed: %v\n%s", err, errBuf.String())
	}

	// Second run: by name - should restart
	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", testFile.Name})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("second run failed: %v\n%s", err, errBuf.String())
	}

	var result apiRunResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if !result.Restarted {
		t.Errorf("expected restarted=true, got false")
	}
}

func TestAPIRunWithOnceFlag_AlreadyRunning(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	// Start the service first
	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", "-f", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("initial start failed: %v\n%s", err, errBuf.String())
	}

	// Run --once while already running
	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", "--once", testFile.Name})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("--once run failed: %v\n%s", err, errBuf.String())
	}

	var result apiRunResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if !result.Skipped {
		t.Errorf("expected skipped=true, got false")
	}
}

func TestAPIRunWithOnceFlag_NotRunning(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	cmd.SetArgs([]string{"api", "run", "--once", "-f", yamlPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiRunResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Skipped {
		t.Errorf("expected skipped=false for a fresh --once start, got: %+v", result)
	}
	if result.PGID <= 0 {
		t.Errorf("expected pgid > 0, got %d", result.PGID)
	}
}

func TestAPIRunWithUnregisteredName(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "run", "nonexistent-service"})
	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if jsonErr := json.NewDecoder(errBuf).Decode(&errResult); jsonErr != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error message in JSON, got: %+v", errResult)
	}
}

func TestAPIRunWithFileNotFound(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "run", "-f", "/nonexistent/path/service.yaml"})
	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if jsonErr := json.NewDecoder(errBuf).Decode(&errResult); jsonErr != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error message in JSON, got: %+v", errResult)
	}
}
