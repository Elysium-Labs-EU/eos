package testmatrix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matrix.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfig_Valid(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
    distro: debian-bookworm
    init: systemd
    golden: eos-golden-debian
  - name: alpine
    distro: alpine-3.23
    init: openrc
    golden: eos-golden-alpine
suites:
  - name: lifecycle
    command: go test ./...
  - name: openrc
    only: [alpine]
    command: go test -run OpenRC
  - name: fixtures
    nightly: true
    command: bash scripts/test-fixtures-orb.sh
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(cfg.Targets))
	}
	if len(cfg.Suites) != 3 {
		t.Fatalf("expected 3 suites, got %d", len(cfg.Suites))
	}
	if !cfg.Suites[2].Nightly {
		t.Fatalf("expected fixtures suite to be nightly")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := writeConfig(t, "targets: [this is not valid: yaml:::")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestLoadConfig_NoTargets(t *testing.T) {
	path := writeConfig(t, `
suites:
  - name: lifecycle
    command: go test ./...
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no targets") {
		t.Fatalf("expected 'no targets' error, got %v", err)
	}
}

func TestLoadConfig_NoSuites(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
    golden: eos-golden-debian
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no suites") {
		t.Fatalf("expected 'no suites' error, got %v", err)
	}
}

func TestLoadConfig_TargetMissingName(t *testing.T) {
	path := writeConfig(t, `
targets:
  - golden: eos-golden-debian
suites:
  - name: lifecycle
    command: go test ./...
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "missing name or golden") {
		t.Fatalf("expected 'missing name or golden' error, got %v", err)
	}
}

func TestLoadConfig_TargetMissingGolden(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
suites:
  - name: lifecycle
    command: go test ./...
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "missing name or golden") {
		t.Fatalf("expected 'missing name or golden' error, got %v", err)
	}
}

func TestLoadConfig_DuplicateTarget(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
    golden: eos-golden-debian
  - name: debian
    golden: eos-golden-debian-2
suites:
  - name: lifecycle
    command: go test ./...
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate target") {
		t.Fatalf("expected 'duplicate target' error, got %v", err)
	}
}

func TestLoadConfig_SuiteMissingName(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
    golden: eos-golden-debian
suites:
  - command: go test ./...
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "missing name or command") {
		t.Fatalf("expected 'missing name or command' error, got %v", err)
	}
}

func TestLoadConfig_SuiteMissingCommand(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
    golden: eos-golden-debian
suites:
  - name: lifecycle
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "missing name or command") {
		t.Fatalf("expected 'missing name or command' error, got %v", err)
	}
}

func TestLoadConfig_SuiteUnknownOnlyTarget(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: debian
    golden: eos-golden-debian
suites:
  - name: openrc
    only: [alpine]
    command: go test -run OpenRC
`)
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("expected 'unknown target' error, got %v", err)
	}
}

func TestSuite_AppliesTo(t *testing.T) {
	all := Suite{Name: "lifecycle", Command: "go test ./..."}
	if !all.AppliesTo("debian") || !all.AppliesTo("alpine") {
		t.Fatal("suite with empty Only should apply to every target")
	}

	scoped := Suite{Name: "openrc", Command: "go test -run OpenRC", Only: []string{"alpine"}}
	if !scoped.AppliesTo("alpine") {
		t.Fatal("scoped suite should apply to target in Only")
	}
	if scoped.AppliesTo("debian") {
		t.Fatal("scoped suite should not apply to target outside Only")
	}
}
