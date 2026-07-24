package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// TODO: Rewrite: marshals a ServiceConfig then unmarshals it via LoadServiceConfig,
// which mostly round-trips yaml.v3 rather than parsing a hand-written YAML file;
// replace with a literal YAML fixture string.
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
		t.Fatalf("writing test config file should not error: %v", err)
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

// TODO: Rewrite: name promises optional fields but only sets Name/Command/Port,
// same as TestLoadServiceConfig; should exercise EnvFile, LogSinks, MemoryLimitMb instead.
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

func TestResolveLogSinks_NoRefs(t *testing.T) {
	resolved, err := ResolveLogSinks("svc", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected no resolved sinks, got %v", resolved)
	}
}

func TestResolveLogSinks_InlineOnly(t *testing.T) {
	refs := []types.LogSinkRef{{Inline: &types.LogSink{Type: "file", Address: "/var/log/eos"}}}
	resolved, err := ResolveLogSinks("svc", refs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Type != "file" {
		t.Errorf("expected inline sink to pass through, got %+v", resolved)
	}
}

func TestResolveLogSinks_NameReference(t *testing.T) {
	registry := map[string]types.LogSink{
		"prod-loki": {Type: "loki", Address: "http://loki:3100"},
	}
	refs := []types.LogSinkRef{{Name: "prod-loki"}}
	resolved, err := ResolveLogSinks("svc", refs, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Type != "loki" || resolved[0].Address != "http://loki:3100" {
		t.Errorf("expected resolved registry sink, got %+v", resolved)
	}
}

func TestResolveLogSinks_MixedInlineAndName(t *testing.T) {
	registry := map[string]types.LogSink{
		"local-file": {Type: "file", Address: "/var/log/eos"},
	}
	refs := []types.LogSinkRef{
		{Name: "local-file"},
		{Inline: &types.LogSink{Type: "otlp", Address: "otel:4317"}},
	}
	resolved, err := ResolveLogSinks("svc", refs, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 2 || resolved[0].Type != "file" || resolved[1].Type != "otlp" {
		t.Errorf("expected both sinks resolved in order, got %+v", resolved)
	}
}

func TestResolveLogSinks_UnknownName(t *testing.T) {
	registry := map[string]types.LogSink{
		"prod-loki":  {Type: "loki"},
		"local-file": {Type: "file"},
	}
	refs := []types.LogSinkRef{{Name: "prod-lokk"}}
	_, err := ResolveLogSinks("api", refs, registry)
	if err == nil {
		t.Fatal("expected error for unknown sink name, got nil")
		return
	}
	want := `service 'api': log_sinks[0]: unknown sink "prod-lokk" — registered: [local-file, prod-loki]`
	if err.Error() != want {
		t.Errorf("unexpected error message:\nwant: %s\ngot:  %s", want, err.Error())
	}
}

func TestResolveLogSinks_UnknownNameEmptyRegistry(t *testing.T) {
	refs := []types.LogSinkRef{{Name: "prod-loki"}}
	_, err := ResolveLogSinks("api", refs, nil)
	if err == nil {
		t.Fatal("expected error for unknown sink name, got nil")
		return
	}
	want := `service 'api': log_sinks[0]: unknown sink "prod-loki" — registered: []`
	if err.Error() != want {
		t.Errorf("unexpected error message:\nwant: %s\ngot:  %s", want, err.Error())
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

func TestValidateServiceConfig_emptyPath(t *testing.T) {
	_, errs := ValidateServiceConfig("")
	if len(errs) == 0 {
		t.Fatal("expected error for empty configFilePath")
	}
}

func TestValidateServiceConfig_fileNotFound(t *testing.T) {
	_, errs := ValidateServiceConfig("non-existent-file.yaml")
	if len(errs) == 0 {
		t.Fatal("expected error for non-existent file")
	}
}

func TestValidateServiceConfig_invalidYaml(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(configFile, []byte("not: valid: yaml: [["), 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	_, errs := ValidateServiceConfig(configFile)
	if len(errs) == 0 {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestValidateServiceConfig_missingRequiredFields(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(configFile, []byte("port: 3000\n"), 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	_, errs := ValidateServiceConfig(configFile)
	if len(errs) < 2 {
		t.Fatalf("expected errors for missing name and command, got: %v", errs)
	}
}

func TestValidateServiceConfig_badRuntimeAndLogSink(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	config := &types.ServiceConfig{
		Name:    "svc",
		Command: "./start.sh",
		Runtime: types.Runtime{Type: "node"},
		LogSinks: []types.LogSinkRef{
			{Inline: &types.LogSink{}},
		},
	}
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	if err := os.WriteFile(configFile, yamlData, 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	_, errs := ValidateServiceConfig(configFile)
	if len(errs) < 2 {
		t.Fatalf("expected errors for runtime and log sink, got: %v", errs)
	}
	foundRuntime, foundLogSink := false, false
	for _, e := range errs {
		if strings.Contains(e.Error(), "runtime:") {
			foundRuntime = true
		}
		if strings.Contains(e.Error(), "log_sinks[0]:") {
			foundLogSink = true
		}
	}
	if !foundRuntime {
		t.Errorf("expected a runtime error, got: %v", errs)
	}
	if !foundLogSink {
		t.Errorf("expected a log_sinks[0] error, got: %v", errs)
	}
}

func TestValidateServiceConfig_valid(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	config := &types.ServiceConfig{
		Name:    "svc",
		Command: "./start.sh",
	}
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	if err := os.WriteFile(configFile, yamlData, 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	cfg, errs := ValidateServiceConfig(configFile)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}
	if cfg == nil || cfg.Name != "svc" {
		t.Errorf("expected loaded config with name 'svc', got: %+v", cfg)
	}
}

func TestValidateCronRestart_empty(t *testing.T) {
	if err := ValidateCronRestart(""); err != nil {
		t.Errorf("expected no error for empty cron_restart, got: %v", err)
	}
}

func TestValidateCronRestart_valid(t *testing.T) {
	if err := ValidateCronRestart("0 3 * * *"); err != nil {
		t.Errorf("expected no error for valid cron_restart, got: %v", err)
	}
}

func TestValidateCronRestart_invalid(t *testing.T) {
	if err := ValidateCronRestart("not a cron expression"); err == nil {
		t.Error("expected an error for invalid cron_restart, got nil")
	}
}

func TestValidateServiceConfig_invalidCronRestart(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	config := &types.ServiceConfig{
		Name:        "svc",
		Command:     "./start.sh",
		CronRestart: "not a cron expression",
	}
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	if err := os.WriteFile(configFile, yamlData, 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	_, errs := ValidateServiceConfig(configFile)
	if len(errs) == 0 {
		t.Fatal("expected an error for invalid cron_restart, got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "cron_restart:") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a cron_restart error, got: %v", errs)
	}
}

func TestLoadServiceConfigWithCronRestart(t *testing.T) {
	expectedConfig := &types.ServiceConfig{
		Name:        "website",
		Command:     "pnpm start",
		CronRestart: "0 3 * * *",
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
	if config.CronRestart != "0 3 * * *" {
		t.Errorf("Expected cron_restart '0 3 * * *' got %q", config.CronRestart)
	}
}

func TestValidateServiceName(t *testing.T) {
	tests := []struct {
		name    string
		svcName string
		wantErr bool
	}{
		{"simple lowercase", "cms", false},
		{"with dash", "my-service", false},
		{"with underscore", "my_service", false},
		{"alphanumeric", "svc123", false},
		{"empty", "", true},
		{"path traversal unix", "../../pwned", true},
		{"path traversal windows-style", "..\\..\\pwned", true},
		{"absolute path", "/etc/passwd", true},
		{"nested separator", "foo/bar", true},
		{"dot only", "..", true},
		{"single dot", ".", true},
		{"leading dot hidden file", ".hidden", true},
		{"contains space", "my service", true},
		{"contains null byte", "svc\x00", true},
		{"too long", strings.Repeat("a", maxServiceNameLength+1), true},
		{"exactly max length", strings.Repeat("a", maxServiceNameLength), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceName(tt.svcName)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateServiceName(%q) = nil, want error", tt.svcName)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateServiceName(%q) = %v, want no error", tt.svcName, err)
			}
		})
	}
}

func TestLoadServiceConfig_pathTraversalName(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(configFile, []byte("name: \"../../pwned\"\ncommand: \"./start.sh\"\n"), 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	if _, err := LoadServiceConfig(configFile); err == nil {
		t.Error("expected LoadServiceConfig to reject a path-traversal name")
	}
}

func TestValidateServiceConfig_pathTraversalName(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "service.yaml")
	if err := os.WriteFile(configFile, []byte("name: \"../../pwned\"\ncommand: \"./start.sh\"\n"), 0644); err != nil {
		t.Fatalf("writing test config file should not error: %v", err)
	}

	if _, errs := ValidateServiceConfig(configFile); len(errs) == 0 {
		t.Error("expected ValidateServiceConfig to reject a path-traversal name")
	}
}

func TestNewServiceCatalogEntryWithPathTraversalName(t *testing.T) {
	if _, err := NewServiceCatalogEntry("../../pwned", "./test-files", "service.yaml"); err == nil {
		t.Error("expected NewServiceCatalogEntry to reject a path-traversal name")
	}
}

func TestDetectSelfDetachRisk(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		wantWarnings int
	}{
		{"plain command", "npm start", 0},
		{"setsid leading", "setsid npm start", 1},
		{"nohup leading", "nohup ./start.sh &", 1},
		{"disown leading", "disown ./start.sh", 1},
		{"mentioned mid-arg, not a leading command", "echo nohup is not scary", 0},
		{"second segment after &&", "echo hi && setsid npm start", 1},
		{"second segment after semicolon", "echo hi; nohup npm start", 1},
		{"piped segment", "npm start | nohup tee out.log", 1},
		{"multiple risky segments", "setsid npm start && nohup npm run worker", 2},
		{"empty command", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := DetectSelfDetachRisk(tt.command)
			if len(warnings) != tt.wantWarnings {
				t.Errorf("DetectSelfDetachRisk(%q) = %v, want %d warning(s)", tt.command, warnings, tt.wantWarnings)
			}
		})
	}
}
