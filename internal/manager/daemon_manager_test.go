package manager

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

func TestIsDaemonRunningNoPidFile(t *testing.T) {
	result := isDaemonRunning("/nonexistent/path/pid")
	if result {
		t.Error("isDaemonRunning should return false when pid file doesn't exist")
	}
}

func TestIsDaemonRunningInvalidPid(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	err := os.WriteFile(pidFile, []byte("not-a-number"), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	result := isDaemonRunning(pidFile)
	if result {
		t.Error("isDaemonRunning should return false for invalid pid")
	}
}

func TestIsDaemonRunningDeadProcess(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	// Use a PID that almost certainly doesn't exist
	err := os.WriteFile(pidFile, []byte("9999999"), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	result := isDaemonRunning(pidFile)
	if result {
		t.Error("isDaemonRunning should return false for a dead process")
	}
}

func TestIsDaemonRunningCurrentProcess(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	pid := os.Getpid()
	err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", pid), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	result := isDaemonRunning(pidFile)
	if !result {
		t.Error("isDaemonRunning should return true for a running process")
	}
}

func TestIsDaemonRunningEmptyPidFile(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	err := os.WriteFile(pidFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	result := isDaemonRunning(pidFile)
	if result {
		t.Error("isDaemonRunning should return false for empty pid file")
	}
}

func TestWaitForSocketExists(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test.sock")

	lc := net.ListenConfig{}
	_, err := lc.Listen(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("Failed to create net listener: %v", err)
	}

	err = waitForSocket(t.Context(), socketPath, 1*time.Second)
	if err != nil {
		t.Errorf("waitForSocket should succeed when socket exists, got: %v", err)
	}
}

func TestWaitForSocketTimeout(t *testing.T) {
	err := waitForSocket(t.Context(), "/nonexistent/socket.sock", 200*time.Millisecond)
	if err == nil {
		t.Fatal("waitForSocket should error on timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Error should mention timeout, got: %v", err)
	}
}

func TestWaitForSocketDelayedCreation(t *testing.T) {
	socketDir, err := os.MkdirTemp("/tmp", "eos")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "s.sock")

	listenerErr := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		lc := net.ListenConfig{}
		_, socketErr := lc.Listen(t.Context(), "unix", socketPath)
		listenerErr <- socketErr
	}()

	err = waitForSocket(t.Context(), socketPath, 2*time.Second)
	if err != nil {
		t.Errorf("waitForSocket should succeed when socket appears within timeout, got: %v", err)
	}

	if err := <-listenerErr; err != nil {
		t.Errorf("failed to start listener in goroutine: %v", err)
	}
}

func TestNewDaemonLogger(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}
	if logger == nil {
		t.Fatal("Logger should not be nil")
	}
}

func TestNewDaemonLoggerCreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	_, err := NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	if _, err := os.Stat(daemonConfig.Standalone.Log.LogDir); os.IsNotExist(err) {
		t.Error("Log directory should have been created")
	}
}

func TestRotatingFileWriterWrite(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	rw, err := newRotatingFileWriter(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("newRotatingFileWriter should not error, got: %v", err)
	}

	if rw.file == nil {
		t.Error("Log file should not be nil")
	}
	if rw.LogPath == "" {
		t.Error("LogPath should not be empty")
	}

	msg := []byte("test message\n")
	n, err := rw.Write(msg)
	if err != nil {
		t.Fatalf("Write should not error, got: %v", err)
	}
	if n != len(msg) {
		t.Errorf("Write should return len(msg)=%d, got %d", len(msg), n)
	}

	content, err := os.ReadFile(rw.LogPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	if !strings.Contains(string(content), "test message") {
		t.Error("Log file should contain the written message")
	}
}

func TestRotatingFileWriterSizeTracking(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	rw, err := newRotatingFileWriter(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("newRotatingFileWriter should not error, got: %v", err)
	}

	initialSize := rw.currentSize
	_, _ = rw.Write([]byte("some message\n"))

	if rw.currentSize <= initialSize {
		t.Error("currentSize should increase after writing")
	}
}

func TestRotatingFileWriterRotation(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	rw, err := newRotatingFileWriter(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("newRotatingFileWriter should not error, got: %v", err)
	}

	// Force a small max size to trigger rotation
	rw.maxSize = 100

	for range 20 {
		_, _ = rw.Write([]byte("This is a log message that should eventually trigger rotation\n"))
	}

	logDir := CreateLogDirPath(tempDir)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("Failed to read log directory: %v", err)
	}

	if len(entries) < 2 {
		t.Errorf("Expected at least 2 log files after rotation, got %d", len(entries))
	}
}

