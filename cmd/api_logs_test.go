package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

func startServiceForLogsTest(t *testing.T, mgr manager.ServiceManager, tempDir string) string {
	t.Helper()
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "run", "-f", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to start service for logs test: %v\n%s", err, errBuf.String())
	}
	return testFile.Name
}

func TestAPILogsWithService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceName := startServiceForLogsTest(t, mgr, tempDir)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "logs", serviceName})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiLogsResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Name != serviceName {
		t.Errorf("expected name %q, got %q", serviceName, result.Name)
	}
	if result.LogPath == "" {
		t.Errorf("expected non-empty log_path, got: %+v", result)
	}
	if result.Lines == nil {
		t.Errorf("expected lines to be non-nil, got: %+v", result)
	}
}

func TestAPILogsWithNoService(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "logs", "nonexistent-service"})
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

func TestAPILogsErrorWithService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceName := startServiceForLogsTest(t, mgr, tempDir)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "logs", serviceName, "--error"})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiLogsResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Name != serviceName {
		t.Errorf("expected name %q, got %q", serviceName, result.Name)
	}
	if !strings.Contains(result.LogPath, "error") {
		t.Errorf("expected log_path to reference error log, got %q", result.LogPath)
	}
}

func TestAPILogsErrorWithNoService(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "logs", "nonexistent-service", "--error"})
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

func TestAPILogsWithExceedingLineNumber(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	serviceName := startServiceForLogsTest(t, mgr, tempDir)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "logs", serviceName, "--lines", "99999"})
	err := c.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if jsonErr := json.NewDecoder(&errBuf).Decode(&errResult); jsonErr != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error message in JSON, got: %+v", errResult)
	}
}
