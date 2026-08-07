package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
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

// apiInfoFakeManager implements manager.ServiceManager by embedding a nil
// interface and overriding only the methods apiInfoRunE's helpers call, so
// error branches can be exercised without a real DB-backed manager.
type apiInfoFakeManager struct {
	manager.ServiceManager
	catalogErr      error
	instanceErr     error
	processErr      error
	logPathErr      error
	errorLogPathErr error
	instance        *types.ServiceInstance
	processEntry    *types.ProcessHistory
	logPath         *string
	errorLogPath    *string
	catalogEntry    types.ServiceCatalogEntry
}

func (f *apiInfoFakeManager) GetServiceCatalogEntry(_ string) (types.ServiceCatalogEntry, error) {
	return f.catalogEntry, f.catalogErr
}

func (f *apiInfoFakeManager) GetServiceInstance(_ string) (*types.ServiceInstance, error) {
	return f.instance, f.instanceErr
}

func (f *apiInfoFakeManager) GetMostRecentProcessHistoryEntry(_ string) (*types.ProcessHistory, error) {
	return f.processEntry, f.processErr
}

func (f *apiInfoFakeManager) GetServiceLogFilePath(_ string, errorLog bool) (*string, error) {
	if errorLog {
		return f.errorLogPath, f.errorLogPathErr
	}
	return f.logPath, f.logPathErr
}

func TestAPIInfoLoadRegisteredServiceErrors(t *testing.T) {
	if _, err := apiInfoLoadRegisteredService(&apiInfoFakeManager{catalogErr: manager.ErrServiceNotRegistered}, "svc"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}

	wantErr := errors.New("boom")
	if _, err := apiInfoLoadRegisteredService(&apiInfoFakeManager{catalogErr: wantErr}, "svc"); err == nil || !strings.Contains(err.Error(), "getting registered service") {
		t.Errorf("expected wrapped 'getting registered service' error, got: %v", err)
	}
}

func TestAPIInfoLoadConfigError(t *testing.T) {
	entry := types.ServiceCatalogEntry{DirectoryPath: t.TempDir(), ConfigFileName: "missing.yaml"}
	if _, err := apiInfoLoadConfig(&entry); err == nil || !strings.Contains(err.Error(), "loading service config") {
		t.Errorf("expected wrapped 'loading service config' error, got: %v", err)
	}
}

func TestAPIInfoLoadServiceInstanceError(t *testing.T) {
	wantErr := errors.New("boom")
	if _, err := apiInfoLoadServiceInstance(&apiInfoFakeManager{instanceErr: wantErr}, "svc"); err == nil || !strings.Contains(err.Error(), "getting service instance") {
		t.Errorf("expected wrapped 'getting service instance' error, got: %v", err)
	}

	instance, err := apiInfoLoadServiceInstance(&apiInfoFakeManager{instanceErr: manager.ErrServiceNotRunning}, "svc")
	if err != nil {
		t.Errorf("expected no error for ErrServiceNotRunning, got: %v", err)
	}
	if instance != nil {
		t.Errorf("expected nil instance, got: %v", instance)
	}
}

func TestAPIInfoLoadProcessEntryError(t *testing.T) {
	wantErr := errors.New("boom")
	if _, err := apiInfoLoadProcessEntry(&apiInfoFakeManager{processErr: wantErr}, "svc"); err == nil || !strings.Contains(err.Error(), "getting process history") {
		t.Errorf("expected wrapped 'getting process history' error, got: %v", err)
	}

	entry, err := apiInfoLoadProcessEntry(&apiInfoFakeManager{processErr: manager.ErrProcessNotFound}, "svc")
	if err != nil {
		t.Errorf("expected no error for ErrProcessNotFound, got: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil process entry, got: %v", entry)
	}
}

