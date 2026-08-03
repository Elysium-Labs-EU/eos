package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// fakeRunMgr lets the "run" command's error branches that a real,
// single-threaded LocalManager can't be made to hit (a catalog write failing
// mid-registration, a registration check racing a DB outage, a catalog entry
// vanishing between the registration check and the lookup, ...) be exercised
// directly, mirroring fakeSnapshotMgr in snapshot_test.go.
type fakeRunMgr struct {
	manager.ServiceManager
	addCatalogErr      error
	isRegisteredErr    error
	serviceInstanceErr error
	catalogEntryErr    error
	startErr           error
	catalogEntry       types.ServiceCatalogEntry
	startPGID          int
	isRegistered       bool
}

func (f *fakeRunMgr) AddServiceCatalogEntry(*types.ServiceCatalogEntry) error {
	return f.addCatalogErr
}

func (f *fakeRunMgr) IsServiceRegistered(string) (bool, error) {
	return f.isRegistered, f.isRegisteredErr
}

func (f *fakeRunMgr) GetServiceInstance(string) (*types.ServiceInstance, error) {
	return nil, f.serviceInstanceErr
}

func (f *fakeRunMgr) GetServiceCatalogEntry(string) (types.ServiceCatalogEntry, error) {
	return f.catalogEntry, f.catalogEntryErr
}

func (f *fakeRunMgr) StartService(string) (int, error) {
	return f.startPGID, f.startErr
}

// newRunCmdWithFakeMgr builds the "run" cobra.Command wired to mgr and a
// minimal *config.SystemConfig (a nil Daemon is fine: warnDaemonDownBeforeStart
// treats "no daemon configured" as not-confirmed-down and stays quiet).
func newRunCmdWithFakeMgr(t *testing.T, mgr manager.ServiceManager) (cmd *cobra.Command, errBuf *bytes.Buffer) {
	t.Helper()
	var outBuf bytes.Buffer
	errBuf = &bytes.Buffer{}
	cmd = newRunCmd(
		func() manager.ServiceManager { return mgr },
		func() *config.SystemConfig { return &config.SystemConfig{} },
	)
	cmd.SetOut(&outBuf)
	cmd.SetErr(errBuf)
	cmd.SetContext(t.Context())
	return cmd, errBuf
}

// TestRunWithFileRegisterGenuineError covers run.go's registerServiceIfNeeded
// error branch for a genuine failure (anything other than
// ErrServiceAlreadyRegistered) — e.g. the catalog store rejecting the write.
// A real single-threaded LocalManager can't be made to fail here on a
// well-formed, freshly parsed service file, so this uses a fake manager the
// same way fakeSnapshotMgr does for the analogous snapshot-restore gaps.
func TestRunWithFileRegisterGenuineError(t *testing.T) {
	tempDir := t.TempDir()
	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("./start-script.sh"), testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	mgr := &fakeRunMgr{addCatalogErr: errors.New("catalog store unavailable")}
	cmd, errBuf := newRunCmdWithFakeMgr(t, mgr)
	if err := cmd.Flags().Set("file", fullPathYaml); err != nil {
		t.Fatalf("set file flag: %v", err)
	}

	runErr := cmd.RunE(cmd, nil)
	if !errors.Is(runErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", runErr)
	}
	if !strings.Contains(errBuf.String(), "handling service file") {
		t.Errorf("expected 'handling service file' error, got: %s", errBuf.String())
	}
}

// TestRunWithNameRegisteredCheckGenuineError covers run.go's branch where
// checking registration of a named service fails with something other than
// "not registered" (e.g. the registration check itself erroring out).
func TestRunWithNameRegisteredCheckGenuineError(t *testing.T) {
	mgr := &fakeRunMgr{isRegisteredErr: errors.New("registry unavailable")}
	cmd, errBuf := newRunCmdWithFakeMgr(t, mgr)

	runErr := cmd.RunE(cmd, []string{"web"})
	if !errors.Is(runErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", runErr)
	}
	if !strings.Contains(errBuf.String(), "handling service name") {
		t.Errorf("expected 'handling service name' error, got: %s", errBuf.String())
	}
}

