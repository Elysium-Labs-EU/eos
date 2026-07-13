package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestValidateCommand(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)

	cfg := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	fullPath := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate", fullPath})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("validate should not error, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "valid") {
		t.Errorf("expected 'valid' in output, got: %s", output)
	}
	if !strings.Contains(output, "cms") {
		t.Errorf("expected service name 'cms' in output, got: %s", output)
	}
}

func TestValidateCommandMissingFile(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate", "nonexistent-path"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "error") {
		t.Errorf("expected 'error' in output, got: %s", output)
	}
}

func TestValidateCommandCollectsAllErrors(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)

	cfg := testutil.NewTestServiceConfigFile(t,
		testutil.WithName(""),
		testutil.WithCommand(""),
		testutil.WithRuntime("nodejs", "/nonexistent/bin"),
	)
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	fullPath := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate", fullPath})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "service name is required") {
		t.Errorf("expected 'service name is required' in output, got: %s", output)
	}
	if !strings.Contains(output, "service command is required") {
		t.Errorf("expected 'service command is required' in output, got: %s", output)
	}
	if !strings.Contains(output, "runtime") {
		t.Errorf("expected 'runtime' error in output, got: %s", output)
	}
}

func TestValidateCommandNoArgs(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate"})

	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("validate should error when no args given")
	}
}

func TestValidateCommandInvalidRuntimePath(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)

	cfg := testutil.NewTestServiceConfigFile(t, testutil.WithRuntime("nodejs", "/nonexistent/path/to/bin"))
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	fullPath := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate", fullPath})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "runtime") {
		t.Errorf("expected 'runtime' in output, got: %s", output)
	}
}
