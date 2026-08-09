package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	eosconfig "github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/logutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// resolveServiceLogRotation returns the effective rotation file-count/size cap
// for a service's stdout/stderr logs: the service's own service.yaml override
// if set, otherwise the daemon's own log rotation default (the only rotation
// cap that exists today, applied here so service logs stop growing unbounded).
func resolveServiceLogRotation(config *types.ServiceConfig) (maxFiles int, sizeLimit int64) {
	maxFiles = config.LogMaxFiles
	if maxFiles <= 0 {
		maxFiles = eosconfig.DaemonLogMaxFiles
	}
	sizeLimit = config.LogFileSizeLimitBytes
	if sizeLimit <= 0 {
		sizeLimit = eosconfig.DaemonLogFileSizeLimit
	}
	return maxFiles, sizeLimit
}

// acquireServiceLogWriter returns the shared, size-capped rotating writer for
// serviceName's stdout (or, if errorLog, stderr) log file — creating it on the
// first acquire. Every caller (the running service's own pipe-forwarding
// goroutine, and the health monitor's breadcrumb writes) goes through this
// same shared instance rather than opening an independent handle onto the
// file, so a rotate() from one caller can't rename the file out from under
// another caller's fd. Must be paired with a releaseServiceLogWriter call.
func (m *LocalManager) acquireServiceLogWriter(serviceName string, errorLog bool, maxFiles int, sizeLimit int64) (*RotatingFileWriter, error) {
	logDir := CreateLogDirPath(m.baseDir)
	fileName := CreateOutputLogFilename(serviceName)
	if errorLog {
		fileName = CreateErrorOutputLogFilename(serviceName)
	}
	return m.acquireLogWriter(logDir, fileName, maxFiles, sizeLimit)
}

// releaseServiceLogWriter releases one reference to serviceName's stdout (or,
// if errorLog, stderr) shared rotating writer, closing it once nothing else
// holds it. Must only be called after a successful acquireServiceLogWriter.
func (m *LocalManager) releaseServiceLogWriter(serviceName string, errorLog bool) error {
	logDir := CreateLogDirPath(m.baseDir)
	fileName := CreateOutputLogFilename(serviceName)
	if errorLog {
		fileName = CreateErrorOutputLogFilename(serviceName)
	}
	return m.releaseLogWriter(logDir, fileName)
}

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

func (m *LocalManager) NewServiceLogFiles(_ context.Context, serviceName string) (logPath string, errorLogPath string, err error) {
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

func (m *LocalManager) GetServiceLogFilePath(_ context.Context, serviceName string, errorLog bool) (*string, error) {
	logDir := CreateLogDirPath(m.baseDir)

	if errorLog {
		errorLogPath, err := joinLogPath(logDir, CreateErrorOutputLogFilename(serviceName))
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(errorLogPath); err != nil {
			return nil, fmt.Errorf("describing error log file (only exists once the service has run at least once): %w", err)
		}

		return &errorLogPath, nil
	}

	logPath, err := joinLogPath(logDir, CreateOutputLogFilename(serviceName))
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(logPath); err != nil {
		return nil, fmt.Errorf("describing the log file (only exists once the service has run at least once): %w", err)
	}

	return &logPath, nil
}

// GetServiceLastErrorLine returns the crash-reason line pgid itself wrote to
// stderr, or ok=false if the error log is missing, empty, or unreadable. Used
// by the health monitor to surface the child process's own failure reason
// (e.g. "bind: Address already in use") instead of a generic exec-layer
// error. pgid scopes the search to this exact process group's own lines (see
// logutil.LastLogMessage), so a restarted attempt already appended to the
// same error log can never be mistaken for this one's reason, and lines the
// health monitor wrote about itself (source=HealthBreadcrumbSource) are
// never echoed back either.
func (m *LocalManager) GetServiceLastErrorLine(serviceName string, pgid int) (line string, ok bool) {
	logPath, err := m.GetServiceLogFilePath(m.ctx, serviceName, true)
	if err != nil {
		return "", false
	}
	return logutil.LastLogMessage(*logPath, pgid)
}

func (m *LocalManager) LogToServiceStdout(serviceName, message string) error {
	return m.appendHealthEventToLog(serviceName, false, slog.LevelInfo, message)
}

func (m *LocalManager) LogToServiceStderr(serviceName, message string) error {
	return m.appendHealthEventToLog(serviceName, true, slog.LevelWarn, message)
}

func (m *LocalManager) appendHealthEventToLog(serviceName string, errorLog bool, level slog.Level, message string) (err error) {
	// GetServiceLogFilePath also confirms the log file already exists: health
	// breadcrumbs only append to a log a prior launch already created, never
	// create one out of thin air.
	if _, pathErr := m.GetServiceLogFilePath(m.ctx, serviceName, errorLog); pathErr != nil {
		return pathErr
	}
	w, openErr := m.acquireServiceLogWriter(serviceName, errorLog, eosconfig.DaemonLogMaxFiles, eosconfig.DaemonLogFileSizeLimit)
	if openErr != nil {
		return fmt.Errorf("opening log file: %w", openErr)
	}
	defer func() {
		if closeErr := m.releaseServiceLogWriter(serviceName, errorLog); closeErr != nil && err == nil {
			err = fmt.Errorf("releasing log file: %w", closeErr)
		}
	}()
	l := logutil.NewJSONLogger(w, false)
	switch {
	case level >= slog.LevelError:
		l.Error(message, "service", serviceName, "source", logutil.HealthBreadcrumbSource)
	case level >= slog.LevelWarn:
		l.Warn(message, "service", serviceName, "source", logutil.HealthBreadcrumbSource)
	default:
		l.Info(message, "service", serviceName, "source", logutil.HealthBreadcrumbSource)
	}
	if syncErr := w.Sync(); syncErr != nil {
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
