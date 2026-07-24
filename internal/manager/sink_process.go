package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

const (
	sinkDefaultBufferSize     = 4096
	sinkDefaultRestartDelayMs = 5000
	sinkReadyTimeout          = 10 * time.Second
	sinkShutdownTimeout       = 3 * time.Second
)

// sinkProcess manages a single log sink plugin subprocess.
// It owns the plugin's lifecycle: launch, READY handshake, record delivery, and restart on crash.
type sinkProcess struct {
	logger   *slog.Logger
	errLog   *slog.Logger
	buf      *ringBuffer
	stopCh   chan struct{}
	doneCh   chan struct{}
	service  string
	sink     types.LogSink
	stopOnce sync.Once
}

type sinkRecord struct {
	line   string
	stream string
}

func newSinkProcess(sink *types.LogSink, serviceName string, logger *slog.Logger, errLog *slog.Logger) *sinkProcess {
	bufSize := sink.BufferSize
	if bufSize <= 0 {
		bufSize = sinkDefaultBufferSize
	}
	return &sinkProcess{
		sink:    *sink,
		service: serviceName,
		logger:  logger,
		errLog:  errLog,
		buf:     newRingBuffer(bufSize),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Send enqueues a log record. Called from the fan-out scanner goroutine. Non-blocking.
func (s *sinkProcess) Send(line, stream string) {
	s.buf.push(sinkRecord{line: line, stream: stream})
}

// Run starts the sink supervisor loop. Blocks until Stop is called.
// Must be called in its own goroutine.
func (s *sinkProcess) Run(ctx context.Context) {
	if s.sink.Mode == "" || s.sink.Address == "" {
		s.logger.Error("sink config invalid: mode and address are required", "type", s.sink.Type)
		close(s.doneCh)
		return
	}
	defer func() {
		if dropped := s.buf.Dropped(); dropped > 0 {
			s.logger.Warn("sink buffer dropped records", "sink", s.sink.Type, "dropped", dropped)
		}
		close(s.doneCh)
	}()

	restartDelayMs := s.sink.RestartDelayMs
	if restartDelayMs <= 0 {
		restartDelayMs = sinkDefaultRestartDelayMs
	}

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		if err := s.runOnce(ctx); err != nil {
			s.logger.Warn(fmt.Sprintf("sink plugin exited (%s/%s)", s.sink.Type, s.service),
				"error", err,
				"restart_in_ms", restartDelayMs,
			)
		}

		t := time.NewTimer(time.Duration(restartDelayMs) * time.Millisecond)
		select {
		case <-s.stopCh:
			t.Stop()
			return
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// Stop signals the supervisor loop to exit and waits for it to finish.
func (s *sinkProcess) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	<-s.doneCh
}

// runOnce launches the plugin binary, performs the READY handshake, drains the ring buffer
// into the plugin's stdin, and returns when the plugin exits or stop is signaled.
func (s *sinkProcess) runOnce(ctx context.Context) error {
	binaryPath, err := s.resolveBinary()
	if err != nil {
		return fmt.Errorf("resolving binary: %w", err)
	}

	optionsEnv, err := buildOptionsEnv(s.sink.Options)
	if err != nil {
		return fmt.Errorf("encoding options: %w", err)
	}

	cmd := exec.CommandContext(ctx, binaryPath, s.sink.Args...) // #nosec G204 -- path validated at config load
	cmd.Env = append(os.Environ(), optionsEnv,
		"EOS_SINK_SERVICE="+s.service,
		"EOS_SINK_TYPE="+s.sink.Type,
		"EOS_SINK_ADDRESS="+s.sink.Address,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting plugin: %w", err)
	}

	// Drain stderr to daemon logger and service error log in background.
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			msg := sc.Text()
			s.logger.Warn("sink plugin stderr", "sink", s.sink.Type, "service", s.service, "msg", msg)
			if s.errLog != nil {
				s.errLog.Warn(msg, "source", "sink:"+s.sink.Type)
			}
		}
	}()

	// Wait for READY on stdout before sending any records.
	readyCh := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdoutPipe)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "READY" {
				readyCh <- nil
				// Keep reading stdout (ACK lines etc.) but discard — not used yet.
				for sc.Scan() {
				}
				return
			}
		}
		readyCh <- fmt.Errorf("plugin exited without sending READY")
	}()

	readyTimer := time.NewTimer(sinkReadyTimeout)
	defer readyTimer.Stop()
	select {
	case err := <-readyCh:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	case <-readyTimer.C:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("timed out waiting for READY from plugin %q", s.sink.Type)
	case <-s.stopCh:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil
	case <-ctx.Done():
		// exec.CommandContext kills the subprocess automatically on ctx cancel.
		_ = cmd.Wait()
		return nil
	}

	s.logger.Info("sink:"+s.sink.Type+" ready", "address", s.sink.Address, "service", s.service)

	// Pump records from the ring buffer into plugin stdin.
	// On stop/ctx cancel we flush remaining buffered records first, then close stdin.
	writer := bufio.NewWriter(stdin)
	pumpErr := s.pump(ctx, writer)

	// Flush writer buffer and close stdin to signal EOF to the plugin.
	_ = writer.Flush()
	_ = stdin.Close()

	// Wait for plugin to exit with a timeout.
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()
	shutdownTimer := time.NewTimer(sinkShutdownTimeout)
	defer shutdownTimer.Stop()
	select {
	case exitErr := <-exitCh:
		if exitErr != nil && pumpErr == nil {
			return exitErr
		}
	case <-shutdownTimer.C:
		s.logger.Warn("sink plugin did not exit in time, killing", "sink", s.sink.Type)
		_ = cmd.Process.Kill()
		<-exitCh
	}

	return pumpErr
}

