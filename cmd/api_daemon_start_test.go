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

func runAPIDaemonStart(t *testing.T, getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) (*bytes.Buffer, error) {
	t.Helper()
	cmd := newAPIDaemonCmd(getConfig)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"start"})
	err := cmd.ExecuteContext(t.Context())
	return &errBuf, err
}

func TestAPIDaemonStart_ConfigError(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return "", nil, userutil.Identity{}, errors.New("config broke")
	}

	errBuf, err := runAPIDaemonStart(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "config broke") {
		t.Errorf("expected config error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonStart_InvalidDaemonConfig(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{}, userutil.Identity{}, nil
	}

	errBuf, err := runAPIDaemonStart(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "resolving daemon mode") {
		t.Errorf("expected daemon mode resolution error, got: %s", errBuf.String())
	}
}

// OpenRC's Start refuses before touching the filesystem or exec'ing anything when
// not running as root, making it the one backend whose error path is exercisable
// here with zero side effects (no real service manager gets invoked).
func TestAPIDaemonStart_OpenRCRequiresRoot(t *testing.T) {
	getConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		return t.TempDir(), &config.SystemConfig{
			Daemon: config.DaemonConfig{OpenRC: &config.OpenRCConfig{}},
		}, userutil.Identity{}, nil
	}

	errBuf, err := runAPIDaemonStart(t, getConfig)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "requires root") {
		t.Errorf("expected requires-root error, got: %s", errBuf.String())
	}
}

// Start always delegates to a real service manager (systemctl/launchctl/rc-service)
// or forks a real daemon process, so there is no backend left to exercise a
// success path against without either root or mutating real host state — see the
// error-path cases above, which mirror how TestDaemonStartError et al. in
// daemon_test.go cover the human-facing command via the same DaemonController.
func TestAPIDaemonStart_JSONShape(t *testing.T) {
	out, err := json.Marshal(apiDaemonStartResult{Started: true})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if string(out) != `{"started":true}` {
		t.Errorf("unexpected JSON shape: %s", out)
	}
}
