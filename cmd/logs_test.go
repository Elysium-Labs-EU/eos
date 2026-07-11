package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
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
	cmd, outBuf, _, tempDir := setupCmd(t)

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

	output := outBuf.String()

	if strings.Contains(output, "An error occurred during getting the log file, got") {
		t.Errorf("Log file should be found")
	}
	if !strings.Contains(output, "showing logs for") {
		t.Errorf("Expected logs to be shown, got: %v", output)
	}
}

func TestLogsNeverRanServiceCommand(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command failed: %v", err)
	}

	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false"})
	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "has never been started") {
		t.Errorf("Expected 'has never been started', got: %s", output)
	}
}

func TestLogsNonExistingServiceCommand(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	cmd.SetArgs([]string{"logs", "cms", "--follow=false"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	output := errBuf.String()

	if !strings.Contains(output, "error cms is not registered") {
		t.Errorf("Expected status to show 'error cms is not registered', got: %s", output)
	}
}

func TestRenderServiceLogLine_plainText(t *testing.T) {
	got := renderServiceLogLine("not json at all", "")
	if got != "not json at all" {
		t.Errorf("expected passthrough for non-JSON, got: %q", got)
	}
}

func TestRenderServiceLogLine_infoJSON(t *testing.T) {
	line := `{"time":"2025-01-01T10:00:00.000000000Z","level":"INFO","msg":"server started","source":"api"}`
	got := renderServiceLogLine(line, "")
	if !strings.Contains(got, "server started") {
		t.Errorf("expected msg in output, got: %q", got)
	}
	if !strings.Contains(got, "api") {
		t.Errorf("expected source in output, got: %q", got)
	}
}

func TestRenderServiceLogLine_errorLevel(t *testing.T) {
	line := `{"time":"2025-01-01T10:00:00.000000000Z","level":"ERROR","msg":"crash","source":"worker"}`
	got := renderServiceLogLine(line, "")
	if !strings.Contains(got, "ERROR") {
		t.Errorf("expected ERROR level in output, got: %q", got)
	}
	if !strings.Contains(got, "crash") {
		t.Errorf("expected msg in output, got: %q", got)
	}
}

func TestRenderServiceLogLine_noSource(t *testing.T) {
	line := `{"time":"2025-01-01T10:00:00.000000000Z","level":"INFO","msg":"hello"}`
	got := renderServiceLogLine(line, "")
	if !strings.Contains(got, "hello") {
		t.Errorf("expected msg in output, got: %q", got)
	}
	if !strings.Contains(got, "info") {
		t.Errorf("expected level-as-source fallback, got: %q", got)
	}
}

func TestRenderServiceLogLine_errorField(t *testing.T) {
	line := `{"time":"2025-01-01T10:00:00.000000000Z","level":"WARN","msg":"sink plugin exited (mytype/mysvc)","error":"\"eos-sink-mytype\" not found on PATH"}`
	got := renderServiceLogLine(line, "")
	if !strings.Contains(got, "sink plugin exited") {
		t.Errorf("expected msg in output, got: %q", got)
	}
	if !strings.Contains(got, "not found on PATH") {
		t.Errorf("expected error field appended to output, got: %q", got)
	}
}

func TestRenderServiceLogLine_noErrorField(t *testing.T) {
	line := `{"time":"2025-01-01T10:00:00.000000000Z","level":"WARN","msg":"something happened"}`
	got := renderServiceLogLine(line, "")
	if !strings.Contains(got, "something happened") {
		t.Errorf("expected msg in output, got: %q", got)
	}
	if strings.Contains(got, ":") && strings.HasSuffix(got, ": ") {
		t.Errorf("should not append colon when no error field, got: %q", got)
	}
}

func TestRenderServiceLogLine_withStreamLabel(t *testing.T) {
	line := `{"time":"2025-01-01T10:00:00.000000000Z","level":"WARN","msg":"sink exited","error":"binary not found"}`
	got := renderServiceLogLine(line, "err")
	if !strings.HasPrefix(got, "err ") {
		t.Errorf("expected stream label prefix, got: %q", got)
	}
	if !strings.Contains(got, "binary not found") {
		t.Errorf("expected error field in output, got: %q", got)
	}
}
