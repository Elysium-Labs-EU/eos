package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/testmatrix"
)

var errStubbedFailure = errors.New("stubbed failure")

// stubRunner succeeds on every orb call it sees, unless failOn matches the
// subcommand (args[0]), in which case it fails every call for that
// subcommand.
type stubRunner struct {
	failOn string
}

func (s *stubRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == s.failOn {
		return "boom", errStubbedFailure
	}
	return "", nil
}

func writeMatrixConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matrix.yml")
	content := `
targets:
  - name: debian
    golden: eos-golden-debian
suites:
  - name: lifecycle
    command: go test ./...
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestRun_Success(t *testing.T) {
	orb := &testmatrix.Orb{Runner: &stubRunner{}}
	configPath := writeMatrixConfig(t)

	err := run(orb, configPath, "", false, true, 0, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRun_SuiteFailure(t *testing.T) {
	orb := &testmatrix.Orb{Runner: &stubRunner{failOn: "run"}}
	configPath := writeMatrixConfig(t)

	err := run(orb, configPath, "", false, true, 0, "")
	if err == nil || !strings.Contains(err.Error(), "one or more suites failed") {
		t.Fatalf("expected suite failure error, got %v", err)
	}
}

func TestRun_BadConfigPath(t *testing.T) {
	orb := &testmatrix.Orb{Runner: &stubRunner{}}

	err := run(orb, filepath.Join(t.TempDir(), "missing.yml"), "", false, true, 0, "")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestRun_WritesJSON(t *testing.T) {
	orb := &testmatrix.Orb{Runner: &stubRunner{}}
	configPath := writeMatrixConfig(t)
	jsonPath := filepath.Join(t.TempDir(), "results.json")

	if err := run(orb, configPath, "", false, true, 0, jsonPath); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json output: %v", err)
	}
	var results []map[string]any
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestRun_BadJSONPath(t *testing.T) {
	orb := &testmatrix.Orb{Runner: &stubRunner{}}
	configPath := writeMatrixConfig(t)

	err := run(orb, configPath, "", false, true, 0, filepath.Join(t.TempDir(), "missing-dir", "results.json"))
	if err == nil || !strings.Contains(err.Error(), "write json results") {
		t.Fatalf("expected json write error, got %v", err)
	}
}

func TestRunID_IsSixLowercaseAlphanumeric(t *testing.T) {
	id := runID()
	if len(id) != 6 {
		t.Fatalf("expected 6-char run id, got %q", id)
	}
	for _, r := range id {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789", r) {
			t.Fatalf("unexpected character %q in run id %q", r, id)
		}
	}
}

func TestRunID_Varies(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		seen[runID()] = true
	}
	if len(seen) < 2 {
		t.Fatal("expected runID to produce varying output across calls")
	}
}
