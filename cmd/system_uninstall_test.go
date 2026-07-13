package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"github.com/spf13/cobra"
)

// stopErrDaemonController is a DaemonController whose Stop always fails, for exercising
// uninstallCmd's daemon-stop-error path (fakeDaemonController.Stop never errors).
type stopErrDaemonController struct{ fakeDaemonController }

func (s *stopErrDaemonController) Stop(_ context.Context, _ *cobra.Command, _ bool) (bool, error) {
	return false, errors.New("stop failed")
}

func TestUserRuntimeDir(t *testing.T) {
	if got, want := userRuntimeDir(1000), "/run/user/1000"; got != want {
		t.Errorf("userRuntimeDir(1000) = %q, want %q", got, want)
	}
	if got, want := userRuntimeDir(0), "/run/user/0"; got != want {
		t.Errorf("userRuntimeDir(0) = %q, want %q", got, want)
	}
}

func TestHandleStoppingServices(t *testing.T) {
	svc := types.ServiceInstance{Name: "svc-a"}
	cfg := &config.SystemConfig{Shutdown: config.ShutdownConfig{GracePeriod: time.Second}}

	t.Run("confirmed, all stop successfully", func(t *testing.T) {
		var removed []string
		mgr := &mockMgr{
			stopSvc: func(name string, _, _ time.Duration) (manager.StopServiceResult, error) {
				return manager.StopServiceResult{}, nil
			},
			removeInstance: func(name string) (bool, error) {
				removed = append(removed, name)
				return true, nil
			},
		}
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})

		ok := handleStoppingServices(cmd, mgr, cfg, []types.ServiceInstance{svc}, true)

		if !ok {
			t.Fatal("expected handleStoppingServices to return true")
		}
		if !strings.Contains(out.String(), "stopped 1 services") {
			t.Errorf("expected stop count in output, got: %s", out.String())
		}
		if len(removed) != 1 || removed[0] != "svc-a" {
			t.Errorf("expected svc-a to be removed, got: %v", removed)
		}
	})

	t.Run("declined confirmation cancels uninstall", func(t *testing.T) {
		mgr := &mockMgr{}
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("n\n"))

		ok := handleStoppingServices(cmd, mgr, cfg, []types.ServiceInstance{svc}, false)

		if ok {
			t.Fatal("expected handleStoppingServices to return false")
		}
		if !strings.Contains(out.String(), "uninstall canceled") {
			t.Errorf("expected cancel message, got: %s", out.String())
		}
	})

	t.Run("stop errors then force-stop declined cancels uninstall", func(t *testing.T) {
		mgr := &mockMgr{
			stopSvc: func(name string, _, _ time.Duration) (manager.StopServiceResult, error) {
				return manager.StopServiceResult{}, errors.New("stop failed")
			},
		}
		var out, errBuf bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetIn(strings.NewReader("y\nn\n"))

		ok := handleStoppingServices(cmd, mgr, cfg, []types.ServiceInstance{svc}, false)

		if ok {
			t.Fatal("expected handleStoppingServices to return false")
		}
		if !strings.Contains(out.String(), "uninstall canceled due to remaining active services") {
			t.Errorf("expected remaining-services cancel message, got: %s", out.String())
		}
	})

	t.Run("stop errors then force-stop confirmed continues", func(t *testing.T) {
		mgr := &mockMgr{
			stopSvc: func(name string, _, _ time.Duration) (manager.StopServiceResult, error) {
				return manager.StopServiceResult{}, errors.New("stop failed")
			},
			forceStop: func(name string) (manager.StopServiceResult, error) {
				return manager.StopServiceResult{}, nil
			},
		}
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("y\ny\n"))

		ok := handleStoppingServices(cmd, mgr, cfg, []types.ServiceInstance{svc}, false)

		if !ok {
			t.Fatal("expected handleStoppingServices to return true")
		}
	})
}

func TestUninstallCmd(t *testing.T) {
	t.Run("get instances error", func(t *testing.T) {
		mgr := &mockMgr{
			getAllInstances: func() ([]types.ServiceInstance, error) {
				return nil, errors.New("db down")
			},
		}
		var errBuf bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetContext(t.Context())
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)

		_ = uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, &fakeDaemonController{}, t.TempDir(), t.TempDir(), true)

		if !strings.Contains(errBuf.String(), "getting all service instances") {
			t.Errorf("expected instance-fetch error, got: %s", errBuf.String())
		}
	})

	t.Run("daemon stop error aborts before binary removal", func(t *testing.T) {
		mgr := &mockMgr{}
		fake := &stopErrDaemonController{}
		var out, errBuf bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetContext(t.Context())
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.Flags().Bool("verbose", false, "")

		_ = uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, fake, t.TempDir(), t.TempDir(), true)

		if !strings.Contains(errBuf.String(), "stopping daemon: stop failed") {
			t.Errorf("expected daemon stop error, got: %s", errBuf.String())
		}
		if strings.Contains(out.String(), "uninstall complete") {
			t.Errorf("expected uninstall to abort on daemon stop, got: %s", out.String())
		}
	})

	t.Run("no active services, confirmed, completes", func(t *testing.T) {
		mgr := &mockMgr{}
		fake := &fakeDaemonController{}
		installDir := t.TempDir()
		baseDir := t.TempDir()
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetContext(t.Context())
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.Flags().Bool("verbose", false, "")

		_ = uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, fake, installDir, baseDir, true)

		if !strings.Contains(out.String(), "uninstall complete") {
			t.Errorf("expected uninstall to complete, got: %s", out.String())
		}
	})

	t.Run("active services declined stop aborts uninstall", func(t *testing.T) {
		mgr := &mockMgr{
			getAllInstances: func() ([]types.ServiceInstance, error) {
				return []types.ServiceInstance{{Name: "svc-a"}}, nil
			},
		}
		fake := &fakeDaemonController{}
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetContext(t.Context())
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("n\n"))
		cmd.Flags().Bool("verbose", false, "")

		_ = uninstallCmd(cmd, func() manager.ServiceManager { return mgr }, func() *config.SystemConfig { return &config.SystemConfig{} }, fake, t.TempDir(), t.TempDir(), false)

		if strings.Contains(out.String(), "uninstall complete") {
			t.Errorf("expected uninstall to abort, got: %s", out.String())
		}
		if !strings.Contains(out.String(), "uninstall canceled") {
			t.Errorf("expected cancel message, got: %s", out.String())
		}
	})
}
