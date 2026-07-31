package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestStatusCommand(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	cmd.SetArgs([]string{"status"})

	err := cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}
	output := errBuf.String()

	if !strings.Contains(output, "error no services are registered") {
		t.Errorf("Expected status to show 'error no services are registered', got: %s", output)
	}
	if !strings.Contains(output, "base dir: "+tempDir) {
		t.Errorf("Expected status to show resolved base dir %q, got: %s", tempDir, output)
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

// TestStatusCommandWithDependencyWait proves a service currently gated on
// depends_on renders "waiting" (and names the still-pending dependency)
// instead of "stopped" — the distinct state issue #136 asks for, since a
// blocked service and one never started otherwise look identical.
func TestStatusCommandWithDependencyWait(t *testing.T) {
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
		t.Fatalf("could not create test-project directory: %v\n", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}

	if err := mgr.SetDependencyWaitStatus(testFile.Name, []string{"proxy"}, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("SetDependencyWaitStatus: %v", err)
	}

	cmd.SetArgs([]string{"status"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("status should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, testFile.Name) {
		t.Errorf("expected service name %q in status table, got: %s", testFile.Name, output)
	}
	if !strings.Contains(output, "waiting") {
		t.Errorf("expected 'waiting' status for a service gated on depends_on, got: %s", output)
	}
	if strings.Contains(output, "stopped") {
		t.Errorf("a waiting service must not also render as 'stopped', got: %s", output)
	}
	if !strings.Contains(output, "proxy") {
		t.Errorf("expected the pending dependency name 'proxy' in output, got: %s", output)
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

func TestPrintStatusTable_GetCatalogError(t *testing.T) {
	mgr := &mockMgr{
		getAllCatalogEntries: func() ([]types.ServiceCatalogEntry, error) {
			return nil, fmt.Errorf("catalog unavailable")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	printStatusTable(cmd, mgr, time.Second)

	if !strings.Contains(errBuf.String(), "getting registered services") {
		t.Errorf("expected catalog error message, got: %s", errBuf.String())
	}
}

func writeStatusTestService(t *testing.T, dir, name string) {
	t.Helper()
	cfg := &types.ServiceConfig{Name: name, Command: "./start.sh"}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal service config: %v", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}
}

func TestPrintStatusTable_GetServiceInstanceError(t *testing.T) {
	dir := t.TempDir()
	writeStatusTestService(t, dir, "svc")

	mgr := &mockMgr{
		getAllCatalogEntries: func() ([]types.ServiceCatalogEntry, error) {
			return []types.ServiceCatalogEntry{{Name: "svc", DirectoryPath: dir, ConfigFileName: "service.yaml"}}, nil
		},
		getServiceInstance: func(string) (*types.ServiceInstance, error) {
			return nil, fmt.Errorf("instance lookup failed")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	printStatusTable(cmd, mgr, time.Second)

	if !strings.Contains(errBuf.String(), "getting service instance") {
		t.Errorf("expected service instance error message, got: %s", errBuf.String())
	}
}

func TestPrintStatusTable_GetProcessHistoryError(t *testing.T) {
	dir := t.TempDir()
	writeStatusTestService(t, dir, "svc")

	mgr := &mockMgr{
		getAllCatalogEntries: func() ([]types.ServiceCatalogEntry, error) {
			return []types.ServiceCatalogEntry{{Name: "svc", DirectoryPath: dir, ConfigFileName: "service.yaml"}}, nil
		},
		getServiceInstance: func(string) (*types.ServiceInstance, error) {
			return nil, manager.ErrServiceNotRunning
		},
		getMostRecentProcess: func(string) (*types.ProcessHistory, error) {
			return nil, fmt.Errorf("process history lookup failed")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	printStatusTable(cmd, mgr, time.Second)

	if !strings.Contains(errBuf.String(), "getting process history") {
		t.Errorf("expected process history error message, got: %s", errBuf.String())
	}
}

func TestPrintStatusTable_CronRestartPending(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithCronRestart("* * * * *"))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}

	cmd.SetArgs([]string{"status"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("status should not return an error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "pending") {
		t.Errorf("expected 'pending' next-restart for a cron-restart service with no scheduled instance, got: %s", output)
	}
}

func TestPrintStatusTable_StaleRow(t *testing.T) {
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
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	if err = os.WriteFile(fullPathScript, []byte("#!/bin/bash\nexec sleep 3600"), 0755); err != nil {
		t.Fatalf("Failed to write the start script file, got: %v", err)
	}

	var setupBuf bytes.Buffer
	cmd.SetOut(&setupBuf)
	cmd.SetErr(&setupBuf)

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add should not return an error, got: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run should not return an error, got: %v", err)
	}

	// updated_at is only populated by an explicit update, always stamped with
	// the current time; sleep afterward so it reads as stale against a
	// deliberately tiny checkInterval, without needing to wait out a real
	// health-check interval.
	mostRecent, err := mgr.GetMostRecentProcessHistoryEntry(testFile.Name)
	if err != nil {
		t.Fatalf("failed to get process history entry: %v", err)
	}
	runningState := types.ProcessStateRunning
	if err := db.UpdateProcessHistoryEntry(t.Context(), mostRecent.PGID, database.ProcessHistoryUpdate{State: &runningState}); err != nil {
		t.Fatalf("failed to mark process as running: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	printStatusTable(cmd, mgr, time.Millisecond)

	output := outBuf.String()
	if !strings.Contains(output, "(stale)") {
		t.Errorf("expected stale-row marker in output, got: %s", output)
	}
}

func TestPrintStatusTable_NextRestartScheduledAndOddRow(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	cmd := newTestRootCmd(mgr)

	var setupBuf bytes.Buffer
	cmd.SetOut(&setupBuf)
	cmd.SetErr(&setupBuf)

	// "svc-a" sorts before "svc-b" (GetAllServiceCatalogEntries orders by
	// name), so having two registered services exercises both the even- and
	// odd-row table styles.
	svcA := testutil.NewTestServiceConfigFile(t, testutil.WithName("svc-a"), testutil.WithoutRuntime())
	svcADir := filepath.Join(tempDir, "svc-a")
	yamlA, err := yaml.Marshal(svcA)
	if err != nil {
		t.Fatalf("marshal svc-a config: %v", err)
	}
	if err = os.MkdirAll(svcADir, 0755); err != nil {
		t.Fatalf("mkdir svc-a: %v", err)
	}
	if err = os.WriteFile(filepath.Join(svcADir, "service.yaml"), yamlA, 0644); err != nil {
		t.Fatalf("write svc-a service.yaml: %v", err)
	}
	cmd.SetArgs([]string{"add", filepath.Join(svcADir, "service.yaml")})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add svc-a should not return an error, got: %v", err)
	}

	svcB := testutil.NewTestServiceConfigFile(t, testutil.WithName("svc-b"), testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime(), testutil.WithCronRestart("0 0 * * *"))
	svcBDir := filepath.Join(tempDir, "svc-b")
	yamlB, err := yaml.Marshal(svcB)
	if err != nil {
		t.Fatalf("marshal svc-b config: %v", err)
	}
	if err = os.MkdirAll(svcBDir, 0755); err != nil {
		t.Fatalf("mkdir svc-b: %v", err)
	}
	if err = os.WriteFile(filepath.Join(svcBDir, "service.yaml"), yamlB, 0644); err != nil {
		t.Fatalf("write svc-b service.yaml: %v", err)
	}
	if err = os.WriteFile(filepath.Join(svcBDir, "start-script.sh"), []byte("#!/bin/bash\nexec sleep 3600"), 0755); err != nil {
		t.Fatalf("write svc-b start script: %v", err)
	}
	cmd.SetArgs([]string{"add", filepath.Join(svcBDir, "service.yaml")})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add svc-b should not return an error, got: %v", err)
	}
	cmd.SetArgs([]string{"run", "svc-b"})
	if err = cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run svc-b should not return an error, got: %v", err)
	}

	nextRestart := time.Now().Add(time.Hour)
	if err = db.UpdateServiceInstance(t.Context(), "svc-b", database.ServiceInstanceUpdate{NextRestartAt: &nextRestart}); err != nil {
		t.Fatalf("failed to schedule next restart: %v", err)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	printStatusTable(cmd, mgr, time.Second)

	output := outBuf.String()
	if !strings.Contains(output, "svc-a") || !strings.Contains(output, "svc-b") {
		t.Fatalf("expected both services in status table, got: %s", output)
	}
	if strings.Contains(output, "pending") {
		t.Errorf("expected a humanized next-restart time, not 'pending', got: %s", output)
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

	renderWatchFrame(cmd, mgr, 5, 2*time.Second)

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
