package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	if !sinkConfigValid(&s.sink) {
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

	restartDelayMs := sinkRestartDelayMs(&s.sink)

	for {
		if s.sinkStopRequested(ctx) {
			return
		}

		if err := s.runOnce(ctx); err != nil {
			s.logger.Warn(fmt.Sprintf("sink plugin exited (%s/%s)", s.sink.Type, s.service),
				"error", err,
				"restart_in_ms", restartDelayMs,
			)
		}

		if s.sinkWaitBeforeRestart(ctx, restartDelayMs) {
			return
		}
	}
}

// sinkConfigValid reports whether the sink config has the fields required to start a plugin.
func sinkConfigValid(sink *types.LogSink) bool {
	return sink.Mode != "" && sink.Address != ""
}

// sinkRestartDelayMs resolves the configured restart delay, applying the package default when unset.
func sinkRestartDelayMs(sink *types.LogSink) int {
	if sink.RestartDelayMs <= 0 {
		return sinkDefaultRestartDelayMs
	}
	return sink.RestartDelayMs
}

// sinkStopRequested reports whether stop or ctx cancellation has already fired, without blocking.
func (s *sinkProcess) sinkStopRequested(ctx context.Context) bool {
	select {
	case <-s.stopCh:
		return true
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// sinkWaitBeforeRestart blocks for the restart delay, returning true if stop or ctx
// cancellation fired first (caller should exit rather than restart).
func (s *sinkProcess) sinkWaitBeforeRestart(ctx context.Context, delayMs int) bool {
	t := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
	defer t.Stop()
	select {
	case <-s.stopCh:
		return true
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
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
	cmd, stdin, stdoutPipe, stderrPipe, err := s.sinkStartPlugin(ctx)
	if err != nil {
		return err
	}

	go s.sinkDrainStderr(stderrPipe)

	ready, err := s.sinkAwaitReady(ctx, cmd, stdoutPipe)
	if !ready {
		return err
	}

	s.logger.Info("sink:"+s.sink.Type+" ready", "address", s.sink.Address, "service", s.service)

	// Pump records from the ring buffer into plugin stdin.
	// On stop/ctx cancel we flush remaining buffered records first, then close stdin.
	writer := bufio.NewWriter(stdin)
	pumpErr := s.pump(ctx, writer)

	// Flush writer buffer and close stdin to signal EOF to the plugin.
	_ = writer.Flush()
	_ = stdin.Close()

	return s.sinkAwaitExit(cmd, pumpErr)
}

// sinkStartPlugin resolves the plugin binary, builds its environment, wires up its
// stdio pipes, and starts it. Callers own the returned pipes and must not use them
// after the process has exited.
func (s *sinkProcess) sinkStartPlugin(ctx context.Context) (cmd *exec.Cmd, stdin io.WriteCloser, stdout, stderr io.ReadCloser, err error) {
	binaryPath, err := s.resolveBinary()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("resolving binary: %w", err)
	}

	optionsEnv, err := buildOptionsEnv(s.sink.Options)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("encoding options: %w", err)
	}

	cmd = exec.CommandContext(ctx, binaryPath, s.sink.Args...) // #nosec G204 -- path validated at config load
	cmd.Env = append(os.Environ(), optionsEnv,
		"EOS_SINK_SERVICE="+s.service,
		"EOS_SINK_TYPE="+s.sink.Type,
		"EOS_SINK_ADDRESS="+s.sink.Address,
	)

	stdin, err = cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err = cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("starting plugin: %w", err)
	}

	return cmd, stdin, stdout, stderr, nil
}

// sinkDrainStderr forwards the plugin's stderr lines to the daemon logger and, if set,
// the service error log. Runs in its own goroutine for the lifetime of the plugin process.
func (s *sinkProcess) sinkDrainStderr(stderr io.Reader) {
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		msg := sc.Text()
		s.logger.Warn("sink plugin stderr", "sink", s.sink.Type, "service", s.service, "msg", msg)
		if s.errLog != nil {
			s.errLog.Warn(msg, "source", "sink:"+s.sink.Type)
		}
	}
}

// sinkAwaitReady waits for the plugin to send "READY" on stdout, or for it to exit,
// time out, or stop/ctx to fire first. ready is true only when READY was received in
// time; on every other branch the process has already been killed and waited on, and
// err (possibly nil) is the runOnce result the caller should return immediately.
func (s *sinkProcess) sinkAwaitReady(ctx context.Context, cmd *exec.Cmd, stdout io.Reader) (ready bool, err error) {
	readyCh := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
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
			return false, err
		}
		return true, nil
	case <-readyTimer.C:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false, fmt.Errorf("timed out waiting for READY from plugin %q", s.sink.Type)
	case <-s.stopCh:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false, nil
	case <-ctx.Done():
		// exec.CommandContext kills the subprocess automatically on ctx cancel.
		_ = cmd.Wait()
		return false, nil
	}
}

// sinkAwaitExit waits for the plugin to exit after stdin has been closed, killing it
// if it does not exit within sinkShutdownTimeout. pumpErr takes priority unless the
// plugin itself exited with an error and the pump reported none.
func (s *sinkProcess) sinkAwaitExit(cmd *exec.Cmd, pumpErr error) error {
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
				return sinkFlushBuffer(s.buf, w, s.service)
			case <-ctx.Done():
				// subprocess killed by exec.CommandContext; no point flushing.
				return nil
			default:
				// No records yet; yield briefly.
				time.Sleep(time.Millisecond)
				continue
			}
		}
		if err := sinkWriteAndFlush(w, r, s.service); err != nil {
			return err
		}
	}
}

// sinkFlushBuffer drains every remaining record from buf into w, in pop order,
// stopping at the first write error. Used on shutdown to deliver buffered records
// before the plugin's stdin is closed.
func sinkFlushBuffer(buf *ringBuffer, w *bufio.Writer, service string) error {
	for {
		rec, ok := buf.pop()
		if !ok {
			return nil
		}
		if err := writeRecord(w, rec, service); err != nil {
			return err
		}
	}
}

// sinkWriteAndFlush writes one record to w and flushes it immediately, wrapping any
// error with which step failed.
func sinkWriteAndFlush(w *bufio.Writer, r sinkRecord, service string) error {
	if err := writeRecord(w, r, service); err != nil {
		return fmt.Errorf("writing record to plugin stdin: %w", err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing record to plugin stdin: %w", err)
	}
	return nil
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
