package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"eos/internal/types"
)

func writeServiceYAML(t *testing.T, tempDir string, cfg *types.ServiceConfig) string {
	t.Helper()
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	dir := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Could not create test-project directory: %v", err)
	}

	path := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(path, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write service.yaml: %v", err)
	}

	return path
}

func TestLogsCommand(t *testing.T) {
	cmd, buf, tempDir := setupCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"run", cfg.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Run command should not return an error, got : %v", err)
	}

	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Status command should not return an error, got : %v", err)
	}

	output := buf.String()

	if strings.Contains(output, "An error occurred during getting the log file, got") {
		t.Errorf("Log file should be found")
	}
	if !strings.Contains(output, "showing logs for") {
		t.Errorf("Expected logs to be shown, got: %v", output)
	}
}

func TestLogsNeverRanServiceCommand(t *testing.T) {
	cmd, buf, tempDir := setupCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command failed: %v", err)
	}

	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Logs command failed: %v", err)
	}

	if !strings.Contains(buf.String(), "has never been started") {
		t.Errorf("Expected 'has never been started', got: %s", buf.String())
	}
}

func TestLogsNonExistingServiceCommand(t *testing.T) {
	cmd, buf, _ := setupCmd(t)

	cmd.SetArgs([]string{"logs", "cms", "--follow=false"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Logs command should not return an error, got : %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "error cms is not registered") {
		t.Errorf("Expected status to show 'error cms is not registered', got: %s", output)
	}
}
