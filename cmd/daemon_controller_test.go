package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

// spawnDisposableChild starts a real, short-lived child process (`sleep 30`) so tests can
// exercise Signal(0)-based liveness checks and SIGTERM delivery without touching the test
// process itself. The caller must Kill+Wait it in cleanup.
func spawnDisposableChild(t *testing.T) *exec.Cmd {
	t.Helper()
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to spawn disposable child: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	return child
}

// deadPID starts and waits out a child process, returning a PID that is guaranteed to be dead.
func deadPID(t *testing.T) int {
	t.Helper()
	child := exec.Command("true")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to spawn short-lived child: %v", err)
	}
	pid := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatalf("failed to wait for short-lived child: %v", err)
	}
	return pid
}

func newStandaloneController(t *testing.T, tempDir string) *standaloneDaemonController {
	t.Helper()
	return &standaloneDaemonController{
		baseDir: tempDir,
		cfg: config.StandaloneDaemonConfig{
			PIDFile:    filepath.Join(tempDir, "eos.pid"),
			SocketPath: filepath.Join(tempDir, "eos.sock"),
			Log: config.DaemonLogConfig{
				LogFileName: "daemon.log",
			},
		},
	}
}

func writePIDFile(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644); err != nil {
		t.Fatalf("writing pid file: %v", err)
	}
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("touching file: %v", err)
	}
}

func TestStandaloneDaemonController_LogsHint(t *testing.T) {
	c := newStandaloneController(t, t.TempDir())
	if got := c.LogsHint(); got != "eos daemon logs" {
		t.Errorf("expected %q, got %q", "eos daemon logs", got)
	}
}

func TestStandaloneDaemonController_Info(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		c.Info(cmd)

		if !strings.Contains(out.String(), "daemon not found") {
			t.Errorf("expected 'daemon not found', got: %s", out.String())
		}
	})

	t.Run("found but not running", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		writePIDFile(t, c.cfg.PIDFile, deadPID(t))
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		c.Info(cmd)

		got := out.String()
		if !strings.Contains(got, "found but not running") {
			t.Errorf("expected 'found but not running', got: %s", got)
		}
		if !strings.Contains(got, c.cfg.SocketPath) {
			t.Errorf("expected printStandaloneDaemonDetails to render socket path, got: %s", got)
		}
	})

	t.Run("running but version unreachable", func(t *testing.T) {
		// No listener on the socket: the daemon "looks" alive via the pid file
		// (a real, if unrelated, live process), but the IPC round-trip must
		// fail cleanly and the version line must be omitted rather than
		// breaking the rest of the output.
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		c.cfg.SocketTimeout = 100 * time.Millisecond
		child := spawnDisposableChild(t)
		writePIDFile(t, c.cfg.PIDFile, child.Process.Pid)
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetContext(context.Background())

		c.Info(cmd)

		got := out.String()
		if !strings.Contains(got, "daemon is running") {
			t.Errorf("expected 'daemon is running', got: %s", got)
		}
		if !strings.Contains(got, "PID:") {
			t.Errorf("expected printStandaloneDaemonDetails to render PID, got: %s", got)
		}
		if strings.Contains(got, "running version:") {
			t.Errorf("expected no 'running version:' line when the daemon socket is unreachable, got: %s", got)
		}
	})

	t.Run("running with version reachable", func(t *testing.T) {
		// A short os.MkdirTemp root, not t.TempDir(): the latter nests under
		// this test's (long) name, and a unix socket path is capped at
		// ~104 bytes — nesting under the test name alone can blow that
		// budget.
		tempDir, err := os.MkdirTemp("", "eos-sock-*")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

		c := newStandaloneController(t, tempDir)
		c.cfg.SocketTimeout = time.Second
		child := spawnDisposableChild(t)
		writePIDFile(t, c.cfg.PIDFile, child.Process.Pid)

		ln, err := net.Listen("unix", c.cfg.SocketPath)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			var req types.DaemonRequest
			if decErr := json.NewDecoder(conn).Decode(&req); decErr != nil {
				return
			}
			data, _ := json.Marshal(types.GetVersionResponse{Version: "v9.9.9"})
			_ = json.NewEncoder(conn).Encode(types.DaemonResponse{Success: true, Data: data})
		}()

		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetContext(context.Background())

		c.Info(cmd)

		got := out.String()
		if !strings.Contains(got, "daemon is running") {
			t.Errorf("expected 'daemon is running', got: %s", got)
		}
		if !strings.Contains(got, "running version: v9.9.9") {
			t.Errorf("expected 'running version: v9.9.9', got: %s", got)
		}
		if !strings.Contains(got, "PID:") {
			t.Errorf("expected printStandaloneDaemonDetails to render PID, got: %s", got)
		}
	})
}

