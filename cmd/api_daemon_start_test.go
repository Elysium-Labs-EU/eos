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
	fake := &fakeAPIDaemonController{startErr: errors.New("already running")}
	cmd := newAPIDaemonStartCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "already running") {
		t.Errorf("expected start error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonStart_Success(t *testing.T) {
	fake := &fakeAPIDaemonController{}
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
