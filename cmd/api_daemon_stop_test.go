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

func runAPIDaemonStop(t *testing.T, getConfig func() (string, *config.SystemConfig, userutil.Identity, error), args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	cmd := newAPIDaemonCmd(getConfig)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"stop"}, args...))
	err := cmd.ExecuteContext(t.Context())
	return &outBuf, &errBuf, err
}

func TestAPIDaemonStop_ConfigError(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return "", nil, userutil.Identity{}, errors.New("config broke")
	}

	_, errBuf, err := runAPIDaemonStop(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "config broke") {
		t.Errorf("expected config error in output, got: %s", errBuf.String())
	}
}

// See TestAPIDaemonStart_OpenRCRequiresRoot: OpenRC's Stop refuses before any
// exec when not running as root, giving a deterministic, side-effect-free error path.
func TestAPIDaemonStop_OpenRCRequiresRoot(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{OpenRC: &config.OpenRCConfig{}},
		}, userutil.Identity{}, nil
	}

	_, errBuf, err := runAPIDaemonStop(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "requires root") {
		t.Errorf("expected requires-root error, got: %s", errBuf.String())
	}
}

// A standalone daemon with no PID file on disk is the one deterministic, real
// (non-mocked) success path: StopStandaloneDaemon short-circuits to (false, nil)
// before touching any process or socket.
func TestAPIDaemonStop_StandaloneNotRunning(t *testing.T) {
	baseDir := t.TempDir()
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return baseDir, &config.SystemConfig{
			Daemon: config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
				PIDFile:    filepath.Join(baseDir, "eos.pid"),
				SocketPath: filepath.Join(baseDir, "eos.sock"),
			}},
		}, userutil.Identity{}, nil
	}

	outBuf, _, err := runAPIDaemonStop(t, getConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonStopResult
	if jsonErr := json.Unmarshal(outBuf.Bytes(), &result); jsonErr != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), jsonErr)
	}
	if result.Stopped {
		t.Errorf("expected stopped=false when no PID file exists, got %+v", result)
	}
}
