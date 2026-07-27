//go:build integration

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeDependentService writes a service dir whose service.yaml carries an
// optional depends_on list and max_wait, reusing the same world-traversable
// dir treatment as writeTestService so the root-launched daemon child can read
// it after dropping privileges.
func writeDependentService(t *testing.T, name string, dependsOn []string, maxWait string) string {
	t.Helper()
	dir := e2eTempDir(t)
	if err := os.Chmod(dir, 0755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("chmod service dir: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "name: %q\ncommand: \"/bin/sleep 3600\"\n", name)
	if len(dependsOn) > 0 {
		b.WriteString("depends_on:\n")
		for _, dep := range dependsOn {
			fmt.Fprintf(&b, "  - %s\n", dep)
		}
	}
	if maxWait != "" {
		fmt.Fprintf(&b, "max_wait: %q\n", maxWait)
	}

	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), []byte(b.String()), 0644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write service.yaml: %v", err)
	}
	return dir
}

// TestDaemonE2E_DependsOn_GatesUntilReady drives real processes to prove the
// core acceptance: starting B (depends_on: [A]) blocks while A is not running,
// and only completes once A's health check reports it Running.
func TestDaemonE2E_DependsOn_GatesUntilReady(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)
	t.Cleanup(func() { killDaemonPID(baseDir) })

	startDaemon(t, bin, baseDir, false)

	aDir := writeDependentService(t, "dep_a", nil, "")
	bDir := writeDependentService(t, "dep_b", []string{"dep_a"}, "30s")
	for _, dir := range []string{aDir, bDir} {
		if out, err := eosCmd(t, bin, baseDir, "add", dir); err != nil {
			t.Fatalf("eos add %s: %v\n%s", dir, err, out)
		}
	}

	// Launch B first, while A is still down: its start must block on the gate.
	bCmd := exec.CommandContext(t.Context(), bin, "run", "dep_b") //nolint:gosec // bin is our own build
	bCmd.Env = append(os.Environ(),
		"EOS_BASE_DIR="+baseDir,
		"EOS_SYSTEMD_TARGET_DIR=/nonexistent-eos-e2e",
	)
	var bOut bytes.Buffer
	bCmd.Stdout = &bOut
	bCmd.Stderr = &bOut
	if err := bCmd.Start(); err != nil {
		t.Fatalf("start run dep_b: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- bCmd.Wait() }()

	// A is down, so B must still be blocking in the dependency gate.
	select {
	case err := <-done:
		t.Fatalf("run dep_b returned before dep_a was started; gate did not block: err=%v\n%s", err, bOut.String())
	case <-time.After(2 * time.Second):
	}

	// Bring A up; the health monitor advances it to Running, releasing B.
	if out, err := eosCmd(t, bin, baseDir, "run", "dep_a"); err != nil {
		t.Fatalf("run dep_a: %v\n%s", err, out)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run dep_b failed after dep_a became ready: %v\n%s", err, bOut.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("run dep_b did not complete after dep_a became ready:\n%s", bOut.String())
	}

	stopDaemon(t, bin, baseDir)
}

// TestDaemonE2E_DependsOn_MaxWaitFailsLoud proves the other half of acceptance:
// when a dependency never becomes ready, starting the dependent fails with a
// clear, actionable error once max_wait elapses — not a silent timeout.
func TestDaemonE2E_DependsOn_MaxWaitFailsLoud(t *testing.T) {
	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)
	t.Cleanup(func() { killDaemonPID(baseDir) })

	startDaemon(t, bin, baseDir, false)

	// gone_a is registered but never started, so it never reports Running.
	aDir := writeDependentService(t, "gone_a", nil, "")
	bDir := writeDependentService(t, "wait_b", []string{"gone_a"}, "1s")
	for _, dir := range []string{aDir, bDir} {
		if out, err := eosCmd(t, bin, baseDir, "add", dir); err != nil {
			t.Fatalf("eos add %s: %v\n%s", dir, err, out)
		}
	}

	out, err := eosCmd(t, bin, baseDir, "run", "wait_b")
	if err == nil {
		t.Fatalf("run wait_b should fail once max_wait elapses, got success:\n%s", out)
	}
	for _, want := range []string{"not ready", "gone_a"} {
		if !strings.Contains(out, want) {
			t.Errorf("run wait_b error missing %q:\n%s", want, out)
		}
	}

	stopDaemon(t, bin, baseDir)
}
