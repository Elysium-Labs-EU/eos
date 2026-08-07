package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func TestRemoveCommandServiceNotRegistered(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"remove", "does-not-exist"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for an unregistered service")
	}
	if !strings.Contains(errBuf.String(), "is not registered") {
		t.Errorf("expected 'is not registered' error, got: %s", errBuf.String())
	}
}

func TestRemoveCommandMissingArgs(t *testing.T) {
	cmd, _, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"remove"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error for missing arguments")
	}
}

// addAndRunService writes a service.yaml + a long-lived start script under tempDir, registers
// it, and starts it via "run" (not the deprecated "start" command), returning its name.
func addAndRunService(t *testing.T, cmd *cobra.Command, tempDir string) string {
	t.Helper()
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal service config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}
	script := "#!/bin/bash\nexec sleep 3600"
	if err := os.WriteFile(filepath.Join(fullDirPath, "start-script.sh"), []byte(script), 0755); err != nil {
		t.Fatalf("failed to write start script: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPathYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}
	cmd.SetArgs([]string{"run", testFile.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: unexpected error: %v", err)
	}
	return testFile.Name
}

func TestRemoveCommandWithActiveInstance_Decline(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	name := addAndRunService(t, cmd, tempDir)

	outBuf.Reset()
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"remove", name})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when the remove is aborted")
	}
	if !strings.Contains(outBuf.String(), "remove aborted") {
		t.Errorf("expected 'remove aborted', got: %s", outBuf.String())
	}
}

func TestRemoveCommandWithActiveInstance_Confirm(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	name := addAndRunService(t, cmd, tempDir)

	outBuf.Reset()
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"remove", name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "unregistered") {
		t.Errorf("expected 'unregistered', got: %s", outBuf.String())
	}
}

// The following tests exercise remove's generic manager-error and false-result
// branches (checking process history/instance state, removing the instance,
// removing the catalog entry). The real LocalManager's sqlite-backed
// implementation won't produce these failures from the CLI without directly
// corrupting the database, so they script a fake daemon peer (see
// newFakeDaemonManager/fakeDaemonServer in reload_gaps_test.go) that answers
// the real DaemonManager IPC client with crafted responses instead.

// TestRemoveCommandProcessHistoryError checks the "checking service state"
// error when GetMostRecentProcessHistoryEntry fails with something other than
// ErrServiceNotRunning/ErrProcessNotFound.
func TestRemoveCommandProcessHistoryError(t *testing.T) {
	mgr := newFakeDaemonManager(t, map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodGetMostRecentProcessHistoryEntry: {
			Success: false,
			Error:   "process history query exploded",
		},
	})
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when process history lookup fails")
	}
	if !strings.Contains(errBuf.String(), "checking service state") {
		t.Errorf("expected 'checking service state' error, got: %s", errBuf.String())
	}
}

// stoppedProcessHistoryOK is the canned GetMostRecentProcessHistoryEntry
// response used by the tests below to clear remove's "currently running,
// confirm?" prompt without blocking on stdin (State: stopped skips it).
func stoppedProcessHistoryOK(t *testing.T) map[types.MethodName]types.DaemonResponse {
	return map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodGetMostRecentProcessHistoryEntry: okDaemonResponse(t, types.GetMostRecentProcessHistoryEntryResponse{
			ProcessEntry: types.ProcessHistory{ServiceName: "cms", State: types.ProcessStateStopped},
		}),
	}
}

// TestRemoveCommandServiceInstanceError checks the "checking service
// instance" error when GetServiceInstance fails with something other than
// ErrServiceNotRunning.
func TestRemoveCommandServiceInstanceError(t *testing.T) {
	responses := stoppedProcessHistoryOK(t)
	responses[types.MethodGetServiceInstance] = types.DaemonResponse{
		Success: false,
		Error:   "instance lookup exploded",
	}
	mgr := newFakeDaemonManager(t, responses)
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when service instance lookup fails")
	}
	if !strings.Contains(errBuf.String(), "checking service instance") {
		t.Errorf("expected 'checking service instance' error, got: %s", errBuf.String())
	}
}

// TestRemoveCommandRemoveInstanceError checks the "removing service instance"
// error when RemoveServiceInstance itself fails.
func TestRemoveCommandRemoveInstanceError(t *testing.T) {
	responses := stoppedProcessHistoryOK(t)
	responses[types.MethodGetServiceInstance] = okDaemonResponse(t, types.GetServiceInstanceResponse{
		Instance: types.ServiceInstance{Name: "cms"},
	})
	responses[types.MethodRemoveServiceInstance] = types.DaemonResponse{
		Success: false,
		Error:   "remove instance exploded",
	}
	mgr := newFakeDaemonManager(t, responses)
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when removing the service instance fails")
	}
	if !strings.Contains(errBuf.String(), "removing service instance") {
		t.Errorf("expected 'removing service instance' error, got: %s", errBuf.String())
	}
}

// TestRemoveCommandRemoveInstanceFalse checks the "unable to remove service
// instance" error when RemoveServiceInstance succeeds but reports removed=false.
func TestRemoveCommandRemoveInstanceFalse(t *testing.T) {
	responses := stoppedProcessHistoryOK(t)
	responses[types.MethodGetServiceInstance] = okDaemonResponse(t, types.GetServiceInstanceResponse{
		Instance: types.ServiceInstance{Name: "cms"},
	})
	responses[types.MethodRemoveServiceInstance] = okDaemonResponse(t, map[string]bool{"removed": false})
	mgr := newFakeDaemonManager(t, responses)
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when removed=false is reported")
	}
	if !strings.Contains(errBuf.String(), "unable to remove service instance") {
		t.Errorf("expected 'unable to remove service instance' error, got: %s", errBuf.String())
	}
}

// TestRemoveCommandRemoveCatalogError checks the "removing service" error
// when RemoveServiceCatalogEntry itself fails. GetServiceInstance is scripted
// to report ErrServiceNotRunning so the instance-removal branch above is
// skipped entirely (serviceInstance stays nil), isolating the catalog-removal
// failure.
func TestRemoveCommandRemoveCatalogError(t *testing.T) {
	responses := stoppedProcessHistoryOK(t)
	responses[types.MethodGetServiceInstance] = types.DaemonResponse{
		Success:   false,
		ErrorCode: manager.CodeServiceNotRunning,
		Error:     "service not running",
	}
	responses[types.MethodRemoveServiceCatalogEntry] = types.DaemonResponse{
		Success: false,
		Error:   "catalog removal exploded",
	}
	mgr := newFakeDaemonManager(t, responses)
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when removing the catalog entry fails")
	}
	if !strings.Contains(errBuf.String(), "removing service:") {
		t.Errorf("expected 'removing service:' error, got: %s", errBuf.String())
	}
}

// TestRemoveCommandRemoveCatalogFalse checks the "could not be removed" error
// when RemoveServiceCatalogEntry succeeds but reports removed=false.
func TestRemoveCommandRemoveCatalogFalse(t *testing.T) {
	responses := stoppedProcessHistoryOK(t)
	responses[types.MethodGetServiceInstance] = types.DaemonResponse{
		Success:   false,
		ErrorCode: manager.CodeServiceNotRunning,
		Error:     "service not running",
	}
	responses[types.MethodRemoveServiceCatalogEntry] = okDaemonResponse(t, map[string]bool{"removed": false})
	mgr := newFakeDaemonManager(t, responses)
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected an error when removed=false is reported")
	}
	if !strings.Contains(errBuf.String(), "could not be removed") {
		t.Errorf("expected 'could not be removed' error, got: %s", errBuf.String())
	}
}
