package cmd

import (
	"bytes"
	"errors"
	"fmt"
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

func TestRemoveCommandIsRegisteredError(t *testing.T) {
	mgr := &mockMgr{
		isServiceRegistered: func(string) (bool, error) {
			return false, fmt.Errorf("registry unavailable")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "svc"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service") {
		t.Errorf("expected 'checking service' error, got: %s", errBuf.String())
	}
}

func TestRemoveCommandRemoveInstanceError(t *testing.T) {
	mgr := &mockMgr{
		isServiceRegistered: func(string) (bool, error) { return true, nil },
		getMostRecentProcess: func(string) (*types.ProcessHistory, error) {
			return nil, manager.ErrProcessNotFound
		},
		getServiceInstance: func(string) (*types.ServiceInstance, error) {
			return &types.ServiceInstance{}, nil
		},
		removeInstance: func(string) (bool, error) {
			return false, fmt.Errorf("instance remove failed")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "svc"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "removing service instance") {
		t.Errorf("expected 'removing service instance' error, got: %s", errBuf.String())
	}
}

func TestRemoveCommandRemoveCatalogError(t *testing.T) {
	mgr := &mockMgr{
		isServiceRegistered: func(string) (bool, error) { return true, nil },
		getMostRecentProcess: func(string) (*types.ProcessHistory, error) {
			return nil, manager.ErrProcessNotFound
		},
		getServiceInstance: func(string) (*types.ServiceInstance, error) {
			return nil, manager.ErrServiceNotRunning
		},
		removeCatalogEntry: func(string) (bool, error) {
			return false, fmt.Errorf("catalog remove failed")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"remove", "svc"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "removing service:") {
		t.Errorf("expected 'removing service:' error, got: %s", errBuf.String())
	}
}
