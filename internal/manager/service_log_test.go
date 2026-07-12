package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

func TestGetServiceLogFilePath(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, _, err := mgr.NewServiceLogFiles("log-path-svc"); err != nil {
		t.Fatalf("NewServiceLogFiles: %v", err)
	}

	logPath, err := mgr.GetServiceLogFilePath("log-path-svc", false)
	if err != nil {
		t.Fatalf("GetServiceLogFilePath(stdout): %v", err)
	}
	if !strings.HasSuffix(*logPath, "log-path-svc-out.log") {
		t.Errorf("expected stdout log path suffix, got %q", *logPath)
	}

	errorLogPath, err := mgr.GetServiceLogFilePath("log-path-svc", true)
	if err != nil {
		t.Fatalf("GetServiceLogFilePath(stderr): %v", err)
	}
	if !strings.HasSuffix(*errorLogPath, "log-path-svc-error.log") {
		t.Errorf("expected stderr log path suffix, got %q", *errorLogPath)
	}
}

func TestGetServiceLogFilePath_missing(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, err := mgr.GetServiceLogFilePath("no-such-svc", false); err == nil {
		t.Fatal("expected error for missing stdout log file")
	}
	if _, err := mgr.GetServiceLogFilePath("no-such-svc", true); err == nil {
		t.Fatal("expected error for missing stderr log file")
	}
}

func TestLogToServiceStdoutAndStderr(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, _, err := mgr.NewServiceLogFiles("health-log-svc"); err != nil {
		t.Fatalf("NewServiceLogFiles: %v", err)
	}

	if err := mgr.LogToServiceStdout("health-log-svc", "stdout message"); err != nil {
		t.Fatalf("LogToServiceStdout: %v", err)
	}
	if err := mgr.LogToServiceStderr("health-log-svc", "stderr message"); err != nil {
		t.Fatalf("LogToServiceStderr: %v", err)
	}

	logPath := filepath.Join(CreateLogDirPath(tempDir), CreateOutputLogFilename("health-log-svc"))
	content, err := os.ReadFile(logPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(content), "stdout message") {
		t.Errorf("expected stdout message in log file, got: %s", content)
	}

	errorLogPath := filepath.Join(CreateLogDirPath(tempDir), CreateErrorOutputLogFilename("health-log-svc"))
	errorContent, err := os.ReadFile(errorLogPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("reading error log file: %v", err)
	}
	if !strings.Contains(string(errorContent), "stderr message") {
		t.Errorf("expected stderr message in error log file, got: %s", errorContent)
	}
}

func TestLogToServiceStdout_missingLogFile(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if err := mgr.LogToServiceStdout("no-such-svc", "message"); err == nil {
		t.Fatal("expected error when log file doesn't exist")
	}
}
