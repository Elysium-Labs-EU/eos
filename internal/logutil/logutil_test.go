package logutil

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTextLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, false)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	logger.Info("hello")
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("expected 'hello' in output, got: %q", buf.String())
	}
}

func TestNewTextLogger_verbose(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, true)
	logger.Debug("debug msg")
	if !strings.Contains(buf.String(), "debug msg") {
		t.Errorf("verbose logger should emit debug; got: %q", buf.String())
	}
}

func TestNewJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, false)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	logger.Info("json test")
	out := buf.String()
	if !strings.Contains(out, `"msg"`) || !strings.Contains(out, "json test") {
		t.Errorf("expected JSON output with msg field, got: %q", out)
	}
}

func TestNewJSONLogger_verbose(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(&buf, true)
	logger.Debug("debug json")
	if !strings.Contains(buf.String(), "debug json") {
		t.Errorf("verbose JSON logger should emit debug; got: %q", buf.String())
	}
}

func TestSlogLevel(t *testing.T) {
	if slogLevel(false) != slog.LevelInfo {
		t.Errorf("non-verbose should return LevelInfo")
	}
	if slogLevel(true) != slog.LevelDebug {
		t.Errorf("verbose should return LevelDebug")
	}
}

func TestTimestampWriter_Write(t *testing.T) {
	var buf bytes.Buffer
	tw := &TimestampWriter{W: &buf}

	n, err := tw.Write([]byte("line one\nline two\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 18 {
		t.Errorf("expected n=18, got %d", n)
	}

	out := buf.String()
	if !strings.Contains(out, "line one") || !strings.Contains(out, "line two") {
		t.Errorf("expected both lines in output, got: %q", out)
	}
	if !strings.Contains(out, "T") {
		t.Errorf("expected ISO timestamp in output, got: %q", out)
	}
}

func TestTimestampWriter_Write_partial(t *testing.T) {
	var buf bytes.Buffer
	tw := &TimestampWriter{W: &buf}

	_, _ = tw.Write([]byte("partial"))
	if buf.Len() != 0 {
		t.Error("partial line (no newline) should not be flushed yet")
	}

	_, _ = tw.Write([]byte(" line\n"))
	if !strings.Contains(buf.String(), "partial line") {
		t.Errorf("expected buffered line to flush after newline, got: %q", buf.String())
	}
}
