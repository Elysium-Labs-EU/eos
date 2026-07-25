package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

func runAPIDaemonInfo(t *testing.T, getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	cmd := newAPIDaemonInfoCmd(getConfig)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	err := cmd.ExecuteContext(t.Context())
	return &outBuf, &errBuf, err
}

func TestAPIDaemonInfo_ConfigError(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return "", nil, userutil.Identity{}, errors.New("config broke")
	}

	_, errBuf, err := runAPIDaemonInfo(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "config broke") {
		t.Errorf("expected config error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonInfo_InvalidDaemonConfig(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{Daemon: config.DaemonConfig{}}, userutil.Identity{}, nil
	}

	_, errBuf, err := runAPIDaemonInfo(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "invalid daemon config") {
		t.Errorf("expected invalid daemon config error, got: %s", errBuf.String())
	}
}

func TestAPIDaemonInfo_Systemd(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{UserUnit: true}},
		}, userutil.Identity{}, nil
	}

	outBuf, _, err := runAPIDaemonInfo(t, getConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonInfoResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), jsonErr)
	}
	if result.Mode != "systemd" {
		t.Errorf("expected mode=systemd, got %q", result.Mode)
	}
	if result.UserUnit == nil || !*result.UserUnit {
		t.Error("expected user_unit=true")
	}
	if result.Running == nil || *result.Running {
		t.Error("expected running=false when nothing is listening on the unit's socket")
	}
	if result.Pid != nil {
		t.Errorf("expected no pid when not running, got %d", *result.Pid)
	}
}

// TestAPIDaemonInfo_Systemd_Running is the regression test for issue #65: a
// systemd-managed daemon must report live running/pid state instead of the
// static {"user_unit":...,"mode":"systemd"} payload, regardless of whether the
// unit is actually active.
func TestAPIDaemonInfo_Systemd_Running(t *testing.T) {
	dir := shortTempSocketDir(t)
	sockPath := filepath.Join(dir, "eos.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()
	stubSystemctl(t, 424242)

	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{UserUnit: true, SocketPath: sockPath}},
		}, userutil.Identity{}, nil
	}

	outBuf, _, err := runAPIDaemonInfo(t, getConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonInfoResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), jsonErr)
	}
	if result.Running == nil || !*result.Running {
		t.Errorf("expected running=true when the unit's socket is accepting connections, got: %s", outBuf.String())
	}
	if result.Pid == nil || *result.Pid != 424242 {
		t.Errorf("expected pid=424242 from the stubbed systemctl, got: %s", outBuf.String())
	}
}

func TestAPIDaemonInfo_Standalone(t *testing.T) {
	baseDir := t.TempDir()
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return baseDir, &config.SystemConfig{
			Daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
				PIDFile:    "/tmp/nonexistent-eos-test.pid",
				SocketPath: "/tmp/nonexistent-eos-test.sock",
				Log:        config.DaemonLogConfig{LogDir: "logs", LogFileName: "daemon.log", LogMaxFiles: 5, LogFileSizeLimit: 1024},
			}},
		}, userutil.Identity{}, nil
	}

	outBuf, _, err := runAPIDaemonInfo(t, getConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonInfoResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), jsonErr)
	}
	if result.Mode != "standalone" {
		t.Errorf("expected mode=standalone, got %q", result.Mode)
	}
	if result.Running == nil || *result.Running {
		t.Error("expected running=false for a daemon with no live PID file")
	}
	if result.LogMaxFiles != 5 {
		t.Errorf("expected log_max_files=5, got %d", result.LogMaxFiles)
	}
}