func TestStandaloneDaemonController_Stop(t *testing.T) {
	t.Run("no pid file", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		killed, err := c.Stop(t.Context(), cmd, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if killed {
			t.Error("expected killed=false when no pid file exists")
		}
	})

	t.Run("invalid pid content errors", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		if err := os.WriteFile(c.cfg.PIDFile, []byte("not-a-pid"), 0644); err != nil {
			t.Fatalf("writing pid file: %v", err)
		}
		touchFile(t, c.cfg.SocketPath)
		var errBuf bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)

		_, err := c.Stop(t.Context(), cmd, true)
		if err == nil {
			t.Fatal("expected error for non-numeric pid file content")
		}
	})

	t.Run("live process is killed", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		child := spawnDisposableChild(t)
		writePIDFile(t, c.cfg.PIDFile, child.Process.Pid)
		touchFile(t, c.cfg.SocketPath)
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		killed, err := c.Stop(t.Context(), cmd, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !killed {
			t.Error("expected killed=true for a live process")
		}
	})
}

func TestStandaloneDaemonController_Remove(t *testing.T) {
	t.Run("running daemon cannot be removed", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		child := spawnDisposableChild(t)
		writePIDFile(t, c.cfg.PIDFile, child.Process.Pid)

		err := c.Remove()
		if err == nil || !strings.Contains(err.Error(), "stop it first") {
			t.Fatalf("expected 'stop it first' error, got: %v", err)
		}
	})

	t.Run("stopped daemon is removed", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		writePIDFile(t, c.cfg.PIDFile, deadPID(t))
		touchFile(t, c.cfg.SocketPath)

		if err := c.Remove(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(c.cfg.PIDFile); !os.IsNotExist(err) {
			t.Error("expected pid file to be removed")
		}
	})
}

func TestStandaloneDaemonController_Logs(t *testing.T) {
	t.Run("log file missing", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		var errBuf bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)

		c.Logs(cmd, 10, false)

		if !strings.Contains(errBuf.String(), "getting log file") {
			t.Errorf("expected log file error, got: %s", errBuf.String())
		}
	})

	t.Run("invalid line count", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		logDir := manager.CreateLogDirPath(tempDir)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			t.Fatalf("creating log dir: %v", err)
		}
		logPath := filepath.Join(logDir, c.cfg.Log.LogFileName)
		if err := os.WriteFile(logPath, []byte("line one\n"), 0644); err != nil {
			t.Fatalf("writing log file: %v", err)
		}
		var errBuf bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)

		c.Logs(cmd, 20000, false)

		if !strings.Contains(errBuf.String(), "invalid line count") {
			t.Errorf("expected invalid line count error, got: %s", errBuf.String())
		}
	})

	t.Run("renders tailed log lines", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		logDir := manager.CreateLogDirPath(tempDir)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			t.Fatalf("creating log dir: %v", err)
		}
		logPath := filepath.Join(logDir, c.cfg.Log.LogFileName)
		if err := os.WriteFile(logPath, []byte("hello from the log\n"), 0644); err != nil {
			t.Fatalf("writing log file: %v", err)
		}
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetContext(t.Context())
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})

		c.Logs(cmd, 10, false)

		if !strings.Contains(out.String(), "hello from the log") {
			t.Errorf("expected tailed log content, got: %s", out.String())
		}
		if !strings.Contains(out.String(), "showing daemon logs") {
			t.Errorf("expected 'showing daemon logs' info line, got: %s", out.String())
		}
	})
}

