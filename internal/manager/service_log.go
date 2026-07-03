package manager

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"codeberg.org/Elysium_Labs/eos/internal/logutil"
)

func (m *LocalManager) NewServiceLogFiles(serviceName string) (logPath string, errorLogPath string, err error) {
	logDir := CreateLogDirPath(m.baseDir)

	err = os.MkdirAll(logDir, 0750)
	if err != nil {
		return "", "", fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}

	logPath = filepath.Join(logDir, CreateOutputLogFilename(serviceName))
	errorLogPath = filepath.Join(logDir, CreateErrorOutputLogFilename(serviceName))

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
		errorLogFilename := CreateErrorOutputLogFilename(serviceName)
		errorLogPath := filepath.Join(logDir, errorLogFilename)
		_, err := os.Stat(errorLogPath)
		if err != nil {
			return nil, fmt.Errorf("describing error log file: %w", err)
		}

		return &errorLogPath, nil
	}

	logFilename := CreateOutputLogFilename(serviceName)
	logPath := filepath.Join(logDir, logFilename)

	_, err := os.Stat(logPath)
	if err != nil {
		return nil, fmt.Errorf("describing the log file: %w", err)
	}

	return &logPath, nil
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
		l.Error(message, "service", serviceName, "source", "health")
	case level >= slog.LevelWarn:
		l.Warn(message, "service", serviceName, "source", "health")
	default:
		l.Info(message, "service", serviceName, "source", "health")
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
