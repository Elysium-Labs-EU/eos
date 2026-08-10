package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
)

// eos api run refuses every local start outright (see apiRefuseLocalStart):
// it promises a pgid for a process that will still exist once the command
// exits, a promise local mode can't keep without a daemon to supervise the
// result. -f <path> and a bare <name> both resolve to the same refusal —
// the start/restart/once logic downstream of that point (shared with the
// plain "eos run" via startOrRestartService) is exercised against a real
// LocalManager in cmd/run_test.go instead, since that is the only local-mode
// command that can actually reach it.
func TestAPIRunWithServiceFileRefusesLocalStart(t *testing.T) {
	cmd, _, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	cmd.SetArgs([]string{"api", "run", "-f", yamlPath})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if !strings.Contains(errResult["error"], "eos run") {
		t.Errorf("expected the refusal to name 'eos run' as the fix, got: %+v", errResult)
	}
}

// TestAPIRunWithServiceNameRefusesLocalStart is the bare-<name> counterpart
// of TestAPIRunWithServiceFileRefusesLocalStart: the service must already be
// registered (api run resolves the name before hitting the local-start
// refusal, same as -f), but starting it is still refused.
func TestAPIRunWithServiceNameRefusesLocalStart(t *testing.T) {
	cmd, _, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)
	cmd.SetArgs([]string{"api", "add", yamlPath})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("api add: unexpected error: %v", err)
	}

	cmd.SetArgs([]string{"api", "run", testFile.Name})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if !strings.Contains(errResult["error"], "eos run") {
		t.Errorf("expected the refusal to name 'eos run' as the fix, got: %+v", errResult)
	}
}

// TestAPIRunWithOnceFlagRefusesLocalStart proves --once does not bypass the
// local-start refusal: the flag only controls whether an already-running
// service is skipped, which api run never reaches locally either way.
func TestAPIRunWithOnceFlagRefusesLocalStart(t *testing.T) {
	cmd, _, errBuf, tempDir := setupAPICmd(t)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlPath := writeServiceFiles(t, tempDir, testFile)

	cmd.SetArgs([]string{"api", "run", "--once", "-f", yamlPath})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected a non-empty refusal message, got: %+v", errResult)
	}
}

// TestAPIRunNoArgsNoFile, TestAPIRunWithUnregisteredName and
// TestAPIRunWithFileNotFound below now exercise apiRefuseLocalStart's
// refusal (it runs before argument/selector resolution) rather than each
// test's own named validation error; the assertions are generic enough
// (ErrAPICommandFailed plus a non-empty JSON error) to still hold either
// way, and the specific "no args", "unregistered", and "file not found"
// validation paths they originally targeted are still exercised by the
// equivalent plain "eos run" tests in cmd/run_test.go, the only local-mode
// command that reaches past the local-start refusal.
func TestAPIRunNoArgsNoFile(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "run"})
	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error message in JSON, got: %+v", errResult)
	}
}

func TestAPIRunWithUnregisteredName(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "run", "nonexistent-service"})
	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error message in JSON, got: %+v", errResult)
	}
}

func TestAPIRunWithFileNotFound(t *testing.T) {
	cmd, _, errBuf, _ := setupAPICmd(t)

	cmd.SetArgs([]string{"api", "run", "-f", "/nonexistent/path/service.yaml"})
	err := cmd.ExecuteContext(t.Context())

	if !errors.Is(err, helpers.ErrAPICommandFailed) {
		t.Fatalf("expected ErrAPICommandFailed, got: %v", err)
	}

	var errResult map[string]string
	if json.NewDecoder(errBuf).Decode(&errResult) != nil {
		t.Fatalf("expected JSON error on stderr, got: %s", errBuf.String())
	}
	if errResult["error"] == "" {
		t.Errorf("expected non-empty error message in JSON, got: %+v", errResult)
	}
}
