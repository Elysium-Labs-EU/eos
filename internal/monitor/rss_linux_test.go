//go:build linux

package monitor

import (
	"os"
	"syscall"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
)

func TestScanStatusFieldBytes_found(t *testing.T) {
	contents := []byte("Name:\tmy-proc\nVmRSS:\t12345 kB\nNSpgid:\t42\n")
	got := scanStatusFieldBytes(contents, []byte("VmRSS:\t"))
	if got == nil {
		t.Fatal("expected to find VmRSS field, got nil")
	}
	if string(got) != "12345 kB" {
		t.Errorf("got %q, want %q", string(got), "12345 kB")
	}
}

func TestScanStatusFieldBytes_notFound(t *testing.T) {
	contents := []byte("Name:\tmy-proc\nVmRSS:\t12345 kB\n")
	got := scanStatusFieldBytes(contents, []byte("NSpgid:\t"))
	if got != nil {
		t.Errorf("expected nil for missing field, got %q", got)
	}
}

func TestScanStatusFieldBytes_lastLine(t *testing.T) {
	contents := []byte("Name:\tmy-proc\nNSpgid:\t99")
	got := scanStatusFieldBytes(contents, []byte("NSpgid:\t"))
	if got == nil {
		t.Fatal("expected to find NSpgid at last line (no trailing newline)")
	}
	if string(got) != "99" {
		t.Errorf("got %q, want %q", string(got), "99")
	}
}

func TestScanStatusFieldBytes_empty(t *testing.T) {
	got := scanStatusFieldBytes([]byte{}, []byte("VmRSS:\t"))
	if got != nil {
		t.Errorf("expected nil for empty contents, got %q", got)
	}
}

func TestReadProcessRSSKb(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("failed to get pgid: %v", err)
	}

	rss, err := readProcessRSSKb(pgid, hm.procBuf[:])
	if err != nil {
		t.Fatalf("readProcessRSSKb: %v", err)
	}
	if rss <= 0 {
		t.Errorf("expected positive RSS for own process group, got %d", rss)
	}
}

// procStatusFixture is a synthetic /proc/[pid]/status body, hand-built to mimic
// the real kernel format (tab-separated fields, "kB"-suffixed memory values) so
// scanStatusFieldBytes can be benchmarked without touching the real /proc.
var procStatusFixture = []byte(
	"Name:\tbash\nState:\tS (sleeping)\nPid:\t1234\nPPid:\t1233\n" +
		"NStgid:\t1234\nNSpid:\t1234\nNSpgid:\t1234\nNSsid:\t1234\n" +
		"VmPeak:\t 22084 kB\nVmSize:\t 22084 kB\nVmRSS:\t  5436 kB\n" +
		"Threads:\t1\n",
)

func BenchmarkScanStatusFieldBytes(b *testing.B) {
	for b.Loop() {
		_ = scanStatusFieldBytes(procStatusFixture, procStatusNSpgid)
		_ = scanStatusFieldBytes(procStatusFixture, procStatusVMRSS)
	}
}
