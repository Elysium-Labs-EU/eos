//go:build integration

package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
)

// Integration tests require:
//   - macOS (launchd as PID 1)
//   - Run via: make test-launchd
//
// Unlike systemd, "launchctl bootstrap/bootout" resolve jobs by domain + label, not by
// a fixed search path, so these tests can use a throwaway plist in t.TempDir() instead
// of writing into the real ~/Library/LaunchAgents — no root required, no residue left
// on the real user LaunchAgents dir.

func requireLaunchd(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skipf("requires macOS (launchd), got %q", runtime.GOOS)
	}
}

const testLaunchdLabel = "org.elysiumlabs.eos-integration-test"

func testLaunchdDomain(t *testing.T) string {
	t.Helper()
	return launchdDomain(true, os.Getuid())
}

func TestStartupCmdLaunchdIntegration(t *testing.T) {
	requireLaunchd(t)

	tempDir := t.TempDir()
	installDir := filepath.Join(tempDir, "eos")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Build a minimal binary so the plist ProgramArguments path exists.
	binPath := filepath.Join(installDir, "eos")
	buildOut, err := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build eos binary: %v\n%s", err, buildOut)
	}
	t.Cleanup(func() { _ = os.Remove(binPath) })

	launchdDir := tempDir + "/"
	plistFileName := testLaunchdLabel + ".plist"
	plistFile := filepath.Join(launchdDir, plistFileName)
	domain := testLaunchdDomain(t)
	target := domain + "/" + testLaunchdLabel

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm plist creation, decline restart (so we don't fork a standalone daemon)
	setStdin(c, "y\nn\n")

	startupCmdLaunchd(
		t.Context(), c, installDir,
		&config.StandaloneDaemonConfig{
			PIDFile:    filepath.Join(tempDir, "eos.pid"),
			SocketPath: filepath.Join(tempDir, "eos.sock"),
		},
		launchdDir, plistFileName, true, false, execRunCmd,
	)

	t.Cleanup(func() {
		_, _ = execRunCmd(context.Background(), "launchctl", "bootout", target)
	})

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "launch agent enabled, eos will start on login") {
		t.Errorf("expected enabled message, got:\n%s", outBuf.String())
	}

	if _, err := os.Stat(plistFile); os.IsNotExist(err) {
		t.Error("plist file was not written")
	}

	out, err := execRunCmd(t.Context(), "launchctl", "print", target)
	if err != nil {
		t.Errorf("expected launchctl print to find bootstrapped job %s: %v\n%s", target, err, out)
	}
}

func TestUnstartupCmdLaunchdIntegration(t *testing.T) {
	requireLaunchd(t)

	tempDir := t.TempDir()
	launchdDir := tempDir + "/"
	plistFileName := testLaunchdLabel + ".plist"
	plistFile := filepath.Join(launchdDir, plistFileName)
	domain := testLaunchdDomain(t)
	target := domain + "/" + testLaunchdLabel

	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + testLaunchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/sleep</string>
		<string>3600</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`
	if err := os.WriteFile(plistFile, []byte(plistContent), 0644); err != nil { // #nosec G306
		t.Fatal(err)
	}

	ctx := t.Context()
	if out, err := execRunCmd(ctx, "launchctl", "bootstrap", domain, plistFile); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_, _ = execRunCmd(context.Background(), "launchctl", "bootout", target)
		_ = os.Remove(plistFile)
	})

	c, outBuf, errBuf := makeTestCmd(t)
	// confirm unstartup, decline restart standalone
	setStdin(c, "y\nn\n")

	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	unstartupCmdLaunchd(ctx, c, config.LaunchdConfig{
		LaunchdTargetDir:     launchdDir,
		LaunchdPlistFileName: plistFileName,
		UserAgent:            true,
	}, true, false, execRunCmd, identity)

	if errBuf.Len() > 0 {
		t.Errorf("unexpected stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "launch agent startup removed") {
		t.Errorf("expected success message, got:\n%s", outBuf.String())
	}
	if _, err := os.Stat(plistFile); !os.IsNotExist(err) {
		t.Error("expected plist file to be removed")
	}
	if out, err := execRunCmd(ctx, "launchctl", "print", target); err == nil {
		t.Errorf("expected job to be unloaded after bootout, but launchctl print succeeded:\n%s", out)
	}
}
