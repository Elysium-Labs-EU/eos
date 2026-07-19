package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
)

// recordingRun captures the last rc-service invocation so tests can assert on the
// exact command eos delegates to, and returns a scripted result.
type recordingRun struct {
	calls  [][]string
	out    []byte
	err    error
	called bool
}

func (r *recordingRun) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.called = true
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.out, r.err
}

func TestNewDaemonController_OpenRC(t *testing.T) {
	identity, err := userutil.ResolveIdentity()
	if err != nil {
		t.Fatalf("resolving identity: %v", err)
	}
	cfg := config.DaemonConfig{OpenRC: &config.OpenRCConfig{InitDir: "/etc/init.d/", InitFileName: "eos"}}
	ctrl, err := newDaemonController(cfg, t.TempDir(), &config.HealthConfig{}, config.ShutdownConfig{}, config.TelemetryConfig{}, false, identity)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := ctrl.(openrcDaemonController); !ok {
		t.Errorf("expected openrcDaemonController, got %T", ctrl)
	}
}

func TestOpenRCDaemonController_Unit(t *testing.T) {
	c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos-test"}}
	if got := c.unit(); got != "eos-test" {
		t.Errorf("expected unit %q, got %q", "eos-test", got)
	}
	empty := openrcDaemonController{cfg: config.OpenRCConfig{}}
	if got := empty.unit(); got != config.OpenRCTargetFileName {
		t.Errorf("expected fallback unit %q, got %q", config.OpenRCTargetFileName, got)
	}
}

func TestOpenRCDaemonController_Stop(t *testing.T) {
	// The root-guarded delegation to rc-service is exercised end-to-end on the
	// real Alpine VM; here we cover both privilege branches deterministically.
	if os.Getuid() == 0 {
		t.Run("delegates to rc-service stop", func(t *testing.T) {
			rec := &recordingRun{out: []byte(" * Stopping eos ... [ ok ]")}
			c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}, run: rec.run}
			cmd := newTestRootCmd(nil)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			killed, err := c.Stop(context.Background(), cmd, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !killed {
				t.Error("expected killed=true on successful rc-service stop")
			}
			if len(rec.calls) != 1 || strings.Join(rec.calls[0], " ") != "rc-service eos stop" {
				t.Errorf("expected 'rc-service eos stop', got %v", rec.calls)
			}
		})

		t.Run("surfaces rc-service failure", func(t *testing.T) {
			rec := &recordingRun{out: []byte("permission denied"), err: errors.New("exit 1")}
			c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}, run: rec.run}
			cmd := newTestRootCmd(nil)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			killed, err := c.Stop(context.Background(), cmd, false)
			if err == nil {
				t.Fatal("expected error when rc-service fails")
			}
			if killed {
				t.Error("expected killed=false on rc-service failure")
			}
			if !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("expected rc-service output in error, got: %v", err)
			}
		})
		return
	}

	// Non-root: must refuse with a clean hint and never touch rc-service — the
	// core of issue #13 is that stop must not report a false success.
	rec := &recordingRun{}
	c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}, run: rec.run}
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	killed, err := c.Stop(context.Background(), cmd, false)
	if err == nil {
		t.Fatal("expected 'requires root' error when non-root")
	}
	if killed {
		t.Error("expected killed=false when refused for lack of root")
	}
	if !strings.Contains(err.Error(), "requires root") {
		t.Errorf("expected 'requires root' hint, got: %v", err)
	}
	if rec.called {
		t.Error("rc-service must not be invoked when non-root")
	}
}

func TestOpenRCDaemonController_Start_NonRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test asserts the non-root guard")
	}
	rec := &recordingRun{}
	c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}, run: rec.run}
	if err := c.Start(context.Background(), true, false, false); err == nil {
		t.Fatal("expected 'requires root' error when non-root")
	}
	if rec.called {
		t.Error("rc-service must not be invoked when non-root")
	}
}

func TestOpenRCDaemonController_Info(t *testing.T) {
	c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}}
	var out, errBuf bytes.Buffer
	cmd := newTestRootCmd(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)

	c.Info(cmd)

	if !strings.Contains(out.String(), "OpenRC managed") {
		t.Errorf("expected 'OpenRC managed', got: %s", out.String())
	}
	if !strings.Contains(errBuf.String(), "rc-service eos status") {
		t.Errorf("expected 'rc-service eos status' hint, got: %s", errBuf.String())
	}
}

func TestOpenRCDaemonController_LogsHint(t *testing.T) {
	c := openrcDaemonController{cfg: config.OpenRCConfig{InitFileName: "eos"}}
	if got := c.LogsHint(); got != "eos daemon logs" {
		t.Errorf("expected 'eos daemon logs', got %q", got)
	}
}
