package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/types"
)

func TestBuildOptionsEnv_empty(t *testing.T) {
	env, err := buildOptionsEnv(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != "EOS_SINK_OPTIONS={}" {
		t.Errorf("expected empty JSON object, got %q", env)
	}
}

func TestBuildOptionsEnv_stringValues(t *testing.T) {
	opts := map[string]any{"key": "value", "num": 42}
	env, err := buildOptionsEnv(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(env, "EOS_SINK_OPTIONS=") {
		t.Errorf("expected EOS_SINK_OPTIONS= prefix, got %q", env)
	}
	jsonPart := strings.TrimPrefix(env, "EOS_SINK_OPTIONS=")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		t.Errorf("result is not valid JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("expected key=value, got %v", parsed["key"])
	}
}

func TestBuildOptionsEnv_varExpansion(t *testing.T) {
	t.Setenv("TEST_SINK_TOKEN", "secret123")
	opts := map[string]any{"token": "${TEST_SINK_TOKEN}"}
	env, err := buildOptionsEnv(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(env, "secret123") {
		t.Errorf("expected expanded value in env, got %q", env)
	}
	if strings.Contains(env, "${TEST_SINK_TOKEN}") {
		t.Errorf("expected variable to be expanded, but literal remains in %q", env)
	}
}

func TestSinkWantsStream_emptyMeansAll(t *testing.T) {
	sp := &sinkProcess{sink: types.LogSink{Streams: nil}}
	if !sinkWantsStream(sp, "stdout") {
		t.Error("empty Streams should accept stdout")
	}
	if !sinkWantsStream(sp, "stderr") {
		t.Error("empty Streams should accept stderr")
	}
}

func TestSinkWantsStream_filtered(t *testing.T) {
	sp := &sinkProcess{sink: types.LogSink{Streams: []string{"stdout"}}}
	if !sinkWantsStream(sp, "stdout") {
		t.Error("should accept stdout when listed")
	}
	if sinkWantsStream(sp, "stderr") {
		t.Error("should not accept stderr when not listed")
	}
}

func TestSinkWantsStream_both(t *testing.T) {
	sp := &sinkProcess{sink: types.LogSink{Streams: []string{"stdout", "stderr"}}}
	if !sinkWantsStream(sp, "stdout") || !sinkWantsStream(sp, "stderr") {
		t.Error("should accept both streams when both listed")
	}
}

func TestWriteRecord_validJSON(t *testing.T) {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	r := sinkRecord{line: "hello world", stream: "stdout"}
	if err := writeRecord(bw, r, "myservice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}
	line := strings.TrimSpace(sb.String())
	var parsed map[string]string
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v — got: %q", err, line)
	}
	if parsed["msg"] != "hello world" {
		t.Errorf("expected msg='hello world', got %q", parsed["msg"])
	}
	if parsed["service"] != "myservice" {
		t.Errorf("expected service='myservice', got %q", parsed["service"])
	}
	if parsed["stream"] != "stdout" {
		t.Errorf("expected stream='stdout', got %q", parsed["stream"])
	}
	if parsed["ts"] == "" {
		t.Error("expected ts field to be set")
	}
}

func TestResolveBinary_execOverride(t *testing.T) {
	sp := &sinkProcess{sink: types.LogSink{Type: "test", Exec: "sh"}}
	path, err := sp.resolveBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "sh" {
		t.Errorf("expected 'sh', got %q", path)
	}
}

func TestResolveBinary_notFound(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", origPath) }()

	sp := &sinkProcess{sink: types.LogSink{Type: "nonexistent"}}
	_, err := sp.resolveBinary()
	if err == nil {
		t.Error("expected error when binary not on PATH")
	}
	if !strings.Contains(err.Error(), "eos-sink-nonexistent") {
		t.Errorf("expected binary name in error, got: %v", err)
	}
}

func TestSinkProcess_runAndStop(t *testing.T) {
	sink := &types.LogSink{
		Type: "test",
		Exec: "sh",
		// sh script: print READY, then drain stdin until EOF
		Args: []string{"-c", "echo READY; while IFS= read -r line; do true; done"},
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go sp.Run(ctx)

	// Send a record and then stop cleanly.
	sp.Send("hello", "stdout")
	sp.Send("world", "stderr")

	// Give the plugin a moment to start and process.
	time.Sleep(100 * time.Millisecond)
	sp.Stop()
}

func TestSinkProcess_readyTimeout(t *testing.T) {
	// Plugin that never prints READY — should time out and restart.
	// We cancel the context quickly so the test doesn't wait the full restart delay.
	sink := &types.LogSink{
		Type:           "test",
		Exec:           "sh",
		Args:           []string{"-c", "sleep 30"},
		RestartDelayMs: 100,
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run exits when ctx is canceled — just verify it doesn't panic.
	sp.Run(ctx)
}

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}
