package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
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

	t.Run("running", func(t *testing.T) {
		tempDir := t.TempDir()
		c := newStandaloneController(t, tempDir)
		child := spawnDisposableChild(t)
		writePIDFile(t, c.cfg.PIDFile, child.Process.Pid)
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		c.Info(cmd)

		got := out.String()
		if !strings.Contains(got, "daemon is running") {
			t.Errorf("expected 'daemon is running', got: %s", got)
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
		ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, false, identity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := ctrl.(*standaloneDaemonController); !ok {
			t.Errorf("expected *standaloneDaemonController, got %T", ctrl)
		}
	})

	t.Run("systemd", func(t *testing.T) {
		cfg := config.DaemonConfig{Systemd: &config.SystemdConfig{}}
		ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, false, identity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := ctrl.(systemdDaemonController); !ok {
			t.Errorf("expected systemdDaemonController, got %T", ctrl)
		}
	})

	t.Run("launchd", func(t *testing.T) {
		cfg := config.DaemonConfig{Launchd: &config.LaunchdConfig{}}
		ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, false, identity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := ctrl.(launchdDaemonController); !ok {
			t.Errorf("expected launchdDaemonController, got %T", ctrl)
		}
	})

	t.Run("none set is an error", func(t *testing.T) {
		_, err := newDaemonController(config.DaemonConfig{}, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, false, identity)
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
