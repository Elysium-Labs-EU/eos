package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestRemoveCommand(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())

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
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"remove", "cms"})
	err = cmd.ExecuteContext(t.Context())

	if err != nil {
		t.Fatalf("Remove command should not return an error, got : %v", err)
	}
	output := outBuf.String()
	errOutput := errBuf.String()

	if !strings.Contains(output, "success cms unregistered") {
		t.Errorf("Expected remove to show 'success cms unregistered', got: %s", output)
	}
	if errOutput != "" {
		t.Errorf("Expected no errors', got: %s", errOutput)
	}
}

// TestRemoveCommandServiceNotRegistered, TestRemoveCommandMissingArgs, and
// TestRemoveCommandWithActiveInstance_{Decline,Confirm} live in remove_gaps_test.go.

// removeCmdFakeManager implements manager.ServiceManager by embedding a nil
// interface and overriding only the methods the removeCmd* helpers call, so
// error branches can be exercised without a real DB-backed manager.
type removeCmdFakeManager struct {
	manager.ServiceManager
	registeredErr     error
	historyErr        error
	instanceErr       error
	removeInstanceErr error
	removeCatalogErr  error
	history           *types.ProcessHistory
	instance          *types.ServiceInstance
	registered        bool
	removedInstance   bool
	removedCatalog    bool
}

func (f *removeCmdFakeManager) IsServiceRegistered(_ string) (bool, error) {
	return f.registered, f.registeredErr
}

func (f *removeCmdFakeManager) GetMostRecentProcessHistoryEntry(_ string) (*types.ProcessHistory, error) {
	return f.history, f.historyErr
}

func (f *removeCmdFakeManager) GetServiceInstance(_ string) (*types.ServiceInstance, error) {
	return f.instance, f.instanceErr
}

func (f *removeCmdFakeManager) RemoveServiceInstance(_ string) (bool, error) {
	return f.removedInstance, f.removeInstanceErr
}

func (f *removeCmdFakeManager) RemoveServiceCatalogEntry(_ string) (bool, error) {
	return f.removedCatalog, f.removeCatalogErr
}

func TestRemoveCmdEnsureRegisteredError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	wantErr := errors.New("boom")

	err := removeCmdEnsureRegistered(cmd, &removeCmdFakeManager{registeredErr: wantErr}, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service:") {
		t.Errorf("expected 'checking service:' error, got: %s", errBuf.String())
	}
}

func TestRemoveCmdConfirmIfRunningHistoryError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	wantErr := errors.New("boom")

	err := removeCmdConfirmIfRunning(cmd, &removeCmdFakeManager{historyErr: wantErr}, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service state:") {
		t.Errorf("expected 'checking service state:' error, got: %s", errBuf.String())
	}
}

func TestRemoveCmdRemoveInstanceGetError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	wantErr := errors.New("boom")

	err := removeCmdRemoveInstance(cmd, &removeCmdFakeManager{instanceErr: wantErr}, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service instance:") {
		t.Errorf("expected 'checking service instance:' error, got: %s", errBuf.String())
	}
}

func TestRemoveCmdRemoveInstanceRemoveError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	wantErr := errors.New("boom")
	fake := &removeCmdFakeManager{instance: &types.ServiceInstance{}, removeInstanceErr: wantErr}

	err := removeCmdRemoveInstance(cmd, fake, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "removing service instance:") {
		t.Errorf("expected 'removing service instance:' error, got: %s", errBuf.String())
	}
}

func TestRemoveCmdRemoveInstanceNotRemoved(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	fake := &removeCmdFakeManager{instance: &types.ServiceInstance{}, removedInstance: false}

	err := removeCmdRemoveInstance(cmd, fake, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "unable to remove service instance") {
		t.Errorf("expected 'unable to remove service instance' error, got: %s", errBuf.String())
	}
}

func TestRemoveCmdRemoveCatalogEntryError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	wantErr := errors.New("boom")

	err := removeCmdRemoveCatalogEntry(cmd, &removeCmdFakeManager{removeCatalogErr: wantErr}, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "removing service:") {
		t.Errorf("expected 'removing service:' error, got: %s", errBuf.String())
	}
}

func TestRemoveCmdRemoveCatalogEntryNotRemoved(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	err := removeCmdRemoveCatalogEntry(cmd, &removeCmdFakeManager{removedCatalog: false}, "svc")

	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "could not be removed") {
		t.Errorf("expected 'could not be removed' error, got: %s", errBuf.String())
	}
}
