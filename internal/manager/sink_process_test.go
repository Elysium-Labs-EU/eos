package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
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

// TestBuildOptionsEnv_varExpansion verifies ${VAR} in string option values is expanded
// against the process environment before being JSON-encoded.
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
		Type:    "test",
		Mode:    "push",
		Address: "http://localhost",
		Exec:    "sh",
		// sh script: print READY, then drain stdin until EOF
		Args: []string{"-c", "echo READY; while IFS= read -r line; do true; done"},
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t), nil)

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
	// Plugin that never prints READY. The 500ms ctx timeout fires well before
	// sinkReadyTimeout (10s), so this exercises the ctx.Done() branch of the
	// READY select in runOnce, not the readyTimer.C branch.
	sink := &types.LogSink{
		Type:           "test",
		Mode:           "push",
		Address:        "http://localhost",
		Exec:           "sh",
		Args:           []string{"-c", "sleep 30"},
		RestartDelayMs: 100,
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run exits when ctx is canceled — just verify it doesn't panic.
	sp.Run(ctx)
}

// Tier 1: defaults applied when config values are zero.
func TestSinkProcess_defaultBufferSize(t *testing.T) {
	sink := &types.LogSink{Type: "test", BufferSize: 0}
	sp := newSinkProcess(sink, "svc", newTestLogger(t), nil)
	if sp.buf.cap != sinkDefaultBufferSize {
		t.Errorf("expected default buffer size %d, got %d", sinkDefaultBufferSize, sp.buf.cap)
	}
}

func TestSinkProcess_customBufferSize(t *testing.T) {
	sink := &types.LogSink{Type: "test", BufferSize: 128}
	sp := newSinkProcess(sink, "svc", newTestLogger(t), nil)
	if sp.buf.cap != 128 {
		t.Errorf("expected buffer size 128, got %d", sp.buf.cap)
	}
}

// Tier 1: plugin exits immediately after READY — Run should loop without panicking.
func TestSinkProcess_pluginExitAfterReady(t *testing.T) {
	sink := &types.LogSink{
		Type:           "test",
		Mode:           "push",
		Address:        "http://localhost",
		Exec:           "sh",
		Args:           []string{"-c", "echo READY"},
		RestartDelayMs: 50,
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run exits when ctx expires; no panic means the restart loop works.
	sp.Run(ctx)
}

// Tier 2: records actually reach plugin stdin as valid NDJSON.
func TestSinkProcess_recordsDelivered(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "received.ndjson")
	sink := &types.LogSink{
		Type:    "test",
		Mode:    "push",
		Address: "http://localhost",
		Exec:    "sh",
		Args:    []string{"-c", `echo READY; while IFS= read -r line; do echo "$line" >> ` + outFile + `; done`},
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go sp.Run(ctx)

	// Wait for plugin to start and send READY.
	time.Sleep(200 * time.Millisecond)

	sp.Send("hello", "stdout")
	sp.Send("world", "stderr")

	// Wait for pump to flush, then stop cleanly.
	time.Sleep(200 * time.Millisecond)
	sp.Stop()

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"hello"`) {
		t.Errorf("expected 'hello' in delivered NDJSON, got: %q", content)
	}
	if !strings.Contains(content, `"world"`) {
		t.Errorf("expected 'world' in delivered NDJSON, got: %q", content)
	}
	if !strings.Contains(content, `"testsvc"`) {
		t.Errorf("expected service name in delivered NDJSON, got: %q", content)
	}
}

// Tier 2: errLog receives sink stderr when provided.
func TestSinkProcess_stderrRoutedToErrLog(t *testing.T) {
	var mu strings.Builder
	handler := slog.NewTextHandler(&mu, &slog.HandlerOptions{Level: slog.LevelDebug})
	errLog := slog.New(handler)

	sink := &types.LogSink{
		Type:    "test",
		Mode:    "push",
		Address: "http://localhost",
		Exec:    "sh",
		// Print a known message to stderr before signaling READY.
		Args: []string{"-c", `echo "sink stderr message" >&2; echo READY; while IFS= read -r _; do true; done`},
	}
	sp := newSinkProcess(sink, "testsvc", newTestLogger(t), errLog)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go sp.Run(ctx)
	time.Sleep(300 * time.Millisecond)
	sp.Stop()

	if !strings.Contains(mu.String(), "sink stderr message") {
		t.Errorf("expected errLog to receive sink stderr, got: %q", mu.String())
	}
}

// TestSinkProcess_invalidConfig_stopDoesNotDeadlock covers the Run() early-return path
// that closes doneCh; without the fix Stop() would deadlock here.
func TestSinkProcess_invalidConfig_stopDoesNotDeadlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		sink types.LogSink
	}{
		{"missing mode", types.LogSink{Type: "test", Address: "http://localhost"}},
		{"missing address", types.LogSink{Type: "test", Mode: "push"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sp := newSinkProcess(&tc.sink, "svc", newTestLogger(t), nil)
			go sp.Run(context.Background())

			done := make(chan struct{})
			go func() { sp.Stop(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Stop() deadlocked after invalid-config Run()")
			}
		})
	}
}

// TestSinkProcess_stopDuringReadyHandshake covers the stopCh branch in runOnce's READY
// select; previously cmd.Wait() could hang if the plugin ignored stdin EOF.
func TestSinkProcess_stopDuringReadyHandshake(t *testing.T) {
	sink := &types.LogSink{
		Type:    "test",
		Mode:    "push",
		Address: "http://localhost",
		Exec:    "sh",
		Args:    []string{"-c", "sleep 30"},
	}
	sp := newSinkProcess(sink, "svc", newTestLogger(t), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go sp.Run(ctx)

	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() { sp.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() deadlocked while waiting for READY")
	}
}

// TestSinkProcess_pluginExitsWithoutReady covers the readyCh error path in runOnce.
func TestSinkProcess_pluginExitsWithoutReady(t *testing.T) {
	sink := &types.LogSink{
		Type:           "test",
		Mode:           "push",
		Address:        "http://localhost",
		Exec:           "sh",
		Args:           []string{"-c", "exit 1"},
		RestartDelayMs: 50,
	}
	sp := newSinkProcess(sink, "svc", newTestLogger(t), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	sp.Run(ctx)
}

// TestStartStopSinkProcesses covers startSinkProcesses and stopSinkProcesses.
func TestStartStopSinkProcesses(t *testing.T) {
	sinks := []types.LogSink{
		{
			Type:    "test",
			Mode:    "push",
			Address: "http://localhost",
			Exec:    "sh",
			Args:    []string{"-c", "echo READY; while IFS= read -r _; do true; done"},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	procs := startSinkProcesses(ctx, sinks, "svc", newTestLogger(t), nil)
	time.Sleep(200 * time.Millisecond)
	stopSinkProcesses(procs)
}

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}
