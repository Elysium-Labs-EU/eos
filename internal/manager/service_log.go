package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (m *LocalManager) CreateServiceLogFiles(serviceName string) (*os.File, *os.File, error) {
	logDir := CreateLogDir(m.baseDir)
	logFilename := CreateOutputLogFilename(serviceName)
	errorLogFilename := CreateErrorOutputLogFilename(serviceName)
	logPath := filepath.Join(logDir, logFilename)
	errorLogPath := filepath.Join(logDir, errorLogFilename)

	err := os.MkdirAll(logDir, 0755)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create the required folder: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("could not open log file: %w", err)
	}

	errorLogFile, err := os.OpenFile(errorLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		defer logFile.Close()
		return nil, nil, fmt.Errorf("could not open error log file: %w", err)
	}

	return logFile, errorLogFile, nil
}

func (m *LocalManager) GetServiceLogFilePath(serviceName string, errorLog bool) (*string, error) {
	logDir := CreateLogDir(m.baseDir)

	if errorLog {
		errorLogFilename := CreateErrorOutputLogFilename(serviceName)
		errorLogPath := filepath.Join(logDir, errorLogFilename)
		_, err := os.Stat(errorLogPath)
		if err != nil {
			return nil, fmt.Errorf("An error occured during getting the error log file, got:\n%v", err)
		}

		return &errorLogPath, nil
	}

	logFilename := CreateOutputLogFilename(serviceName)
	logPath := filepath.Join(logDir, logFilename)

	_, err := os.Stat(logPath)
	if err != nil {
		return nil, fmt.Errorf("An error occured during getting the log file, got:\n%v", err)
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

func (m *LocalManager) appendToFile(filePath *string, content string) error {
	file, err := os.OpenFile(*filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", *filePath, err)
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to log file %s: %w", *filePath, err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync log file %s: %w", *filePath, err)
	}

	return nil
}

func formatLogMessage(msg string) string {
	return fmt.Sprintf("[%s] [HEALTH MONITOR] %s\n",
		time.Now().Format("2006-01-02 15:04:05"), msg)
}

func CreateLogDir(baseDir string) string {
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
