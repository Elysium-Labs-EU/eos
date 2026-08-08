package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func writeEosConfigFile(t *testing.T, dir string, cfg *config.EosConfig) string {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal eos config: %v", err)
	}
	path := filepath.Join(dir, config.EosConfigFileName)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return path
}

func TestConfigShowCommandDefaults(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)
	cmd.SetArgs([]string{"config", "show"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("config show should not error, got: %v", err)
	}

	output := outBuf.String()
	for _, want := range []string{
		"not found", "Sinks", "(none)", "Telemetry", "enabled:", "false",
		"Health", "2000", "30000", "300 / 60000", "0.75 / 0.85 / 0.95",
		"Log", "10485760",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got: %s", want, output)
		}
	}
}

func TestConfigShowCommandWithFile(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	writeEosConfigFile(t, tempDir, &config.EosConfig{
		Sinks: map[string]types.LogSink{
			"prod-loki": {Type: "loki", Mode: "push", Address: "http://loki:3100"},
		},
		Telemetry: config.EosTelemetryConfig{
			Enable:   true,
			Endpoint: "http://otel:4317",
			Insecure: true,
		},
		Health: config.EosHealthConfig{
			CheckIntervalMs:     1000,
			MemSampleIntervalMs: 5000,
			Backoff:             config.EosBackoffConfig{BaseMs: 100, MaxMs: 2000},
			Memory: config.EosMemoryConfig{
				WarningThreshold:      0.5,
				SoftRestartThreshold:  0.6,
				ForceRestartThreshold: 0.7,
			},
		},
		Log: config.EosLogConfig{MaxFiles: 3, FileSizeLimitBytes: 1024},
	})

	cmd.SetArgs([]string{"config", "show"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("config show should not error, got: %v", err)
	}

	output := outBuf.String()
	for _, want := range []string{
		"loaded", "prod-loki", "loki", "http://loki:3100",
		"http://otel:4317", "1000", "5000", "100 / 2000", "0.50 / 0.60 / 0.70",
		"3", "1024",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got: %s", want, output)
		}
	}
}

func TestConfigShowCommandInvalidFile(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	path := filepath.Join(tempDir, config.EosConfigFileName)
	if err := os.WriteFile(path, []byte("sinks: [unclosed"), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	cmd.SetArgs([]string{"config", "show"})
	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	if !strings.Contains(errBuf.String(), "error") {
		t.Errorf("expected 'error' in output, got: %s", errBuf.String())
	}
}

func TestConfigShowCommandBaseDirError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	t.Setenv("EOS_BASE_DIR", "/tmp")

	cmd.SetArgs([]string{"config", "show"})
	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "resolving base dir") {
		t.Errorf("expected 'resolving base dir' in output, got: %s", output)
	}
}

func TestConfigInitCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	cmd.SetArgs([]string{"config", "init"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("config init should not error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "created") {
		t.Errorf("expected 'created' in output, got: %s", output)
	}
	if !strings.Contains(output, "eos config show") {
		t.Errorf("expected next-step hint in output, got: %s", output)
	}

	written, err := os.ReadFile(filepath.Join(tempDir, config.EosConfigFileName))
	if err != nil {
		t.Fatalf("reading written config.yaml: %v", err)
	}
	content := string(written)
	for _, want := range []string{"# sinks:", "# telemetry:", "checkIntervalMs: 2000", "fileSizeLimitBytes: 10485760"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected written config.yaml to contain %q, got: %s", want, content)
		}
	}
}

func TestConfigInitCommandAlreadyExistsNoForce(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	path := filepath.Join(tempDir, config.EosConfigFileName)
	if err := os.WriteFile(path, []byte("sentinel-content\n"), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	cmd.SetArgs([]string{"config", "init"})
	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "already exists") {
		t.Errorf("expected 'already exists' in output, got: %s", errBuf.String())
	}

	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	if string(unchanged) != "sentinel-content\n" {
		t.Errorf("expected file to be left unchanged, got: %s", string(unchanged))
	}
}

