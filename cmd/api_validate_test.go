package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

func TestAPIValidateValidYaml(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	cmd.SetArgs([]string{"api", "validate", yamlPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiValidateResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if !result.Valid {
		t.Errorf("expected valid=true, errors: %v", result.Errors)
	}
	if result.Name != testFile.Name {
		t.Errorf("expected name %q, got %q", testFile.Name, result.Name)
	}
	if result.ConfigFile != "service.yaml" {
		t.Errorf("expected config_file %q, got %q", "service.yaml", result.ConfigFile)
	}
	if result.Path == "" {
		t.Errorf("expected non-empty path")
	}
}

func TestAPIValidateDirectory(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)
	dirPath := filepath.Dir(yamlPath)

	cmd.SetArgs([]string{"api", "validate", dirPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error, got: %v\n%s", err, errBuf.String())
	}

	var result apiValidateResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if !result.Valid {
		t.Errorf("expected valid=true, errors: %v", result.Errors)
	}
}

func TestAPIValidateInvalidYaml(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	// Write yaml missing required fields
	dir := filepath.Join(tempDir, "invalid-project")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlPath := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: \"\"\ncommand: \"\"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd.SetArgs([]string{"api", "validate", yamlPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("expected no error (validation result in JSON), got: %v\n%s", err, errBuf.String())
	}

	var result apiValidateResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Valid {
		t.Errorf("expected valid=false for empty name+command")
	}
	if len(result.Errors) == 0 {
		t.Errorf("expected at least one validation error")
	}
}

func TestAPIValidateMissingFile(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "validate", "/nonexistent/path/does/not/exist"})
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
