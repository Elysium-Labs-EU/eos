package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// setupLogsTestCmd mirrors setupCmd but also exposes the manager, needed to
// call WaitPipes() (ensuring async log-writing goroutines have flushed
// before reading log files back) and to build fresh cmd instances between
// invocations that set different flags (flag state persists on a cobra
// *Command across cmd.SetArgs/ExecuteContext calls otherwise).
func setupLogsTestCmd(t *testing.T) (cmd *cobra.Command, mgr *manager.LocalManager, outBuf, errBuf *bytes.Buffer, tempDir string) {
	t.Helper()
	db, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := manager.NewLocalManager(db, td, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(m.WaitPipes)
	c := newTestRootCmd(m)

	var ob, eb bytes.Buffer
	c.SetOut(&ob)
	c.SetErr(&eb)

	return c, m, &ob, &eb, td
}

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
		t.Fatalf("Logs command should not return an error, got : %v", err)
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
	if !strings.Contains(output, "eos run cms") {
		t.Errorf("Expected logs to hint 'eos run cms', got: %s", output)
	}
	if strings.Contains(output, "eos start") {
		t.Errorf("Expected logs not to reference removed 'eos start' command, got: %s", output)
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

func TestLogsCommandLinesOutOfRange(t *testing.T) {
	cmd, mgr, _, errBuf, tempDir := setupLogsTestCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)
	scriptPath := filepath.Join(filepath.Dir(path), "start-script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho a-line\n"), 0755); err != nil {
		t.Fatalf("Failed to write start script: %v", err)
	}

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command failed: %v", err)
	}
	cmd.SetArgs([]string{"run", cfg.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Run command failed: %v", err)
	}
	mgr.WaitPipes()

	for _, lines := range []string{"-1", "10001"} {
		errBuf.Reset()
		cmd = newTestRootCmd(mgr)
		cmd.SetErr(errBuf)
		cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--lines", lines})
		if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
			t.Fatalf("--lines=%s: expected ErrCommandFailed, got: %v", lines, err)
		}
		output := errBuf.String()
		if !strings.Contains(output, "line count must be between 0 and 10000") {
			t.Errorf("--lines=%s: expected range error, got: %s", lines, output)
		}
	}
}

func TestLogsCommandMutuallyExclusiveFlags(t *testing.T) {
	cmd, _, _, _, tempDir := setupLogsTestCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command failed: %v", err)
	}

	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--error", "--output"})
	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("expected an error for mutually exclusive --error/--output flags")
	}
}

func TestLogsCommandSingleStreamModes(t *testing.T) {
	cmd, mgr, outBuf, _, tempDir := setupLogsTestCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)
	scriptPath := filepath.Join(filepath.Dir(path), "start-script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho stdout-line\necho stderr-line >&2\n"), 0755); err != nil {
		t.Fatalf("Failed to write start script: %v", err)
	}

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command failed: %v", err)
	}
	cmd.SetArgs([]string{"run", cfg.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Run command failed: %v", err)
	}
	mgr.WaitPipes()

	outBuf.Reset()
	cmd = newTestRootCmd(mgr)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--output"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("logs --output should not return an error, got: %v", err)
	}
	if !strings.Contains(outBuf.String(), "stdout-line") {
		t.Errorf("expected stdout-only output, got: %s", outBuf.String())
	}
	if strings.Contains(outBuf.String(), "stderr-line") {
		t.Errorf("--output should not include stderr, got: %s", outBuf.String())
	}

	outBuf.Reset()
	cmd = newTestRootCmd(mgr)
	cmd.SetOut(outBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--error"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("logs --error should not return an error, got: %v", err)
	}
	if !strings.Contains(outBuf.String(), "stderr-line") {
		t.Errorf("expected stderr-only output, got: %s", outBuf.String())
	}
	if strings.Contains(outBuf.String(), "stdout-line") {
		t.Errorf("--error should not include stdout, got: %s", outBuf.String())
	}
}

func TestLogsCommandFollow(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)
	scriptPath := filepath.Join(filepath.Dir(path), "start-script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\nsleep 0.2\necho followed-line\nsleep 5\n"), 0755); err != nil {
		t.Fatalf("Failed to write start script: %v", err)
	}

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Add command failed: %v", err)
	}
	cmd.SetArgs([]string{"run", cfg.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("Run command failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=true"})
	if err := cmd.ExecuteContext(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("follow should exit cleanly on context cancellation, got: %v", err)
	}

	if !strings.Contains(outBuf.String(), "followed-line") {
		t.Errorf("expected the followed line to appear in streamed output, got: %s", outBuf.String())
	}
}