func TestConfigInitCommandForce(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	path := filepath.Join(tempDir, config.EosConfigFileName)
	if err := os.WriteFile(path, []byte("sentinel-content\n"), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	cmd.SetArgs([]string{"config", "init", "--force"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("config init --force should not error, got: %v", err)
	}
	if !strings.Contains(outBuf.String(), "created") {
		t.Errorf("expected 'created' in output, got: %s", outBuf.String())
	}

	overwritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	if !strings.Contains(string(overwritten), "checkIntervalMs: 2000") {
		t.Errorf("expected overwritten config.yaml to contain scaffold content, got: %s", string(overwritten))
	}
}

func TestConfigInitCommandBaseDirError(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	t.Setenv("EOS_BASE_DIR", "/tmp")

	cmd.SetArgs([]string{"config", "init"})
	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "creating eos directory") {
		t.Errorf("expected 'creating eos directory' in output, got: %s", output)
	}
}

func TestConfigValidateCommandValid(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	path := writeEosConfigFile(t, tempDir, &config.EosConfig{
		Sinks: map[string]types.LogSink{
			"prod-loki": {Type: "loki", Mode: "push", Address: "http://loki:3100"},
		},
		Health: config.EosHealthConfig{
			CheckIntervalMs:     2000,
			MemSampleIntervalMs: 30000,
			Backoff:             config.EosBackoffConfig{BaseMs: 300, MaxMs: 60000},
			Memory: config.EosMemoryConfig{
				WarningThreshold:      0.75,
				SoftRestartThreshold:  0.85,
				ForceRestartThreshold: 0.95,
			},
		},
		Log: config.EosLogConfig{MaxFiles: 5, FileSizeLimitBytes: 10 * 1024 * 1024},
	})

	cmd.SetArgs([]string{"config", "validate"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("config validate should not error, got: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "valid") {
		t.Errorf("expected 'valid' in output, got: %s", output)
	}
	if !strings.Contains(output, path) {
		t.Errorf("expected file path in output, got: %s", output)
	}
}

func TestConfigValidateCommandMissingFile(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)
	cmd.SetArgs([]string{"config", "validate"})

	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "does not exist") {
		t.Errorf("expected 'does not exist' in output, got: %s", errBuf.String())
	}
}

func TestConfigValidateCommandInvalidContent(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)
	writeEosConfigFile(t, tempDir, &config.EosConfig{
		Health: config.EosHealthConfig{
			CheckIntervalMs: 2000,
			Backoff:         config.EosBackoffConfig{BaseMs: 300, MaxMs: 60000},
			Memory: config.EosMemoryConfig{
				// Deliberately non-ascending to trigger Validate()'s ordering check.
				WarningThreshold:      0.9,
				SoftRestartThreshold:  0.5,
				ForceRestartThreshold: 0.95,
			},
		},
	})

	cmd.SetArgs([]string{"config", "validate"})
	if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}

	output := errBuf.String()
	if !strings.Contains(output, "invalid") {
		t.Errorf("expected 'invalid' in output, got: %s", output)
	}
	if !strings.Contains(output, "ascending") {
		t.Errorf("expected ascending-order error detail in output, got: %s", output)
	}
}

func TestSortedSinkNames(t *testing.T) {
	sinks := map[string]types.LogSink{
		"zeta":  {Type: "file"},
		"alpha": {Type: "loki"},
		"mid":   {Type: "sse"},
	}
	got := sortedSinkNames(sinks)
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("expected %d names, got %d: %v", len(want), len(got), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("expected got[%d]=%q, got %q (full: %v)", i, name, got[i], got)
		}
	}
}

func TestRenderConfigInitFile(t *testing.T) {
	content, err := renderConfigInitFile()
	if err != nil {
		t.Fatalf("renderConfigInitFile should not error, got: %v", err)
	}
	for _, want := range []string{
		"checkIntervalMs: 2000",
		"memSampleIntervalMs: 30000",
		"baseMs: 300",
		"maxMs: 60000",
		"warningThreshold: 0.75",
		"softRestartThreshold: 0.85",
		"forceRestartThreshold: 0.95",
		"maxFiles: 5",
		"fileSizeLimitBytes: 10485760",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected rendered content to contain %q, got: %s", want, content)
		}
	}
}
