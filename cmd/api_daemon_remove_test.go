package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

func runAPIDaemonRemove(t *testing.T, getConfig func() (string, *config.SystemConfig, userutil.Identity, error), args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	cmd := newAPIDaemonCmd(getConfig)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"remove"}, args...))
	err := cmd.ExecuteContext(t.Context())
	return &outBuf, &errBuf, err
}

func TestAPIDaemonRemove_ConfigError(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return "", nil, userutil.Identity{}, errors.New("config broke")
	}

	_, errBuf, err := runAPIDaemonRemove(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "config broke") {
		t.Errorf("expected config error in output, got: %s", errBuf.String())
	}
}

// systemd's Remove is a plain os.Remove of the unit file, so it's exercisable
// end-to-end (both failure and success) with a real temp file and no host
// service manager involved.
func TestAPIDaemonRemove_SystemdFileMissing(t *testing.T) {
	dir := t.TempDir()
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return dir, &config.SystemConfig{
			Daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{
				SystemdTargetDir:      dir + "/",
				SystemdTargetFileName: "eos.service",
			}},
		}, userutil.Identity{}, nil
	}

	_, errBuf, err := runAPIDaemonRemove(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "no such file") {
		t.Errorf("expected missing-file error, got: %s", errBuf.String())
	}
}

func TestAPIDaemonRemove_SystemdSuccess(t *testing.T) {
	dir := t.TempDir()
	unitFile := filepath.Join(dir, "eos.service")
	if err := os.WriteFile(unitFile, []byte("[Unit]"), 0644); err != nil {
		t.Fatalf("writing unit file: %v", err)
	}

	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return dir, &config.SystemConfig{
			Daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{
				SystemdTargetDir:      dir + "/",
				SystemdTargetFileName: "eos.service",
			}},
		}, userutil.Identity{}, nil
	}

	outBuf, _, err := runAPIDaemonRemove(t, getConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonRemoveResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), jsonErr)
	}
	if !result.Removed {
		t.Errorf("expected removed=true, got %+v", result)
	}
	if _, statErr := os.Stat(unitFile); !os.IsNotExist(statErr) {
		t.Errorf("expected unit file to be removed from disk")
	}
}
