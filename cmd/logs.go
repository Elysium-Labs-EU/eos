package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	streamLabelOut = lipgloss.NewStyle().Faint(true).Foreground(ui.ColorMuted).Render("out")
	streamLabelErr = lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWarning).Render("err")
)

func newLogsCmd(getManager func() manager.ServiceManager, warnDaemonDown func(*cobra.Command)) *cobra.Command {
	var lines int
	var errorOnly bool
	var outputOnly bool
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View logs for a registered service",
		Long: `Stream or display logs for a registered service. Shows both stdout and stderr logs interleaved by default.
Use --output for stdout only, --error for stderr only, --lines to control history depth, and --follow to tail in real time.

In combined mode --lines applies per stream, so up to 2x lines may be shown. Each line is prefixed with a dim "out" or bold "err" label to identify the source stream.`,
		Example: `  eos logs cms                   # last 300 lines from both streams combined
  eos logs cms --lines 100      # last 100 lines per stream combined
  eos logs cms --follow         # stream live output from both streams
  eos logs cms --error          # error stream only
  eos logs cms --output         # stdout stream only`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
		SilenceUsage:      true,
		SilenceErrors:     true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]

			// Probe daemon liveness before getManager: in standalone mode
			// getManager auto-starts the daemon, which would mask an outage.
			warnDaemonDown(cmd)

			mgr := getManager()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("checking service: %v", err))
				return helpers.ErrCommandFailed
			}
			if !exists {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "is not registered")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos add <path>"), ui.TextMuted.Render("to register it"))
				return helpers.ErrCommandFailed
			}

			processHistoryEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting process history: %v", err))
				return helpers.ErrCommandFailed
			}
			if processHistoryEntry == nil {
				cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceName), "has never been started")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos run %s", serviceName)), ui.TextMuted.Render("to start it"))
				return helpers.ErrCommandFailed
			}

			if lines < 0 || lines > 10000 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "line count must be between 0 and 10000")
				return helpers.ErrCommandFailed
			}

			if follow {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "streaming logs for", ui.TextBold.Render(serviceName))
			} else {
				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "showing logs for", ui.TextBold.Render(serviceName))
			}

			combined := !errorOnly && !outputOnly

			if combined {
				outPath, outErr := mgr.GetServiceLogFilePath(serviceName, false)
				if outErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file path: %v", outErr))
					return helpers.ErrCommandFailed
				}
				errPath, errPathErr := mgr.GetServiceLogFilePath(serviceName, true)
				if errPathErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting error log file path: %v", errPathErr))
					return helpers.ErrCommandFailed
				}
				if follow {
					followCombinedLogs(cmd.Context(), cmd.OutOrStdout(), *outPath, *errPath)
				} else {
					showCombinedLogs(cmd.OutOrStdout(), cmd.ErrOrStderr(), *outPath, *errPath, lines)
				}
				return nil
			}

			logPath, err := mgr.GetServiceLogFilePath(serviceName, errorOnly)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file path: %v", err))
				return helpers.ErrCommandFailed
			}

			tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
			if follow {
				tailArgs = append(tailArgs, "-f")
			}
			tailArgs = append(tailArgs, *logPath)

			// #nosec G204 - args are validated above
			tailLogCommand := exec.CommandContext(cmd.Context(), "tail", tailArgs...)
			tailLogCommand.Stderr = cmd.ErrOrStderr()

			stdout, pipeErr := tailLogCommand.StdoutPipe()
			if pipeErr != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating log pipe: %v", pipeErr))
				return helpers.ErrCommandFailed
			}
			if startErr := tailLogCommand.Start(); startErr != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading log file: %v", startErr))
				return helpers.ErrCommandFailed
			}
			renderServiceLogs(cmd.OutOrStdout(), stdout, "")
			if waitErr := tailLogCommand.Wait(); waitErr != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading log file: %v", waitErr))
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 300, "number of lines to display (per stream in combined mode)")
	cmd.Flags().BoolVar(&errorOnly, "error", false, "show error stream only")
	cmd.Flags().BoolVar(&outputOnly, "output", false, "show stdout stream only")
	cmd.Flags().BoolVar(&follow, "follow", false, "follow log output")
	cmd.MarkFlagsMutuallyExclusive("error", "output")

	return cmd
}

type serviceLogEntry struct {
	Time   string `json:"time"`
	Level  string `json:"level"`
	Msg    string `json:"msg"`
	Source string `json:"source"`
	Error  string `json:"error,omitempty"`
}

// streamLabel is the pre-rendered label to prepend ("out", "err", or "" for single-stream mode).
func renderServiceLogs(w io.Writer, r io.Reader, streamLabel string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		_, _ = fmt.Fprintln(w, renderServiceLogLine(scanner.Text(), streamLabel))
	}
	_ = scanner.Err()
}

