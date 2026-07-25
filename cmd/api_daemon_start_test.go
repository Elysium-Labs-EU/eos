package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeAPIDaemonController is a configurable DaemonController double shared by
// the api_daemon_start/stop/remove tests, distinct from daemon_test.go's
// fakeDaemonController which only supports Start.
type fakeAPIDaemonController struct {
	startErr  error
	stopErr   error
	removeErr error
	stopped   bool
}

func (f *fakeAPIDaemonController) Start(_ context.Context, _ bool, _ bool, _ bool) error {
	return f.startErr
}

func (f *fakeAPIDaemonController) Stop(_ context.Context, _ *cobra.Command, _ bool) (bool, error) {
	return f.stopped, f.stopErr
}

func (f *fakeAPIDaemonController) Remove() error                        { return f.removeErr }
func (f *fakeAPIDaemonController) Info(_ *cobra.Command)                {}
func (f *fakeAPIDaemonController) IsRunning(_ context.Context) bool     { return !f.stopped }
func (f *fakeAPIDaemonController) Logs(_ *cobra.Command, _ int, _ bool) {}
func (f *fakeAPIDaemonController) LogsHint() string                     { return "" }

func TestAPIDaemonStart_ControllerError(t *testing.T) {
	cmd := newAPIDaemonStartCmdWithController(func() (DaemonController, error) {
		return nil, errors.New("resolving controller failed")
	})
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "resolving controller failed") {
		t.Errorf("expected controller error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonStart_StartError(t *testing.T) {
	// stopped:true -> IsRunning() reports false, so the new idempotency
	// pre-check falls through to Start(), which fails for some other reason
	// (e.g. fork/permission failure), not because it's already running.
	fake := &fakeAPIDaemonController{stopped: true, startErr: errors.New("boom")}
	cmd := newAPIDaemonStartCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "boom") {
		t.Errorf("expected start error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonStart_Success(t *testing.T) {
	fake := &fakeAPIDaemonController{stopped: true}
	cmd := newAPIDaemonStartCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonStartResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), err)
	}
	if !result.Started {
		t.Error("expected started=true")
	}
}

// TestAPIDaemonStart_AlreadyRunning mirrors TestAPIDaemonStop_NotRunning:
// calling start on an already-running daemon is idempotent, exits 0, and
// reports started=false instead of erroring (issue #68).
func TestAPIDaemonStart_AlreadyRunning(t *testing.T) {
	fake := &fakeAPIDaemonController{stopped: false}
	cmd := newAPIDaemonStartCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonStartResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), err)
	}
	if result.Started {
		t.Error("expected started=false")
	}
}
