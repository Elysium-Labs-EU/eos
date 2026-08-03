package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
)

// TestLogsProcessHistoryGenericError checks that a process-history lookup
// failure other than "never started" (ErrProcessNotFound) surfaces as a
// generic "getting process history" error instead of being misreported as
// "has never been started". The real LocalManager's sqlite-backed
// implementation won't produce that generic error from the CLI, so this
// scripts a fake daemon peer instead, exercising the real DaemonManager IPC
// client code against a test-controlled stub.
func TestLogsProcessHistoryGenericError(t *testing.T) {
	mgr := newFakeDaemonManager(t, map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodGetMostRecentProcessHistoryEntry: {
			Success: false,
			Error:   "process history query exploded",
		},
	})
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"logs", "cms", "--follow=false"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "getting process history") {
		t.Errorf("expected 'getting process history' error, got: %s", errBuf.String())
	}
}

// addAndRunLogsService adds and runs a short-lived echo service via the given
// (LocalManager-backed) cmd, waits for its log pipes to flush, and returns the
// service config so the caller can locate/tamper with its on-disk log files.
func addAndRunLogsService(t *testing.T, cmd *cobra.Command, mgr *manager.LocalManager, tempDir string) *types.ServiceConfig {
	t.Helper()
	cfg := &types.ServiceConfig{Name: "cms", Command: "./start-script.sh", Port: 1337}
	path := writeServiceYAML(t, tempDir, cfg)
	scriptPath := filepath.Join(filepath.Dir(path), "start-script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho a-line\n"), 0755); err != nil {
		t.Fatalf("failed to write start script: %v", err)
	}

	cmd.SetArgs([]string{"add", path})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command failed: %v", err)
	}
	cmd.SetArgs([]string{"run", cfg.Name})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run command failed: %v", err)
	}
	mgr.WaitPipes()
	return cfg
}

// TestLogsCombinedOutLogMissing checks combined mode's "getting log file
// path" error when the stdout log file has gone missing (e.g. deleted out
// from under a registered, previously-run service) but the process history
// entry still shows it started.
func TestLogsCombinedOutLogMissing(t *testing.T) {
	cmd, mgr, _, errBuf, tempDir := setupLogsTestCmd(t)
	cfg := addAndRunLogsService(t, cmd, mgr, tempDir)

	outLogPath := filepath.Join(manager.CreateLogDirPath(tempDir), manager.CreateOutputLogFilename(cfg.Name))
	if err := os.Remove(outLogPath); err != nil {
		t.Fatalf("removing out log file: %v", err)
	}

	cmd = newTestRootCmd(mgr)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "getting log file path") {
		t.Errorf("expected 'getting log file path' error, got: %s", errBuf.String())
	}
}

// TestLogsCombinedErrLogMissing is the mirror of TestLogsCombinedOutLogMissing
// for the stderr log file, which combined mode checks second.
func TestLogsCombinedErrLogMissing(t *testing.T) {
	cmd, mgr, _, errBuf, tempDir := setupLogsTestCmd(t)
	cfg := addAndRunLogsService(t, cmd, mgr, tempDir)

	errLogPath := filepath.Join(manager.CreateLogDirPath(tempDir), manager.CreateErrorOutputLogFilename(cfg.Name))
	if err := os.Remove(errLogPath); err != nil {
		t.Fatalf("removing error log file: %v", err)
	}

	cmd = newTestRootCmd(mgr)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "getting error log file path") {
		t.Errorf("expected 'getting error log file path' error, got: %s", errBuf.String())
	}
}

// TestLogsSingleStreamLogMissing checks the single-stream (--output/--error)
// branch's own "getting log file path" error, a separate code path from the
// combined-mode checks above.
func TestLogsSingleStreamLogMissing(t *testing.T) {
	cmd, mgr, _, errBuf, tempDir := setupLogsTestCmd(t)
	cfg := addAndRunLogsService(t, cmd, mgr, tempDir)

	outLogPath := filepath.Join(manager.CreateLogDirPath(tempDir), manager.CreateOutputLogFilename(cfg.Name))
	if err := os.Remove(outLogPath); err != nil {
		t.Fatalf("removing out log file: %v", err)
	}

	cmd = newTestRootCmd(mgr)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--output"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "getting log file path") {
		t.Errorf("expected 'getting log file path' error, got: %s", errBuf.String())
	}
}

// TestLogsTailStartFailure checks that a failure to even start the "tail"
// subprocess (e.g. the binary isn't found on PATH) surfaces as a "reading log
// file" error and fails the command, rather than panicking or hanging.
func TestLogsTailStartFailure(t *testing.T) {
	cmd, mgr, _, errBuf, tempDir := setupLogsTestCmd(t)
	cfg := addAndRunLogsService(t, cmd, mgr, tempDir)

	// Strip PATH so exec.CommandContext can't resolve "tail". This only
	// affects this test's own process env from here on; the service was
	// already added/run above via absolute/relative paths that don't need
	// PATH lookup, and it already exited (the script just echoes and exits).
	t.Setenv("PATH", "")

	cmd = newTestRootCmd(mgr)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--output"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "reading log file") {
		t.Errorf("expected 'reading log file' error, got: %s", errBuf.String())
	}
}

// TestLogsTailWaitFailure checks that "tail" starting successfully but then
// exiting non-zero (here: permission denied opening an unreadable log file)
// is reported via the same "reading log file" message, without failing the
// overall command (the RunE return is nil on this path; only the message is
// printed to stderr).
func TestLogsTailWaitFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: cannot test file permission restrictions as root")
	}
	cmd, mgr, _, errBuf, tempDir := setupLogsTestCmd(t)
	cfg := addAndRunLogsService(t, cmd, mgr, tempDir)

	outLogPath := filepath.Join(manager.CreateLogDirPath(tempDir), manager.CreateOutputLogFilename(cfg.Name))
	if err := os.Chmod(outLogPath, 0000); err != nil {
		t.Fatalf("chmod out log file: %v", err)
	}
	if data, readErr := os.ReadFile(outLogPath); readErr == nil {
		t.Skipf("log file still readable after chmod 0000 (elevated privileges?) - cannot exercise unreadable-log path, got: %q", data)
	}

	cmd = newTestRootCmd(mgr)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"logs", cfg.Name, "--follow=false", "--output"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error (wait failure is reported, not fatal): %v", err)
	}
	if !strings.Contains(errBuf.String(), "reading log file") {
		t.Errorf("expected 'reading log file' error from tail wait failure, got: %s", errBuf.String())
	}
}

// TestShowCombinedLogsBothStreamsUnreadable checks showCombinedLogs's own
// error path when neither log file can be read at all (as opposed to
// TestShowCombinedLogsInterleavesByTimestamp/NonJSONLineInheritsStreamTime in
// logs_test.go, which cover the successful-merge path).
func TestShowCombinedLogsBothStreamsUnreadable(t *testing.T) {
	tempDir := t.TempDir()
	var out, errOut bytes.Buffer
	showCombinedLogs(&out, &errOut, filepath.Join(tempDir, "missing-out.log"), filepath.Join(tempDir, "missing-err.log"), 100)

	if out.Len() != 0 {
		t.Errorf("expected no stdout output when both streams are unreadable, got: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "reading log files") {
		t.Errorf("expected 'reading log files' error, got: %s", errOut.String())
	}
}
