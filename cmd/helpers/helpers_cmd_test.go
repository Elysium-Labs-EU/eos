package helpers

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptConfirm(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"yes short", "y\n", true},
		{"yes long", "yes\n", true},
		{"no", "n\n", false},
		{"garbage", "maybe\n", false},
		{"no trailing newline still processed", "y", true},
		{"empty input", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			errBuf := &bytes.Buffer{}
			cmd := newTestCmd(out, errBuf)
			cmd.SetIn(strings.NewReader(tt.input))

			got := PromptConfirm(cmd, "confirm?")
			if got != tt.want {
				t.Errorf("PromptConfirm(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if !strings.Contains(out.String(), "confirm?") {
				t.Errorf("expected prompt in output, got: %q", out.String())
			}
		})
	}

	t.Run("empty input prints read error", func(t *testing.T) {
		out := &bytes.Buffer{}
		errBuf := &bytes.Buffer{}
		cmd := newTestCmd(out, errBuf)
		cmd.SetIn(strings.NewReader(""))

		PromptConfirm(cmd, "confirm?")

		if errBuf.Len() == 0 {
			t.Error("expected error message on stderr for empty input, got nothing")
		}
	})
}

func TestDebugf(t *testing.T) {
	t.Run("verbose true prints", func(t *testing.T) {
		errBuf := &bytes.Buffer{}
		cmd := newTestCmd(&bytes.Buffer{}, errBuf)

		Debugf(cmd, true, "value is %d", 42)

		got := errBuf.String()
		if !strings.Contains(got, "debug") || !strings.Contains(got, "value is 42") {
			t.Errorf("expected debug message in output, got: %q", got)
		}
	})

	t.Run("verbose false is silent", func(t *testing.T) {
		errBuf := &bytes.Buffer{}
		cmd := newTestCmd(&bytes.Buffer{}, errBuf)

		Debugf(cmd, false, "value is %d", 42)

		if errBuf.Len() != 0 {
			t.Errorf("expected no output when verbose is false, got: %q", errBuf.String())
		}
	})
}

func TestPrintSudoHint(t *testing.T) {
	errBuf := &bytes.Buffer{}
	cmd := newTestCmd(&bytes.Buffer{}, errBuf)

	PrintSudoHint(cmd)

	got := errBuf.String()
	if !strings.Contains(got, "sudo") {
		t.Errorf("expected sudo hint in output, got: %q", got)
	}
}

func TestPrintRequiresSudo(t *testing.T) {
	errBuf := &bytes.Buffer{}
	cmd := newTestCmd(&bytes.Buffer{}, errBuf)

	PrintRequiresSudo(cmd, "removing the daemon")

	got := errBuf.String()
	if !strings.Contains(got, "removing the daemon requires root") {
		t.Errorf("expected action requires root message, got: %q", got)
	}
	if !strings.Contains(got, "sudo") {
		t.Errorf("expected sudo hint chained in output, got: %q", got)
	}
}
