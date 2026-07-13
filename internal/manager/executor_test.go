package manager

import (
	"context"
	"testing"
)

func TestOsExecutor_LookPath(t *testing.T) {
	var e osExecutor
	if _, err := e.LookPath("sh"); err != nil {
		t.Errorf("expected 'sh' to be found in PATH, got: %v", err)
	}
	if _, err := e.LookPath("definitely-not-a-real-binary-xyz"); err == nil {
		t.Error("expected error for non-existent binary")
	}
}

func TestOsExecutor_CommandContext(t *testing.T) {
	var e osExecutor
	cmd := e.CommandContext(context.Background(), "echo", "hi")
	if cmd == nil {
		t.Fatal("expected non-nil *exec.Cmd")
	}
	if cmd.Args[0] != "echo" || cmd.Args[1] != "hi" {
		t.Errorf("expected args [echo hi], got: %v", cmd.Args)
	}
}
