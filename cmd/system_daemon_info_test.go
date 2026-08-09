package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/spf13/cobra"
)

// TestPrintDaemonInfo covers every supervisor branch of the "eos system info"
// Daemon block. Only standalone and systemd used to be handled, so a launchd-
// or OpenRC-managed install dereferenced a nil SystemdConfig, and no supervised
// mode ever printed the socket the CLI actually talks to.
func TestPrintDaemonInfo(t *testing.T) {
	tests := []struct {
		name   string
		daemon config.DaemonConfig
		want   []string
	}{
		{
			name: "standalone",
			daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
				PIDFile:       "/home/u/.eos/eos.pid",
				SocketPath:    "/home/u/.eos/eos.sock",
				SocketTimeout: 5 * time.Second,
				Log: config.DaemonLogConfig{
					LogDir:           "/home/u/.eos/logs",
					LogFileName:      "daemon.log",
					LogMaxFiles:      5,
					LogFileSizeLimit: 1024,
				},
			}},
			want: []string{"systemd managed:", "false", "/home/u/.eos/eos.pid", "/home/u/.eos/eos.sock", "daemon.log", "1024"},
		},
		{
			name: "systemd",
			daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{
				SystemdTargetDir:      "/etc/systemd/system/",
				SystemdTargetFileName: "eos.service",
				SocketPath:            "/home/u/.eos/eos.sock",
				UserUnit:              true,
			}},
			want: []string{"systemd managed:", "true", "/etc/systemd/system/", "eos.service", "/home/u/.eos/eos.sock", "user unit:"},
		},
		{
			name: "launchd",
			daemon: config.DaemonConfig{Launchd: &config.LaunchdConfig{
				LaunchdTargetDir:     "/Library/LaunchDaemons/",
				LaunchdPlistFileName: "org.elysiumlabs.eos.plist",
				SocketPath:           "/Users/u/.eos/eos.sock",
			}},
			want: []string{"launchd managed:", "/Library/LaunchDaemons/", "org.elysiumlabs.eos.plist", "/Users/u/.eos/eos.sock", "user agent:"},
		},
		{
			name: "openrc",
			daemon: config.DaemonConfig{OpenRC: &config.OpenRCConfig{
				InitDir:      "/etc/init.d/",
				InitFileName: "eos",
				SocketPath:   "/home/u/.eos/eos.sock",
			}},
			want: []string{"openrc managed:", "/etc/init.d/", "/home/u/.eos/eos.sock"},
		},
		{
			name:   "unconfigured",
			daemon: config.DaemonConfig{},
			want:   []string{"(none configured)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&out)

			printDaemonInfo(cmd, tt.daemon)

			got := out.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in Daemon block, got:\n%s", want, got)
				}
			}
		})
	}
}

// TestPrintStandaloneStaleWarning covers printStandaloneStaleWarning's own
// liveness/staleness gating: unlike "eos daemon info", this Daemon block used
// to run no liveness check at all, so a standalone daemon running a
// since-replaced binary was previously invisible here.
func TestPrintStandaloneStaleWarning(t *testing.T) {
	t.Run("no pid file, no warning", func(t *testing.T) {
		cfg := &config.StandaloneDaemonConfig{PIDFile: filepath.Join(t.TempDir(), "eos.pid")}
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printStandaloneStaleWarning(cmd, cfg)

		if out.Len() != 0 {
			t.Errorf("expected no output when no daemon is running, got: %s", out.String())
		}
	})

	t.Run("dead pid, no warning", func(t *testing.T) {
		cfg := &config.StandaloneDaemonConfig{PIDFile: filepath.Join(t.TempDir(), "eos.pid")}
		writePIDFile(t, cfg.PIDFile, deadPID(t))
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printStandaloneStaleWarning(cmd, cfg)

		if out.Len() != 0 {
			t.Errorf("expected no output for a dead pid, got: %s", out.String())
		}
	})

	t.Run("running the current binary, no warning", func(t *testing.T) {
		cfg := &config.StandaloneDaemonConfig{PIDFile: filepath.Join(t.TempDir(), "eos.pid")}
		writePIDFile(t, cfg.PIDFile, os.Getpid())
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printStandaloneStaleWarning(cmd, cfg)

		if strings.Contains(out.String(), "restart needed") {
			t.Errorf("a daemon running the current binary must never be flagged stale, got: %s", out.String())
		}
	})

	t.Run("running a since-replaced binary, warns", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("/proc/<pid>/exe resolution is Linux-only")
		}
		cfg := &config.StandaloneDaemonConfig{PIDFile: filepath.Join(t.TempDir(), "eos.pid")}
		child := spawnDisposableChild(t)
		writePIDFile(t, cfg.PIDFile, child.Process.Pid)
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printStandaloneStaleWarning(cmd, cfg)

		got := out.String()
		if !strings.Contains(got, "since-replaced binary, restart needed") {
			t.Errorf("expected the stale-binary warning, got: %s", got)
		}
		if !strings.Contains(got, fmt.Sprintf("pid %d", child.Process.Pid)) {
			t.Errorf("expected the pid rendered, got: %s", got)
		}
	})

	t.Run("status error, no warning", func(t *testing.T) {
		cfg := &config.StandaloneDaemonConfig{PIDFile: t.TempDir()} // a directory, not a file
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)

		printStandaloneStaleWarning(cmd, cfg)

		if out.Len() != 0 {
			t.Errorf("expected no output when daemon status can't be read, got: %s", out.String())
		}
	})
}

// TestSupervisedDaemonConfigsCarrySocket pins the wiring that makes
// ResolveDaemonEndpoint usable: every supervised daemon config the CLI builds
// must name this base dir's socket, not leave it empty.
func TestSupervisedDaemonConfigsCarrySocket(t *testing.T) {
	baseDir := t.TempDir()
	wantSock := baseDir + "/" + config.DaemonSocketPath
	logCfg := config.EosLogConfig{}

	t.Run("systemd", func(t *testing.T) {
		cfg := newDaemonConfig(baseDir, true, false, "/etc/systemd/system/", false, logCfg)
		if cfg.Systemd == nil {
			t.Fatal("expected a Systemd config")
		}
		if cfg.Systemd.SocketPath != wantSock {
			t.Errorf("SocketPath = %q, want %q", cfg.Systemd.SocketPath, wantSock)
		}
	})

	t.Run("launchd", func(t *testing.T) {
		cfg := newDaemonConfigLaunchd(baseDir, true, false, "/Library/LaunchDaemons/", false, logCfg)
		if cfg.Launchd == nil {
			t.Fatal("expected a Launchd config")
		}
		if cfg.Launchd.SocketPath != wantSock {
			t.Errorf("SocketPath = %q, want %q", cfg.Launchd.SocketPath, wantSock)
		}
	})

	t.Run("openrc", func(t *testing.T) {
		cfg := newDaemonConfigOpenRC(baseDir, true, false, "/etc/init.d/", logCfg)
		if cfg.OpenRC == nil {
			t.Fatal("expected an OpenRC config")
		}
		if cfg.OpenRC.SocketPath != wantSock {
			t.Errorf("SocketPath = %q, want %q", cfg.OpenRC.SocketPath, wantSock)
		}
	})
}
