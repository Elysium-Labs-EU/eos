package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// TODO: Rewrite
func TestLoadServiceConfig(t *testing.T) {
	runtime := types.Runtime{
		Type: "nodejs",
	}
	expectedConfig := &types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
		Port:    1337,
		Runtime: runtime,
	}

	yamlData, err := yaml.Marshal(expectedConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")

	err = os.WriteFile(configFile, yamlData, 0644)
	if err != nil {
		t.Fatalf("LoadingProjectCofnig should not error: %v", err)
	}

	config, err := LoadServiceConfig(configFile)
	if err != nil {
		t.Fatalf("LoadServiceConfig should not error: %v", err)
	}
	if config.Name != "cms" {
		t.Errorf("Expected name 'cms', got %s", config.Name)
	}
	if config.Command != "/home/user/start-script.sh" {
		t.Errorf("Expected command '/home/user/start-script.sh' got '%s'", config.Command)
	}
	if config.Port != 1337 {
		t.Errorf("Expected port 1337, got %d", config.Port)
	}
}

// TODO: Rewrite
func TestLoadServiceConfigWithOptionalFields(t *testing.T) {
	runtime := types.Runtime{
		Type: "nodejs",
	}
	expectedConfig := &types.ServiceConfig{
		Name:    "website",
		Command: "pnpm start",
		Port:    3000,
		Runtime: runtime,
	}
	yamlData, err := yaml.Marshal(expectedConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")

	err = os.WriteFile(configFile, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	config, err := LoadServiceConfig(configFile)
	if err != nil {
		t.Fatalf("LoadServiceConfig should not error: %v", err)
	}
	if config.Port != 3000 {
		t.Errorf("Expected port '3000' got '%d'", config.Port)
	}
}

func TestLoadServiceConfigFileNotFound(t *testing.T) {
	_, err := LoadServiceConfig("non-existent-file.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent config file")
	}
}

func TestValidateRuntimeBinaryPython(t *testing.T) {
	for _, runtimeType := range []string{"python", "python3"} {
		t.Run(runtimeType, func(t *testing.T) {
			rt := types.Runtime{Type: runtimeType}
			err := ValidateRuntimeBinary(rt)
			// python/python3 may not be present in all CI envs; accept both outcomes.
			// What matters: no panic and the error message is meaningful if present.
			if err != nil && !strings.Contains(err.Error(), "python") {
				t.Errorf("expected python-related error message, got: %v", err)
			}
		})
	}
}

func TestValidateRuntimeBinaryPythonNotFound(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	for _, runtimeType := range []string{"python", "python3"} {
		t.Run(runtimeType, func(t *testing.T) {
			rt := types.Runtime{Type: runtimeType}
			err := ValidateRuntimeBinary(rt)
			if err == nil {
				t.Errorf("expected error when python not in PATH, got nil")
			}
			if !strings.Contains(err.Error(), "python") {
				t.Errorf("expected python-related error message, got: %v", err)
			}
		})
	}
}

func TestValidateLogSink_missingType(t *testing.T) {
	errs := ValidateLogSink(&types.LogSink{})
	if len(errs) == 0 {
		t.Error("expected error for missing type")
	}
}

func TestValidateLogSink_binaryOnPath(t *testing.T) {
	// "sh" is guaranteed to exist; name it so eos-sink-sh would be looked up but exec overrides
	sink := types.LogSink{Type: "test", Exec: "sh"}
	errs := ValidateLogSink(&sink)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid exec, got: %v", errs)
	}
}

func TestValidateLogSink_execNotFound(t *testing.T) {
	sink := types.LogSink{Type: "test", Exec: "/nonexistent/eos-sink-test"}
	errs := ValidateLogSink(&sink)
	if len(errs) == 0 {
		t.Error("expected error for non-existent exec path")
	}
}

func TestValidateLogSink_pluginNotOnPath(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	sink := types.LogSink{Type: "datadog"}
	errs := ValidateLogSink(&sink)
	if len(errs) == 0 {
		t.Error("expected error when eos-sink-datadog not on PATH")
		return
	}
	if !strings.Contains(errs[0].Error(), "eos-sink-datadog") {
		t.Errorf("expected binary name in error, got: %v", errs[0])
	}
}

func TestValidateLogSink_negativeBufferSize(t *testing.T) {
	sink := types.LogSink{Type: "test", Exec: "sh", BufferSize: -1}
	errs := ValidateLogSink(&sink)
	if len(errs) == 0 {
		t.Error("expected error for negative buffer_size")
	}
}

func TestValidateLogSink_invalidStream(t *testing.T) {
	sink := types.LogSink{Type: "test", Exec: "sh", Streams: []string{"stdout", "invalid"}}
	errs := ValidateLogSink(&sink)
	if len(errs) == 0 {
		t.Error("expected error for invalid stream value")
	}
}

func TestValidateLogSink_validStreams(t *testing.T) {
	sink := types.LogSink{Type: "test", Exec: "sh", Streams: []string{"stdout", "stderr"}}
	errs := ValidateLogSink(&sink)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid streams, got: %v", errs)
	}
}

func TestNewServiceCatalogEntry(t *testing.T) {
	_, err := NewServiceCatalogEntry("website", "./test-files", "service.yaml")
	if err != nil {
		t.Errorf("TestNewServiceCatalogEntry should not error: %v", err)
	}
}

func TestNewServiceCatalogEntryWithEmptyName(t *testing.T) {
	_, err := NewServiceCatalogEntry("", "./test-files", "service.yaml")
	if err == nil {
		t.Errorf("TestNewServiceCatalogEntry should error on empty name")
	}
}

func TestNewServiceCatalogEntryWithEmptyPath(t *testing.T) {
	_, err := NewServiceCatalogEntry("website", "", "service.yaml")
	if err == nil {
		t.Errorf("TestNewServiceCatalogEntry should error on empty path")
	}
}

func TestNewServiceCatalogEntryWithEmptyConfigFile(t *testing.T) {
	_, err := NewServiceCatalogEntry("website", "./test-files", "")
	if err == nil {
		t.Errorf("TestNewServiceCatalogEntry should error on empty configFile")
	}
}
