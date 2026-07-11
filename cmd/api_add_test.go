package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

func TestAPIAddValidYaml(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	cmd.SetArgs([]string{"api", "add", yamlPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiAddResult
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

func TestAPIAddAlreadyRegistered(t *testing.T) {
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
		t.Fatalf("first add failed: %v\n%s", err, errBuf.String())
	}

	c = newTestRootCmd(mgr)
	outBuf.Reset()
	errBuf.Reset()
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", yamlPath})
	err := c.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed for duplicate add, got: %v", err)
	}

	var errResult map[string]string
	if jsonErr := json.NewDecoder(&errBuf).Decode(&errResult); jsonErr != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error, got: %+v", errResult)
	}
}

func TestAPIAddBadPath(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "add", "/nonexistent/path/does/not/exist"})
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

// TestAPIAddDirectory covers the path where the argument resolves to a
// directory rather than a yaml file directly; parseServiceFile must locate
// service.yaml inside it.
func TestAPIAddDirectory(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)
	dirPath := filepath.Dir(yamlPath)

	cmd.SetArgs([]string{"api", "add", dirPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error adding by directory, got: %v\n%s", err, errBuf.String())
	}

	var result apiAddResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, result.Name)
	}
}
