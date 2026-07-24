// Package logutil provides structured logger constructors for daemon and local (stderr) logging.
package logutil

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
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

// LastLogMessage returns the "msg" field of the last well-formed JSON log
// line (as written by NewJSONLogger) in the file at path, scanning from the
// end, skipping lines tagged with source=HealthBreadcrumbSource. Without that
// skip, a caller that re-reads its own previously-written breadcrumb (e.g.
// the health monitor snapshotting a service's error log across repeated
// restart-failure cycles) would keep nesting its own prior message into the
// next one, growing without bound. It returns ("", false) if the file can't
// be read, is empty, or contains no non-breadcrumb line that decodes to a
// JSON object with a non-empty msg field.
func LastLogMessage(path string) (string, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-controlled, not user input
	if err != nil {
		return "", false
	}

	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	for _, raw := range slices.Backward(lines) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Msg    string `json:"msg"`
			Source string `json:"source"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Msg != "" && entry.Source != HealthBreadcrumbSource {
			return entry.Msg, true
		}
	}
	return "", false
}
