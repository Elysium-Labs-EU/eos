package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAPIDaemonRemove_ControllerError(t *testing.T) {
	cmd := newAPIDaemonRemoveCmdWithController(func() (DaemonController, error) {
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

func TestAPIDaemonRemove_RemoveError(t *testing.T) {
	fake := &fakeAPIDaemonController{removeErr: errors.New("daemon is running")}
	cmd := newAPIDaemonRemoveCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBuf.String(), "daemon is running") {
		t.Errorf("expected remove error in output, got: %s", errBuf.String())
	}
}

func TestAPIDaemonRemove_Success(t *testing.T) {
	fake := &fakeAPIDaemonController{}
	cmd := newAPIDaemonRemoveCmdWithController(func() (DaemonController, error) { return fake, nil })
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result apiDaemonRemoveResult
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON output, got: %s (%v)", outBuf.String(), err)
	}
	if !result.Removed {
		t.Error("expected removed=true")
	}
}