func TestNewDaemonController(t *testing.T) {
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}

	t.Run("standalone", func(t *testing.T) {
		cfg := config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{PIDFile: "/tmp/eos.pid"}}
		ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, config.TelemetryConfig{}, false, identity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := ctrl.(*standaloneDaemonController); !ok {
			t.Errorf("expected *standaloneDaemonController, got %T", ctrl)
		}
	})

	t.Run("systemd", func(t *testing.T) {
		cfg := config.DaemonConfig{Systemd: &config.SystemdConfig{}}
		ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, config.TelemetryConfig{}, false, identity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := ctrl.(systemdDaemonController); !ok {
			t.Errorf("expected systemdDaemonController, got %T", ctrl)
		}
	})

	t.Run("launchd", func(t *testing.T) {
		cfg := config.DaemonConfig{Launchd: &config.LaunchdConfig{}}
		ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, config.TelemetryConfig{}, false, identity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := ctrl.(launchdDaemonController); !ok {
			t.Errorf("expected launchdDaemonController, got %T", ctrl)
		}
	})

	t.Run("none set is an error", func(t *testing.T) {
		_, err := newDaemonController(config.DaemonConfig{}, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, config.TelemetryConfig{}, false, identity)
		if err == nil {
			t.Fatal("expected error when standalone, systemd, and launchd are all nil")
		}
	})
}

func TestLaunchdDaemonController_Domain(t *testing.T) {
	systemCtrl := launchdDaemonController{cfg: config.LaunchdConfig{UserAgent: false}}
	if got := systemCtrl.domain(); got != "system" {
		t.Errorf("expected %q, got %q", "system", got)
	}

	userCtrl := launchdDaemonController{cfg: config.LaunchdConfig{UserAgent: true}}
	if got := userCtrl.domain(); !strings.HasPrefix(got, "gui/") {
		t.Errorf("expected gui/<uid>, got %q", got)
	}
}

func TestLaunchdDaemonController_Target(t *testing.T) {
	ctrl := launchdDaemonController{cfg: config.LaunchdConfig{
		UserAgent:            false,
		LaunchdPlistFileName: "org.elysiumlabs.eos.plist",
	}}
	if got := ctrl.target(); got != "system/org.elysiumlabs.eos" {
		t.Errorf("expected %q, got %q", "system/org.elysiumlabs.eos", got)
	}
}

func TestLaunchdDaemonController_LogsHint(t *testing.T) {
	c := launchdDaemonController{}
	if got := c.LogsHint(); got != "eos daemon logs" {
		t.Errorf("expected %q, got %q", "eos daemon logs", got)
	}
}

func TestLaunchdDaemonController_Remove(t *testing.T) {
	tempDir := t.TempDir()
	plistFile := filepath.Join(tempDir, "org.elysiumlabs.eos.plist")
	touchFile(t, plistFile)

	c := launchdDaemonController{cfg: config.LaunchdConfig{
		LaunchdTargetDir:     tempDir + "/",
		LaunchdPlistFileName: "org.elysiumlabs.eos.plist",
	}}
	if err := c.Remove(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(plistFile); !os.IsNotExist(err) {
		t.Error("expected plist file to be removed")
	}
}

func TestLaunchdDaemonController_Logs(t *testing.T) {
	tempDir := t.TempDir()
	logDir := manager.CreateLogDirPath(tempDir)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("creating log dir: %v", err)
	}
	logPath := filepath.Join(logDir, config.DaemonLogFileName)
	if err := os.WriteFile(logPath, []byte("hello from launchd\n"), 0644); err != nil {
		t.Fatalf("writing log file: %v", err)
	}

	c := launchdDaemonController{baseDir: tempDir}
	var out bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetContext(t.Context())
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	c.Logs(cmd, 10, false)

	if !strings.Contains(out.String(), "hello from launchd") {
		t.Errorf("expected tailed log content, got: %s", out.String())
	}
}

func TestPrintLaunchdDaemonDetails(t *testing.T) {
	for _, userAgent := range []bool{false, true} {
		var out, errOut bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)

		printLaunchdDaemonDetails(cmd, userAgent)

		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "launchd managed") {
			t.Errorf("userAgent=%v: expected 'launchd managed', got: %s", userAgent, combined)
		}
		if !strings.Contains(combined, "launchctl print") {
			t.Errorf("userAgent=%v: expected launchctl print hint, got: %s", userAgent, combined)
		}
	}
}

