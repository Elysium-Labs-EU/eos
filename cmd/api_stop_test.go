package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// startServiceForStopTest registers and starts a service so tests have a
// running process to stop. Shared with cmd/api_remove_test.go. It starts the
// service directly through runResolveAndStart (the same real start logic
// newRunCmd's RunE calls) rather than through "eos api run": that command
// now refuses every local start outright (its own documented pgid-outlives-
// the-command contract is unsatisfiable without a daemon), so it can no
// longer be used as test setup in local mode.
func startServiceForStopTest(t *testing.T, mgr manager.ServiceManager, tempDir string) string {
	t.Helper()
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
	startServiceForTest(t, mgr, testFile.Name)
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

// TestAPIStopPersistsDisabled proves "eos api stop" persists the same
// desired-boot-state flag the plain "eos stop" command does (issue #172).
func TestAPIStopPersistsDisabled(t *testing.T) {
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

	entry, err := mgr.GetServiceCatalogEntry(t.Context(), serviceName)
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if entry.Enabled {
		t.Error("expected Enabled=false after 'eos api stop'")
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
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
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

// TestAPIStopGracePeriodExceeded exercises "eos api stop" against a real
// process that ignores SIGTERM: the JSON error must name --force and carry
// the "grace_period_exceeded" code, the service must stay enabled since the
// process is still alive, and the documented recovery command (--force) must
// actually reap it.
//
// This genuinely waits out the full grace period: newTestRootCmd's getConfig
// hardcodes a 5s GracePeriod (see cmd/stop_gaps_test.go's identical note on
// TestStopCommandGracePeriodExceeded_ErroredAndDeclined, whose stubborn-script
// pattern this mirrors), so the ~5s runtime here is real, not a leftover
// sleep.
func TestAPIStopGracePeriodExceeded(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}

	stubbornScript := `#!/bin/bash
trap '' SIGTERM
touch trap-installed
while true; do
    sleep 1
done`

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("error writing service.yaml: %v", err)
	}
	fullPathScript := filepath.Join(fullDirPath, "start-script.sh")
	if err = os.WriteFile(fullPathScript, []byte(stubbornScript), 0755); err != nil {
		t.Fatalf("error writing start-script.sh: %v", err)
	}

	c := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "add", fullPathYaml})
	if err = c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("api add: unexpected error: %v\n%s", err, errBuf.String())
	}

	// "eos api run" now refuses every local start outright, so start the
	// service directly through the same real logic (runResolveAndStart).
	startServiceForTest(t, mgr, testFile.Name)

	// Wait for the trap-installed marker so SIGTERM is never sent before bash
	// has actually installed its SIGTERM-ignoring trap.
	markerPath := filepath.Join(fullDirPath, "trap-installed")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := os.Stat(markerPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s to appear", markerPath)
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		if latest, latestErr := db.GetMostRecentProcessHistoryEntryByName(t.Context(), testFile.Name); latestErr == nil {
			_ = syscall.Kill(-latest.PGID, syscall.SIGKILL)
		}
	})

	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "stop", testFile.Name})
	err = c.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var decoded map[string]string
	if jsonErr := json.NewDecoder(&errBuf).Decode(&decoded); jsonErr != nil {
		t.Fatalf("expected valid JSON error, got: %s", errBuf.String())
	}
	if decoded["code"] != "grace_period_exceeded" {
		t.Errorf("expected code %q, got %q", "grace_period_exceeded", decoded["code"])
	}
	if !strings.Contains(decoded["error"], "--force") {
		t.Errorf("expected error to name the --force escalation, got: %q", decoded["error"])
	}

	entry, err := mgr.GetServiceCatalogEntry(t.Context(), testFile.Name)
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if !entry.Enabled {
		t.Error("expected Enabled to remain true while the process is still alive after a failed graceful stop")
	}

	// The documented recovery path must actually reap the process.
	outBuf.Reset()
	errBuf.Reset()
	c = newTestRootCmd(mgr)
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs([]string{"api", "stop", testFile.Name, "--force"})
	if err = c.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("api stop --force: unexpected error: %v\n%s", err, errBuf.String())
	}

	var forceResult apiStopResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &forceResult); jsonErr != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if forceResult.Stopped != 1 {
		t.Errorf("expected stopped=1 after --force, got %+v", forceResult)
	}

	entry, err = mgr.GetServiceCatalogEntry(t.Context(), testFile.Name)
	if err != nil {
		t.Fatalf("GetServiceCatalogEntry: %v", err)
	}
	if entry.Enabled {
		t.Error("expected Enabled=false once --force actually reaped the process")
	}
}

