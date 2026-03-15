package logutil

import "fmt"

type LogLevel string

const (
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
)

type ProcessLogger interface {
	Log(level LogLevel, message string)
}

// StderrLogger is a simple ProcessLogger that writes to stderr.
type StderrLogger struct{}

func (l *StderrLogger) Log(level LogLevel, message string) {
	fmt.Printf("[%s] %s\n", level, message)
}
