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

func TestAPIUpdateRegisteredService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	// Register initial service
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to register: %v\n%s", err, errBuf.String())
	}

	// Update pointing at the same path; exercises the success path, not a move
	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "update", testFile.Name, yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiUpdateResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, result.Name)
	}
	if result.Path == "" {
		t.Errorf("expected non-empty path")
	}
	if result.ConfigFile != "service.yaml" {
		t.Errorf("expected config_file %q, got %q", "service.yaml", result.ConfigFile)
	}
}

func TestAPIUpdateNonexistentService(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "update", "nonexistent-service", "/some/path"})
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

func TestAPIUpdateInvalidNewPath(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", yamlPath})
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("failed to register: %v\n%s", err, errBuf.String())
	}

	// Update with nonexistent path
	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "update", testFile.Name, "/nonexistent/path/does/not/exist"})
	err := c.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if jsonErr := json.NewDecoder(&errBuf).Decode(&errResult); jsonErr != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error, got: %+v", errResult)
	}
}
