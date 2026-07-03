// Package logutil provides structured logger constructors for daemon and local (stderr) logging.
package logutil

import (
	"io"
	"log/slog"
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