func TestAPIInfoLoadLogPathsErrors(t *testing.T) {
	wantErr := errors.New("boom")

	if _, _, err := apiInfoLoadLogPaths(&apiInfoFakeManager{logPathErr: wantErr}, "svc", true); err == nil || !strings.Contains(err.Error(), "getting log path") {
		t.Errorf("expected wrapped 'getting log path' error, got: %v", err)
	}

	if _, _, err := apiInfoLoadLogPaths(&apiInfoFakeManager{errorLogPathErr: wantErr}, "svc", true); err == nil || !strings.Contains(err.Error(), "getting error log path") {
		t.Errorf("expected wrapped 'getting error log path' error, got: %v", err)
	}

	if _, _, err := apiInfoLoadLogPaths(&apiInfoFakeManager{logPathErr: wantErr, errorLogPathErr: wantErr}, "svc", false); err != nil {
		t.Errorf("expected no error when instance isn't running, got: %v", err)
	}
}

// apiInfoWriteValidConfig writes a minimal valid service.yaml under dir and
// returns a catalog entry pointing at it, for exercising apiInfoRunE's
// downstream error branches once config loading itself has succeeded.
func apiInfoWriteValidConfig(t *testing.T, dir string) types.ServiceCatalogEntry {
	t.Helper()
	testFile := testutil.NewTestServiceConfigFile(t)
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}
	return types.ServiceCatalogEntry{Name: testFile.Name, DirectoryPath: dir, ConfigFileName: "service.yaml"}
}

func apiInfoNewTestCmd(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	return cmd, &errBuf
}

func TestAPIInfoRunEConfigLoadError(t *testing.T) {
	cmd, errBuf := apiInfoNewTestCmd(t)
	entry := types.ServiceCatalogEntry{DirectoryPath: t.TempDir(), ConfigFileName: "missing.yaml"}
	mgr := &apiInfoFakeManager{catalogEntry: entry}

	if err := apiInfoRunE(cmd, "svc", mgr); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errBuf.String(), "loading service config") {
		t.Errorf("expected stderr to mention 'loading service config', got: %s", errBuf.String())
	}
}

func TestAPIInfoRunEServiceInstanceError(t *testing.T) {
	cmd, errBuf := apiInfoNewTestCmd(t)
	entry := apiInfoWriteValidConfig(t, t.TempDir())
	mgr := &apiInfoFakeManager{catalogEntry: entry, instanceErr: errors.New("boom")}

	if err := apiInfoRunE(cmd, entry.Name, mgr); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errBuf.String(), "getting service instance") {
		t.Errorf("expected stderr to mention 'getting service instance', got: %s", errBuf.String())
	}
}

func TestAPIInfoRunEProcessHistoryError(t *testing.T) {
	cmd, errBuf := apiInfoNewTestCmd(t)
	entry := apiInfoWriteValidConfig(t, t.TempDir())
	mgr := &apiInfoFakeManager{catalogEntry: entry, processErr: errors.New("boom")}

	if err := apiInfoRunE(cmd, entry.Name, mgr); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errBuf.String(), "getting process history") {
		t.Errorf("expected stderr to mention 'getting process history', got: %s", errBuf.String())
	}
}

func TestAPIInfoRunELogPathError(t *testing.T) {
	cmd, errBuf := apiInfoNewTestCmd(t)
	entry := apiInfoWriteValidConfig(t, t.TempDir())
	instance := &types.ServiceInstance{}
	mgr := &apiInfoFakeManager{catalogEntry: entry, instance: instance, logPathErr: errors.New("boom")}

	if err := apiInfoRunE(cmd, entry.Name, mgr); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errBuf.String(), "getting log path") {
		t.Errorf("expected stderr to mention 'getting log path', got: %s", errBuf.String())
	}
}