func TestRotatingFileWriterRotation_EnforcesMaxFiles(t *testing.T) {
	tempDir := t.TempDir()
	const maxFiles = 3
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"), testutil.WithLogMaxFiles(maxFiles))

	rw, err := newRotatingFileWriter(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("newRotatingFileWriter should not error, got: %v", err)
	}

	rw.maxSize = 100
	for range 50 {
		_, _ = rw.Write([]byte("This is a log message that should eventually trigger many rotations\n"))
	}

	logDir := CreateLogDirPath(tempDir)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("Failed to read log directory: %v", err)
	}
	if len(entries) > maxFiles {
		t.Errorf("expected at most %d log files under maxFiles enforcement, got %d: %v", maxFiles, len(entries), entries)
	}
}

func TestHandleRenameExistingLogs_DeletesOldestBeyondMaxFiles(t *testing.T) {
	tempDir := t.TempDir()

	for _, name := range []string{"test.log", "test.log.1", "test.log.2"} {
		f, err := os.Create(filepath.Join(tempDir, name))
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("Closing the file errored: %v", err)
		}
	}

	if err := handleRenameExistingLogs(tempDir, "test.log", 3); err != nil {
		t.Fatalf("handleRenameExistingLogs should not error, got: %v", err)
	}

	// test.log.2 was the oldest; with maxFiles=3 it must be deleted, not shifted to test.log.3.
	if _, err := os.Stat(filepath.Join(tempDir, "test.log.3")); !os.IsNotExist(err) {
		t.Error("test.log.3 should not exist; oldest file should have been deleted, not renamed")
	}
	if _, err := os.Stat(filepath.Join(tempDir, "test.log.1")); os.IsNotExist(err) {
		t.Error("test.log.1 should exist after rename")
	}
	if _, err := os.Stat(filepath.Join(tempDir, "test.log.2")); os.IsNotExist(err) {
		t.Error("test.log.2 should exist after rename")
	}
}

func TestDaemonLoggerWritesJSON(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	logger.Info("formatted message")

	logPath := filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName)
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logContent := string(content)
	if !strings.Contains(logContent, `"level":"INFO"`) {
		t.Errorf("Log should contain JSON level field, got: %s", logContent)
	}
	if !strings.Contains(logContent, "formatted message") {
		t.Error("Log should contain the message")
	}
}