// TestRunWithOnceFlagRunningCheckGenuineError covers the --once branch where
// checking whether the service is already running fails with a genuine error
// (as opposed to manager.ErrServiceNotRunning).
func TestRunWithOnceFlagRunningCheckGenuineError(t *testing.T) {
	mgr := &fakeRunMgr{isRegistered: true, serviceInstanceErr: errors.New("liveness probe unavailable")}
	cmd, errBuf := newRunCmdWithFakeMgr(t, mgr)
	if err := cmd.Flags().Set("once", "true"); err != nil {
		t.Fatalf("set once flag: %v", err)
	}

	runErr := cmd.RunE(cmd, []string{"web"})
	if !errors.Is(runErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", runErr)
	}
	if !strings.Contains(errBuf.String(), "check service running status") {
		t.Errorf("expected 'check service running status' error, got: %s", errBuf.String())
	}
}

// TestRunWithNameCatalogEntryGenuineError covers run.go's branch where the
// service passed registration but its catalog entry can no longer be fetched
// (e.g. a vanished/corrupted row) — distinct from the earlier "not
// registered" check, which already passed.
func TestRunWithNameCatalogEntryGenuineError(t *testing.T) {
	mgr := &fakeRunMgr{isRegistered: true, catalogEntryErr: errors.New("catalog entry vanished")}
	cmd, errBuf := newRunCmdWithFakeMgr(t, mgr)

	runErr := cmd.RunE(cmd, []string{"web"})
	if !errors.Is(runErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", runErr)
	}
	if !strings.Contains(errBuf.String(), "getting registered service") {
		t.Errorf("expected 'getting registered service' error, got: %s", errBuf.String())
	}
}

// TestRunWithNameStartServiceGenuineError covers run.go's branch where
// startOrRestartService fails outright (StartService returning something
// other than manager.ErrAlreadyRunning). GetServiceCatalogEntry must point at
// a real, dependency-free service.yaml so gateDependencies (which reads the
// file straight off disk, bypassing the fake manager) clears before
// startOrRestartService is ever reached.
func TestRunWithNameStartServiceGenuineError(t *testing.T) {
	tempDir := t.TempDir()
	entry := writeGateTestService(t, tempDir, &types.ServiceConfig{Name: "web", Command: "/bin/true"})

	mgr := &fakeRunMgr{isRegistered: true, catalogEntry: entry, startErr: errors.New("fork/exec: resource temporarily unavailable")}
	cmd, errBuf := newRunCmdWithFakeMgr(t, mgr)

	runErr := cmd.RunE(cmd, []string{"web"})
	if !errors.Is(runErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", runErr)
	}
	if !strings.Contains(errBuf.String(), "running service") {
		t.Errorf("expected 'running service' error, got: %s", errBuf.String())
	}
}

// TestRunCommandDependencyGateFailsLoud drives the gate-dependency failure
// through the real "run" command end to end (real SQLite-backed manager, real
// service.yaml files, real RecordDependencyWait), proving run.go's own error
// wrapping around gateDependencies (as opposed to gateDependencies itself,
// already covered directly in run_gate_dependencies_test.go).
func TestRunCommandDependencyGateFailsLoud(t *testing.T) {
	cmd, _, errBuf, tempDir := setupCmd(t)

	depConfig := testutil.NewTestServiceConfigFile(t, testutil.WithCommand("/bin/true"), testutil.WithoutRuntime())
	depConfig.DependsOn = []string{"never-started"}
	depConfig.MaxWait = "150ms"
	yamlData, err := yaml.Marshal(depConfig)
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "dependent-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fullPathYaml := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPathYaml, yamlData, 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	cmd.SetArgs([]string{"run", "-f", fullPathYaml})
	runErr := cmd.ExecuteContext(t.Context())
	if !errors.Is(runErr, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", runErr)
	}

	output := errBuf.String()
	if !strings.Contains(output, "never-started") {
		t.Errorf("expected the unmet dependency named in the error, got: %s", output)
	}
	if !strings.Contains(output, "not started") && !strings.Contains(output, "not ready") {
		t.Errorf("expected a not-ready/not-started gate error, got: %s", output)
	}
}
