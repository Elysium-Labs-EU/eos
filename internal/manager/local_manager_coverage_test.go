package manager

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// TestWaitServicesReturnsImmediatelyWithNoLaunches covers the zero-launch
// case: with nothing tracked on serviceWg, WaitServices must return without
// blocking.
func TestWaitServicesReturnsImmediatelyWithNoLaunches(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	done := make(chan struct{})
	go func() {
		mgr.WaitServices()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitServices did not return with nothing tracked")
	}
}

// TestWaitServicesBlocksUntilTracked covers the case where serviceWg has a
// pending entry: WaitServices must block until it completes, not return
// early.
func TestWaitServicesBlocksUntilTracked(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	release := make(chan struct{})
	mgr.serviceWg.Go(func() {
		<-release
	})

	done := make(chan struct{})
	go func() {
		mgr.WaitServices()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("WaitServices returned before the tracked goroutine finished")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitServices did not return after the tracked goroutine finished")
	}
}

func TestIsReloadInProgress(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	if mgr.IsReloadInProgress("svc") {
		t.Fatal("expected no reload in progress before beginReload")
	}
	mgr.beginReload("svc")
	if !mgr.IsReloadInProgress("svc") {
		t.Fatal("expected reload in progress after beginReload")
	}
	mgr.endReload("svc")
	if mgr.IsReloadInProgress("svc") {
		t.Fatal("expected no reload in progress after endReload")
	}
}

func TestWithSinkRegistry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	registry := map[string]types.LogSink{"main": {Type: "file"}}

	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithSinkRegistry(registry))
	if len(mgr.sinkRegistry) != 1 || mgr.sinkRegistry["main"].Type != "file" {
		t.Fatalf("expected sinkRegistry to be set from WithSinkRegistry, got %v", mgr.sinkRegistry)
	}
}

func TestWithTelemetry(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	handles := otelx.NoopHandles()

	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithTelemetry(handles))
	if mgr.telemetry != handles {
		t.Fatal("expected telemetry to be set from WithTelemetry")
	}
}

// TestKillAndWrapKillFails covers the branch where the group is already
// gone: syscall.Kill on a made-up, almost-certainly-unused PGID returns
// ESRCH, so killAndWrap must report pgid 0 (manual cleanup needed).
func TestKillAndWrapKillFails(t *testing.T) {
	const bogusPGID = 999999
	if isProcessAlive(bogusPGID) {
		t.Skip("bogus PGID happens to be alive on this machine; skipping to avoid a false result")
	}

	cleanPGID, err := killAndWrap(bogusPGID, errString("original failure"), "doing thing")
	if cleanPGID != 0 {
		t.Fatalf("expected pgid 0 when kill fails, got %d", cleanPGID)
	}
	if err == nil {
		t.Fatal("expected a non-nil wrapped error")
	}
}

// TestKillAndWrapKillSucceeds covers the branch where the group is still
// alive: killAndWrap must actually kill it and return the original pgid.
func TestKillAndWrapKillSucceeds(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting helper process: %v", err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Wait() })

	returnedPGID, err := killAndWrap(pgid, errString("bookkeeping failed"), "registering instance")
	if returnedPGID != pgid {
		t.Fatalf("expected pgid %d to be returned on successful kill, got %d", pgid, returnedPGID)
	}
	if err == nil {
		t.Fatal("expected a non-nil wrapped error")
	}

	if !pollUntil(2*time.Second, func() bool { return !isProcessAlive(pgid) }) {
		t.Fatalf("expected process group %d to be dead after killAndWrap", pgid)
	}
}

// TestValidateRuntimeBinaryInvalidConfiguredPath covers the third branch of
// validateRuntimeBinary — config.Runtime.Path set but pointing nowhere real —
// which local_manager_lifecycle_test.go's "no runtime configured" and
// "LookPath fails" cases don't reach (both take the Path == "" arm).
func TestValidateRuntimeBinaryInvalidConfiguredPath(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t), WithExecutor(fakeExecutor{}))

	cfg := &types.ServiceConfig{Runtime: types.Runtime{Type: "node", Path: filepath.Join(tempDir, "does-not-exist")}}
	if err := m.validateRuntimeBinary(cfg); err == nil {
		t.Fatal("expected an error validating a configured runtime path that doesn't exist")
	}
}

