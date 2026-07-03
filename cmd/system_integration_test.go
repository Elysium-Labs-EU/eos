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
)

// Integration tests require:
//   - Linux with systemd as PID 1
//   - Root (or sudo)
//   - Run via: make test-integration
//     or on OrbStack: orb run -m <machine> -- sudo go test ./cmd/... -tags integration -v

func requireSystemd(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("requires root")
	}
	runtime, err := detectActiveSystemRuntime()
	if err != nil || runtime != "systemd" {
		t.Skipf("requires systemd as PID 1 (got %q, err: %v)", runtime, err)
	}
}

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

	systemdDir := filepath.Join(tempDir, "systemd") + "/"
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		t.Fatal(err)
	}
	unitFile := systemdDir + "eos-test.service"

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unit file, decline restart (so we don't actually switch daemon mode)
	setStdin(c, "y\nn\n")

	var calls []string
	startupCmd(
		t.Context(), c, installDir,
		&config.StandaloneDaemonConfig{
			PIDFile:    filepath.Join(tempDir, "eos.pid"),
			SocketPath: filepath.Join(tempDir, "eos.sock"),
		},
		systemdDir, "eos-test.service", false,
		detectActiveSystemRuntime, execRunCmd,
	)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "eos enabled, will start on boot") {
		t.Errorf("expected enabled message, got:\n%s", outBuf.String())
	}

	_ = calls // real systemctl was used

	// Verify the unit file was written and enabled.
	if _, err := os.Stat(unitFile); os.IsNotExist(err) {
		t.Error("unit file was not written")
	}

	// Cleanup: disable and remove the test unit.
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = execRunCmd(ctx, "systemctl", "disable", "eos-test")
		_ = os.Remove(unitFile)
		_, _ = execRunCmd(ctx, "systemctl", "daemon-reload")
	})
}

func TestUnstartupCmdIntegration(t *testing.T) {
	requireSystemd(t)

	tempDir := t.TempDir()
	systemdDir := filepath.Join(tempDir, "systemd") + "/"
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		t.Fatal(err)
	}

	unitFile := systemdDir + "eos-test.service"
	unitContent := `[Unit]
Description=eos test integration service
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
	if out, err := execRunCmd(ctx, "systemctl", "enable", "eos-test"); err != nil {
		t.Fatalf("enable eos-test: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_, _ = execRunCmd(context.Background(), "systemctl", "disable", "eos-test")
		_ = os.Remove(unitFile)
		_, _ = execRunCmd(context.Background(), "systemctl", "daemon-reload")
	})

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unstartup, decline restart standalone
	setStdin(c, "y\nn\n")

	unstartupCmd(ctx, c, config.SystemdConfig{
		SystemdTargetDir:      systemdDir,
		SystemdTargetFileName: "eos-test.service",
	}, false, detectActiveSystemRuntime, execRunCmd)

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