// pump reads from the ring buffer and writes NDJSON lines to the plugin stdin.
// Returns when stop is signaled or ctx is canceled.
func (s *sinkProcess) pump(ctx context.Context, w *bufio.Writer) error {
	for {
		r, ok := s.buf.pop()
		if !ok {
			// Buffer empty — check stop signals before blocking.
			select {
			case <-s.stopCh:
				// Flush remaining buffer before returning.
				for {
					rec, ok := s.buf.pop()
					if !ok {
						return nil
					}
					if err := writeRecord(w, rec, s.service); err != nil {
						return err
					}
				}
			case <-ctx.Done():
				// subprocess killed by exec.CommandContext; no point flushing.
				return nil
			default:
				// No records yet; yield briefly.
				time.Sleep(time.Millisecond)
				continue
			}
		}
		if err := writeRecord(w, r, s.service); err != nil {
			return fmt.Errorf("writing record to plugin stdin: %w", err)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("flushing record to plugin stdin: %w", err)
		}
	}
}

func writeRecord(w *bufio.Writer, r sinkRecord, service string) error {
	rec := map[string]string{
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"service": service,
		"stream":  r.stream,
		"msg":     r.line,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	return w.WriteByte('\n')
}

func (s *sinkProcess) resolveBinary() (string, error) {
	if s.sink.Exec != "" {
		return s.sink.Exec, nil
	}
	name := "eos-sink-" + s.sink.Type
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%q not found on PATH; install a sink plugin from https://github.com/Elysium-Labs-EU/eos-plugins", name)
	}
	return path, nil
}

// startSinkProcesses creates and starts a sinkProcess for each configured sink.
// errLog is the service error log logger; sink plugin stderr is written there in addition to the daemon logger.
func startSinkProcesses(ctx context.Context, sinkConfigs []types.LogSink, serviceName string, logger *slog.Logger, errLog *slog.Logger) []*sinkProcess {
	procs := make([]*sinkProcess, 0, len(sinkConfigs))
	for i := range sinkConfigs {
		sp := newSinkProcess(&sinkConfigs[i], serviceName, logger, errLog)
		go sp.Run(ctx)
		procs = append(procs, sp)
	}
	return procs
}

// stopSinkProcesses stops all sink processes.
func stopSinkProcesses(sinks []*sinkProcess) {
	for _, s := range sinks {
		s.Stop()
	}
}

// sinkWantsStream reports whether the sink should receive records from the given stream.
// An empty Streams list means "all streams".
func sinkWantsStream(s *sinkProcess, stream string) bool {
	if len(s.sink.Streams) == 0 {
		return true
	}
	return slices.Contains(s.sink.Streams, stream)
}

// buildOptionsEnv JSON-encodes the options map (with ${VAR} expansion on string values)
// and returns the EOS_SINK_OPTIONS=<json> env string.
func buildOptionsEnv(options map[string]any) (string, error) {
	if len(options) == 0 {
		return "EOS_SINK_OPTIONS={}", nil
	}
	expanded := make(map[string]any, len(options))
	for k, v := range options {
		if s, ok := v.(string); ok {
			expanded[k] = os.ExpandEnv(s)
		} else {
			expanded[k] = v
		}
	}
	b, err := json.Marshal(expanded)
	if err != nil {
		return "", err
	}
	return "EOS_SINK_OPTIONS=" + string(b), nil
}
