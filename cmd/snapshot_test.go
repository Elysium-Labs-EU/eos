package cmd

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func withDependsOn(deps []string) testutil.ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.DependsOn = deps
	}
}

func withMaxWait(maxWait string) testutil.ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.MaxWait = maxWait
	}
}

// writeSnapshotTestService writes a service.yaml (not yet registered) whose
// process just sleeps, long-lived enough to survive save/restore round
// trips. Returns the yaml path to pass to "eos add".
func writeSnapshotTestService(t *testing.T, tempDir, name string, opts ...testutil.ServiceConfigOption) string {
	t.Helper()

	allOpts := append([]testutil.ServiceConfigOption{
		testutil.WithName(name),
		testutil.WithCommand("sleep 30"),
		testutil.WithoutRuntime(),
	}, opts...)
	testFile := testutil.NewTestServiceConfigFile(t, allOpts...)

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}

	dir := filepath.Join(tempDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "service.yaml")
	if err := os.WriteFile(path, yamlData, 0644); err != nil {
		t.Fatalf("write service.yaml: %v", err)
	}
	return path
}

// stopAllForCleanup force-stops each named service so a leftover "sleep 30"
// doesn't outlive the test. Best-effort: errors are ignored, since a service
// the test already stopped is a no-op here, not a failure.
func stopAllForCleanup(t *testing.T, cmd *cobra.Command, names ...string) {
	t.Helper()
	for _, name := range names {
		cmd.SetArgs([]string{"stop", name, "--force"})
		_ = cmd.ExecuteContext(t.Context())
	}
}

func TestSnapshotSaveCommand(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	t.Cleanup(func() { stopAllForCleanup(t, cmd, "web", "api") })

	webYaml := writeSnapshotTestService(t, tempDir, "web")
	apiYaml := writeSnapshotTestService(t, tempDir, "api")

	for _, yamlPath := range []string{webYaml, apiYaml} {
		cmd.SetArgs([]string{"add", yamlPath})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("add %s: %v", yamlPath, err)
		}
	}
	for _, name := range []string{"web", "api"} {
		cmd.SetArgs([]string{"run", name})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
	}

	outBuf.Reset()
	cmd.SetArgs([]string{"snapshot", "save"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot save: %v, output: %s", err, outBuf.String())
	}

	output := outBuf.String()
	if !strings.Contains(output, "saved snapshot of 2 running service(s)") {
		t.Errorf("expected save summary in output, got: %s", output)
	}
	if !strings.Contains(output, "web") || !strings.Contains(output, "api") {
		t.Errorf("expected both service names in output, got: %s", output)
	}

	snap, err := manager.LoadSnapshot(manager.SnapshotFilePath(tempDir))
	if err != nil {
		t.Fatalf("loading snapshot from disk: %v", err)
	}
	want := []string{"api", "web"}
	if len(snap.Services) != len(want) || snap.Services[0] != want[0] || snap.Services[1] != want[1] {
		t.Errorf("snapshot.Services = %v, want %v", snap.Services, want)
	}
}

func TestSnapshotSaveCommandNoRunningServices(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)

	cmd.SetArgs([]string{"snapshot", "save"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot save: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "saved an empty snapshot") {
		t.Errorf("expected empty-snapshot warning, got: %s", output)
	}

	snap, err := manager.LoadSnapshot(manager.SnapshotFilePath(tempDir))
	if err != nil {
		t.Fatalf("loading snapshot from disk: %v", err)
	}
	if len(snap.Services) != 0 {
		t.Errorf("expected empty snapshot, got: %v", snap.Services)
	}
}

