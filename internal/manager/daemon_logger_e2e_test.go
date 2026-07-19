package manager_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
)

// newE2ELogger is a helper that creates a NewDaemonLogger writing to a temp dir
// and returns the logger and the path to the log file.
func newE2ELogger(t *testing.T, verbose bool) (logger interface {
	Info(string, ...any)
	Debug(string, ...any)
}, logPath string) {
	t.Helper()
	tempDir := t.TempDir()
	cfg := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("e2e.log"))
	lc := cfg.Standalone.Log

	l, err := manager.NewDaemonLogger(false, verbose, lc.LogDir, lc.LogFileName, lc.LogMaxFiles, lc.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger: %v", err)
	}
	return l, filepath.Join(lc.LogDir, lc.LogFileName)
}

// readJSONLines reads path and unmarshals each line, validating that every
// line the logger wrote is well-formed JSON (not just readable text).
func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file %q: %v", path, err)
	}
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("non-JSON log line: %q", line)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestDaemonLogger_E2E_VerboseOff_SuppressesDebug(t *testing.T) {
	logger, logPath := newE2ELogger(t, false)

	logger.Info("info visible")
	logger.Debug("debug hidden")

	entries := readJSONLines(t, logPath)

	var sawInfo, sawDebug bool
	for _, e := range entries {
		switch e["level"] {
		case "INFO":
			sawInfo = true
		case "DEBUG":
			sawDebug = true
		}
	}

	if !sawInfo {
		t.Error("expected INFO entry, got none")
	}
	if sawDebug {
		t.Error("DEBUG entry present with verbose=false — should be suppressed")
	}
}

func TestDaemonLogger_E2E_VerboseOn_IncludesDebug(t *testing.T) {
	logger, logPath := newE2ELogger(t, true)

	logger.Info("info visible")
	logger.Debug("debug also visible")

	entries := readJSONLines(t, logPath)

	var sawInfo, sawDebug bool
	for _, e := range entries {
		switch e["level"] {
		case "INFO":
			sawInfo = true
		case "DEBUG":
			sawDebug = true
		}
	}

	if !sawInfo {
		t.Error("expected INFO entry with verbose=true, got none")
	}
	if !sawDebug {
		t.Error("expected DEBUG entry with verbose=true, got none")
	}
}

func TestDaemonLogger_E2E_JSONStructure(t *testing.T) {
	logger, logPath := newE2ELogger(t, false)
	logger.Info("structured log", "key", "value", "count", 42)

	entries := readJSONLines(t, logPath)
	if len(entries) == 0 {
		t.Fatal("no log entries written")
	}

	e := entries[len(entries)-1]

	if _, ok := e["time"]; !ok {
		t.Error("missing 'time' field in JSON log entry")
	}
	if _, ok := e["level"]; !ok {
		t.Error("missing 'level' field in JSON log entry")
	}
	if _, ok := e["msg"]; !ok {
		t.Error("missing 'msg' field in JSON log entry")
	}
	if e["msg"] != "structured log" {
		t.Errorf("msg = %q, want %q", e["msg"], "structured log")
	}
	if e["key"] != "value" {
		t.Errorf("key = %q, want %q", e["key"], "value")
	}
	if e["count"] != float64(42) {
		t.Errorf("count = %v, want 42", e["count"])
	}
}

func TestDaemonLogger_E2E_MultiWriter_WritesToFile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("e2e.log"))
	lc := cfg.Standalone.Log

	// logToFileAndConsole=true — also writes to stdout, but we only assert the file.
	logger, err := manager.NewDaemonLogger(true, false, lc.LogDir, lc.LogFileName, lc.LogMaxFiles, lc.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("NewDaemonLogger: %v", err)
	}

	logger.Info("multiwriter message")

	logPath := filepath.Join(lc.LogDir, lc.LogFileName)
	entries := readJSONLines(t, logPath)

	var found bool
	for _, e := range entries {
		if e["msg"] == "multiwriter message" {
			found = true
		}
	}
	if !found {
		t.Error("message not found in log file when logToFileAndConsole=true")
	}
}
