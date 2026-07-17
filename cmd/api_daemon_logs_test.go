package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
)

func runAPIDaemonLogs(t *testing.T, getConfig func() (string, *config.SystemConfig, userutil.Identity, error), args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	cmd := newAPIDaemonCmd(getConfig)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"logs"}, args...))
	err := cmd.ExecuteContext(t.Context())
	return &outBuf, &errBuf, err
}

func TestAPIDaemonLogs_ConfigError(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return "", nil, userutil.Identity{}, errors.New("config broke")
	}

	_, errBuf, err := runAPIDaemonLogs(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "config broke") {
		t.Errorf("expected config error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonLogs_SystemdManagedIsError(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Systemd: &config.SystemdConfig{}},
		}, userutil.Identity{}, nil
	}

	_, errBuf, err := runAPIDaemonLogs(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "journalctl") {
		t.Errorf("expected journalctl hint in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonLogs_InvalidLineCount(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{}},
		}, userutil.Identity{}, nil
	}

	_, errBuf, err := runAPIDaemonLogs(t, getConfig, "--lines", "-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "must be between 0 and 10000") {
		t.Errorf("expected line count error, got: %s", errBuf.String())
	}
}

func TestAPIDaemonLogs_LogFileMissing(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
				Log: config.DaemonLogConfig{LogFileName: "daemon.log"},
			}},
		}, userutil.Identity{}, nil
	}

	_, errBuf, err := runAPIDaemonLogs(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "reading log file") {
		t.Errorf("expected log read error, got: %s", errBuf.String())
	}
}

func TestAPIDaemonLogs_Success(t *testing.T) {
	baseDir := t.TempDir()
	logDir := manager.CreateLogDirPath(baseDir)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("creating log dir: %v", err)
	}
	logPath := filepath.Join(logDir, "daemon.log")
	if err := os.WriteFile(logPath, []byte("line one\nline two\nline three\n"), 0644); err != nil {
		t.Fatalf("writing log file: %v", err)
	}

	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return baseDir, &config.SystemConfig{
			Daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
				Log: config.DaemonLogConfig{LogFileName: "daemon.log"},
			}},
		}, userutil.Identity{}, nil
	}

	outBuf, _, err := runAPIDaemonLogs(t, getConfig, "--lines", "2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonLogsResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), jsonErr)
	}
	if result.LogPath != logPath {
		t.Errorf("expected log_path %q, got %q", logPath, result.LogPath)
	}
	if len(result.Lines) != 2 || result.Lines[0] != "line two" || result.Lines[1] != "line three" {
		t.Errorf("expected last 2 lines [line two, line three], got %v", result.Lines)
	}
}
