package manager

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Elysium-Labs-EU/eos/internal/logutil"
)

// joinLogPath joins filename onto logDir and refuses the result if it
// resolves outside logDir. ValidateServiceName already forbids the path
// separators and ".." segments that would make escape possible, but this is
// the last place before a log file is actually created or opened on disk,
// so it does not trust that upstream validation ran.
func joinLogPath(logDir, filename string) (string, error) {
	joined := filepath.Join(logDir, filename)
	cleanDir := filepath.Clean(logDir)
	if joined != cleanDir && !strings.HasPrefix(joined, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("resolved log path %q escapes log directory %q", joined, cleanDir)
	}
	return joined, nil
}

func (m *LocalManager) NewServiceLogFiles(serviceName string) (logPath string, errorLogPath string, err error) {
	logDir := CreateLogDirPath(m.baseDir)

	err = os.MkdirAll(logDir, 0750)
	if err != nil {
		return "", "", fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}

	logPath, err = joinLogPath(logDir, CreateOutputLogFilename(serviceName))
	if err != nil {
		return "", "", err
	}
	errorLogPath, err = joinLogPath(logDir, CreateErrorOutputLogFilename(serviceName))
	if err != nil {
		return "", "", err
	}

	for _, path := range []string{logPath, errorLogPath} {
		f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644) // #nosec G302 -- log files should be readable by other users/tools
		if err != nil {
			return "", "", fmt.Errorf("failed to create log file %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return "", "", fmt.Errorf("failed to close log file %s: %w", path, err)
		}
	}

	return logPath, errorLogPath, nil
}

func OpenLogFile(logPath string) (*os.File, error) {
	logFile, err := os.OpenFile(filepath.Clean(logPath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G302 -- log files should be readable by other users/tools
	if err != nil {
		return nil, fmt.Errorf("could not open log file: %w", err)
	}
	return logFile, nil
}

func (m *LocalManager) GetServiceLogFilePath(serviceName string, errorLog bool) (*string, error) {
	logDir := CreateLogDirPath(m.baseDir)

	if errorLog {
		errorLogPath, err := joinLogPath(logDir, CreateErrorOutputLogFilename(serviceName))
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(errorLogPath); err != nil {
			return nil, fmt.Errorf("describing error log file: %w", err)
		}

		return &errorLogPath, nil
	}

	logPath, err := joinLogPath(logDir, CreateOutputLogFilename(serviceName))
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(logPath); err != nil {
		return nil, fmt.Errorf("describing the log file: %w", err)
	}

	return &logPath, nil
}

// GetServiceLastErrorLine returns the most recent non-empty line the service
// itself wrote to stderr, or ok=false if the error log is missing, empty, or
// unreadable. Used by the health monitor to surface the child process's own
// failure reason (e.g. "bind: Address already in use") instead of a generic
// exec-layer error. LastLogMessage skips lines the health monitor wrote about
// itself (source=HealthBreadcrumbSource), so this never echoes back the
// monitor's own prior "restart failed: ..." breadcrumb from an earlier cycle.
func (m *LocalManager) GetServiceLastErrorLine(serviceName string) (line string, ok bool) {
	logPath, err := m.GetServiceLogFilePath(serviceName, true)
	if err != nil {
		return "", false
	}
	return logutil.LastLogMessage(*logPath)
}

func (m *LocalManager) LogToServiceStdout(serviceName string, message string) error {
	return m.appendHealthEventToLog(serviceName, false, slog.LevelInfo, message)
}

func (m *LocalManager) LogToServiceStderr(serviceName string, message string) error {
	return m.appendHealthEventToLog(serviceName, true, slog.LevelWarn, message)
}

func (m *LocalManager) appendHealthEventToLog(serviceName string, errorLog bool, level slog.Level, message string) (err error) {
	logPath, pathErr := m.GetServiceLogFilePath(serviceName, errorLog)
	if pathErr != nil {
		return pathErr
	}
	file, openErr := os.OpenFile(filepath.Clean(*logPath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G302 -- log files should be readable by other users/tools
	if openErr != nil {
		return fmt.Errorf("opening log file: %w", openErr)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing log file: %w", closeErr)
		}
	}()
	l := logutil.NewJSONLogger(file, false)
	switch {
	case level >= slog.LevelError:
		l.Error(message, "service", serviceName, "source", logutil.HealthBreadcrumbSource)
	case level >= slog.LevelWarn:
		l.Warn(message, "service", serviceName, "source", logutil.HealthBreadcrumbSource)
	default:
		l.Info(message, "service", serviceName, "source", logutil.HealthBreadcrumbSource)
	}
	if syncErr := file.Sync(); syncErr != nil {
		return fmt.Errorf("syncing log file: %w", syncErr)
	}
	return nil
}

func CreateLogDirPath(baseDir string) string {
	logDir := filepath.Join(baseDir, "logs")
	return logDir
}

func CreateOutputLogFilename(serviceName string) string {
	logFilename := fmt.Sprintf("%s-out.log", serviceName)
	return logFilename
}

func CreateErrorOutputLogFilename(serviceName string) string {
	errorLogFilename := fmt.Sprintf("%s-error.log", serviceName)
	return errorLogFilename
}
