package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

// startServiceForStopTest registers and starts a service via "api run" so
// tests have a running process to stop. Shared with cmd/api_remove_test.go.
func startServiceForStopTest(t *testing.T, mgr manager.ServiceManager, tempDir string) string {
	t.Helper()
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", "-f", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to start service: %v\n%s", err, errBuf.String())
	}
	return testFile.Name
}

func TestAPIStopRunningService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	serviceName := startServiceForStopTest(t, mgr, tempDir)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "stop", serviceName})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStopResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Name != serviceName {
		t.Errorf("expected name %q, got %q", serviceName, result.Name)
	}
	if result.Force {
		t.Errorf("expected force=false")
	}
}

func TestAPIStopNonexistentService(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "stop", "nonexistent-service"})
	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if jsonErr := json.NewDecoder(errBuf).Decode(&errResult); jsonErr != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error, got: %+v", errResult)
	}
}

func TestAPIStopForceFlag(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	serviceName := startServiceForStopTest(t, mgr, tempDir)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "stop", serviceName, "--force"})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiStopResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if !result.Force {
		t.Errorf("expected force=true")
	}
	if result.Name != serviceName {
		t.Errorf("expected name %q, got %q", serviceName, result.Name)
	}
	// Failed is only ever set on the --force path, so check both counters
	// for the one process the test started.
	if result.Stopped+result.Failed == 0 {
		t.Errorf("expected at least 1 process attempt, got stopped=%d failed=%d", result.Stopped, result.Failed)
	}
}

func TestAPIStopNotRunningService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to register service: %v\n%s", err, errBuf.String())
	}

	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "stop", testFile.Name})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error stopping non-running service, got: %v\n%s", err, errBuf.String())
	}

	var result apiStopResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, result.Name)
	}
}
