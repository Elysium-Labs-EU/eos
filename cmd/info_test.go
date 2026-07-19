package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestInfoOnlyRegisteredServiceCommand(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	runtimeDir := filepath.Join(tempDir, "runtime-bin")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("could not create runtime directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "node"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatalf("could not write fake node binary: %v", err)
	}

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithRuntimePath(runtimeDir))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	cmd.SetArgs([]string{"info", "cms"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("info command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	output := outBuf.String()
	if !strings.Contains(output, "name") || !strings.Contains(output, "cms") {
		t.Errorf("expected name to be 'cms'")
	}
	if !strings.Contains(output, "path") || !strings.Contains(output, fullDirPath) {
		t.Errorf("expected path to contain '%s'", fullDirPath)
	}
	if !strings.Contains(output, "command") || !strings.Contains(output, "/home/user/start-script.sh") {
		t.Errorf("expected command to be '/home/user/start-script.sh'")
	}
	if !strings.Contains(output, "port") || !strings.Contains(output, "1337") {
		t.Errorf("expected port to be '1337'")
	}
	if !strings.Contains(output, "runtime") || !strings.Contains(output, "nodejs") {
		t.Errorf("expected runtime to be 'nodejs'")
	}
	if !strings.Contains(output, "runtime path") || !strings.Contains(output, runtimeDir) {
		t.Errorf("expected runtime path to be %q", runtimeDir)
	}
}

func TestInfoOnlyRegisteredServiceIncompleteCommand(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := &types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
		Port:    1337,
	}
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
		return
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	cmd.SetArgs([]string{"info", "cms"})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("info command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	output := outBuf.String()

	if !strings.Contains(output, "command") || !strings.Contains(output, "/home/user/start-script.sh") {
		t.Errorf("expected command to be present in config section")
	}
	if !strings.Contains(output, "runtime") || !strings.Contains(output, "N/A") {
		t.Errorf("expected runtime to show 'N/A' for incomplete config, got: %s", output)
	}
	if !strings.Contains(output, "runtime path") || !strings.Contains(output, "N/A") {
		t.Errorf("expected runtime path to show 'N/A' for incomplete config, got: %s", output)
	}
}

func TestInfoInvalidNumberArgumentsCommand(t *testing.T) {
	cmd, _, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"info"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatalf("info command should return an error")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Errorf("expected error to mention 'accepts 1 arg(s), received 0', got: %v", err)
	}
}

func TestInfoNonExistentServiceCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"info", "cms"})

	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v\nerr output: %s", err, errBuf.String())
	}
	output := errBuf.String()

	if !strings.Contains(output, "service not registered") {
		t.Errorf("expected info to show 'service not registered', got: %s", output)
	}
}

func TestInfoNeverStartedShowsMutedSections(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v", err)
	}

	cmd.SetArgs([]string{"info", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("info command should not return an error, got: %v", err)
	}

	// The 5 section headers all render to outBuf via cmd.Printf, but the
	// "no X found" muted notices are sent via cmd.PrintErr and land in
	// errBuf even though they aren't real errors.
	output := outBuf.String()
	for _, section := range []string{"Process", "Service", "Logging", "Instance", "Config"} {
		if !strings.Contains(output, section) {
			t.Errorf("expected section header %q in output, got: %s", section, output)
		}
	}
	processIdx := strings.Index(output, "Process")
	serviceIdx := strings.Index(output, "Service")
	loggingIdx := strings.Index(output, "Logging")
	instanceIdx := strings.Index(output, "Instance")
	configIdx := strings.Index(output, "Config")
	if processIdx >= serviceIdx || serviceIdx >= loggingIdx || loggingIdx >= instanceIdx || instanceIdx >= configIdx {
		t.Errorf("expected sections in order Process, Service, Logging, Instance, Config, got: %s", output)
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "no process found") {
		t.Errorf("expected 'no process found' in errBuf, got: %s", errOutput)
	}
	if !strings.Contains(errOutput, "no service instance found") {
		t.Errorf("expected 'no service instance found' in errBuf, got: %s", errOutput)
	}
}

func TestInfoPortZeroShowsNA(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	testFile.Port = 0
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	cmd.SetArgs([]string{"info", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("info command should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "port") || !strings.Contains(output, "N/A") {
		t.Errorf("expected port to show 'N/A' when unset, got: %s", output)
	}
}

func TestInfoWithLogSinks(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithLogSinks(
		types.LogSink{Type: "loki", Mode: "push", Address: "http://localhost:3100", Exec: "sh", Streams: []string{"stdout", "stderr"}},
		types.LogSink{Type: "otlp", Mode: "push", Address: "http://localhost:4317", Exec: "sh"},
	))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	cmd.SetArgs([]string{"info", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("info command should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "sink 1") || !strings.Contains(output, "loki") || !strings.Contains(output, "stdout, stderr") {
		t.Errorf("expected sink 1 with streams joined, got: %s", output)
	}
	if !strings.Contains(output, "sink 2") || !strings.Contains(output, "otlp") || !strings.Contains(output, "all") {
		t.Errorf("expected sink 2 falling back to 'all' streams, got: %s", output)
	}
}

func TestInfoConfigLoadError(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	// Corrupt the yaml file on disk after registration; the catalog entry
	// still points at it but loading it now fails.
	if err := os.WriteFile(fullPath, []byte("invalid: yaml: {{{"), 0644); err != nil {
		t.Fatalf("Failed to corrupt the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"info", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("info should not return an error even with a broken config, got: %v", err)
	}

	errOutput := errBuf.String()
	if !strings.Contains(errOutput, "loading service config") {
		t.Errorf("expected 'loading service config' error, got: %s", errOutput)
	}
	// "no config loaded" is a muted notice sent via cmd.PrintErr, not a
	// genuine error, but it still lands in errBuf rather than outBuf.
	if !strings.Contains(errOutput, "no config loaded") {
		t.Errorf("expected Config section to show 'no config loaded', got: %s", errOutput)
	}

	// The other sections should still render despite the config failure.
	output := outBuf.String()
	if !strings.Contains(output, "name") || !strings.Contains(output, "cms") {
		t.Errorf("expected Service section to still render, got: %s", output)
	}
}

func TestInfoWithRunningServiceState(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command should not return an error, got: %v", err)
	}

	testutil.SeedServiceInstances(t, t.Context(), db, []string{"cms"}, testutil.WithSeedRestartCount(2))
	testutil.SeedProcessHistory(t, t.Context(), db, "cms", 1, testutil.WithHistoryState(types.ProcessStateRunning), testutil.WithBasePGID(54321))

	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"info", "cms"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("info command should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "running") {
		t.Errorf("expected 'running' status in Process section, got: %s", output)
	}
	if !strings.Contains(output, "54321") {
		t.Errorf("expected PGID 54321 in Process section, got: %s", output)
	}
	if !strings.Contains(output, "restarts") || !strings.Contains(output, "2") {
		t.Errorf("expected restarts=2 in Instance section, got: %s", output)
	}
}
