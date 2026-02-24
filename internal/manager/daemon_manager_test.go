package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eos/internal/testutil"
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

	f, err := os.Create(socketPath)
	if err != nil {
		t.Fatalf("Failed to create socket file: %v", err)
	}
	err = f.Close()
	if err != nil {
		t.Fatalf("Closing the file errored: %v", err)
	}

	err = waitForSocket(socketPath, 1*time.Second)
	if err != nil {
		t.Errorf("waitForSocket should succeed when socket exists, got: %v", err)
	}
}

func TestWaitForSocketTimeout(t *testing.T) {
	err := waitForSocket("/nonexistent/socket.sock", 200*time.Millisecond)
	if err == nil {
		t.Fatal("waitForSocket should error on timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Error should mention timeout, got: %v", err)
	}
}

func TestWaitForSocketDelayedCreation(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test.sock")

	// Create the socket after a short delay
	go func() {
		time.Sleep(300 * time.Millisecond)
		f, err := os.Create(socketPath)
		if err != nil {
			return
		}
		err = f.Close()
		if err != nil {
			t.Errorf("Closing the file errored: %v", err)
		}
	}()

	err := waitForSocket(socketPath, 2*time.Second)
	if err != nil {
		t.Errorf("waitForSocket should succeed when socket appears within timeout, got: %v", err)
	}
}

func TestNewDaemonLogger(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}
	if logger == nil {
		t.Fatal("Logger should not be nil")
	}
	if logger.LogPath == "" {
		t.Error("LogPath should not be empty")
	}
	if logger.file == nil {
		t.Error("Log file should not be nil")
	}
}

func TestNewDaemonLoggerCreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	_, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	if _, err := os.Stat(daemonConfig.LogDir); os.IsNotExist(err) {
		t.Error("Log directory should have been created")
	}
}

func TestDaemonLoggerLog(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	logger.Log(LogLevelInfo, "test message")

	content, err := os.ReadFile(logger.LogPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logContent := string(content)
	if len(logContent) == 0 {
		t.Error("Log file should not be empty after logging")
	}
	if !strings.Contains(logContent, "INFO") {
		t.Error("Log should contain INFO level")
	}
	if !strings.Contains(logContent, "test message") {
		t.Error("Log should contain the message")
	}
}

func TestDaemonLoggerLogLevels(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)

	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	logger.Log(LogLevelInfo, "info message")
	logger.Log(LogLevelWarn, "warn message")
	logger.Log(LogLevelError, "error message")

	content, err := os.ReadFile(logger.LogPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logContent := string(content)
	if !strings.Contains(logContent, "INFO") {
		t.Error("Log should contain INFO level")
	}
	if !strings.Contains(logContent, "WARN") {
		t.Error("Log should contain WARN level")
	}
	if !strings.Contains(logContent, "ERROR") {
		t.Error("Log should contain ERROR level")
	}
}

func TestDaemonLoggerRotation(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	// Force a small max size to trigger rotation
	logger.maxSize = 100

	for range 20 {
		logger.Log(LogLevelInfo, "This is a log message that should eventually trigger rotation")
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

func TestDaemonLoggerLogToConsole(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	if !logger.logToConsole {
		t.Error("logToConsole should be true when logToFileAndConsole is true")
	}
}

func TestDaemonLoggerLogFormat(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	logger.Log(LogLevelInfo, "formatted message")

	content, err := os.ReadFile(logger.LogPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logContent := string(content)
	// Log format should be: [timestamp] LEVEL: message
	if !strings.Contains(logContent, "] INFO: formatted message") {
		t.Errorf("Log format unexpected, got: %s", logContent)
	}
}

func TestDaemonLoggerCurrentSizeTracking(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("test.log"))

	logger, err := NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger should not error, got: %v", err)
	}

	initialSize := logger.currentSize
	logger.Log(LogLevelInfo, "some message")

	if logger.currentSize <= initialSize {
		t.Error("currentSize should increase after logging")
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

	err := handleRenameExistingLogs(tempDir, "test.log")
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

	err := handleRenameExistingLogs(tempDir, "test.log")
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

	err = handleRenameExistingLogs(tempDir, "test.log")
	if err != nil {
		t.Fatalf("handleRenameExistingLogs should not error, got: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "test.log.1")); os.IsNotExist(err) {
		t.Error("test.log should be renamed to test.log.1")
	}
}
