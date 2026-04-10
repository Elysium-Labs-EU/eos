package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestAPIInfoOnlyRegisteredServiceCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	testFile := testutil.NewTestServiceConfigFile(t)
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
	if result.Config.Runtime.Path != "/path/to/node" {
		t.Errorf("expected runtime path to be '/path/to/node', got: %q", result.Config.Runtime.Path)
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
	cmd.SetArgs([]string{"info"})

	err := cmd.ExecuteContext(t.Context())

	if err == nil {
		t.Fatalf("expected error, got: %v\nerr output: %s", err, errBuf.String())
	}
	output := errBuf.String()

	if !strings.Contains(output, "Error: accepts 1 arg(s), received 0") {
		t.Errorf("expected info to show 'Error: accepts 1 arg(s), received 0', got: %s", output)
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
	if !strings.Contains(errResp.Error, "service not registered") {
		t.Errorf("expected error to contain 'service not registered', got: %q", errResp.Error)
	}
}
