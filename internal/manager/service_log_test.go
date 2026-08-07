package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
)

func TestJoinLogPath(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{"normal filename", "svc-out.log", false},
		{"traversal escapes logDir", "../../pwned-out.log", true},
		{"traversal collapses back inside logDir", "sub/../svc-out.log", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := joinLogPath(logDir, tt.filename)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("joinLogPath(%q, %q) = %q, want error", logDir, tt.filename, path)
				}
				return
			}
			if err != nil {
				t.Fatalf("joinLogPath(%q, %q) unexpected error: %v", logDir, tt.filename, err)
			}
			if !strings.HasPrefix(path, filepath.Clean(logDir)+string(filepath.Separator)) {
				t.Errorf("joinLogPath(%q, %q) = %q, want prefix %q", logDir, tt.filename, path, logDir)
			}
		})
	}
}

// NewServiceLogFiles takes a serviceName straight from ValidateServiceName's
// safe charset in every real caller; this test bypasses that upstream
// guarantee to exercise joinLogPath's own defense-in-depth check in
// isolation, simulating what would happen if a future caller forgot to
// validate the name first.
func TestNewServiceLogFiles_rejectsPathTraversalName(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, _, err := mgr.NewServiceLogFiles(t.Context(), "../../pwned"); err == nil {
		t.Fatal("expected NewServiceLogFiles to reject a path-traversal service name")
	}

	if _, err := os.Stat(filepath.Join(tempDir, "..", "..", "pwned-out.log")); !os.IsNotExist(err) {
		t.Errorf("expected no file to have escaped tempDir, stat err: %v", err)
	}
}

func TestGetServiceLogFilePath(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, _, err := mgr.NewServiceLogFiles(t.Context(), "log-path-svc"); err != nil {
		t.Fatalf("NewServiceLogFiles: %v", err)
	}

	logPath, err := mgr.GetServiceLogFilePath(t.Context(), "log-path-svc", false)
	if err != nil {
		t.Fatalf("GetServiceLogFilePath(stdout): %v", err)
	}
	if !strings.HasSuffix(*logPath, "log-path-svc-out.log") {
		t.Errorf("expected stdout log path suffix, got %q", *logPath)
	}

	errorLogPath, err := mgr.GetServiceLogFilePath(t.Context(), "log-path-svc", true)
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

	if _, err := mgr.GetServiceLogFilePath(t.Context(), "no-such-svc", false); err == nil {
		t.Fatal("expected error for missing stdout log file")
	}
	if _, err := mgr.GetServiceLogFilePath(t.Context(), "no-such-svc", true); err == nil {
		t.Fatal("expected error for missing stderr log file")
	}
}

func TestLogToServiceStdoutAndStderr(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, _, err := mgr.NewServiceLogFiles(t.Context(), "health-log-svc"); err != nil {
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

// TestAcquireServiceLogWriter_ConcurrentAcquireSharesOneInstance is a
// regression test for a bug where a running service's stdout/stderr
// pipe-forwarding writer and the health monitor's breadcrumb writes
// (LogToServiceStdout/LogToServiceStderr, which fires while a service is
// running — see health_monitor.go's checkStartProcess) each opened their own
// independent RotatingFileWriter onto the same log path. Two independent
// writers meant two independent size counters and two independent *os.File
// handles: one writer's rotate() renaming the file out from under the other's
// fd broke both the size cap and log continuity. Every caller must now share
// exactly one RotatingFileWriter per log path.
func TestAcquireServiceLogWriter_ConcurrentAcquireSharesOneInstance(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	const serviceName = "shared-writer-svc"
	const holders = 20

	var wg sync.WaitGroup
	writers := make([]*RotatingFileWriter, holders)
	errs := make([]error, holders)
	for i := range holders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, err := mgr.acquireServiceLogWriter(serviceName, false, 5, 10*1024*1024)
			writers[i] = w
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	first := writers[0]
	if first == nil {
		t.Fatal("acquire returned a nil writer")
	}
	for i, w := range writers {
		if w != first {
			t.Errorf("acquire %d returned a distinct *RotatingFileWriter instance; every concurrent acquire for the same log path must share one instance", i)
		}
	}

	for i := range holders {
		if err := mgr.releaseServiceLogWriter(serviceName, false); err != nil {
			t.Errorf("release %d: %v", i, err)
		}
	}

	mgr.logWritersMu.Lock()
	_, stillTracked := mgr.logWriters[filepath.Join(CreateLogDirPath(tempDir), CreateOutputLogFilename(serviceName))]
	mgr.logWritersMu.Unlock()
	if stillTracked {
		t.Error("expected the shared writer to be removed from the registry once every acquire was released")
	}
}

// TestAcquireServiceLogWriter_ConcurrentPipeAndHealthEventWrites simulates the
// real-world race from issue #78's follow-up: a running service's
// pipe-forwarding goroutine holds the writer for the service's whole
// lifetime (one long acquire, many writes, one release — like
// pipeToLogFile), while concurrent health-monitor breadcrumbs each do a
// short-lived acquire/write/release (like appendHealthEventToLog). With a
// small size limit forcing frequent rotation, the rotated-file count must
// still respect maxFiles and no write may error, which only holds if every
// writer shares one RotatingFileWriter's lock and size counter.
func TestAcquireServiceLogWriter_ConcurrentPipeAndHealthEventWrites(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	const serviceName = "concurrent-rotate-svc"
	const maxFiles = 3
	const sizeLimit = int64(500)

	pipeDone := make(chan error, 1)
	go func() {
		w, err := mgr.acquireServiceLogWriter(serviceName, false, maxFiles, sizeLimit)
		if err != nil {
			pipeDone <- fmt.Errorf("acquire: %w", err)
			return
		}
		for i := range 300 {
			if _, err := fmt.Fprintf(w, "pipe forwarder line %d ....................\n", i); err != nil {
				pipeDone <- fmt.Errorf("write: %w", err)
				return
			}
		}
		pipeDone <- mgr.releaseServiceLogWriter(serviceName, false)
	}()

	const healthEvents = 200
	var healthWg sync.WaitGroup
	healthErrs := make([]error, healthEvents)
	for i := range healthEvents {
		healthWg.Add(1)
		go func(i int) {
			defer healthWg.Done()
			w, err := mgr.acquireServiceLogWriter(serviceName, false, maxFiles, sizeLimit)
			if err != nil {
				healthErrs[i] = fmt.Errorf("acquire: %w", err)
				return
			}
			if _, err := fmt.Fprintf(w, "health breadcrumb %d\n", i); err != nil {
				healthErrs[i] = fmt.Errorf("write: %w", err)
				return
			}
			healthErrs[i] = mgr.releaseServiceLogWriter(serviceName, false)
		}(i)
	}

	if err := <-pipeDone; err != nil {
		t.Fatalf("pipe forwarder: %v", err)
	}
	healthWg.Wait()
	for i, err := range healthErrs {
		if err != nil {
			t.Errorf("health event %d: %v", i, err)
		}
	}

	logDir := CreateLogDirPath(tempDir)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("reading log dir: %v", err)
	}
	if len(entries) > maxFiles {
		t.Errorf("expected at most %d rotated log files under maxFiles enforcement, got %d: %v", maxFiles, len(entries), entries)
	}

	mgr.logWritersMu.Lock()
	remaining := len(mgr.logWriters)
	mgr.logWritersMu.Unlock()
	if remaining != 0 {
		t.Errorf("expected every acquire to have been released and the registry emptied, got %d entries still tracked", remaining)
	}
}