func TestShowCombinedLogsInterleavesByTimestamp(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "out.log")
	errPath := filepath.Join(tempDir, "err.log")

	outLines := `{"time":"2025-01-01T10:00:00.000000000Z","level":"INFO","msg":"first"}
{"time":"2025-01-01T10:00:02.000000000Z","level":"INFO","msg":"third"}
`
	errLines := `{"time":"2025-01-01T10:00:01.000000000Z","level":"WARN","msg":"second"}
`
	if err := os.WriteFile(outPath, []byte(outLines), 0644); err != nil {
		t.Fatalf("write outPath: %v", err)
	}
	if err := os.WriteFile(errPath, []byte(errLines), 0644); err != nil {
		t.Fatalf("write errPath: %v", err)
	}

	var out, errOut bytes.Buffer
	showCombinedLogs(&out, &errOut, outPath, errPath, 100)

	output := out.String()
	firstIdx := strings.Index(output, "first")
	secondIdx := strings.Index(output, "second")
	thirdIdx := strings.Index(output, "third")
	if firstIdx == -1 || secondIdx == -1 || thirdIdx == -1 {
		t.Fatalf("expected all three lines in output, got: %s", output)
	}
	if firstIdx >= secondIdx || secondIdx >= thirdIdx {
		t.Errorf("expected lines interleaved in chronological order (first, second, third), got: %s", output)
	}
}

// Regression test: a non-JSON line must sort next to its stream's last known
// timestamp, not collapse to the very start of the merged output.
func TestShowCombinedLogsNonJSONLineInheritsStreamTime(t *testing.T) {
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "out.log")
	errPath := filepath.Join(tempDir, "err.log")

	outLines := `{"time":"2025-01-01T10:00:00.000000000Z","level":"INFO","msg":"json-early"}
plain text line with no timestamp
{"time":"2025-01-01T10:00:05.000000000Z","level":"INFO","msg":"json-late"}
`
	errLines := `{"time":"2025-01-01T10:00:03.000000000Z","level":"WARN","msg":"err-middle"}
`
	if err := os.WriteFile(outPath, []byte(outLines), 0644); err != nil {
		t.Fatalf("write outPath: %v", err)
	}
	if err := os.WriteFile(errPath, []byte(errLines), 0644); err != nil {
		t.Fatalf("write errPath: %v", err)
	}

	var out, errOut bytes.Buffer
	showCombinedLogs(&out, &errOut, outPath, errPath, 100)

	output := out.String()
	jsonEarlyIdx := strings.Index(output, "json-early")
	plainIdx := strings.Index(output, "plain text line")
	errMiddleIdx := strings.Index(output, "err-middle")
	jsonLateIdx := strings.Index(output, "json-late")
	if jsonEarlyIdx == -1 || plainIdx == -1 || errMiddleIdx == -1 || jsonLateIdx == -1 {
		t.Fatalf("expected all four lines in output, got: %s", output)
	}

	// The plain-text line has no timestamp of its own, so it inherits
	// "json-early"'s time (10:00:00) and must stay ahead of the 10:00:03 and
	// 10:00:05 entries, not jump to the absolute front of the merge.
	if jsonEarlyIdx >= plainIdx || plainIdx >= errMiddleIdx || errMiddleIdx >= jsonLateIdx {
		t.Errorf("expected order json-early, plain, err-middle, json-late; got: %s", output)
	}
}

func TestStartTailGoroutineReportsStartFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	ch := make(chan followMsg, 4)
	startTailGoroutine(ctx, filepath.Join(t.TempDir(), "does-not-exist.log"), true, ch)

	select {
	case msg := <-ch:
		if !strings.Contains(msg.text, "failed to tail") {
			t.Errorf("expected a 'failed to tail' message, got: %q", msg.text)
		}
		if !msg.isErr {
			t.Errorf("expected the failure message to be flagged as an error stream message")
		}
	case <-ctx.Done():
		t.Fatal("expected a failure message before the context timeout")
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
