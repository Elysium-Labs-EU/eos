// Package logutil provides structured logger constructors for daemon and local (stderr) logging.
package logutil

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
)

// NewTextLogger returns a *slog.Logger writing human-readable text to w.
// Used for local (no-daemon) mode where output goes to a terminal.
func NewTextLogger(w io.Writer, verbose bool) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slogLevel(verbose)}))
}

// NewJSONLogger returns a *slog.Logger writing structured JSON to w.
// Used for daemon file logs; JSON format is Loki/Promtail-compatible.
func NewJSONLogger(w io.Writer, verbose bool) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slogLevel(verbose)}))
}

func slogLevel(verbose bool) slog.Level {
	if verbose {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// HealthBreadcrumbSource tags the "source" field of log lines the health
// monitor writes about itself (e.g. "restarting", "restart failed: ...") via
// LogToServiceStdout/LogToServiceStderr, as opposed to lines genuinely
// produced by the monitored child process (tagged "stdout"/"stderr" by
// pipeToLogFile/pipeToErrorLogFile). LastLogMessage uses this to avoid ever
// surfacing the health monitor's own prior breadcrumb as if it were fresh
// child output.
const HealthBreadcrumbSource = "health"

// errorLineMarkers are case-insensitive substrings that mark a line as
// plausibly naming a genuine crash reason, as opposed to a runtime's own
// startup/version banner (e.g. "Bun v1.3.14 (Linux arm64)", "Node.js
// v20.20.2"). It is deliberately short: a miss only falls back to
// LastLogMessage's prior last-non-empty-line behavior (see there), so
// growing coverage here can only improve results, never regress them the way
// a denylist of banner shapes would if a runtime's banner format changed.
var errorLineMarkers = []string{"error", "exception", "panic", "fatal", "errno", "code:"}

// looksLikeErrorLine reports whether msg contains one of errorLineMarkers.
func looksLikeErrorLine(msg string) bool {
	lower := strings.ToLower(msg)
	for _, marker := range errorLineMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// LastLogMessage returns the crash-reason line for pgid's own output within
// the JSON log file at path (as written by NewJSONLogger), scanning from the
// end and considering only lines tagged with a matching "pgid" field —
// skipping both health-monitor breadcrumbs (source=HealthBreadcrumbSource)
// and any other process group's lines sharing the same file, such as a
// restarted attempt's own startup banner already appended by the time this
// runs. Restricting to pgid is what makes that bound possible: a denylist of
// banner text can't tell "this restart's real error" from "that restart's
// banner" when both are literally the last line at different moments, since
// both are just text with no notion of which cycle wrote them.
//
// Within pgid's window it prefers the most recent line that
// looksLikeErrorLine, since real multi-line Node.js/Bun crash output often
// ends in a trailing banner or blank separator that carries no diagnostic
// value even though it belongs to the same process group. If no line in the
// window matches, it falls back to that window's own most recent non-empty
// line (the original heuristic, now at least scoped to the right process)
// rather than reporting nothing — most synthetic single-line crash scripts
// have no line that matches a marker at all.
//
// It returns ("", false) only when pgid's window is empty: the file can't be
// read, or the process group logged nothing but breadcrumbs (or nothing at
// all).
func LastLogMessage(path string, pgid int) (string, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-controlled, not user input
	if err != nil {
		return "", false
	}

	var fallback string
	haveFallback := false
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	for _, raw := range slices.Backward(lines) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Msg    string `json:"msg"`
			Source string `json:"source"`
			PGID   int    `json:"pgid"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Msg == "" || entry.Source == HealthBreadcrumbSource || entry.PGID != pgid {
			continue
		}
		if !haveFallback {
			fallback, haveFallback = entry.Msg, true
		}
		if looksLikeErrorLine(entry.Msg) {
			return entry.Msg, true
		}
	}
	return fallback, haveFallback
}
