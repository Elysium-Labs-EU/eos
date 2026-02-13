package manager

import (
	"encoding/json"
	"eos/internal/config"
	"eos/internal/types"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type DaemonManager struct {
	socketPath string
}

func NewDaemonManager() (*DaemonManager, error) {
	socketPath := config.DaemonSocketPath
	pidFile := config.DaemonPIDFile

	if isDaemonRunning(pidFile) {
		return &DaemonManager{socketPath: socketPath}, nil
	}

	if err := startDaemonProcess(); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}

	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		return nil, fmt.Errorf("daemon started but socket not ready: %w", err)
	}

	return &DaemonManager{socketPath: socketPath}, nil
}

func isDaemonRunning(pidFile string) bool {
	data, err := os.ReadFile(pidFile)
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

func startDaemonProcess() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	cmd := exec.Command(exePath, "daemon", "start", "logToFile")

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
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

func (dm *DaemonManager) sendRequest(method types.MethodName, args []string) (response types.DaemonResponse, err error) {
	conn, err := net.Dial("unix", dm.socketPath)
	if err != nil {
		return types.DaemonResponse{}, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close daemon connection: %w", closeErr)
		}
	}()

	request := types.DaemonRequest{
		Method: method,
		Args:   args,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		return types.DaemonResponse{}, fmt.Errorf("failed to send request: %w", err)
	}

	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&response); err != nil {
		return types.DaemonResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if !response.Success {
		return types.DaemonResponse{}, fmt.Errorf("daemon error: %s", response.Error)
	}

	return response, nil
}

func (dm *DaemonManager) GetServiceInstance(name string) (*types.ServiceRuntime, error) {
	response, err := dm.sendRequest(types.MethodGetServiceInstance, []string{name})

	if err != nil {
		return nil, fmt.Errorf("the GetServiceInstance request errored, got:\n %v", err)
	}

	var result types.GetServiceInstanceResponse
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response data: %w", err)
	}

	return &result.Instance, nil
}

// func (dm *DaemonManager) GetService(name string) (types.Service, error) {
// 	response, err := dm.sendRequest(types.MethodGetService, []string{name})

// 	if err != nil {
// 		return types.Service{}, fmt.Errorf("the GetService request errored, got:\n %v", err)
// 	}

// 	var result types.Service
// 	if err := json.Unmarshal(response.Data, &result); err != nil {
// 		return types.Service{}, fmt.Errorf("failed to parse response data: %w", err)
// 	}

// 	return result, nil
// }

func (dm *DaemonManager) RemoveServiceInstance(name string) (bool, error) {
	response, err := dm.sendRequest(types.MethodRemoveServiceInstance, []string{name})

	if err != nil {
		return false, fmt.Errorf("the RemoveServiceInstance request errored, got:\n %v", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return false, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result["removed"], nil
}

func (dm *DaemonManager) StartService(name string) (int, error) {
	response, err := dm.sendRequest(types.MethodStartService, []string{name})

	if err != nil {
		return 0, fmt.Errorf("the StartService request errored, got:\n %v", err)
	}

	var result map[string]int
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return 0, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result["pid"], nil
}

func (dm *DaemonManager) RestartService(name string) (int, error) {
	response, err := dm.sendRequest(types.MethodRestartService, []string{name})

	if err != nil {
		return 0, fmt.Errorf("the RestartService request errored, got:\n %v", err)
	}

	var result map[string]int
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return 0, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result["pid"], nil
}

func (dm *DaemonManager) StopService(name string) (StopResult, error) {
	response, err := dm.sendRequest(types.MethodStopService, []string{name})

	if err != nil {
		return StopResult{}, fmt.Errorf("the StopService request errored, got:\n %v", err)
	}

	var result StopResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return StopResult{}, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) ForceStopService(name string) (StopResult, error) {
	response, err := dm.sendRequest(types.MethodForceStopService, []string{name})

	if err != nil {
		return StopResult{}, fmt.Errorf("the ForceStopService request errored, got:\n %v", err)
	}

	var result StopResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return StopResult{}, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) AddServiceCatalogEntry(service *types.ServiceCatalogEntry) error {
	serviceJSON, err := json.Marshal(service)
	if err != nil {
		return fmt.Errorf("failed to marshal service: %w", err)
	}
	_, err = dm.sendRequest(types.MethodAddServiceCatalogEntry, []string{string(serviceJSON)})

	if err != nil {
		return fmt.Errorf("the AddServiceCatalogEntry request errored, got:\n %v", err)
	}

	return nil
}

func (dm *DaemonManager) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) {
	response, err := dm.sendRequest(types.MethodGetAllServiceCatalogEntries, []string{})

	if err != nil {
		return nil, fmt.Errorf("the GetAllServiceCatalogEntries request errored, got:\n %v", err)
	}

	var result []types.ServiceCatalogEntry
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result, nil
}

func (dm *DaemonManager) GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error) {
	response, err := dm.sendRequest(types.MethodGetServiceCatalogEntry, []string{name})

	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("the GetServiceCatalogEntry request errored, got:\n %v", err)
	}

	var result types.ServiceCatalogEntry
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result, nil
}

