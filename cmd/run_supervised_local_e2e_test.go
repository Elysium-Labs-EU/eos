//go:build integration

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// eosRunPGIDPattern extracts the pgid printStartedSuccessOutput prints in
// "success <svc> started with PGID: <n>".
var eosRunPGIDPattern = regexp.MustCompile(`PGID: (\d+)`)

// startLocalEosRun starts "eos run <name> --no-daemon" as a background
// process (Start, not the blocking eosCmd/CombinedOutput every other e2e
// helper in this package uses) and returns the live *exec.Cmd plus a buffer
// capturing its combined output, so the caller can observe whether or when
// it returns instead of only ever seeing its final result.
func startLocalEosRun(t *testing.T, bin, baseDir, name string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(bin, "--no-daemon", "run", name) //nolint:gosec // fixed argv, test fixture
	cmd.Env = append(os.Environ(),
		"EOS_BASE_DIR="+baseDir,
		"EOS_SYSTEMD_TARGET_DIR=/nonexistent-eos-e2e",
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting eos run --no-daemon: %v", err)
	}
	return cmd, &buf
}

// waitForPGIDInOutput polls buf for the "started with PGID: <n>" banner
// runStartRegisteredService prints once the service has actually launched,
// and returns the pgid it names.
func waitForPGIDInOutput(t *testing.T, buf *bytes.Buffer) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m := eosRunPGIDPattern.FindStringSubmatch(buf.String()); m != nil {
			pgid, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("unparsable pgid in output %q: %v", m[1], err)
			}
			return pgid
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("eos run never printed a PGID within 5s; output so far:\n%s", buf.String())
	return 0
}

// TestSupervisedLocalRunE2E_BlocksUntilInterrupted proves runSuperviseIfLocal
// and runBlockAndSupervise actually block and order execution, rather than
// merely leaving the service running in the background the way the pre-fix,
// fire-and-forget "eos run" did (the defect this whole change addresses) — a
// test that only checks the service's eventual state (like
// TestRunCommandReEnablesAfterStop) would pass identically under either
// behavior, since both leave the service running afterward. This drives the
// real compiled binary as a genuinely separate OS process: only a real
// process boundary lets a test observe "has this call returned yet" from the
// outside while the call under test may still be blocked, which an in-process
// cobra Command.ExecuteContext call cannot — the test goroutine calling it
// would itself be the one blocked.
func TestSupervisedLocalRunE2E_BlocksUntilInterrupted(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process-group liveness assertions require /proc (Linux)")
	}

	bin := buildEosBinary(t)
	baseDir := e2eTempDir(t)

	svcDir := writeTestService(t, "supervisedlocalsvc")
	// --no-daemon here too, not just on the "run" below: a bare "eos add"
	// resolves/auto-starts a standalone daemon, which is a separate concern
	// from what this test is proving and, on some hosts, can itself time out.
	if out, err := eosCmd(t, bin, baseDir, "--no-daemon", "add", svcDir); err != nil {
		t.Fatalf("eos add: %v\n%s", err, out)
	}

	runCmd, out := startLocalEosRun(t, bin, baseDir, "supervisedlocalsvc")
	waitDone := make(chan error, 1)
	go func() { waitDone <- runCmd.Wait() }()

	pgid := waitForPGIDInOutput(t, out)
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

	// The service having started proves nothing about blocking on its own —
	// the pre-fix "eos run" printed this exact banner and returned in the
	// same breath. The property actually under test is that "eos run" has
	// NOT returned some time later: it is still blocked, supervising.
	select {
	case err := <-waitDone:
		t.Fatalf("eos run returned before being interrupted (blocking/ordering regression): err=%v\noutput:\n%s", err, out.String())
	case <-time.After(2 * time.Second):
	}

	if _, _, alive := procPPIDAndPGID(t, pgid); !alive {
		t.Fatalf("service pgid %d died on its own before the interrupt; nothing left to prove ordering against", pgid)
	}

	if err := runCmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT to eos run: %v", err)
	}

	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("eos run did not exit cleanly after SIGINT: %v\noutput:\n%s", err, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("eos run did not exit within 10s of SIGINT; output so far:\n%s", out.String())
	}

	// The ordering property under test: at the moment "eos run" has already
	// returned, the service it was supervising must already be gone too —
	// not merely gone "eventually". runStopSupervisedService stops the
	// service before runBlockAndSupervise returns, so there must be no
	// window where the CLI has exited but the service it supervised is
	// still alive; checking this immediately, without any extra wait, is
	// what actually exercises that ordering rather than just its outcome.
	if _, _, alive := procPPIDAndPGID(t, pgid); alive {
		t.Errorf("service pgid %d still alive immediately after eos run exited — stop was not ordered before return", pgid)
	}
	t.Logf("eos run output:\n%s", out.String())
}