func TestPrintStandaloneDaemonDetails(t *testing.T) {
	var out bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)

	cfg := &config.StandaloneDaemonConfig{
		PIDFile:    "/tmp/eos.pid",
		SocketPath: "/tmp/eos.sock",
		Log: config.DaemonLogConfig{
			LogDir:           "/tmp/logs",
			LogFileName:      "daemon.log",
			LogMaxFiles:      5,
			LogFileSizeLimit: 1024,
		},
	}
	printStandaloneDaemonDetails(cmd, 4242, cfg)

	got := out.String()
	for _, want := range []string{"4242", "/tmp/eos.pid", "/tmp/eos.sock", "/tmp/logs", "daemon.log"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got: %s", want, got)
		}
	}
}

func TestPrintSystemdDaemonDetails(t *testing.T) {
	for _, userUnit := range []bool{false, true} {
		var out, errOut bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		cmd.SetContext(context.Background())

		printSystemdDaemonDetails(cmd, userUnit)

		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "systemd managed") {
			t.Errorf("userUnit=%v: expected 'systemd managed', got: %s", userUnit, combined)
		}
		if !strings.Contains(combined, "journalctl") {
			t.Errorf("userUnit=%v: expected journalctl hint, got: %s", userUnit, combined)
		}
	}
}

// TestSystemdUserBusReachable_FalseWhenEnvUnset guards issue #41: the bus-availability check must
// key off whether $XDG_RUNTIME_DIR is actually exported in this process, not whether some
// accessible runtime dir happens to exist for the uid elsewhere on disk — a lingering user manager
// can leave /run/user/<uid> present while this shell's environment simply lacks the var.
func TestSystemdUserBusReachable_FalseWhenEnvUnset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if systemdUserBusReachable(os.Getuid()) {
		t.Error("expected bus to be reported unreachable when XDG_RUNTIME_DIR is unset")
	}
}

func TestSystemdUserBusReachable_FalseWhenEnvPointsAtMissingDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "gone"))
	if systemdUserBusReachable(os.Getuid()) {
		t.Error("expected bus to be reported unreachable when XDG_RUNTIME_DIR points at a nonexistent dir")
	}
}

func TestSystemdUserBusReachable_TrueWhenEnvPointsAtAccessibleOwnDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if !systemdUserBusReachable(os.Getuid()) {
		t.Error("expected bus to be reported reachable when XDG_RUNTIME_DIR points at an accessible dir owned by uid")
	}
}

// TestPrintSystemdDaemonDetails_WarnsWhenXDGRuntimeDirUnset is the integration-level counterpart
// to TestSystemdUserBusReachable_FalseWhenEnvUnset: it exercises the actual warning text printed
// by `eos daemon info` for a systemd --user daemon when the bus can't be reached.
func TestPrintSystemdDaemonDetails_WarnsWhenXDGRuntimeDirUnset(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())
	t.Setenv("XDG_RUNTIME_DIR", "")

	printSystemdDaemonDetails(cmd, true)

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "no active systemd user bus") {
		t.Errorf("expected bus warning when XDG_RUNTIME_DIR is unset, got: %s", combined)
	}
}

func TestPrintSystemdDaemonDetails_NoWarningWhenXDGRuntimeDirAccessible(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	printSystemdDaemonDetails(cmd, true)

	combined := out.String() + errOut.String()
	if strings.Contains(combined, "no active systemd user bus") {
		t.Errorf("expected no bus warning when XDG_RUNTIME_DIR points at an accessible dir, got: %s", combined)
	}
}

// TestPrintSystemdDaemonDetails_DebugsVersionResolutionFailureWhenVerbose guards issue #41's other
// half: a failure to resolve the running version must not be swallowed silently — with --verbose it
// should surface why.
func TestPrintSystemdDaemonDetails_DebugsVersionResolutionFailureWhenVerbose(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.Flags().Bool("verbose", true, "")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	// userUnit=false keeps this independent of the XDG_RUNTIME_DIR/bus warning above; running
	// version resolution fails regardless (no systemd "eos" unit exists in the test environment).
	printSystemdDaemonDetails(cmd, false)

	if !strings.Contains(errOut.String(), "resolving running version") {
		t.Errorf("expected debug output about the running-version lookup failure, got: %s", errOut.String())
	}
}
