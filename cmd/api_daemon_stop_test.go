package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAPIDaemonStop_ControllerError(t *testing.T) {
	cmd := newAPIDaemonStopCmdWithController(func() (DaemonController, error) {
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

func TestAPIDaemonStop_StopError(t *testing.T) {
	fake := &fakeAPIDaemonController{stopErr: errors.New("signal failed")}
	cmd := newAPIDaemonStopCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "signal failed") {
		t.Errorf("expected stop error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonStop_NotRunning(t *testing.T) {
	fake := &fakeAPIDaemonController{stopped: false}
	cmd := newAPIDaemonStopCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonStopResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), err)
	}
	if result.Stopped {
		t.Error("expected stopped=false")
	}
}

func TestAPIDaemonStop_Success(t *testing.T) {
	fake := &fakeAPIDaemonController{stopped: true}
	cmd := newAPIDaemonStopCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonStopResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), err)
	}
	if !result.Stopped {
		t.Error("expected stopped=true")
	}
}
