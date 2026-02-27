package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"eos/internal/logutil"
	"eos/internal/types"
)

type DaemonManager struct {
	ctx        context.Context
	socketPath string
}

func NewDaemonManager(ctx context.Context, socketPath string, pidFile string, socketTimeout time.Duration) (*DaemonManager, error) {
	if isDaemonRunning(pidFile) {
		return &DaemonManager{ctx: ctx, socketPath: socketPath}, nil
	}

	if err := startDaemonProcess(ctx); err != nil {
		return nil, fmt.Errorf("starting daemon: %w", err)
	}

	if err := waitForSocket(socketPath, socketTimeout); err != nil {
		return nil, fmt.Errorf("daemon started but socket not ready: %w", err)
	}

	return &DaemonManager{ctx: ctx, socketPath: socketPath}, nil
}

func isDaemonRunning(pidFile string) bool {
	data, err := os.ReadFile(pidFile) // #nosec G304 -- path sanitized in config.NewDaemonConfig
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// Stay in sync with "forkDaemon"
func startDaemonProcess(ctx context.Context) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	cmd := exec.CommandContext(ctx, exePath, "daemon", "start", "--log-to-file-and-console") // #nosec G204 -- exePath is from os.Executable(), not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon process: %w", err)
	}

	return nil
}

func waitForSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return nil // Socket exists
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for socket")
}

func (dm *DaemonManager) sendRequest(method types.MethodName, args json.RawMessage) (response types.DaemonResponse, err error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(dm.ctx, "unix", dm.socketPath)
	if err != nil {
		return types.DaemonResponse{}, fmt.Errorf("connecting to daemon: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing daemon connection: %w", closeErr)
		}
	}()

	request := types.DaemonRequest{
		Method: method,
		Args:   args,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		return types.DaemonResponse{}, fmt.Errorf("sending request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return types.DaemonResponse{}, fmt.Errorf("reading response: %w", err)
	}

	if !response.Success {
		return types.DaemonResponse{}, fmt.Errorf("daemon error: %s", response.Error)
	}

	return response, nil
}

