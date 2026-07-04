package helpers

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func newTestCmd(outBuf, errBuf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	return cmd
}

func TestWriteJSON(t *testing.T) {
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd := newTestCmd(out, errBuf)

	if err := WriteJSON(cmd, map[string]string{"key": "val"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if got == "" {
		t.Error("expected JSON output, got empty")
	}
	if got[0] != '{' {
		t.Errorf("expected JSON object, got: %q", got)
	}
}

func TestWriteJSONErr(t *testing.T) {
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd := newTestCmd(out, errBuf)

	err := WriteJSONErr(cmd, errors.New("something broke"))
	if !errors.Is(err, ErrAPICommandFailed) {
		t.Errorf("expected ErrAPICommandFailed, got %v", err)
	}
	if errBuf.Len() == 0 {
		t.Error("expected error written to stderr, got nothing")
	}
}
