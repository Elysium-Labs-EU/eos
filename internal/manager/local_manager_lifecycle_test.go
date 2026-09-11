package manager

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"go.uber.org/goleak"
)

// pollUntil polls cond every 5ms until it returns true or deadline elapses,
// returning the final result. Used throughout this file instead of a single
// fixed sleep, since these tests assert on goroutine side effects with no
// other synchronization point available.
func pollUntil(deadline time.Duration, cond func() bool) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// isFileClosed reports whether f has already been closed, by attempting a
// zero-effect operation on it.
func isFileClosed(f *os.File) bool {
	_, err := f.Stat()
	return err != nil
}

// --- closePipeOnCancel: kill-before-Wait / defer-close ordering coverage ---
//
// closePipeOnCancel's four branches (stop-before-cancel, cancel+no-grace,
// cancel+grace-elapses, cancel+grace+stop-wins) are exactly the "kill before
// Wait on every exit path" shape STYLE.md calls out: whichever path fires,
// the pipe must end up in a well-defined open/closed state, never racing.

func TestClosePipeOnCancelStopBeforeCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := &LocalManager{ctx: ctx}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	defer func() { _ = w.Close() }()

	stop := m.closePipeOnCancel(r)
	stop()

	// The watcher goroutine must exit promptly without touching r: cancel
	// never fires, so r must still be open.
	if !pollUntil(time.Second, func() bool { return true }) {
		t.Fatal("unreachable")
	}
	time.Sleep(20 * time.Millisecond)
	if isFileClosed(r) {
		t.Fatal("stop before cancel must not close the pipe")
	}
	_ = r.Close()
}

func TestClosePipeOnCancelNoGracePeriod(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(t.Context())
	m := &LocalManager{ctx: ctx}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	defer func() { _ = w.Close() }()

	stop := m.closePipeOnCancel(r)
	defer stop()

	cancel()
	if !pollUntil(time.Second, func() bool { return isFileClosed(r) }) {
		t.Fatal("expected pipe to be closed immediately after cancel with no grace period")
	}
}

func TestClosePipeOnCancelGracePeriodElapses(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(t.Context())
	m := &LocalManager{ctx: ctx, shutdownGracePeriod: 30 * time.Millisecond}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	defer func() { _ = w.Close() }()

	stop := m.closePipeOnCancel(r)
	defer stop()

	cancel()
	// Immediately after cancel, still within the grace period: must remain open.
	time.Sleep(5 * time.Millisecond)
	if isFileClosed(r) {
		t.Fatal("pipe closed before shutdown grace period elapsed")
	}
	if !pollUntil(time.Second, func() bool { return isFileClosed(r) }) {
		t.Fatal("expected pipe to close once shutdown grace period elapsed")
	}
}

