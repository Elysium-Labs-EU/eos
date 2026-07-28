package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
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
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
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

	d, err := newStandaloneDaemon(ctx, false /* logToFileAndConsole */, true /* verbose */, dbDir, standalone, config.ShutdownConfig{}, config.TelemetryConfig{})
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}
	d.shutdown(ctx)

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
		if !slices.Contains(debugMsgs, want) {
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

	d, err := newStandaloneDaemon(ctx, false /* logToFileAndConsole */, false /* verbose */, dbDir, standalone, config.ShutdownConfig{}, config.TelemetryConfig{})
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}
	d.shutdown(ctx)

	for _, e := range readDaemonLog(t, standalone) {
		if e["level"] == "DEBUG" {
			raw, _ := json.Marshal(e)
			t.Errorf("unexpected DEBUG entry with verbose=false: %s", raw)
		}
	}
}

// TestNewStandaloneDaemon_E2E_SocketIsOwnerOnly proves the control socket is
// pinned to 0600 right after bind(2), rather than left at whatever mode the
// process umask happened to produce — the fix for the socket half of issue
// #121 (peer-uid checking in handleConnection is the other half).
func TestNewStandaloneDaemon_E2E_SocketIsOwnerOnly(t *testing.T) {
	sockDir := shortTempDir(t)
	_, _, dbDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	t.Setenv("EOS_BASE_DIR", dbDir)

	standalone := daemonInitCfg(sockDir)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d, err := newStandaloneDaemon(ctx, false /* logToFileAndConsole */, false /* verbose */, dbDir, standalone, config.ShutdownConfig{}, config.TelemetryConfig{})
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}
	defer d.shutdown(ctx)

	info, err := os.Stat(standalone.SocketPath)
	if err != nil {
		t.Fatalf("stat %s: %v", standalone.SocketPath, err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("expected socket mode 0600, got %#o", got)
	}
}

// ownerUID returns the owning uid of path, or fails the test.
func ownerUID(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("non-POSIX filesystem; cannot read owner uid")
	}
	return int(stat.Uid)
}

// TestNewStandaloneDaemon_E2E_RootAlignsPIDFileAndLogOwnership verifies the
// fix for issue #91: when newStandaloneDaemon runs as root, the PID file and
// the log dir/file it creates are chowned to match baseDir's owner rather
// than being left root-owned — the same self-healing behavior
// alignDataFileOwnership already gives state.db (issue #14). PIDFile/LogDir
// deliberately live under sockDir here, separate from baseDir (dbDir), to
// prove ownership is aligned to baseDir's owner regardless of where those
// paths sit relative to it.
func TestNewStandaloneDaemon_E2E_RootAlignsPIDFileAndLogOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to chown files to another uid")
	}
	const targetUID, targetGID = 12345, 12345

	sockDir := shortTempDir(t)
	_, _, dbDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	if err := os.Chown(dbDir, targetUID, targetGID); err != nil {
		t.Fatalf("chown base dir to target uid: %v", err)
	}

	standalone := daemonInitCfg(sockDir)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d, err := newStandaloneDaemon(ctx, false /* logToFileAndConsole */, false /* verbose */, dbDir, standalone, config.ShutdownConfig{}, config.TelemetryConfig{})
	if err != nil {
		t.Fatalf("newStandaloneDaemon: %v", err)
	}
	defer d.shutdown(ctx)

	logPath := filepath.Join(standalone.Log.LogDir, standalone.Log.LogFileName)
	for _, p := range []string{standalone.PIDFile, standalone.Log.LogDir, logPath} {
		if got := ownerUID(t, p); got != targetUID {
			t.Errorf("%s: expected owner uid %d (matching base dir), got %d", p, targetUID, got)
		}
	}
}
