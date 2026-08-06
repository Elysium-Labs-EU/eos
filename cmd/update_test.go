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
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"gopkg.in/yaml.v3"
)

// newYamlServiceFile writes a valid service.yaml into dir and returns its path.
func newYamlServiceFile(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("could not create dir %s: %v", dir, err)
	}
	data, err := yaml.Marshal(testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime()))
	if err != nil {
		t.Fatalf("failed to marshal service config: %v", err)
	}
	path := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}
	return path
}

func TestUpdateCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	firstPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v1"))
	cmd.SetArgs([]string{"add", firstPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: unexpected error: %v", err)
	}

	secondPath := newYamlServiceFile(t, filepath.Join(tempDir, "project-v2"))
	outBuf.Reset()
	cmd.SetArgs([]string{"update", "cms", secondPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("update: unexpected error: %v", err)
	}

	if !strings.Contains(outBuf.String(), "updated") {
		t.Errorf("expected 'updated' in output, got: %s", outBuf.String())
	}
}

// TestUpdateCommandServiceNotRegistered, TestUpdateCommandInvalidPath,
// TestUpdateCommandMissingArgs, and TestUpdateCommandTooManyArgs live in update_gaps_test.go.

func TestUpdateCommandIsRegisteredError(t *testing.T) {
	mgr := &mockMgr{
		isServiceRegistered: func(string) (bool, error) {
			return false, fmt.Errorf("registry unavailable")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"update", "svc", "/some/path"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "checking service") {
		t.Errorf("expected 'checking service' error, got: %s", errBuf.String())
	}
}

func TestUpdateCommandUpdateCatalogError(t *testing.T) {
	newPath := newYamlServiceFile(t, t.TempDir())
	mgr := &mockMgr{
		isServiceRegistered: func(string) (bool, error) { return true, nil },
		updateCatalogEntry: func(string, string, string) error {
			return fmt.Errorf("catalog update failed")
		},
	}
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"update", "svc", newPath})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "updating service") {
		t.Errorf("expected 'updating service' error, got: %s", errBuf.String())
	}
}