func TestAPIInfoOnlyRegisteredServiceCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

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
		t.Fatalf("failed to marshal test config: %v", err)
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
		t.Fatalf("failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add command should not return an error, got: %v\n", err)
	}
	outBuf.Reset()

	cmd.SetArgs([]string{"api", "info", "cms"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("info command should not return an error, got: %v", err)
	}

	var result apiInfoResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf.String())), &result); err != nil {
		t.Fatalf("failed to unmarshal info output: %v\noutput: %s", err, outBuf.String())
	}

	if result.Name != "cms" {
		t.Errorf("expected name to be 'cms', got: %q", result.Name)
	}
	if result.Path != fullDirPath {
		t.Errorf("expected path to be %q, got: %q", fullDirPath, result.Path)
	}
	if result.Config == nil {
		t.Fatal("expected config to be present")
		return
	}
	if result.Config.Command != "/home/user/start-script.sh" {
		t.Errorf("expected command to be '/home/user/start-script.sh', got: %q", result.Config.Command)
	}
	if result.Config.Port != 1337 {
		t.Errorf("expected port to be 1337, got: %d", result.Config.Port)
	}
	if result.Config.Runtime.Type != "nodejs" {
		t.Errorf("expected runtime type to be 'nodejs', got: %q", result.Config.Runtime.Type)
	}
	if result.Config.Runtime.Path != runtimeDir {
		t.Errorf("expected runtime path to be %q, got: %q", runtimeDir, result.Config.Runtime.Path)
	}
}

func TestAPIInfoOnlyRegisteredServiceIncompleteCommand(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupAPICmd(t)

	yamlData, err := yaml.Marshal(&types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
		Port:    1337,
	})
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
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
		t.Fatalf("failed to write the service.yaml file, got: %v", err)
	}

	cmd.SetArgs([]string{"add", fullPath})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("add command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}
	outBuf.Reset()

	cmd.SetArgs([]string{"api", "info", "cms"})
	err = cmd.ExecuteContext(t.Context())
	if err != nil {
		t.Fatalf("info command should not return an error, got: %v\nerr output: %s", err, errBuf.String())
	}

	var result apiInfoResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf.String())), &result); err != nil {
		t.Fatalf("failed to unmarshal info output: %v\noutput: %s", err, outBuf.String())
	}

	if result.Config == nil {
		t.Fatal("expected config to be present")
		return
	}
	if result.Config.Command != "/home/user/start-script.sh" {
		t.Errorf("expected command to be '/home/user/start-script.sh', got: %q", result.Config.Command)
	}
	if result.Config.Runtime.Type != "" {
		t.Errorf("expected runtime type to be empty for incomplete config, got: %q", result.Config.Runtime.Type)
	}
	if result.Config.Runtime.Path != "" {
		t.Errorf("expected runtime path to be empty for incomplete config, got: %q", result.Config.Runtime.Path)
	}
}

func TestAPIInfoInvalidNumberArgumentsCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)
	cmd.SetArgs([]string{"api", "info"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatalf("expected error, got: %v\nerr output: %s", err, errBuf.String())
		return
	}

	// api info has SilenceErrors/SilenceUsage set and RunE never runs on an
	// arg-count failure, so cobra's error is only returned, never written to
	// stderr (unlike the JSON error contract used for runtime errors).
	if !strings.Contains(err.Error(), "accepts 1 arg(s), received 0") {
		t.Errorf("expected error to be 'accepts 1 arg(s), received 0', got: %v", err)
	}
	if errBuf.String() != "" {
		t.Errorf("expected no stderr output, got: %s", errBuf.String())
	}
}

func TestAPIInfoNonExistentServiceCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)
	cmd.SetArgs([]string{"api", "info", "cms"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatalf("expected error, got: %v\nerr output: %s", err, errBuf.String())
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(errBuf.String())), &errResp); jsonErr != nil {
		t.Fatalf("failed to unmarshal error output: %v\noutput: %s", jsonErr, errBuf.String())
	}
	if !strings.Contains(errResp.Error, "not found") {
		t.Errorf("expected error to contain 'not found', got: %q", errResp.Error)
	}
}
