package logutil

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTextLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTextLogger(&buf, false)
	if logger == nil {
		t.Fatal("expected non-nil logger")
		return
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
		return
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

	// A single Write call with two newline-terminated lines should flush both
	// lines separately, each with its own timestamp, not one timestamp for the whole call.
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

// jsonLine builds one JSON log line in the shape NewJSONLogger/slog's JSON
// handler produces, with the given msg and source field.
func jsonLine(msg, source string) string {
	return fmt.Sprintf(`{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":%q,"source":%q}`, msg, source)
}

func TestLastLogMessage(t *testing.T) {
	tests := []struct {
		name     string
		content  string // "" and missing file are handled via notFile below
		wantLine string
		notFile  bool // if true, don't create the file at all
		wantOK   bool
	}{
		{
			name:    "missing file",
			notFile: true,
			wantOK:  false,
		},
		{
			name:    "empty file",
			content: "",
			wantOK:  false,
		},
		{
			name:    "only blank lines",
			content: "\n\n   \n\n",
			wantOK:  false,
		},
		{
			name:    "only malformed JSON lines",
			content: "not json at all\n{broken\n" + "\n",
			wantOK:  false,
		},
		{
			name:     "single genuine stderr line",
			content:  jsonLine("bind: Address already in use", "stderr") + "\n",
			wantLine: "bind: Address already in use",
			wantOK:   true,
		},
		{
			name:    "only a health breadcrumb line is skipped, not returned",
			content: jsonLine("[svc] restarting", "health") + "\n",
			wantOK:  false,
		},
		{
			// This is the exact scenario the health monitor hits: its own
			// breadcrumb was written most recently (last line), but the
			// genuine child-stderr line from an earlier write must still be
			// the one returned, not the breadcrumb.
			name: "health breadcrumb after a genuine line skips the breadcrumb and returns the genuine line",
			content: jsonLine("exec: runtime not found", "stderr") + "\n" +
				jsonLine("[svc] restart failed: exec: runtime not found (exec: runtime not found)", "health") + "\n",
			wantLine: "exec: runtime not found",
			wantOK:   true,
		},
		{
			name: "multiple genuine lines returns the most recent, not the first",
			content: jsonLine("first failure", "stderr") + "\n" +
				jsonLine("second failure", "stderr") + "\n" +
				jsonLine("third failure", "stderr") + "\n",
			wantLine: "third failure",
			wantOK:   true,
		},
		{
			name:    "empty msg field is skipped",
			content: `{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"","source":"stderr"}` + "\n",
			wantOK:  false,
		},
		{
			name: "malformed and empty lines interleaved with a genuine line are skipped",
			content: "garbage\n\n" +
				jsonLine("real cause", "stderr") + "\n" +
				"\nmore garbage\n",
			wantLine: "real cause",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.notFile {
				path = filepath.Join(t.TempDir(), "does-not-exist.log")
			} else {
				path = filepath.Join(t.TempDir(), "test.log")
				if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
					t.Fatalf("writing test log file: %v", err)
				}
			}

			line, ok := LastLogMessage(path)
			if ok != tt.wantOK {
				t.Fatalf("LastLogMessage() ok = %v, want %v (line=%q)", ok, tt.wantOK, line)
			}
			if ok && line != tt.wantLine {
				t.Errorf("LastLogMessage() = %q, want %q", line, tt.wantLine)
			}
		})
	}
}
