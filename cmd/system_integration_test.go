//go:build integration

package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
)

// Integration tests require:
//   - Linux with systemd as PID 1
//   - Root (or sudo)
//   - Run via: make test-integration
//     or on OrbStack: orb run -m <machine> -- sudo go test ./cmd/... -tags integration -v

func requireSystemd(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Logf("SKIP %s: needs root to enable/disable a real systemd unit; run via `make test-integration` on Linux as root (e.g. OrbStack). This systemd path is NOT covered on this run.", t.Name())
		t.Skip("requires root")
	}
	runtime, err := detectActiveSystemRuntime()
	if err != nil || runtime != "systemd" {
		t.Logf("SKIP %s: needs systemd as PID 1 (got %q, err: %v); this systemd path is NOT covered on this run.", t.Name(), runtime, err)
		t.Skipf("requires systemd as PID 1 (got %q, err: %v)", runtime, err)
	}
}

// testUnitName is a throwaway systemd unit name distinct from the real "eos"
// unit these tests must not touch. The unit file must live in a real systemd
// search path (config.SystemdTargetDir) for "systemctl enable/stop <name>" to
// find it by name — a tempdir doesn't work, systemd doesn't search it.
const testUnitName = "eos-integration-test"

func TestStartupCmdIntegration(t *testing.T) {
	requireSystemd(t)

	tempDir := t.TempDir()
	installDir := filepath.Join(tempDir, "eos")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Build a minimal binary so the unit file ExecStart path exists.
	binPath := filepath.Join(installDir, "eos")
	buildOut, err := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build eos binary: %v\n%s", err, buildOut)
	}
	t.Cleanup(func() { _ = os.Remove(binPath) })

	systemdDir := config.SystemdTargetDir
	systemdFile := testUnitName + ".service"
	unitFile := filepath.Join(systemdDir, systemdFile)

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unit file, decline restart (so we don't actually switch daemon mode)
	setStdin(c, "y\nn\n")

	startupCmd(
		t.Context(), c, installDir,
		&config.StandaloneDaemonConfig{
			PIDFile:    filepath.Join(tempDir, "eos.pid"),
			SocketPath: filepath.Join(tempDir, "eos.sock"),
		},
		systemdDir, systemdFile, false, false,
		detectActiveSystemRuntime, execRunCmd,
	)

	// Cleanup: disable and remove the test unit, regardless of assertion outcome.
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = execRunCmd(ctx, "systemctl", "disable", testUnitName)
		_ = os.Remove(unitFile)
		_, _ = execRunCmd(ctx, "systemctl", "daemon-reload")
	})

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "system unit enabled, eos will start on boot") {
		t.Errorf("expected enabled message, got:\n%s", outBuf.String())
	}

	// Verify the unit file was written and enabled.
	if _, err := os.Stat(unitFile); os.IsNotExist(err) {
		t.Error("unit file was not written")
	}
	if out, err := execRunCmd(context.Background(), "systemctl", "is-enabled", testUnitName); err != nil || strings.TrimSpace(string(out)) != "enabled" {
		t.Errorf("expected %s to be enabled, got %q (err: %v)", testUnitName, out, err)
	}
}

func TestUnstartupCmdIntegration(t *testing.T) {
	requireSystemd(t)

	systemdDir := config.SystemdTargetDir
	systemdFile := testUnitName + ".service"
	unitFile := filepath.Join(systemdDir, systemdFile)
	unitContent := `[Unit]
Description=eos integration test unit (throwaway, safe to remove)
[Service]
Type=simple
ExecStart=/bin/sleep 3600
[Install]
WantedBy=multi-user.target`
	if err := os.WriteFile(unitFile, []byte(unitContent), 0644); err != nil { // #nosec G306
		t.Fatal(err)
	}

	ctx := t.Context()
	if out, err := execRunCmd(ctx, "systemctl", "daemon-reload"); err != nil {
		t.Fatalf("daemon-reload: %v\n%s", err, out)
	}
	if out, err := execRunCmd(ctx, "systemctl", "enable", testUnitName); err != nil {
		t.Fatalf("enable %s: %v\n%s", testUnitName, err, out)
	}
	t.Cleanup(func() {
		_, _ = execRunCmd(context.Background(), "systemctl", "disable", testUnitName)
		_ = os.Remove(unitFile)
		_, _ = execRunCmd(context.Background(), "systemctl", "daemon-reload")
	})

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unstartup, decline restart standalone
	setStdin(c, "y\nn\n")

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	unstartupCmd(ctx, c, config.SystemdConfig{
		SystemdTargetDir:      systemdDir,
		SystemdTargetFileName: systemdFile,
	}, false, false, detectActiveSystemRuntime, execRunCmd, identity)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "system unit startup removed") {
		t.Errorf("expected success message, got:\n%s", outBuf.String())
	}
	if _, err := os.Stat(unitFile); !os.IsNotExist(err) {
		t.Error("expected unit file to be removed")
	}
}