func TestClosePipeOnCancelStopDuringGracePeriod(t *testing.T) {
	defer goleak.VerifyNone(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := &LocalManager{ctx: ctx, shutdownGracePeriod: time.Second}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	defer func() { _ = w.Close() }()

	stop := m.closePipeOnCancel(r)
	cancel()
	time.Sleep(10 * time.Millisecond)
	stop() // caller's own read loop exited before the grace timer fired

	time.Sleep(50 * time.Millisecond)
	if isFileClosed(r) {
		t.Fatal("stop during grace period must win the race and leave the pipe open")
	}
	_ = r.Close()
}

// --- pipeToLogFile: scanner-error branch ---

// TestPipeToLogFileScanError feeds a single line longer than bufio.Scanner's
// default token limit, forcing scanErr != nil on a still-live ctx so the
// scanning-error log branch runs, then confirms the goroutine still tears
// down its pipe and log-writer reference exactly like the clean-EOF path.
func TestPipeToLogFileScanError(t *testing.T) {
	defer goleak.VerifyNone(t)
	m := &LocalManager{ctx: context.Background(), logger: testutil.NewTestLogger(t)}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	go func() {
		_, _ = w.Write(bytes.Repeat([]byte("a"), 70000)) // no newline: exceeds scanner's default buffer
		_ = w.Close()
	}()

	var buf bytes.Buffer
	m.pipeWg.Add(1)
	m.pipeToLogFile(r, &buf, "svc", 123, nil, nil)
	m.WaitPipes()

	if !isFileClosed(r) {
		t.Fatal("expected read pipe to be closed after scan error")
	}
}

// TestPipeToErrorLogFileScanError mirrors the above for the stderr path.
func TestPipeToErrorLogFileScanError(t *testing.T) {
	defer goleak.VerifyNone(t)
	m := &LocalManager{ctx: context.Background(), logger: testutil.NewTestLogger(t)}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	go func() {
		_, _ = w.Write(bytes.Repeat([]byte("b"), 70000))
		_ = w.Close()
	}()

	errLogger := testutil.NewTestLogger(t)
	var wg sync.WaitGroup
	wg.Add(1)
	m.pipeWg.Add(1)
	m.pipeToErrorLogFile(r, errLogger, "svc", 456, nil, &wg)
	m.WaitPipes()
	wg.Wait() // the wg-non-nil branch: pipeToErrorLogFile must have called wg.Done()

	if !isFileClosed(r) {
		t.Fatal("expected read pipe to be closed after scan error")
	}
}

// --- prepareLaunchIO / prepareLogFiles: log-path error branch ---

// TestPrepareLaunchIOLogFileError exercises prepareLaunchIO's first error
// return (prepareLogFiles failing) by giving it a name that makes
// joinLogPath's traversal check fail, without needing a real OS-level
// failure injection.
func TestPrepareLaunchIOLogFileError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	_, err := m.prepareLaunchIO("../escape", &types.ServiceConfig{})
	if err == nil {
		t.Fatal("expected prepareLaunchIO to fail when the log path escapes the log directory")
	}
}

func TestPrepareLogFilesEscapingNameFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	_, _, err := m.prepareLogFiles("../escape", &types.ServiceConfig{})
	if err == nil {
		t.Fatal("expected prepareLogFiles to fail for a name that escapes the log directory")
	}
}

// --- closeAll: joined-close-error branch ---

func TestLaunchIOCloseAllReportsCloseErrors(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	lio, err := m.prepareLaunchIO("svc", &types.ServiceConfig{})
	if err != nil {
		t.Fatalf("prepareLaunchIO: %v", err)
	}
	// Pre-close one fd so closeAll's own Close() call on it fails, joining a
	// close error into its return instead of the nil-error happy path.
	if closeErr := lio.readLog.Close(); closeErr != nil {
		t.Fatalf("pre-closing readLog: %v", closeErr)
	}

	if err := lio.closeAll(m, "svc"); err == nil {
		t.Fatal("expected closeAll to report the already-closed fd")
	}
}

// --- buildLaunchCommand: shutdown-grace-period and env-error branches ---

func TestBuildLaunchCommandSetsCancelWhenGracePeriodConfigured(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithShutdownGracePeriod(5*time.Second), WithExecutor(fakeExecutor{}))

	lio, err := m.prepareLaunchIO("svc", &types.ServiceConfig{Command: "true"})
	if err != nil {
		t.Fatalf("prepareLaunchIO: %v", err)
	}
	defer func() { _ = lio.closeAll(m, "svc") }()

	service := &types.ServiceCatalogEntry{Name: "svc", DirectoryPath: tempDir}
	cmd, err := m.buildLaunchCommand(service, &types.ServiceConfig{Command: "true"}, lio)
	if err != nil {
		t.Fatalf("buildLaunchCommand: %v", err)
	}
	if cmd.Cancel == nil {
		t.Fatal("expected Cancel to be set when shutdownGracePeriod > 0")
	}
	if cmd.WaitDelay != 5*time.Second {
		t.Fatalf("expected WaitDelay to equal the configured grace period, got %v", cmd.WaitDelay)
	}
}

func TestBuildLaunchCommandEnvironmentErrorPropagates(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	cfg := &types.ServiceConfig{Command: "true", EnvFile: "../escape"}
	lio, err := m.prepareLaunchIO("svc", cfg)
	if err != nil {
		t.Fatalf("prepareLaunchIO: %v", err)
	}
	defer func() { _ = lio.closeAll(m, "svc") }()

	service := &types.ServiceCatalogEntry{Name: "svc", DirectoryPath: tempDir}
	if _, err := m.buildLaunchCommand(service, cfg, lio); err == nil {
		t.Fatal("expected buildLaunchCommand to fail when env_file escapes the service directory")
	}
}

