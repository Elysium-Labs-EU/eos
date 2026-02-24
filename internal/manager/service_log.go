package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (m *LocalManager) CreateServiceLogFiles(serviceName string) (logPath string, errorLogPath string, err error) {
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
			return nil, fmt.Errorf("an error occurred during getting the error log file, got:\n%w", err)
		}

		return &errorLogPath, nil
	}

	logFilename := CreateOutputLogFilename(serviceName)
	logPath := filepath.Join(logDir, logFilename)

	_, err := os.Stat(logPath)
	if err != nil {
		return nil, fmt.Errorf("an error occurred during getting the log file, got:\n%w", err)
	}

	return &logPath, nil
}

func (m *LocalManager) LogToServiceStdout(serviceName string, message string) error {
	logPath, err := m.GetServiceLogFilePath(serviceName, false)
	if err != nil {
		return err
	}
	return m.appendToFile(logPath, formatLogMessage(message))
}

func (m *LocalManager) LogToServiceStderr(serviceName string, message string) error {
	logPath, err := m.GetServiceLogFilePath(serviceName, true)
	if err != nil {
		return err
	}
	return m.appendToFile(logPath, formatLogMessage(message))
}

func (m *LocalManager) appendToFile(filePath *string, content string) (err error) {
	file, openErr := os.OpenFile(*filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G302 -- log files should be readable by other users/tools
	if openErr != nil {
		return fmt.Errorf("failed to open log file %s: %w", *filePath, openErr)
	}
	defer func() {
		if closeError := file.Close(); closeError != nil && err == nil {
			err = fmt.Errorf("failed to close the file connection for %s: %w", *filePath, err)
		}
	}()

	if _, err = file.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to log file %s: %w", *filePath, err)
	}

	if err = file.Sync(); err != nil {
		return fmt.Errorf("failed to sync log file %s: %w", *filePath, err)
	}

	return nil
}

func formatLogMessage(msg string) string {
	return fmt.Sprintf("[%s] [HEALTH MONITOR] %s\n",
		time.Now().Format("2006-01-02 15:04:05"), msg)
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
