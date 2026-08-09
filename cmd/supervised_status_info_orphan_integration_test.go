//go:build integration

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// TestSupervisedStatusInfo_SurfaceLiveOlderProcessGroup is the direct
// regression test for "eos status/info hide a live process pinned to an
// older process_history row": it spawns a genuinely separate OS process in
// its own process group (not through eos, and not the test binary's own
// pgid), registers it as an OLDER process_history row alongside a newer,
// inactive row, then asserts `status` and `info` surface the live older
// group instead of describing the service purely as stopped. Liveness is
// confirmed straight from the OS (syscall.Kill(pgid, 0)) before trusting any
// eos-reported field, so the fixture proves something a self-referential
// (test-binary-pgid) fake cannot.
func TestSupervisedStatusInfo_SurfaceLiveOlderProcessGroup(t *testing.T) {
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	root := newTestRootCmd(mgr)

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime())
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("marshaling test config: %v", err)
	}
	fullDirPath := filepath.Join(tempDir, "test-project")
	if err := os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("creating test-project directory: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err := os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("writing service.yaml: %v", err)
	}
	serviceName := testFile.Name

	root.SetArgs([]string{"add", fullPath})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("add command: %v", err)
	}

	// A genuinely separate OS process, in its own process group, standing in
	// for a leaked earlier instance -- eos never spawned this one.
	bystander := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	bystander.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := bystander.Start(); err != nil {
		t.Fatalf("starting bystander process: %v", err)
	}
	pgid, err := syscall.Getpgid(bystander.Process.Pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = bystander.Process.Wait()
	})
	// Give the shell time to install its TERM trap before anything else in
	// this test could race a signal against it.
	time.Sleep(100 * time.Millisecond)

	startedAtTicks, err := procutil.StartTime(pgid)
	if err != nil {
		t.Fatalf("StartTime: %v", err)
	}

	// Older row: the real bystander process, genuinely alive.
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), pgid, startedAtTicks, serviceName, types.ProcessStateRunning); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry (older, live): %v", err)
	}

	// Newer row: an unrelated PGID this test never spawned, so it is
	// definitely dead. This is the row a most-recent-only read would
	// describe the whole service by.
	deadPGID := os.Getpid() + 1_000_000
	if procutil.IsAlive(deadPGID) {
		t.Skipf("pgid %d happens to be alive on this machine -- cannot test the inactive most-recent row", deadPGID)
	}
	time.Sleep(10 * time.Millisecond) // ensure started_at orders strictly after the bystander's row
	if _, err := db.RegisterProcessHistoryEntry(t.Context(), deadPGID, 0, serviceName, types.ProcessStateStopped); err != nil {
		t.Fatalf("RegisterProcessHistoryEntry (newer, dead): %v", err)
	}

	// Prove liveness independently of eos, straight from the OS process
	// table, before trusting anything eos reports below.
	if killErr := syscall.Kill(-pgid, 0); killErr != nil {
		t.Fatalf("bystander pgid %d is not actually alive per the OS: %v", pgid, killErr)
	}

	var statusOut bytes.Buffer
	root.SetOut(&statusOut)
	root.SetArgs([]string{"status"})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("status command: %v", err)
	}
	if got := statusOut.String(); !strings.Contains(got, "orphaned") || !strings.Contains(got, strconv.Itoa(pgid)) {
		t.Errorf("expected status to report orphaned with live pgid %d, got: %s", pgid, got)
	}

	var infoOut bytes.Buffer
	root.SetOut(&infoOut)
	root.SetArgs([]string{"info", serviceName})
	if err := root.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("info command: %v", err)
	}
	if got := infoOut.String(); !strings.Contains(got, "orphaned") || !strings.Contains(got, strconv.Itoa(pgid)) {
		t.Errorf("expected info to report orphaned with live pgid %d, got: %s", pgid, got)
	}
}