// func (dm *DaemonManager) GetServiceStatus(name string) (types.ServiceStatus, error) {
// 	response, err := dm.sendRequest(types.MethodGetServiceStatus, []string{name})

// 	if err != nil {
// 		return types.ServiceStatusStopped, fmt.Errorf("the GetServiceStatus request errored, got:\n %v", err)
// 	}

// 	var result types.ServiceStatus
// 	if err := json.Unmarshal(response.Data, &result); err != nil {
// 		return types.ServiceStatusStopped, fmt.Errorf("failed to parse response data: %w", err)
// 	}

// 	return result, nil
// }

func (dm *DaemonManager) IsServiceRegistered(name string) (bool, error) {
	response, err := dm.sendRequest(types.MethodIsServiceRegistered, []string{name})

	if err != nil {
		return false, fmt.Errorf("the IsServiceRegistered request errored, got:\n %v", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return false, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result["exists"], nil
}

func (dm *DaemonManager) RemoveServiceCatalogEntry(name string) (bool, error) {
	response, err := dm.sendRequest(types.MethodRemoveServiceCatalogEntry, []string{name})

	if err != nil {
		return false, fmt.Errorf("the RemoveServiceCatalogEntry request errored, got:\n %v", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return false, fmt.Errorf("failed to parse response data: %w", err)
	}

	return result["removed"], nil
}

func (dm *DaemonManager) UpdateServiceCatalogEntry(name string, newDirectoryPath string, newConfigFileName string) error {
	_, err := dm.sendRequest(types.MethodUpdateServiceCatalogEntry, []string{name, newDirectoryPath, newConfigFileName})

	if err != nil {
		return fmt.Errorf("the UpdateServiceCatalogEntry request errored, got:\n %v", err)
	}

	return nil
}

func (dm *DaemonManager) GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error) {
	response, err := dm.sendRequest(types.MethodGetMostRecentProcessHistoryEntry, []string{name})

	if err != nil {
		return nil, fmt.Errorf("the GetMostRecentProcessHistoryEntry request errored, got:\n %v", err)
	}

	var result types.GetMostRecentProcessHistoryEntryResponse
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response data: %w", err)
	}

	return &result.ProcessEntry, nil
}

type ServiceLogFilesResult struct {
	LogFilePath      string `json:"logFile"`
	ErrorLogFilePath string `json:"errorLogFile"`
}

func (dm *DaemonManager) CreateServiceLogFiles(serviceName string) (logPath string, errorLogPath string, err error) {
	response, err := dm.sendRequest(types.MethodCreateServiceLogFiles, []string{serviceName})

	if err != nil {
		return "", "", fmt.Errorf("the CreateServiceLogFiles request errored, got:\n %v", err)
	}

	var result ServiceLogFilesResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse response data: %w", err)
	}

	return result.LogFilePath, result.ErrorLogFilePath, nil
}

func (dm *DaemonManager) GetServiceLogFilePath(serviceName string, errorLog bool) (*string, error) {
	type GetServiceLogFilePathRequest struct {
		ServiceName string
		ErrorLog    bool
	}
	serviceLogFilePathJSON, err := json.Marshal(GetServiceLogFilePathRequest{ServiceName: serviceName, ErrorLog: errorLog})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal service log path: %w", err)
	}
	response, err := dm.sendRequest(types.MethodGetServiceLogFilePath, []string{string(serviceLogFilePathJSON)})

	if err != nil {
		return nil, fmt.Errorf("the GetServiceLogFilePath request errored, got:\n %v", err)
	}

	var result map[string]*string
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response data: %w", err)
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

func NewDaemonLogger(logToFileAndConsole bool, baseDir string, fileName string) (*DaemonLogger, error) {
	logDir := CreateLogDirPath(baseDir)

	maxFiles := 5
	fileSizeLimit := int64(10 * 1024 * 1024)
	logPath := filepath.Join(logDir, fileName)

	err := os.MkdirAll(logDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("could not create the required folders: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	fileInfo, err := f.Stat()
	if err != nil {
		return nil, err
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
	timestamp := time.Now().Format(time.RFC3339)
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
		return fmt.Errorf("failed to close the file: %w", err)
	}

	err = handleRenameExistingLogs(l.logDir, l.fileName)
	if err != nil {
		return fmt.Errorf("failed to rename log files: %w", err)
	}

	newF, err := os.OpenFile(l.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create new log file: %w", err)
	}

	l.file = newF
	l.currentSize = 0

	return nil
}

// TODO: Add max file rotation in here.
func handleRenameExistingLogs(logDir string, defaultFileName string) error {
	dirEntries, err := os.ReadDir(logDir)
	if err != nil {
		return err
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
			return err
		}
	}

	return nil
}
