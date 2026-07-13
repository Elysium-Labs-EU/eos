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
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestStatusCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"status"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}
	output := errBuf.String()

	if !strings.Contains(output, "error no services are registered") {
		t.Errorf("Expected status to show 'error no services are registered', got: %s", output)
	}
}

// TODO: func TestStatusCommandGetCatalogError (requires mock manager)
// TODO: func TestStatusCommandGetInstanceError (requires mock manager)
// TODO: func TestStatusCommandGetProcessHistoryError (requires mock manager)

func TestStatusCommandWithRegisteredService(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}

	cmd.SetArgs([]string{"status"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("status should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, testFile.Name) {
		t.Errorf("expected service name %q in status table, got: %s", testFile.Name, output)
	}
	if !strings.Contains(output, "stopped") {
		t.Errorf("expected 'stopped' status for a never-started service, got: %s", output)
	}
}

// A freshly started process is recorded as "starting" until the health monitor
// (a separate process not exercised by these command-level tests) confirms it
// and flips it to "running"; simulate that confirmation directly via the DB.
func TestStatusCommandWithRunningService(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755)
	if err != nil {
		t.Fatalf("Failed to write the start script file, got: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("run should not return an error, got: %v", err)
	}

	mostRecent, err := mgr.GetMostRecentProcessHistoryEntry(testFile.Name)
	if err != nil {
		t.Fatalf("failed to get process history entry: %v", err)
	}
	runningState := types.ProcessStateRunning
	err = db.UpdateProcessHistoryEntry(t.Context(), mostRecent.PGID, database.ProcessHistoryUpdate{State: &runningState})
	if err != nil {
		t.Fatalf("failed to mark process as running: %v", err)
	}

	cmd.SetArgs([]string{"status"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("status should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, testFile.Name) {
		t.Errorf("expected service name %q in status table, got: %s", testFile.Name, output)
	}
	if !strings.Contains(output, "running") {
		t.Errorf("expected 'running' status for a running service, got: %s", output)
	}
}

func TestStatusCommandConfigLoadError(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}

	// Corrupt the yaml file on disk after registration, so the catalog entry
	// still points at it but loading it now fails.
	err = os.WriteFile(fullPathYaml, []byte("invalid: yaml: {{{"), 0644)
	if err != nil {
		t.Fatalf("Failed to corrupt the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"status"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("status should not return an error, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "loading service config") {
		t.Errorf("expected 'loading service config' error, got: %s", output)
	}
}

func TestStatusCommandConfigNameMismatch(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPathYaml, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}

	// Change the on-disk config's name after registration; the catalog keeps
	// the original name.
	testFile.Name = "renamed"
	renamedYaml, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal renamed config: %v", err)
	}
	err = os.WriteFile(fullPathYaml, renamedYaml, 0644)
	if err != nil {
		t.Fatalf("Failed to write the renamed service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"status"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("status should not return an error, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "service file contains different name than registered") {
		t.Errorf("expected name-mismatch error, got: %s", output)
	}
}

func TestStatusCommandIntervalTooLow(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"status", "--watch", "--interval", "0"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "--interval must be at least 1 second") {
		t.Errorf("expected interval validation error, got: %s", output)
	}
}

func TestStatusWatchFlags(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"status", "--help"})
	_ = cmd.ExecuteContext(t.Context())
	out := outBuf.String()
	if !strings.Contains(out, "--watch") {
		t.Errorf("expected --watch flag in help, got: %s", out)
	}
	if !strings.Contains(out, "--interval") {
		t.Errorf("expected --interval flag in help, got: %s", out)
	}
}

func TestStatusHelpText(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"status", "--help"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Status help should not return an error, got: %v", err)
	}
	output := outBuf.String()

	if !strings.Contains(output, "Display the current status of all configured services") {
		t.Errorf("Expected status help to describe the command, got: %s", output)
	}
	if !strings.Contains(output, "eos status") {
		t.Errorf("Expected status help to show usage, got: %s", output)
	}
}

func TestRenderWatchFrame(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	renderWatchFrame(cmd, mgr, 5)

	output := outBuf.String()
	if !strings.Contains(output, "\033[2J\033[H") {
		t.Errorf("expected clear-screen escape sequence, got: %q", output)
	}
	if !strings.Contains(output, "Every 5s: eos status") {
		t.Errorf("expected watch header with interval, got: %q", output)
	}
	if !strings.Contains(errBuf.String(), "error no services are registered") {
		t.Errorf("expected renderWatchFrame to delegate to printStatusTable, got: %q", errBuf.String())
	}
}
