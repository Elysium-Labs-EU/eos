package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

// shortTempDir creates a temp dir under /tmp to avoid hitting the macOS Unix
// socket path length limit (104 bytes including the null terminator).
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "eos-e2e-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// daemonInitCfg builds a StandaloneDaemonConfig with paths rooted at dir.
func daemonInitCfg(dir string) *config.StandaloneDaemonConfig {
	logDir := filepath.Join(dir, "logs")
	return &config.StandaloneDaemonConfig{
		PIDFile:       filepath.Join(dir, "eos.pid"),
		SocketPath:    filepath.Join(dir, "eos.sock"),
		SocketTimeout: 5 * time.Second,
		Log: config.DaemonLogConfig{
			LogDir:           logDir,
			LogFileName:      "daemon.log",
			LogMaxFiles:      config.DaemonLogMaxFiles,
			LogFileSizeLimit: config.DaemonLogFileSizeLimit,
		},
	}
}

func readDaemonLog(t *testing.T, standalone *config.StandaloneDaemonConfig) []map[string]any {
	t.Helper()
	logPath := filepath.Join(standalone.Log.LogDir, standalone.Log.LogFileName)
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log %q: %v", logPath, err)
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

func TestNewStandaloneDaemon_E2E_VerboseOn_WritesDebugLifecycleLogs(t *testing.T) {
	sockDir := shortTempDir(t)
	_, _, dbDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	standalone := daemonInitCfg(sockDir)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d, err := newStandaloneDaemon(ctx, false /* logToFileAndConsole */, true /* verbose */, dbDir, standalone)
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}
	d.shutdown()

	entries := readDaemonLog(t, standalone)

	wantDebugMsgs := []string{
		"PID written",
		"socket listening",
		"database connected",
	}

	var debugMsgs []string
	for _, e := range entries {
		if e["level"] == "DEBUG" {
			if msg, ok := e["msg"].(string); ok {
				debugMsgs = append(debugMsgs, msg)
			}
		}
	}

	for _, want := range wantDebugMsgs {
		found := false
		for _, got := range debugMsgs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected DEBUG log %q not found; got: %v", want, debugMsgs)
		}
	}
}

func TestNewStandaloneDaemon_E2E_VerboseOff_NoDebugLifecycleLogs(t *testing.T) {
	sockDir := shortTempDir(t)
	_, _, dbDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	standalone := daemonInitCfg(sockDir)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d, err := newStandaloneDaemon(ctx, false /* logToFileAndConsole */, false /* verbose */, dbDir, standalone)
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}
	d.shutdown()

	for _, e := range readDaemonLog(t, standalone) {
		if e["level"] == "DEBUG" {
			raw, _ := json.Marshal(e)
			t.Errorf("unexpected DEBUG entry with verbose=false: %s", raw)
		}
	}
}