func (dm *DaemonManager) GetServiceInstance(name string) (*types.ServiceRuntime, error) {
	args, err := json.Marshal(types.GetServiceInstanceArgs{Name: name})
	if err != nil {
		return nil, fmt.Errorf("GetServiceInstance: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodGetServiceInstance, args)

	if err != nil {
		return nil, fmt.Errorf("GetServiceInstance: request errored: %w", err)
	}

	var result types.GetServiceInstanceResponse
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("GetServiceInstance: parse response data: %w", err)
	}

	return &result.Instance, nil
}

func (dm *DaemonManager) RemoveServiceInstance(name string) (bool, error) {
	args, err := json.Marshal(types.RemoveServiceInstanceArgs{Name: name})
	if err != nil {
		return false, fmt.Errorf("RemoveServiceInstance: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodRemoveServiceInstance, args)

	if err != nil {
		return false, fmt.Errorf("RemoveServiceInstance: request errored: %w", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return false, fmt.Errorf("RemoveServiceInstance: parse response data: %w", err)
	}

	return result["removed"], nil
}

func (dm *DaemonManager) StartService(name string) (int, error) {
	args, err := json.Marshal(types.StartServiceArgs{Name: name})
	if err != nil {
		return 0, fmt.Errorf("StartService: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodStartService, args)

	if err != nil {
		return 0, fmt.Errorf("StartService: request errored: %w", err)
	}

	var result map[string]int
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return 0, fmt.Errorf("StartService: parse response data: %w", err)
	}

	return result["pid"], nil
}

func (dm *DaemonManager) RestartService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (int, error) {
	args, err := json.Marshal(types.RestartServiceArgs{
		Name:         name,
		GracePeriod:  gracePeriod.String(),
		TickerPeriod: tickerPeriod.String(),
	})
	if err != nil {
		return 0, fmt.Errorf("RestartService: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodRestartService, args)

	if err != nil {
		return 0, fmt.Errorf("RestartService: request errored: %w", err)
	}

	var result map[string]int
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return 0, fmt.Errorf("RestartService: parse response data: %w", err)
	}

	return result["pid"], nil
}

func (dm *DaemonManager) StopService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (StopServiceResult, error) {
	args, err := json.Marshal(types.StopServiceArgs{
		Name:         name,
		GracePeriod:  gracePeriod.String(),
		TickerPeriod: tickerPeriod.String(),
	})
	if err != nil {
		return StopServiceResult{}, fmt.Errorf("StopService: marshaling args: %w", err)
	}

	response, err := dm.sendRequest(types.MethodStopService, args)

	if err != nil {
		return StopServiceResult{}, fmt.Errorf("StopService: request errored: %w", err)
	}

	var result StopServiceResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return StopServiceResult{}, fmt.Errorf("StopService: parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) ForceStopService(name string) (StopServiceResult, error) {
	args, err := json.Marshal(types.ForceStopServiceArgs{Name: name})
	if err != nil {
		return StopServiceResult{}, fmt.Errorf("ForceStopService: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodForceStopService, args)

	if err != nil {
		return StopServiceResult{}, fmt.Errorf("ForceStopService: request errored: %w", err)
	}

	var result StopServiceResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return StopServiceResult{}, fmt.Errorf("ForceStopService: parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) AddServiceCatalogEntry(service *types.ServiceCatalogEntry) error {
	args, err := json.Marshal(types.AddServiceCatalogEntryArgs{Service: service})
	if err != nil {
		return fmt.Errorf("AddServiceCatalogEntry: marshaling args: %w", err)
	}
	_, err = dm.sendRequest(types.MethodAddServiceCatalogEntry, args)

	if err != nil {
		return fmt.Errorf("AddServiceCatalogEntry: request errored: %w", err)
	}

	return nil
}

func (dm *DaemonManager) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) {
	response, err := dm.sendRequest(types.MethodGetAllServiceCatalogEntries, nil)

	if err != nil {
		return nil, fmt.Errorf("GetAllServiceCatalogEntries: request errored: %w", err)
	}

	var result []types.ServiceCatalogEntry
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("GetAllServiceCatalogEntries: parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error) {
	args, err := json.Marshal(types.GetServiceCatalogEntryArgs{Name: name})
	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("GetServiceCatalogEntry: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodGetServiceCatalogEntry, args)

	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("GetServiceCatalogEntry: request errored: %w", err)
	}

	var result types.ServiceCatalogEntry
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("GetServiceCatalogEntry: parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) IsServiceRegistered(name string) (bool, error) {
	args, err := json.Marshal(types.GetServiceCatalogEntryArgs{Name: name})
	if err != nil {
		return false, fmt.Errorf("IsServiceRegistered: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodIsServiceRegistered, args)

	if err != nil {
		return false, fmt.Errorf("IsServiceRegistered: request errored: %w", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return false, fmt.Errorf("IsServiceRegistered: parse response data: %w", err)
	}

	return result["exists"], nil
}

func (dm *DaemonManager) RemoveServiceCatalogEntry(name string) (bool, error) {
	args, err := json.Marshal(types.GetServiceCatalogEntryArgs{Name: name})
	if err != nil {
		return false, fmt.Errorf("RemoveServiceCatalogEntry: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodRemoveServiceCatalogEntry, args)

	if err != nil {
		return false, fmt.Errorf("RemoveServiceCatalogEntry: request errored: %w", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return false, fmt.Errorf("RemoveServiceCatalogEntry: parse response data: %w", err)
	}

	return result["removed"], nil
}

func (dm *DaemonManager) UpdateServiceCatalogEntry(name string, newDirectoryPath string, newConfigFileName string) error {
	args, err := json.Marshal(types.UpdateServiceCatalogEntryArgs{Name: name})
	if err != nil {
		return fmt.Errorf("UpdateServiceCatalogEntry: marshaling args: %w", err)
	}
	_, err = dm.sendRequest(types.MethodUpdateServiceCatalogEntry, args)

	if err != nil {
		return fmt.Errorf("UpdateServiceCatalogEntry: request errored: %w", err)
	}

	return nil
}

func (dm *DaemonManager) GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error) {
	args, err := json.Marshal(types.GetMostRecentProcessHistoryEntryArgs{Name: name})
	if err != nil {
		return nil, fmt.Errorf("GetMostRecentProcessHistoryEntry: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodGetMostRecentProcessHistoryEntry, args)

	if err != nil {
		return nil, fmt.Errorf("GetMostRecentProcessHistoryEntry: request errored: %w", err)
	}

	var result types.GetMostRecentProcessHistoryEntryResponse
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("GetMostRecentProcessHistoryEntry: parse response data: %w", err)
	}

	return &result.ProcessEntry, nil
}

type ServiceLogFilesResult struct {
	LogFilePath      string `json:"logFile"`
	ErrorLogFilePath string `json:"errorLogFile"`
}

func (dm *DaemonManager) CreateServiceLogFiles(serviceName string) (logPath string, errorLogPath string, err error) {
	args, err := json.Marshal(types.CreateServiceLogFilesArgs{ServiceName: serviceName})
	if err != nil {
		return "", "", fmt.Errorf("CreateServiceLogFiles: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodCreateServiceLogFiles, args)

	if err != nil {
		return "", "", fmt.Errorf("CreateServiceLogFiles: request errored: %w", err)
	}

	var result ServiceLogFilesResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return "", "", fmt.Errorf("CreateServiceLogFiles: parse response data: %w", err)
	}

	return result.LogFilePath, result.ErrorLogFilePath, nil
}

func (dm *DaemonManager) GetServiceLogFilePath(serviceName string, errorLog bool) (*string, error) {
	args, err := json.Marshal(types.GetServiceLogFilePathArgs{ServiceName: serviceName, ErrorLog: errorLog})
	if err != nil {
		return nil, fmt.Errorf("GetServiceLogFilePath: marshaling args: %w", err)
	}
	response, err := dm.sendRequest(types.MethodGetServiceLogFilePath, args)

	if err != nil {
		return nil, fmt.Errorf("GetServiceLogFilePath: request errored: %w", err)
	}

	var result map[string]*string
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("GetServiceLogFilePath: parse response data: %w", err)
	}

	return result["filepath"], nil
}

type DaemonLogger struct {
	file         *os.File
	LogPath      string
	logDir       string
	fileName     string
	currentSize  int64
	maxSize      int64
	maxFiles     int
	logToConsole bool
}

type LogLevel string

const (
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
)

func NewDaemonLogger(logToFileAndConsole bool, logDir string, fileName string, maxFiles int, fileSizeLimit int64) (*DaemonLogger, error) {
	logPath := filepath.Clean(filepath.Join(logDir, fileName))

	err := os.MkdirAll(logDir, 0750)
	if err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G302 -- log files should be readable by other users/tools
	if err != nil {
		return nil, err
	}

	fileInfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("file state info %q: %w", logPath, err)
	}

	return &DaemonLogger{
		file:         f,
		LogPath:      logPath,
		logDir:       logDir,
		fileName:     fileName,
		currentSize:  fileInfo.Size(),
		maxSize:      fileSizeLimit,
		maxFiles:     maxFiles,
		logToConsole: logToFileAndConsole,
	}, nil
}

func (l *DaemonLogger) Log(level LogLevel, message string) {
	timestamp := time.Now().UTC().Format(logutil.TimestampFormat)
	logMessage := fmt.Sprintf("[%s] %s: %s\n", timestamp, level, message)

	if l.logToConsole {
		fmt.Print(logMessage)
	}

	if l.currentSize+int64(len(logMessage)) >= l.maxSize {
		err := l.rotate()
		if err != nil {
			fmt.Printf("Errored during rotating logs: %v", err)
		}
		l.currentSize = 0
	}

	n, _ := l.file.WriteString(logMessage)
	l.currentSize += int64(n)
}

func (l *DaemonLogger) rotate() error {
	err := l.file.Close()
	if err != nil {
		return fmt.Errorf("closing file: %w", err)
	}

	err = handleRenameExistingLogs(l.logDir, l.fileName)
	if err != nil {
		return fmt.Errorf("renaming log files: %w", err)
	}

	newF, err := os.OpenFile(l.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G302 -- log files should be readable by other users/tools
	if err != nil {
		return fmt.Errorf("creating new log file: %w", err)
	}

	l.file = newF
	l.currentSize = 0

	return nil
}

// TODO: Add max file rotation in here.
func handleRenameExistingLogs(logDir string, defaultFileName string) error {
	dirEntries, err := os.ReadDir(logDir)
	if err != nil {
		return fmt.Errorf("read dir %q: %w", logDir, err)
	}

	var validatedDirEntries []os.DirEntry
	for _, entry := range dirEntries {
		if strings.HasPrefix(entry.Name(), defaultFileName) {
			validatedDirEntries = append(validatedDirEntries, entry)
		}
	}

	for i := len(validatedDirEntries) - 1; i >= 0; i-- {
		currentName := validatedDirEntries[i].Name()
		currentLogPath := filepath.Join(logDir, currentName)

		newName := fmt.Sprintf("%s.%s", defaultFileName, strconv.Itoa(i+1))
		newLogPath := filepath.Join(logDir, newName)
		err := os.Rename(currentLogPath, newLogPath)
		if err != nil {
			return fmt.Errorf("rotate log file %q to %q: %w", currentLogPath, newLogPath, err)
		}
	}

	return nil
}