// apiStopFakeManager implements manager.ServiceManager by embedding a nil
// interface and overriding only the methods newAPIStopCmd's RunE calls, so
// its grace-period-exceeded and boot-flag-persistence branches can be driven
// directly without a real DB-backed manager or a genuinely stubborn process.
type apiStopFakeManager struct {
	manager.ServiceManager
	registeredErr     error
	stopErr           error
	forceStopErr      error
	setEnabledErr     error
	removeInstanceErr error
	stopResult        manager.StopServiceResult
	forceStopResult   manager.StopServiceResult
	registered        bool
	setEnabledCalled  bool
}

func (f *apiStopFakeManager) IsServiceRegistered(context.Context, string) (bool, error) {
	return f.registered, f.registeredErr
}

func (f *apiStopFakeManager) StopService(context.Context, string, time.Duration, time.Duration) (manager.StopServiceResult, error) {
	return f.stopResult, f.stopErr
}

func (f *apiStopFakeManager) ForceStopService(context.Context, string) (manager.StopServiceResult, error) {
	return f.forceStopResult, f.forceStopErr
}

func (f *apiStopFakeManager) SetServiceEnabled(context.Context, string, bool) error {
	f.setEnabledCalled = true
	return f.setEnabledErr
}

func (f *apiStopFakeManager) RemoveServiceInstance(context.Context, string) (bool, error) {
	return true, f.removeInstanceErr
}

func newTestAPIStopCmd(fake *apiStopFakeManager) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := newAPIStopCmd(
		func() manager.ServiceManager { return fake },
		func() *config.SystemConfig {
			return &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Millisecond}}
		},
		func() localMode { return localMode{} },
	)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"svc"})
	return cmd, &outBuf, &errBuf
}

// TestAPIStopGracePeriodExceededUnit proves the graceful path names the
// --force escalation and returns a stable, script-matchable "code" instead of
// only human-readable prose, and never persists the boot-disabled flag while
// a process is still alive.
func TestAPIStopGracePeriodExceededUnit(t *testing.T) {
	fake := &apiStopFakeManager{
		registered: true,
		stopResult: manager.StopServiceResult{Errored: map[int]string{123: "killing service: exceeded grace period"}},
	}
	cmd, _, errBuf := newTestAPIStopCmd(fake)

	err := cmd.Execute()
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var decoded map[string]string
	if jsonErr := json.NewDecoder(errBuf).Decode(&decoded); jsonErr != nil {
		t.Fatalf("expected valid JSON error, got: %s", errBuf.String())
	}
	if decoded["code"] != "grace_period_exceeded" {
		t.Errorf("expected code %q, got %q", "grace_period_exceeded", decoded["code"])
	}
	if !strings.Contains(decoded["error"], "--force") {
		t.Errorf("expected error to name the --force escalation, got: %q", decoded["error"])
	}
	if fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled not to be called when the process is still alive")
	}
}