// selectiveFailDB fails exactly one named method on its first call, delegating
// every other call (and every later call to that same method) to the wrapped
// database.Database. Used to reach an error-wrapping branch that sits behind
// an earlier db call in the same function — closing the whole connection (as
// local_manager_lifecycle_test.go's TestManagerMethodsPropagateDBErrors does)
// only ever reaches the first call in each function.
type selectiveFailDB struct {
	database.Database
	err    error
	method string
	called bool
}

func (f *selectiveFailDB) IsServiceRegistered(ctx context.Context, name string) (bool, error) {
	if f.method == "IsServiceRegistered" && !f.called {
		f.called = true
		return false, f.err
	}
	return f.Database.IsServiceRegistered(ctx, name)
}

func (f *selectiveFailDB) FindServiceNameCaseInsensitive(ctx context.Context, name string) (string, bool, error) {
	if f.method == "FindServiceNameCaseInsensitive" && !f.called {
		f.called = true
		return "", false, f.err
	}
	return f.Database.FindServiceNameCaseInsensitive(ctx, name)
}

func (f *selectiveFailDB) GetServiceInstance(ctx context.Context, name string) (types.ServiceInstance, error) {
	if f.method == "GetServiceInstance" && !f.called {
		f.called = true
		return types.ServiceInstance{}, f.err
	}
	return f.Database.GetServiceInstance(ctx, name)
}

func (f *selectiveFailDB) GetServiceCatalogEntry(ctx context.Context, name string) (types.ServiceCatalogEntry, error) {
	if f.method == "GetServiceCatalogEntry" && !f.called {
		f.called = true
		return types.ServiceCatalogEntry{}, f.err
	}
	return f.Database.GetServiceCatalogEntry(ctx, name)
}

// TestAddServiceCatalogEntry_CaseCollisionCheckFails reaches
// AddServiceCatalogEntry's second db call (FindServiceNameCaseInsensitive),
// which IsServiceRegistered succeeding first is required to even get to.
func TestAddServiceCatalogEntry_CaseCollisionCheckFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	sentinel := errString("case check unavailable")
	m.db = &selectiveFailDB{Database: db, method: "FindServiceNameCaseInsensitive", err: sentinel}

	entry := &types.ServiceCatalogEntry{Name: "svc", DirectoryPath: tempDir, ConfigFileName: "service.yaml"}
	err := m.AddServiceCatalogEntry(t.Context(), entry)
	if err == nil {
		t.Fatal("expected AddServiceCatalogEntry to surface a case-collision-check failure")
	}
}

// TestGetServiceInstance_UnderlyingFetchFails reaches GetServiceInstance's
// second db call, past the first IsServiceRegistered check that has to
// succeed first.
func TestGetServiceInstance_UnderlyingFetchFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	sentinel := errString("instance fetch unavailable")
	m.db = &selectiveFailDB{Database: db, method: "GetServiceInstance", err: sentinel}

	if _, err := m.GetServiceInstance(t.Context(), "svc"); err == nil {
		t.Fatal("expected GetServiceInstance to surface the underlying fetch failure")
	}
}

