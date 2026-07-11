package config

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetBaseDir_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EOS_BASE_DIR", dir)

	got, err := GetBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("expected %q, got %q", dir, got)
	}
}

func TestGetBaseDir_SudoUser(t *testing.T) {
	t.Setenv("EOS_BASE_DIR", "")

	// Uses the current user as its own SUDO_USER, so this only proves the
	// SUDO_USER branch is taken and resolves via user.Lookup; it cannot
	// verify resolution to a different user's home directory.
	u, err := user.Current()
	if err != nil {
		t.Skip("cannot determine current user")
	}
	t.Setenv("SUDO_USER", u.Username)

	got, err := GetBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(u.HomeDir, "."+Name)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCreateBaseDir_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newdir")
	t.Setenv("EOS_BASE_DIR", dir)

	got, err := CreateBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("expected path %q, got %q", dir, got)
	}
	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("stat failed: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", dir)
	}
}

func TestCreateBaseDir_Idempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EOS_BASE_DIR", dir)

	if _, err := CreateBaseDir(); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := CreateBaseDir(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

func TestCreateBaseDir_RootWithoutSudoUserBlocked(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test root guard when actually running as root")
	}
	// Non-root: guard must not trigger regardless of env state.
	dir := filepath.Join(t.TempDir(), "guard-test")
	t.Setenv("EOS_BASE_DIR", dir)
	t.Setenv("SUDO_USER", "")

	_, err := CreateBaseDir()
	if err != nil {
		t.Errorf("non-root invocation should not error, got: %v", err)
	}
}

func TestLoadEosConfig_Absent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadEosConfig(dir)
	if err != nil {
		t.Fatalf("absent config file should not error, got: %v", err)
	}
	def := DefaultEosConfig()
	if cfg.Health.CheckIntervalMs != def.Health.CheckIntervalMs {
		t.Errorf("checkIntervalMs: want %d, got %d", def.Health.CheckIntervalMs, cfg.Health.CheckIntervalMs)
	}
	if cfg.Health.Backoff.BaseMs != def.Health.Backoff.BaseMs {
		t.Errorf("backoff.baseMs: want %d, got %d", def.Health.Backoff.BaseMs, cfg.Health.Backoff.BaseMs)
	}
	if cfg.Health.Memory.WarningThreshold != def.Health.Memory.WarningThreshold {
		t.Errorf("memory.warningThreshold: want %f, got %f", def.Health.Memory.WarningThreshold, cfg.Health.Memory.WarningThreshold)
	}
	if cfg.Log.MaxFiles != def.Log.MaxFiles {
		t.Errorf("log.maxFiles: want %d, got %d", def.Log.MaxFiles, cfg.Log.MaxFiles)
	}
}

func TestLoadEosConfig_Partial(t *testing.T) {
	dir := t.TempDir()
	yaml := "health:\n  checkIntervalMs: 5000\n"
	if err := os.WriteFile(filepath.Join(dir, EosConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadEosConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Health.CheckIntervalMs != 5000 {
		t.Errorf("checkIntervalMs: want 5000, got %d", cfg.Health.CheckIntervalMs)
	}
	// Unset fields should retain defaults.
	if cfg.Health.Backoff.BaseMs != HealthBackoffBaseMs {
		t.Errorf("backoff.baseMs: want default %d, got %d", HealthBackoffBaseMs, cfg.Health.Backoff.BaseMs)
	}
	if cfg.Log.MaxFiles != DaemonLogMaxFiles {
		t.Errorf("log.maxFiles: want default %d, got %d", DaemonLogMaxFiles, cfg.Log.MaxFiles)
	}
}

func TestLoadEosConfig_Full(t *testing.T) {
	dir := t.TempDir()
	yaml := `health:
  checkIntervalMs: 3000
  backoff:
    baseMs: 500
    maxMs: 30000
  memory:
    warningThreshold: 0.60
    softRestartThreshold: 0.70
    forceRestartThreshold: 0.80
log:
  maxFiles: 3
  fileSizeLimitBytes: 5242880
`
	if err := os.WriteFile(filepath.Join(dir, EosConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadEosConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Health.CheckIntervalMs != 3000 {
		t.Errorf("checkIntervalMs: want 3000, got %d", cfg.Health.CheckIntervalMs)
	}
	if cfg.Health.Backoff.BaseMs != 500 {
		t.Errorf("backoff.baseMs: want 500, got %d", cfg.Health.Backoff.BaseMs)
	}
	if cfg.Health.Memory.ForceRestartThreshold != 0.80 {
		t.Errorf("forceRestartThreshold: want 0.80, got %f", cfg.Health.Memory.ForceRestartThreshold)
	}
	if cfg.Log.MaxFiles != 3 {
		t.Errorf("log.maxFiles: want 3, got %d", cfg.Log.MaxFiles)
	}
}

func TestLoadEosConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, EosConfigFileName), []byte("health: [not: valid"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadEosConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadEosConfig_ThresholdsOutOfOrder(t *testing.T) {
	dir := t.TempDir()
	yaml := `health:
  memory:
    warningThreshold: 0.90
    softRestartThreshold: 0.80
    forceRestartThreshold: 0.95
`
	if err := os.WriteFile(filepath.Join(dir, EosConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadEosConfig(dir)
	if err == nil {
		t.Fatal("expected error for out-of-order thresholds, got nil")
	}
	if !strings.Contains(err.Error(), "ascending") {
		t.Errorf("expected 'ascending' in error, got: %v", err)
	}
}

func TestLoadEosConfig_NegativeCheckInterval(t *testing.T) {
	dir := t.TempDir()
	yaml := "health:\n  checkIntervalMs: -1\n"
	if err := os.WriteFile(filepath.Join(dir, EosConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadEosConfig(dir)
	if err == nil {
		t.Fatal("expected error for negative checkIntervalMs, got nil")
	}
}

func TestEosConfig_Validate_BackoffMaxLessThanBase(t *testing.T) {
	cfg := DefaultEosConfig()
	cfg.Health.Backoff.BaseMs = 1000
	cfg.Health.Backoff.MaxMs = 500
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when maxMs < baseMs, got nil")
	}
}