// TestSnapshotRestoreCommandWarnsWhenDaemonDown is the regression test for the
// review finding that newSnapshotRestoreCmd never called warnDaemonDownBeforeStart:
// restore is precisely the post-reboot recovery path where the daemon is most
// likely still down, so a restored service must not silently end up pinned in
// 'starting' with no operator warning — same guarantee newRunCmd already gives
// "eos run" (cmd/run.go:291).
func TestSnapshotRestoreCommandWarnsWhenDaemonDown(t *testing.T) {
	tempDir := t.TempDir()
	if err := manager.SaveSnapshot(manager.SnapshotFilePath(tempDir), manager.Snapshot{Services: []string{"ghost"}}); err != nil {
		t.Fatalf("writing snapshot: %v", err)
	}

	// "ghost" is unregistered, so restoreSnapshotService resolves it as a fast
	// no-op "missing" outcome — the warning must still have fired before that,
	// once up front for the whole restore, not conditioned on any one service
	// actually starting.
	mgr := &fakeSnapshotMgr{isRegistered: false}
	cfg := &config.SystemConfig{
		BaseDir: tempDir,
		Daemon: config.DaemonConfig{
			Standalone: &config.StandaloneDaemonConfig{
				SocketPath: filepath.Join(shortTempSocketDir(t), "eos.sock"),
			},
		},
	}

	restoreCmd := newSnapshotRestoreCmd(func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return cfg })
	var out bytes.Buffer
	restoreCmd.SetOut(&out)
	restoreCmd.SetErr(&out)
	restoreCmd.SetContext(t.Context())

	if err := restoreCmd.RunE(restoreCmd, nil); err != nil {
		t.Fatalf("restore: %v, output: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "eos daemon start") {
		t.Errorf("expected daemon-down start warning, got: %s", out.String())
	}
}

// TestSnapshotRestoreCommandQuietWhenDaemonUp proves the new warning is
// conditional, not unconditional noise: when the daemon's socket answers,
// restore stays quiet about it, matching warnDaemonDownBeforeStart's own
// already-tested behavior (TestWarnDaemonDownBeforeStart_StandaloneUpQuiet).
func TestSnapshotRestoreCommandQuietWhenDaemonUp(t *testing.T) {
	tempDir := t.TempDir()
	if err := manager.SaveSnapshot(manager.SnapshotFilePath(tempDir), manager.Snapshot{Services: []string{"ghost"}}); err != nil {
		t.Fatalf("writing snapshot: %v", err)
	}

	sockPath := filepath.Join(shortTempSocketDir(t), "eos.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	mgr := &fakeSnapshotMgr{isRegistered: false}
	cfg := &config.SystemConfig{
		BaseDir: tempDir,
		Daemon: config.DaemonConfig{
			Standalone: &config.StandaloneDaemonConfig{SocketPath: sockPath},
		},
	}

	restoreCmd := newSnapshotRestoreCmd(func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return cfg })
	var out bytes.Buffer
	restoreCmd.SetOut(&out)
	restoreCmd.SetErr(&out)
	restoreCmd.SetContext(t.Context())

	if err := restoreCmd.RunE(restoreCmd, nil); err != nil {
		t.Fatalf("restore: %v, output: %s", err, out.String())
	}

	if strings.Contains(out.String(), "eos daemon start") {
		t.Errorf("expected no daemon-down warning when the socket answers, got: %s", out.String())
	}
}

