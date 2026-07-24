package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

func runAPIDaemonInfo(t *testing.T, getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	cmd := newAPIDaemonCmd(getConfig)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"info"})
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

func TestAPIDaemonInfo_StandaloneNotRunning(t *testing.T) {
	baseDir := t.TempDir()
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return baseDir, &config.SystemConfig{
			Daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
				PIDFile: filepath.Join(baseDir, "eos.pid"),
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
	if result.ManagedBy != "standalone" {
		t.Errorf("expected managed_by=standalone, got %+v", result)
	}
	if result.Running == nil || *result.Running {
		t.Errorf("expected running=false, got %+v", result)
	}
	if result.Pid != nil {
		t.Errorf("expected no pid, got %+v", result)
	}
	if result.LogsHint != "eos daemon logs" {
		t.Errorf("expected logs hint, got %+v", result)
	}
}

func TestAPIDaemonInfo_Systemd(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{}},
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
	if result.ManagedBy != "systemd" {
		t.Errorf("expected managed_by=systemd, got %+v", result)
	}
	if result.Running != nil || result.Pid != nil {
		t.Errorf("expected no running/pid for systemd, got %+v", result)
	}
	if !strings.Contains(result.LogsHint, "journalctl") {
		t.Errorf("expected journalctl hint, got %+v", result)
	}
}

func TestAPIDaemonInfo_Launchd(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Launchd: &config.LaunchdConfig{}},
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
	if result.ManagedBy != "launchd" {
		t.Errorf("expected managed_by=launchd, got %+v", result)
	}
}

func TestAPIDaemonInfo_OpenRC(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{OpenRC: &config.OpenRCConfig{}},
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
	if result.ManagedBy != "openrc" {
		t.Errorf("expected managed_by=openrc, got %+v", result)
	}
}
