package cmd

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/spf13/cobra"
)

func TestPrintDaemonDownBanner(t *testing.T) {
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	printDaemonDownBanner(cmd)

	out := errBuf.String()
	if !strings.Contains(out, "warning:") {
		t.Errorf("banner missing severity label, got: %q", out)
	}
	if !strings.Contains(out, "eos daemon is not running - state below is last-known and may be stale") {
		t.Errorf("banner missing headline, got: %q", out)
	}
	if !strings.Contains(out, "start it with:") || !strings.Contains(out, "eos daemon start") {
		t.Errorf("banner missing fix-command hint, got: %q", out)
	}
}

func TestPrintDaemonDownStartWarning(t *testing.T) {
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	printDaemonDownStartWarning(cmd)

	out := errBuf.String()
	if !strings.Contains(out, "warning:") {
		t.Errorf("banner missing severity label, got: %q", out)
	}
	// The operator must learn the concrete consequence of starting with a dead
	// daemon: a service pinned in 'starting' with no supervision.
	for _, want := range []string{"starting", "health checks", "metrics", "log forwarding"} {
		if !strings.Contains(out, want) {
			t.Errorf("banner missing %q, got: %q", want, out)
		}
	}
	if !strings.Contains(out, "start the daemon with:") || !strings.Contains(out, "eos daemon start") {
		t.Errorf("banner missing fix-command hint, got: %q", out)
	}
}

func TestWarnDaemonDownBeforeStart_SystemdDownWarns(t *testing.T) {
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)
	cmd.SetContext(t.Context())

	// No active eos systemd unit in the test env -> confirmed down -> warns.
	warnDaemonDownBeforeStart(cmd, &config.DaemonConfig{Systemd: &config.SystemdConfig{}})

	if !strings.Contains(errBuf.String(), "eos daemon start") {
		t.Errorf("expected start warning for a down systemd unit, got: %q", errBuf.String())
	}
}

func TestWarnDaemonDownBeforeStart_StandaloneUpQuiet(t *testing.T) {
	dir := shortTempSocketDir(t)
	sockPath := filepath.Join(dir, "eos.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)
	cmd.SetContext(t.Context())

	// Socket answers (the standalone auto-start path already healed it): no warn.
	warnDaemonDownBeforeStart(cmd, &config.DaemonConfig{
		Standalone: &config.StandaloneDaemonConfig{SocketPath: sockPath},
	})

	if errBuf.Len() != 0 {
		t.Errorf("expected no warning when the daemon socket answers, got: %q", errBuf.String())
	}
}

func TestWarnDaemonDownBeforeStart_NilQuiet(t *testing.T) {
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	warnDaemonDownBeforeStart(cmd, nil)

	if errBuf.Len() != 0 {
		t.Errorf("expected no warning for nil daemon config, got: %q", errBuf.String())
	}
}

// shortTempSocketDir returns a temp dir under /tmp: t.TempDir() paths under
// macOS /var/folders exceed the 104-byte Unix-socket path limit.
func shortTempSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "eos-live-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestDaemonIsDown_StandaloneSocket(t *testing.T) {
	dir := shortTempSocketDir(t)
	sockPath := filepath.Join(dir, "eos.sock")

	daemon := &config.DaemonConfig{
		Standalone: &config.StandaloneDaemonConfig{SocketPath: sockPath},
	}

	// No listener yet: confirmed down.
	if !daemonIsDown(t.Context(), daemon) {
		t.Error("expected daemonIsDown=true when nothing is listening on the socket")
	}

	// Bring up a listener: now live.
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if daemonIsDown(t.Context(), daemon) {
		t.Error("expected daemonIsDown=false when the socket is accepting connections")
	}
}

func TestDaemonIsDown_SystemdInactive(t *testing.T) {
	// No active eos systemd unit exists in the test environment (and on macOS
	// systemctl is absent), so is-active never returns "active" -> down.
	daemon := &config.DaemonConfig{Systemd: &config.SystemdConfig{}}
	if !daemonIsDown(t.Context(), daemon) {
		t.Error("expected daemonIsDown=true for an inactive/absent systemd eos unit")
	}
}

func TestDaemonIsDown_Unconfigured(t *testing.T) {
	// Neither systemd nor standalone (e.g. launchd, or nothing configured):
	// liveness is not probed here, so never report a false outage.
	if daemonIsDown(t.Context(), &config.DaemonConfig{}) {
		t.Error("expected daemonIsDown=false when no probeable supervisor is configured")
	}
	if daemonIsDown(t.Context(), &config.DaemonConfig{Launchd: &config.LaunchdConfig{}}) {
		t.Error("expected daemonIsDown=false for a launchd-managed daemon")
	}
}