// TestAPIStopSetEnabledErrorGraceful covers the graceful path's persist
// failure branch, once the stop itself fully succeeded.
func TestAPIStopSetEnabledErrorGraceful(t *testing.T) {
	fake := &apiStopFakeManager{registered: true, setEnabledErr: errors.New("db closed")}
	cmd, _, errBuf := newTestAPIStopCmd(fake)

	err := cmd.Execute()
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}
	if !fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled to be called once the graceful stop fully succeeded")
	}
	var decoded map[string]string
	if jsonErr := json.NewDecoder(errBuf).Decode(&decoded); jsonErr != nil {
		t.Fatalf("expected valid JSON error, got: %s", errBuf.String())
	}
	if !strings.Contains(decoded["error"], "db closed") {
		t.Errorf("expected 'db closed' in error, got: %q", decoded["error"])
	}
}

// TestAPIStopForcePartialFailureSkipsPersist proves --force does not persist
// the boot-disabled flag when a PGID survives even SIGKILL.
func TestAPIStopForcePartialFailureSkipsPersist(t *testing.T) {
	fake := &apiStopFakeManager{
		registered:      true,
		forceStopResult: manager.StopServiceResult{Errored: map[int]string{123: "kill: operation not permitted"}},
		setEnabledErr:   errors.New("must not be called"),
	}
	cmd, outBuf, _ := newTestAPIStopCmd(fake)
	cmd.SetArgs([]string{"svc", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled not to be called when a PGID survives force kill")
	}
	var result apiStopResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON, got: %s", outBuf.String())
	}
	if result.Failed != 1 {
		t.Errorf("expected failed=1, got %+v", result)
	}
}

// TestAPIStopForceSetEnabledError covers --force's persist failure branch
// once the force kill fully succeeded.
func TestAPIStopForceSetEnabledError(t *testing.T) {
	fake := &apiStopFakeManager{
		registered:      true,
		forceStopResult: manager.StopServiceResult{Stopped: map[int]bool{123: true}},
		setEnabledErr:   errors.New("db closed"),
	}
	cmd, _, errBuf := newTestAPIStopCmd(fake)
	cmd.SetArgs([]string{"svc", "--force"})

	err := cmd.Execute()
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}
	if !fake.setEnabledCalled {
		t.Error("expected SetServiceEnabled to be called once the force stop fully succeeded")
	}
	var decoded map[string]string
	if jsonErr := json.NewDecoder(errBuf).Decode(&decoded); jsonErr != nil {
		t.Fatalf("expected valid JSON error, got: %s", errBuf.String())
	}
	if !strings.Contains(decoded["error"], "db closed") {
		t.Errorf("expected 'db closed' in error, got: %q", decoded["error"])
	}
}

// TestAPIStopServiceErrPassthrough covers StopService itself erroring
// (distinct from individual PGIDs failing).
func TestAPIStopServiceErrPassthrough(t *testing.T) {
	fake := &apiStopFakeManager{registered: true, stopErr: errors.New("db down")}
	cmd, _, errBuf := newTestAPIStopCmd(fake)

	err := cmd.Execute()
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "stopping service:") {
		t.Errorf("expected 'stopping service:', got: %s", errBuf.String())
	}
}

// TestAPIStopForceServiceErrPassthrough covers ForceStopService itself
// erroring, the --force counterpart of TestAPIStopServiceErrPassthrough.
func TestAPIStopForceServiceErrPassthrough(t *testing.T) {
	fake := &apiStopFakeManager{registered: true, forceStopErr: errors.New("db down")}
	cmd, _, errBuf := newTestAPIStopCmd(fake)
	cmd.SetArgs([]string{"svc", "--force"})

	err := cmd.Execute()
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "force stopping service:") {
		t.Errorf("expected 'force stopping service:', got: %s", errBuf.String())
	}
}

// TestAPIStopIsServiceRegisteredErrPassthrough covers the registration check
// itself erroring, distinct from "not found".
func TestAPIStopIsServiceRegisteredErrPassthrough(t *testing.T) {
	fake := &apiStopFakeManager{registeredErr: errors.New("db down")}
	cmd, _, errBuf := newTestAPIStopCmd(fake)

	err := cmd.Execute()
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service:") {
		t.Errorf("expected 'checking service:', got: %s", errBuf.String())
	}
}
