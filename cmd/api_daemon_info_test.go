package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
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