func TestSnapshotRestoreCommandStartsStoppedService(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "1s")
	cmd, outBuf, _, tempDir := setupCmd(t)
	t.Cleanup(func() { stopAllForCleanup(t, cmd, "web") })

	webYaml := writeSnapshotTestService(t, tempDir, "web")
	cmd.SetArgs([]string{"add", webYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: %v", err)
	}
	cmd.SetArgs([]string{"run", "web"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Save while running, then stop — restore must bring it back.
	cmd.SetArgs([]string{"snapshot", "save"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot save: %v", err)
	}

	cmd.SetArgs([]string{"stop", "web"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	outBuf.Reset()
	cmd.SetArgs([]string{"snapshot", "restore"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot restore: %v, output: %s", err, outBuf.String())
	}

	output := outBuf.String()
	if !strings.Contains(output, "started with PGID:") {
		t.Errorf("expected web to be started, got: %s", output)
	}
	if !strings.Contains(output, "1 started, 0 restarted, 0 already running, 0 no longer registered, 0 failed") {
		t.Errorf("expected restore summary, got: %s", output)
	}
}

func TestSnapshotRestoreCommandSkipsAlreadyRunning(t *testing.T) {
	cmd, outBuf, _, tempDir := setupCmd(t)
	t.Cleanup(func() { stopAllForCleanup(t, cmd, "web") })

	webYaml := writeSnapshotTestService(t, tempDir, "web")
	cmd.SetArgs([]string{"add", webYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: %v", err)
	}
	cmd.SetArgs([]string{"run", "web"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	cmd.SetArgs([]string{"snapshot", "save"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot save: %v", err)
	}

	// web is still running — restore must leave it alone, not restart it.
	outBuf.Reset()
	cmd.SetArgs([]string{"snapshot", "restore"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot restore: %v, output: %s", err, outBuf.String())
	}

	output := outBuf.String()
	if !strings.Contains(output, "already running - skipped") {
		t.Errorf("expected already-running skip message, got: %s", output)
	}
	if !strings.Contains(output, "0 started, 0 restarted, 1 already running, 0 no longer registered, 0 failed") {
		t.Errorf("expected restore summary, got: %s", output)
	}
}

func TestSnapshotRestoreCommandSkipsUnregisteredService(t *testing.T) {
	t.Setenv("SHUTDOWN_GRACE_PERIOD", "1s")
	cmd, outBuf, _, tempDir := setupCmd(t)

	webYaml := writeSnapshotTestService(t, tempDir, "web")
	cmd.SetArgs([]string{"add", webYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: %v", err)
	}
	cmd.SetArgs([]string{"run", "web"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	cmd.SetArgs([]string{"snapshot", "save"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot save: %v", err)
	}

	cmd.SetArgs([]string{"stop", "web"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	cmd.SetArgs([]string{"remove", "web"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("remove: %v", err)
	}

	outBuf.Reset()
	cmd.SetArgs([]string{"snapshot", "restore"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot restore: %v, output: %s", err, outBuf.String())
	}

	output := outBuf.String()
	if !strings.Contains(output, "no longer registered - skipped") {
		t.Errorf("expected no-longer-registered skip message, got: %s", output)
	}
	if !strings.Contains(output, "0 started, 0 restarted, 0 already running, 1 no longer registered, 0 failed") {
		t.Errorf("expected restore summary, got: %s", output)
	}
}

func TestSnapshotRestoreCommandNoSnapshotFile(t *testing.T) {
	cmd, _, errBuf, _ := setupCmd(t)

	cmd.SetArgs([]string{"snapshot", "restore"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "no snapshot found") {
		t.Errorf("expected 'no snapshot found', got: %s", errBuf.String())
	}
}

func TestSnapshotRestoreCommandCorruptSnapshotFile(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	if err := os.WriteFile(manager.SnapshotFilePath(tempDir), []byte("not json"), 0600); err != nil {
		t.Fatalf("writing corrupt snapshot file: %v", err)
	}

	cmd.SetArgs([]string{"snapshot", "restore"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "loading snapshot") {
		t.Errorf("expected 'loading snapshot' error, got: %s", errBuf.String())
	}
}

// TestSnapshotSaveCommandWriteError covers the manager.SaveSnapshot error
// branch (cmd/snapshot.go:56-59). Repointing EOS_BASE_DIR at a directory
// that doesn't exist (and that SaveSnapshot never creates) makes the
// underlying os.WriteFile fail with ENOENT — config.GetBaseDir itself only
// validates ownership of a path that already exists, so a nonexistent
// override is otherwise accepted right up until the write.
func TestSnapshotSaveCommandWriteError(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	t.Setenv("EOS_BASE_DIR", filepath.Join(tempDir, "does-not-exist"))

	cmd.SetArgs([]string{"snapshot", "save"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "saving snapshot") {
		t.Errorf("expected 'saving snapshot' error, got: %s", errBuf.String())
	}
}

func TestSnapshotSaveCommandServiceInstanceLookupError(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)
	t.Setenv("EOS_BASE_DIR", tempDir)

	var errBuf strings.Builder
	cmd.SetErr(&errBuf)

	// Close the DB out from under the manager so GetAllServiceInstances fails,
	// exercising save's own error path rather than the happy one.
	if err := db.CloseDBConnection(); err != nil {
		t.Fatalf("closing db: %v", err)
	}

	cmd.SetArgs([]string{"snapshot", "save"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "getting running services") {
		t.Errorf("expected 'getting running services' error, got: %s", errBuf.String())
	}
}

// TestSnapshotRestoreCommandDependencyFailure proves a snapshotted service
// whose dependency never comes up surfaces as a loud per-service failure
// (and a non-zero exit) rather than hanging or being silently dropped —
// mirroring "eos run"'s own gateDependencies behavior, reused as-is here.
func TestSnapshotRestoreCommandDependencyFailure(t *testing.T) {
	cmd, outBuf, errBuf, tempDir := setupCmd(t)

	// "ghost" is referenced by depends_on but never registered, so it can
	// never become ready; max_wait is kept tiny so the test doesn't hang.
	webYaml := writeSnapshotTestService(t, tempDir, "web", withDependsOn([]string{"ghost"}), withMaxWait("50ms"))
	cmd.SetArgs([]string{"add", webYaml})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := manager.SaveSnapshot(manager.SnapshotFilePath(tempDir), manager.Snapshot{Services: []string{"web"}}); err != nil {
		t.Fatalf("writing snapshot directly: %v", err)
	}

	cmd.SetArgs([]string{"snapshot", "restore"})
	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v, stdout: %s, stderr: %s", err, outBuf.String(), errBuf.String())
	}

	if !strings.Contains(errBuf.String(), "web") {
		t.Errorf("expected per-service failure line naming web, got: %s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "0 started, 0 restarted, 0 already running, 0 no longer registered, 1 failed") {
		t.Errorf("expected restore summary with 1 failed, got: %s", outBuf.String())
	}
}

func TestSnapshotRestoreCommandEmptySnapshot(t *testing.T) {
	cmd, outBuf, _, _ := setupCmd(t)

	cmd.SetArgs([]string{"snapshot", "save"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot save: %v", err)
	}

	outBuf.Reset()
	cmd.SetArgs([]string{"snapshot", "restore"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("snapshot restore: %v", err)
	}
	if !strings.Contains(outBuf.String(), "snapshot is empty - nothing to restore") {
		t.Errorf("expected empty-snapshot message, got: %s", outBuf.String())
	}
}

// TestLoadDependsOnMap exercises loadDependsOnMap directly against a real
// LocalManager: two registered services, one depending on the other, plus a
// name that was never registered. This is the piece that feeds
// manager.OrderByDependencies (itself covered exhaustively in
// internal/manager/snapshot_test.go) — here it's only proving the map gets
// built from the right source (each service's own depends_on on disk).
func TestLoadDependsOnMap(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	cmd := newTestRootCmd(mgr)

	dbYaml := writeSnapshotTestService(t, tempDir, "db")
	apiYaml := writeSnapshotTestService(t, tempDir, "api", withDependsOn([]string{"db"}))

	for _, yamlPath := range []string{dbYaml, apiYaml} {
		cmd.SetArgs([]string{"add", yamlPath})
		if err := cmd.ExecuteContext(t.Context()); err != nil {
			t.Fatalf("add %s: %v", yamlPath, err)
		}
	}

	got := loadDependsOnMap(mgr, []string{"db", "api", "ghost"})

	if len(got["db"]) != 0 {
		t.Errorf("db depends_on = %v, want empty", got["db"])
	}
	want := []string{"db"}
	if len(got["api"]) != 1 || got["api"][0] != want[0] {
		t.Errorf("api depends_on = %v, want %v", got["api"], want)
	}
	if _, ok := got["ghost"]; ok {
		t.Errorf("expected unregistered %q to be absent from map, got entry %v", "ghost", got["ghost"])
	}
}

// fakeSnapshotMgr implements manager.ServiceManager, only overriding the
// methods restoreSnapshotService touches; every other call panics via the
// nil embedded interface, same idiom as cmd/helpers/completions_test.go's
// fakeCatalogMgr. It exists to force restoreSnapshotService's DB-failure
// branches deterministically — real conditions (registration lookup fails,
// running-status lookup fails, catalog entry vanishes between checks) that a
// real LocalManager in a single-threaded test can't be made to hit.
type fakeSnapshotMgr struct {
	manager.ServiceManager
	isRegisteredErr    error
	serviceInstanceErr error
	catalogEntryErr    error
	startErr           error
	restartErr         error
	serviceInstance    *types.ServiceInstance
	catalogEntry       types.ServiceCatalogEntry
	restartPGID        int
	isRegistered       bool
}

func (f *fakeSnapshotMgr) IsServiceRegistered(string) (bool, error) {
	return f.isRegistered, f.isRegisteredErr
}

func (f *fakeSnapshotMgr) GetServiceInstance(string) (*types.ServiceInstance, error) {
	return f.serviceInstance, f.serviceInstanceErr
}

func (f *fakeSnapshotMgr) GetServiceCatalogEntry(string) (types.ServiceCatalogEntry, error) {
	return f.catalogEntry, f.catalogEntryErr
}

func (f *fakeSnapshotMgr) StartService(string) (int, error) {
	return 0, f.startErr
}

func (f *fakeSnapshotMgr) RestartService(string, time.Duration, time.Duration) (int, error) {
	return f.restartPGID, f.restartErr
}

func newTestCmdWithContext(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(t.Context())
	return cmd, &out
}

func TestRestoreSnapshotServiceRegistrationCheckError(t *testing.T) {
	cmd, _ := newTestCmdWithContext(t)
	mgr := &fakeSnapshotMgr{isRegisteredErr: errors.New("db unavailable")}

	_, err := restoreSnapshotService(cmd, mgr, &config.SystemConfig{}, "web")
	if err == nil || !strings.Contains(err.Error(), "checking registration") {
		t.Fatalf("expected 'checking registration' error, got: %v", err)
	}
}

func TestRestoreSnapshotServiceRunningCheckError(t *testing.T) {
	cmd, _ := newTestCmdWithContext(t)
	mgr := &fakeSnapshotMgr{
		isRegistered:       true,
		serviceInstanceErr: errors.New("db unavailable"),
	}

	_, err := restoreSnapshotService(cmd, mgr, &config.SystemConfig{}, "web")
	if err == nil || !strings.Contains(err.Error(), "checking running status") {
		t.Fatalf("expected 'checking running status' error, got: %v", err)
	}
}

func TestRestoreSnapshotServiceCatalogEntryError(t *testing.T) {
	cmd, _ := newTestCmdWithContext(t)
	mgr := &fakeSnapshotMgr{
		isRegistered:       true,
		serviceInstanceErr: manager.ErrServiceNotRunning,
		catalogEntryErr:    errors.New("catalog entry vanished"),
	}

	_, err := restoreSnapshotService(cmd, mgr, &config.SystemConfig{}, "web")
	if err == nil || !strings.Contains(err.Error(), "getting registered service") {
		t.Fatalf("expected 'getting registered service' error, got: %v", err)
	}
}

// TestRestoreSnapshotServiceRestartsWhenAlreadyRunningRaceDetected covers the
// defensive Restarted branch: isServiceRunning said not-running, but
// StartService's own liveness check (a stale-instance-row self-heal, or a
// genuine start/restore race) disagrees and reports ErrAlreadyRunning —
// startOrRestartService then falls back to RestartService, same as a plain
// "eos run" would.
func TestRestoreSnapshotServiceRestartsWhenAlreadyRunningRaceDetected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), []byte("name: web\ncommand: sleep 30\n"), 0644); err != nil {
		t.Fatalf("writing service.yaml: %v", err)
	}

	cmd, out := newTestCmdWithContext(t)
	mgr := &fakeSnapshotMgr{
		isRegistered:       true,
		serviceInstanceErr: manager.ErrServiceNotRunning,
		catalogEntry: types.ServiceCatalogEntry{
			Name:           "web",
			DirectoryPath:  dir,
			ConfigFileName: "service.yaml",
		},
		startErr:    manager.ErrAlreadyRunning,
		restartPGID: 4242,
	}

	outcome, err := restoreSnapshotService(cmd, mgr, &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Second}}, "web")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if outcome != restoreOutcomeRestarted {
		t.Errorf("expected restoreOutcomeRestarted, got: %v", outcome)
	}
	if !strings.Contains(out.String(), "restarted with PGID: 4242") {
		t.Errorf("expected restarted-with-PGID output, got: %s", out.String())
	}
}
