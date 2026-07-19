package config

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

func TestResolveScope(t *testing.T) {
	const sysDir = "/etc/systemd/system/"
	const usrDir = "/home/u/.config/systemd/user/"

	t.Run("system managed wins", func(t *testing.T) {
		dir, managed, userScope, err := resolveScope(sysDir,
			func() (string, error) { return usrDir, nil },
			func(d string) (bool, error) { return d == sysDir, nil })
		if err != nil || dir != sysDir || !managed || userScope {
			t.Fatalf("got dir=%q managed=%v user=%v err=%v", dir, managed, userScope, err)
		}
	})

	t.Run("user managed when system is not", func(t *testing.T) {
		dir, managed, userScope, err := resolveScope(sysDir,
			func() (string, error) { return usrDir, nil },
			func(d string) (bool, error) { return d == usrDir, nil })
		if err != nil || dir != usrDir || !managed || !userScope {
			t.Fatalf("got dir=%q managed=%v user=%v err=%v", dir, managed, userScope, err)
		}
	})

	t.Run("system check error propagates", func(t *testing.T) {
		wantErr := errors.New("stat boom")
		if _, _, _, err := resolveScope(sysDir,
			func() (string, error) { return usrDir, nil },
			func(string) (bool, error) { return false, wantErr }); !errors.Is(err, wantErr) {
			t.Fatalf("expected system check error, got %v", err)
		}
	})

	t.Run("user dir error propagates", func(t *testing.T) {
		wantErr := errors.New("home boom")
		if _, _, _, err := resolveScope(sysDir,
			func() (string, error) { return "", wantErr },
			func(string) (bool, error) { return false, nil }); !errors.Is(err, wantErr) {
			t.Fatalf("expected user dir error, got %v", err)
		}
	})

	t.Run("user check error propagates", func(t *testing.T) {
		wantErr := errors.New("user stat boom")
		if _, _, _, err := resolveScope(sysDir,
			func() (string, error) { return usrDir, nil },
			func(d string) (bool, error) {
				if d == usrDir {
					return false, wantErr
				}
				return false, nil
			}); !errors.Is(err, wantErr) {
			t.Fatalf("expected user check error, got %v", err)
		}
	})

	t.Run("unmanaged falls back to privilege level", func(t *testing.T) {
		dir, managed, userScope, err := resolveScope(sysDir,
			func() (string, error) { return usrDir, nil },
			func(string) (bool, error) { return false, nil })
		if err != nil || managed {
			t.Fatalf("expected unmanaged, got managed=%v err=%v", managed, err)
		}
		if os.Getuid() == 0 {
			if dir != sysDir || userScope {
				t.Fatalf("root should fall back to system scope, got dir=%q user=%v", dir, userScope)
			}
		} else if dir != usrDir || !userScope {
			t.Fatalf("non-root should fall back to user scope, got dir=%q user=%v", dir, userScope)
		}
	})
}

func TestGetBaseDir_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EOS_BASE_DIR", dir)

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Skip("cannot resolve identity")
	}

	got, err := GetBaseDir(id)
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

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	got, err := GetBaseDir(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(u.HomeDir, "."+Name)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestGetBaseDir_SudoUserIgnoredWhenNotRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test non-root SUDO_USER guard when actually running as root")
	}
	t.Setenv("EOS_BASE_DIR", "")

	// `sudo -u <non-root-user>` also sets SUDO_USER to the invoking user, even
	// though the process is not running as root. GetBaseDir must ignore
	// SUDO_USER in that case and resolve to the current process's own home,
	// not the invoking user's — otherwise data lands in the wrong home dir.
	t.Setenv("SUDO_USER", "someone-else")

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	got, err := GetBaseDir(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot determine home dir: %v", err)
	}
	want := filepath.Join(homeDir, "."+Name)
	if got != want {
		t.Errorf("expected %q (own home, SUDO_USER ignored), got %q", want, got)
	}
}

func TestCreateBaseDir_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newdir")
	t.Setenv("EOS_BASE_DIR", dir)

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	got, err := CreateBaseDir(id)
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

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	if _, err := CreateBaseDir(id); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := CreateBaseDir(id); err != nil {
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

	id, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	_, err = CreateBaseDir(id)
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

func TestLoadEosConfig_Sinks(t *testing.T) {
	dir := t.TempDir()
	yaml := `sinks:
  prod-loki:
    type: loki
    mode: push
    address: http://loki:3100
  local-file:
    type: file
    mode: serve
    address: /var/log/eos
`
	if err := os.WriteFile(filepath.Join(dir, EosConfigFileName), []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadEosConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Sinks) != 2 {
		t.Fatalf("expected 2 registered sinks, got %d", len(cfg.Sinks))
	}
	loki, ok := cfg.Sinks["prod-loki"]
	if !ok {
		t.Fatal("expected sink \"prod-loki\" to be registered")
	}
	if loki.Type != "loki" || loki.Address != "http://loki:3100" {
		t.Errorf("prod-loki: unexpected sink %+v", loki)
	}
	if _, ok := cfg.Sinks["local-file"]; !ok {
		t.Fatal("expected sink \"local-file\" to be registered")
	}
}

func TestLoadEosConfig_SinksAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadEosConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Sinks != nil {
		t.Errorf("expected nil sink registry by default, got %+v", cfg.Sinks)
	}
}

func TestEosConfig_Validate_SinkMissingType(t *testing.T) {
	cfg := DefaultEosConfig()
	cfg.Sinks = map[string]types.LogSink{
		"broken": {Address: "http://example.com"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for sink missing type, got nil")
	}
	if !strings.Contains(err.Error(), "sinks.broken") {
		t.Errorf("expected error to mention sinks.broken, got: %v", err)
	}
}