func renderServiceLogLine(line, streamLabel string) string {
	var entry serviceLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		if streamLabel != "" {
			return fmt.Sprintf("%s %s", streamLabel, line)
		}
		return line
	}

	timeStr := entry.Time
	if t, err := time.Parse(time.RFC3339Nano, entry.Time); err == nil {
		timeStr = t.Format("15:04:05.000")
	}

	source := entry.Source
	if source == "" {
		source = strings.ToLower(entry.Level)
	}

	msg := entry.Msg
	if entry.Error != "" {
		msg = fmt.Sprintf("%s: %s", entry.Msg, entry.Error)
	}

	var body string
	if entry.Level == "WARN" || entry.Level == "ERROR" {
		body = fmt.Sprintf("%s %-6s [%s] %s", timeStr, source, entry.Level, msg)
	} else {
		body = fmt.Sprintf("%s %-6s %s", timeStr, source, msg)
	}

	if streamLabel != "" {
		return fmt.Sprintf("%s %s", streamLabel, body)
	}
	return body
}

type logLineWithTime struct {
	t     time.Time
	raw   string
	isErr bool
}

func parseLogLineTime(line string) time.Time {
	var entry serviceLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, entry.Time)
	if err != nil {
		return time.Time{}
	}
	return t
}

// logLinesWithTimes attaches a timestamp to each line, in the line's own
// stream order. Lines without a parseable timestamp (e.g. plain-text output
// from services that don't emit the internal JSON log format) inherit the
// last parsed timestamp from the same stream, so they sort next to the
// output they were interleaved with instead of collapsing to the very start
// of the merged, combined-mode output.
func logLinesWithTimes(lines []string) []logLineWithTime {
	result := make([]logLineWithTime, len(lines))
	var last time.Time
	for i, l := range lines {
		t := parseLogLineTime(l)
		if t.IsZero() {
			t = last
		} else {
			last = t
		}
		result[i] = logLineWithTime{t: t, raw: l}
	}
	return result
}

func showCombinedLogs(out, errW io.Writer, outPath, errPath string, lines int) {
	outLines, outErr := tailLogLines(outPath, lines)
	errLines, errErr := tailLogLines(errPath, lines)
	if outErr != nil && errErr != nil {
		_, _ = fmt.Fprintf(errW, "%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("reading log files: %v, %v", outErr, errErr))
		return
	}

	merged := make([]logLineWithTime, 0, len(outLines)+len(errLines))
	for _, l := range logLinesWithTimes(outLines) {
		merged = append(merged, logLineWithTime{t: l.t, raw: l.raw, isErr: false})
	}
	for _, l := range logLinesWithTimes(errLines) {
		merged = append(merged, logLineWithTime{t: l.t, raw: l.raw, isErr: true})
	}

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].t.Before(merged[j].t)
	})

	for _, entry := range merged {
		label := streamLabelOut
		if entry.isErr {
			label = streamLabelErr
		}
		_, _ = fmt.Fprintln(out, renderServiceLogLine(entry.raw, label))
	}
}

type followMsg struct {
	text  string
	isErr bool
}

func followCombinedLogs(ctx context.Context, out io.Writer, outPath, errPath string) {
	ch := make(chan followMsg, 256)

	startTailGoroutine(ctx, outPath, false, ch)
	startTailGoroutine(ctx, errPath, true, ch)

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			label := streamLabelOut
			if msg.isErr {
				label = streamLabelErr
			}
			_, _ = fmt.Fprintln(out, renderServiceLogLine(msg.text, label))
		}
	}
}

func startTailGoroutine(ctx context.Context, path string, isErr bool, ch chan<- followMsg) {
	// #nosec G204 - path comes from manager, not user input
	tail := exec.CommandContext(ctx, "tail", "-n", "0", "-f", path)
	var stderr bytes.Buffer
	tail.Stderr = &stderr

	stdout, err := tail.StdoutPipe()
	if err != nil {
		sendFollowErr(ctx, ch, path, err)
		return
	}
	if err := tail.Start(); err != nil {
		sendFollowErr(ctx, ch, path, err)
		return
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ch <- followMsg{text: scanner.Text(), isErr: isErr}:
			case <-ctx.Done():
				return
			}
		}
		_ = scanner.Err()
		if waitErr := tail.Wait(); waitErr != nil && ctx.Err() == nil {
			sendFollowErr(ctx, ch, path, errors.New(strings.TrimSpace(stderr.String())))
		}
	}()
}

func sendFollowErr(ctx context.Context, ch chan<- followMsg, path string, err error) {
	msg := followMsg{text: fmt.Sprintf("failed to tail %s: %v", path, err), isErr: true}
	select {
	case ch <- msg:
	case <-ctx.Done():
	}
}