// TestGetServiceCatalogEntry_UnderlyingFetchFails mirrors the above for
// GetServiceCatalogEntry's own second db call.
func TestGetServiceCatalogEntry_UnderlyingFetchFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	m := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	sentinel := errString("catalog fetch unavailable")
	m.db = &selectiveFailDB{Database: db, method: "GetServiceCatalogEntry", err: sentinel}

	if _, err := m.GetServiceCatalogEntry(t.Context(), "svc"); err == nil {
		t.Fatal("expected GetServiceCatalogEntry to surface the underlying fetch failure")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// TestPrepareLogFiles_ErrorLogOpenFails covers the branch where the normal
// log file's writer acquisition already succeeded but the error-log writer's
// fails: the normal log file is pre-created (so its OpenFile needs no
// directory-write permission), then the directory is made read-only so
// creating the new error-log file fails, proving prepareLogFiles releases the
// writer it already acquired instead of leaking it.
func TestPrepareLogFiles_ErrorLogOpenFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	const name = "errlog-blocked-svc"
	logDir := CreateLogDirPath(tempDir)
	if err := os.MkdirAll(logDir, 0750); err != nil {
		t.Fatalf("mkdir logDir: %v", err)
	}
	outPath := filepath.Join(logDir, CreateOutputLogFilename(name))
	if err := os.WriteFile(outPath, nil, 0644); err != nil {
		t.Fatalf("pre-creating out log: %v", err)
	}
	if err := os.Chmod(logDir, 0555); err != nil {
		t.Fatalf("chmod logDir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(logDir, 0750) })

	_, _, err := mgr.prepareLogFiles(name, &types.ServiceConfig{})
	if err == nil {
		t.Fatal("expected prepareLogFiles to fail opening the error log file in a read-only directory")
	}
}

func (f *selectiveFailDB) RegisterServiceInstance(ctx context.Context, name string) error {
	if f.method == "RegisterServiceInstance" && !f.called {
		f.called = true
		return f.err
	}
	return f.Database.RegisterServiceInstance(ctx, name)
}

func (f *selectiveFailDB) UpdateServiceInstance(ctx context.Context, name string, updates database.ServiceInstanceUpdate) error {
	if f.method == "UpdateServiceInstance" && !f.called {
		f.called = true
		return f.err
	}
	return f.Database.UpdateServiceInstance(ctx, name, updates)
}

func (f *selectiveFailDB) RegisterProcessHistoryEntry(ctx context.Context, pgid int, startedAtTicks int64, serviceName string, state types.ProcessState) (types.ProcessHistory, error) {
	if f.method == "RegisterProcessHistoryEntry" && !f.called {
		f.called = true
		return types.ProcessHistory{}, f.err
	}
	return f.Database.RegisterProcessHistoryEntry(ctx, pgid, startedAtTicks, serviceName, state)
}

// startSleeperForKillTest starts a real, detached-process-group child so a
// DB-failure test can prove killAndWrap actually killed it (STYLE.md:
// kill-before-Wait on every exit path applies just as much when the "exit
// path" is a bookkeeping failure, not a signal).
func startSleeperForKillTest(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting helper process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	return cmd.Process.Pid
}

// TestRecordStartedInstance_UpdateServiceInstanceFails reaches
// recordStartedInstance's second db call (past RegisterServiceInstance
// succeeding), proving that failure also kills the just-started process
// group rather than only the first call's failure doing so.
func TestRecordStartedInstance_UpdateServiceInstanceFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	pgid := startSleeperForKillTest(t)
	mgr.db = &selectiveFailDB{Database: db, method: "UpdateServiceInstance", err: errString("update failed")}

	if _, err := mgr.recordStartedInstance(&types.ServiceCatalogEntry{Name: "svc"}, pgid, 0); err == nil {
		t.Fatal("expected recordStartedInstance to fail when UpdateServiceInstance fails")
	}
	if !pollUntil(2*time.Second, func() bool { return !isProcessAlive(pgid) }) {
		t.Fatalf("expected process group %d to be killed", pgid)
	}
}

// TestRecordStartedInstance_RegisterProcessHistoryEntryFails reaches
// recordStartedInstance's third db call.
func TestRecordStartedInstance_RegisterProcessHistoryEntryFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	pgid := startSleeperForKillTest(t)
	mgr.db = &selectiveFailDB{Database: db, method: "RegisterProcessHistoryEntry", err: errString("history registration failed")}

	if _, err := mgr.recordStartedInstance(&types.ServiceCatalogEntry{Name: "svc"}, pgid, 0); err == nil {
		t.Fatal("expected recordStartedInstance to fail when RegisterProcessHistoryEntry fails")
	}
	if !pollUntil(2*time.Second, func() bool { return !isProcessAlive(pgid) }) {
		t.Fatalf("expected process group %d to be killed", pgid)
	}
}

// TestRecordRestartedInstance_RegisterProcessHistoryEntryFails reaches
// recordRestartedInstance's second db call, past UpdateServiceInstance
// succeeding.
func TestRecordRestartedInstance_RegisterProcessHistoryEntryFails(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	pgid := startSleeperForKillTest(t)
	mgr.db = &selectiveFailDB{Database: db, method: "RegisterProcessHistoryEntry", err: errString("history registration failed")}

	if _, err := mgr.recordRestartedInstance(&types.ServiceCatalogEntry{Name: "svc"}, 0, pgid, 0); err == nil {
		t.Fatal("expected recordRestartedInstance to fail when RegisterProcessHistoryEntry fails")
	}
	if !pollUntil(2*time.Second, func() bool { return !isProcessAlive(pgid) }) {
		t.Fatalf("expected process group %d to be killed", pgid)
	}
}

// TestGetMostRecentProcessHistoryEntry_NotFound covers the
// ErrProcessHistoryNotFound-normalization branch: a service with no history
// rows at all must report ErrProcessNotFound, not the raw db sentinel.
func TestGetMostRecentProcessHistoryEntry_NotFound(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))

	_, err := mgr.GetMostRecentProcessHistoryEntry(t.Context(), "never-had-history")
	if !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("expected ErrProcessNotFound, got: %v", err)
	}
}