// --- wireLogPipes: close-error and sink-fan-out branches, with a goleak
// check specific to the log-pipe goroutines it launches ---

func TestWireLogPipesReportsWriteLogCloseError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	lio, err := m.prepareLaunchIO("svc", &types.ServiceConfig{})
	if err != nil {
		t.Fatalf("prepareLaunchIO: %v", err)
	}
	if closeErr := lio.writeLog.Close(); closeErr != nil {
		t.Fatalf("pre-closing writeLog: %v", closeErr)
	}

	if err := m.wireLogPipes(lio, nil, "svc", 1); err == nil {
		t.Fatal("expected wireLogPipes to fail when writeLog is already closed")
	}
	_ = lio.readLog.Close()
	_ = lio.readErr.Close()
	_ = lio.writeErr.Close()
	_ = m.releaseServiceLogWriter("svc", false)
	_ = m.releaseServiceLogWriter("svc", true)
}

func TestWireLogPipesReportsWriteErrCloseError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	lio, err := m.prepareLaunchIO("svc", &types.ServiceConfig{})
	if err != nil {
		t.Fatalf("prepareLaunchIO: %v", err)
	}
	if closeErr := lio.writeErr.Close(); closeErr != nil {
		t.Fatalf("pre-closing writeErr: %v", closeErr)
	}

	if err := m.wireLogPipes(lio, nil, "svc", 1); err == nil {
		t.Fatal("expected wireLogPipes to fail when writeErr is already closed")
	}
	_ = lio.readLog.Close()
	_ = lio.readErr.Close()
	_ = lio.writeLog.Close()
	_ = m.releaseServiceLogWriter("svc", false)
	_ = m.releaseServiceLogWriter("svc", true)
}

// TestWireLogPipesFansOutToSinks exercises wireLogPipes' len(sinks) > 0
// branch (the sinkWg-gated stopSinkProcesses goroutine) with a sink config
// that's intentionally invalid (no mode/address), so sinkProcess.Run exits
// immediately instead of spawning a real plugin subprocess. It then confirms
// every goroutine wireLogPipes launched — the two pipe forwarders and the
// sink-stop watcher — actually exits, which is exactly the drain-ordering
// concern STYLE.md calls out for concurrent shutdown of a feed/drain pair.
func TestWireLogPipesFansOutToSinks(t *testing.T) {
	defer goleak.VerifyNone(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	// t.Cleanup-based close (registered inside SetupTestDB) only runs after
	// this test function's own defers — including goleak.VerifyNone above —
	// have already fired, so database/sql's connectionOpener goroutine for
	// this test's own db is still reliably alive at that point. Close it
	// explicitly before returning so goleak observes a torn-down connection
	// rather than racing its background goroutine's own shutdown.
	defer func() { _ = db.CloseDBConnection() }()
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	lio, err := m.prepareLaunchIO("svc", &types.ServiceConfig{})
	if err != nil {
		t.Fatalf("prepareLaunchIO: %v", err)
	}

	sinks := []types.LogSink{{Type: "noop"}} // invalid: no Mode/Address, so Run() exits at once
	if err := m.wireLogPipes(lio, sinks, "svc", 1); err != nil {
		t.Fatalf("wireLogPipes: %v", err)
	}

	// Nothing else holds the write ends (wireLogPipes already closed them and
	// no child process exists), so both forwarders see EOF almost immediately.
	// A single WaitPipes call, not a goroutine spawned per poll attempt: the
	// latter leaves however many timed-out attempts still winding down at the
	// moment goleak.VerifyNone runs, which is exactly the kind of drain-
	// ordering race STYLE.md warns about.
	done := make(chan struct{})
	go func() { m.WaitPipes(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected pipe-forwarding goroutines to finish")
	}
}

// --- acquireLogWriter: escaping-path branch ---

func TestAcquireLogWriterEscapingPathFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if _, err := m.acquireLogWriter(CreateLogDirPath(tempDir), "../escape", 1, 1024); err == nil {
		t.Fatal("expected acquireLogWriter to fail for a path that escapes the log directory")
	}
}