func TestHandleRenameExistingLogs(t *testing.T) {
	tempDir := t.TempDir()

	// Create initial log files
	for _, name := range []string{"test.log", "test.log.1", "test.log.2"} {
		f, err := os.Create(filepath.Join(tempDir, name))
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
		err = f.Close()
		if err != nil {
			t.Fatalf("Closing the file errored: %v", err)
		}
	}

	err := handleRenameExistingLogs(tempDir, "test.log", 0)
	if err != nil {
		t.Fatalf("handleRenameExistingLogs should not error, got: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "test.log.1")); os.IsNotExist(err) {
		t.Error("test.log.1 should exist after rename")
	}
	if _, err := os.Stat(filepath.Join(tempDir, "test.log.2")); os.IsNotExist(err) {
		t.Error("test.log.2 should exist after rename")
	}
	if _, err := os.Stat(filepath.Join(tempDir, "test.log.3")); os.IsNotExist(err) {
		t.Error("test.log.3 should exist after rename")
	}
}

func TestHandleRenameExistingLogsEmpty(t *testing.T) {
	tempDir := t.TempDir()

	err := handleRenameExistingLogs(tempDir, "test.log", 0)
	if err != nil {
		t.Fatalf("handleRenameExistingLogs should not error on empty dir, got: %v", err)
	}
}

func TestHandleRenameExistingLogsSingleFile(t *testing.T) {
	tempDir := t.TempDir()

	f, err := os.Create(filepath.Join(tempDir, "test.log"))
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	err = f.Close()
	if err != nil {
		t.Fatalf("Closing the file errored: %v", err)
	}

	err = handleRenameExistingLogs(tempDir, "test.log", 0)
	if err != nil {
		t.Fatalf("handleRenameExistingLogs should not error, got: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "test.log.1")); os.IsNotExist(err) {
		t.Error("test.log should be renamed to test.log.1")
	}
}

func TestStartDaemonProcess_AlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")

	pid := os.Getpid()
	if err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", pid), 0644); err != nil {
		t.Fatalf("Failed to write pid file: %v", err)
	}

	err := startDaemonProcess(context.Background(), pidFile, false)
	if err == nil {
		t.Fatal("expected error when daemon is already running")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

// The stale-pidfile-then-exec success path spawns a real subprocess via os.Executable();
// same Linux+root/e2e-territory exclusion as forkDaemon's success path, skip.

func TestCapturedWriter_WriteAndString(t *testing.T) {
	var cw CapturedWriter
	n, err := cw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write should not error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if cw.String() != "hello" {
		t.Errorf("expected 'hello', got %q", cw.String())
	}
}

func TestCapturedWriter_TruncatesPastMax(t *testing.T) {
	var cw CapturedWriter
	big := strings.Repeat("a", maxCapturedStderr+100)
	n, err := cw.Write([]byte(big))
	if err != nil {
		t.Fatalf("Write should not error: %v", err)
	}
	if n != len(big) {
		t.Errorf("Write should report full length written (io.Writer contract), got %d want %d", n, len(big))
	}
	if len(cw.String()) != maxCapturedStderr {
		t.Errorf("expected buffer capped at %d bytes, got %d", maxCapturedStderr, len(cw.String()))
	}
}

func TestCapturedWriter_MultipleWritesRespectCap(t *testing.T) {
	var cw CapturedWriter
	half := strings.Repeat("b", maxCapturedStderr/2+10)
	if _, err := cw.Write([]byte(half)); err != nil {
		t.Fatalf("first Write should not error: %v", err)
	}
	if _, err := cw.Write([]byte(half)); err != nil {
		t.Fatalf("second Write should not error: %v", err)
	}
	if len(cw.String()) != maxCapturedStderr {
		t.Errorf("expected buffer capped at %d bytes across writes, got %d", maxCapturedStderr, len(cw.String()))
	}
}

func TestWaitForPIDFileReadyReturnsNil(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")
	// A PID file naming this live test process makes isDaemonRunning report
	// running, so waitForPIDFile should succeed on the first tick.
	if err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", os.Getpid()), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	if err := waitForPIDFile(context.Background(), pidFile, &CapturedWriter{}); err != nil {
		t.Errorf("waitForPIDFile should return nil when the daemon is running, got %v", err)
	}
}

func TestWaitForPIDFileCanceledContext(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "test.pid")
	// No PID file on disk, so isDaemonRunning stays false; a canceled context
	// must make waitForPIDFile return promptly with the context error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForPIDFile(ctx, pidFile, &CapturedWriter{})
	if err == nil {
		t.Fatal("waitForPIDFile should return an error when the context is canceled")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("expected canceled context error, got %v", err)
	}
}

func TestPIDFileTimeoutErrIncludesStderr(t *testing.T) {
	cw := &CapturedWriter{}
	if _, err := cw.Write([]byte("boom happened")); err != nil {
		t.Fatalf("write to capture buffer: %v", err)
	}

	err := pidFileTimeoutErr("/tmp/some.pid", cw)
	if !strings.Contains(err.Error(), "boom happened") {
		t.Errorf("expected captured stderr in error, got %v", err)
	}

	if err := pidFileTimeoutErr("/tmp/some.pid", &CapturedWriter{}); strings.Contains(err.Error(), "child stderr") {
		t.Errorf("expected no stderr section when capture empty, got %v", err)
	}
}
