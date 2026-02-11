package process

import (
	"encoding/json"
	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/monitor"
	"eos/internal/types"
	"eos/internal/util"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func StartDaemon(logToFileAndConsole bool, baseDir string, logFileName string) error {
	logger, err := manager.NewDaemonLogger(logToFileAndConsole, baseDir, logFileName)
	if err != nil {
		errorMessage := fmt.Errorf("failed to setup daemn logger: %w", err)
		logger.Log(manager.LogLevelInfo, errorMessage.Error())
		return errorMessage
	} else {
		logger.Log(manager.LogLevelInfo, "Started daemon logger")
	}
	pidFile := config.DaemonPIDFile
	if _, err := os.Stat(pidFile); err == nil {
		data, _ := os.ReadFile(pidFile)
		oldPid, _ := strconv.Atoi(string(data))

		if process, err := os.FindProcess(oldPid); err == nil {
			if process.Signal(syscall.Signal(0)) == nil {
				errorMessage := fmt.Errorf("daemon already running with PID %d", oldPid)
				logger.Log(manager.LogLevelInfo, errorMessage.Error())
				return errorMessage
			}
		}
		os.Remove(pidFile)
	}

	myPID := os.Getpid()
	err = os.WriteFile(pidFile, fmt.Appendf(nil, "%d", myPID), 0644)
	if err != nil {
		errorMessage := fmt.Errorf("failed to write to pid file: %w", err)
		logger.Log(manager.LogLevelInfo, errorMessage.Error())
		return errorMessage
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGCHLD)

	socketPath := config.DaemonSocketPath
	os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		errorMessage := fmt.Errorf("failed to create socket: %w", err)
		logger.Log(manager.LogLevelInfo, errorMessage.Error())
		return errorMessage
	}

	db, err := database.NewDB()
	if err != nil {
		errorMessage := fmt.Errorf("failed to connect to database: %w", err)
		logger.Log(manager.LogLevelInfo, errorMessage.Error())
		return errorMessage
	}
	defer func() {
		if err := db.CloseDBConnection(); err != nil {
			logger.Log(manager.LogLevelError, fmt.Sprintf("failed to close database: %v", err))
		}
	}()

	mgr := manager.NewLocalManager(db, baseDir)
	go handleIncomingCommands(listener, mgr, logger)

	healthMonitor := monitor.NewHealthMonitor(mgr, db, logger)
	go healthMonitor.Start()

	logger.Log(manager.LogLevelInfo, "Daemon started successfully")
	for {
		sig, ok := <-sigChan

		if !ok {
			return nil
		}

		switch sig {
		case syscall.SIGCHLD:
			for {
				var status syscall.WaitStatus
				pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
				if err != nil {
					logger.Log(manager.LogLevelError, fmt.Sprintf("Errored during cleaning up child process with PID '%d'\n: %v", pid, err))
					break
				}
				if pid == 0 {
					break
				}
				if pid < 0 {
					logger.Log(manager.LogLevelError, fmt.Sprintf("An error during cleaning up child process with PID '%d'", pid))
					continue
				}

				logger.Log(manager.LogLevelError, fmt.Sprintf("Reaped zombie process: %d\n", pid))

				if status.ExitStatus() == 0 {
					updates := database.ProcessHistoryUpdate{
						State:     util.ProcessStatePtr(types.ProcessStateStopped),
						StoppedAt: util.TimePtr(time.Now()),
					}
					err := db.UpdateProcessHistoryEntry(pid, updates)
					if err != nil {
						logger.Log(manager.LogLevelError, fmt.Sprintf("unable to update the reaped process in the database, got: %v", err))
					}
					continue
				}

				updates := database.ProcessHistoryUpdate{
					State:     util.ProcessStatePtr(types.ProcessStateFailed),
					StoppedAt: util.TimePtr(time.Now()),
					Error:     util.StringPtr("Zombie process has been reaped"),
				}

				err = db.UpdateProcessHistoryEntry(pid, updates)
				if err != nil {
					logger.Log(manager.LogLevelError, fmt.Sprintf("unable to update the reaped process in the database, got: %v", err))
				}

				continue
			}

		case syscall.SIGTERM, syscall.SIGINT:
			listener.Close()
			os.Remove(socketPath)
			os.Remove(pidFile)
			return nil
		}
	}
}

func StopDaemon() error {
	pidFile := config.DaemonPIDFile

	_, err := os.Stat(pidFile)
	if err != nil {
		return fmt.Errorf("failed to get stat info on pid of daemon: %w", err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("failed to read the pid file: %w", err)
	}

	activePid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("failed to convert the pid data to string: %w", err)
	}

	process, err := os.FindProcess(activePid)
	if err != nil {
		return fmt.Errorf("failed to find the process matching the pid: %w", err)
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return fmt.Errorf("failed to check for active deamon: %w", err)
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		return fmt.Errorf("failed to kill active deamon: %w", err)
	}
	return nil
}

type DaemonStatus struct {
	Running bool
	Pid     *int
	Process *os.Process
}

func StatusDaemon() (*DaemonStatus, error) {
	pidFile := config.DaemonPIDFile

	_, err := os.Stat(pidFile)
	if err != nil {
		return &DaemonStatus{
			Running: false,
			Pid:     nil,
			Process: nil,
		}, nil
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the pid file: %w", err)
	}

	activePid, err := strconv.Atoi(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to convert the pid data to string: %w", err)
	}

	process, err := os.FindProcess(activePid)
	if err != nil {
		return nil, fmt.Errorf("failed to find the process matching the pid: %w", err)
	}

	return &DaemonStatus{
		Running: true,
		Pid:     &activePid,
		Process: process,
	}, nil
}

func handleIncomingCommands(listener net.Listener, mgr manager.ServiceManager, logger *manager.DaemonLogger) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logger.Log(manager.LogLevelInfo, "Listener closed, shutting down gracefully")
			} else {
				logger.Log(manager.LogLevelError, fmt.Sprintf("An error during accepting the connection: %v", err))
			}
			return
		}

		go handleConnection(conn, mgr, logger)
	}
}

func handleConnection(conn net.Conn, mgr manager.ServiceManager, logger *manager.DaemonLogger) {
	defer conn.Close()

	var request types.DaemonRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		sendErrorResponse(conn, fmt.Sprintf("invalid request: %v", err), logger)
		return
	}

	response := executeRequest(mgr, request)

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		logger.Log(manager.LogLevelError, fmt.Sprintf("Failed to send response: %v\n", err))
	}
}

func executeRequest(mgr manager.ServiceManager, request types.DaemonRequest) types.DaemonResponse {
	switch request.Method {
	case types.MethodGetServiceInstance:

		if len(request.Args) < 1 {
			return errorResponse("GetServiceInstance requires service name")
		}

		result, err := mgr.GetServiceInstance(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		if result == nil {
			return errorResponse("failed to get a result, returned nil")
		}
		data, err := json.Marshal(types.GetServiceInstanceResponse{
			Instance: *result,
		})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodRemoveServiceInstance:
		if len(request.Args) < 1 {
			return errorResponse("RemoveServiceInstance requires service name")
		}
		removed, err := mgr.RemoveServiceInstance(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(map[string]bool{"removed": removed})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{Success: true, Data: data}

	case types.MethodStartService:
		if len(request.Args) < 1 {
			return errorResponse("StartService requires service name")
		}
		pid, err := mgr.StartService(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(map[string]int{"pid": pid})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodRestartService:
		if len(request.Args) < 1 {
			return errorResponse("RestartService requires service name")
		}
		pid, err := mgr.RestartService(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(map[string]int{"pid": pid})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodStopService:
		if len(request.Args) < 1 {
			return errorResponse("StopService requires service name")
		}
		result, err := mgr.StopService(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(result)

		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodForceStopService:
		if len(request.Args) < 1 {
			return errorResponse("ForceStopService requires service name")
		}
		result, err := mgr.ForceStopService(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodAddServiceCatalogEntry:
		if len(request.Args) < 1 {
			return errorResponse("AddServiceCatalogEntry requires service name")
		}

		var service types.ServiceCatalogEntry
		if err := json.Unmarshal([]byte(request.Args[0]), &service); err != nil {
			return errorResponse(fmt.Sprintf("invalid service data: %v", err))
		}
		err := mgr.AddServiceCatalogEntry(&service)
		if err != nil {
			return errorResponse(err.Error())
		}
		return types.DaemonResponse{Success: true}

	case types.MethodGetAllServiceCatalogEntries:
		result, err := mgr.GetAllServiceCatalogEntries()
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodGetServiceCatalogEntry:
		if len(request.Args) < 1 {
			return errorResponse("GetServiceCatalogEntry requires service name")
		}
		result, err := mgr.GetServiceCatalogEntry(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	// case types.MethodGetServiceStatus:
	// 	if len(request.Args) < 1 {
	// 		return errorResponse("GetServiceStatus requires service name")
	// 	}
	// 	result, err := mgr.GetServiceStatus(request.Args[0])
	// 	if err != nil {
	// 		return errorResponse(err.Error())
	// 	}
	// 	data, err := json.Marshal(result)
	// 	if err != nil {
	// 		return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
	// 	}
	// 	return types.DaemonResponse{
	// 		Success: true,
	// 		Data:    data,
	// 	}

	case types.MethodIsServiceRegistered:
		if len(request.Args) < 1 {
			return errorResponse("IsServiceRegistered requires service name")
		}
		result, err := mgr.IsServiceRegistered(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(map[string]bool{"exists": result})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodRemoveServiceCatalogEntry:
		if len(request.Args) < 1 {
			return errorResponse("RemoveServiceCatalogEntry requires service name")
		}
		removed, err := mgr.RemoveServiceCatalogEntry(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		data, err := json.Marshal(map[string]bool{"removed": removed})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}

		return types.DaemonResponse{Success: true, Data: data}

	case types.MethodUpdateServiceCatalogEntry:
		if len(request.Args) < 3 {
			return errorResponse("UpdateServiceCatalogEntry requires name, newDirectoryPath, newConfigFileName")
		}
		err := mgr.UpdateServiceCatalogEntry(request.Args[0], request.Args[1], request.Args[2])
		if err != nil {
			return errorResponse(err.Error())
		}
		return types.DaemonResponse{Success: true}

	case types.MethodGetMostRecentProcessHistoryEntry:
		if len(request.Args) < 1 {
			return errorResponse("GetMostRecentProcessHistoryEntry requires service name")
		}
		result, err := mgr.GetMostRecentProcessHistoryEntry(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}
		if result == nil {
			return errorResponse("no process history entry found")
		}
		data, err := json.Marshal(types.GetMostRecentProcessHistoryEntryResponse{
			ProcessEntry: *result,
		})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}

		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodCreateServiceLogFiles:
		if len(request.Args) < 1 {
			return errorResponse("CreateServiceLogFiles requires service name")
		}

		logPath, errorLogPath, err := mgr.CreateServiceLogFiles(request.Args[0])
		if err != nil {
			return errorResponse(err.Error())
		}

		data, err := json.Marshal(map[string]string{"logPath": logPath, "errorLogPath": errorLogPath})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}

		return types.DaemonResponse{Success: true, Data: data}

	case types.MethodGetServiceLogFilePath:
		if len(request.Args) < 1 {
			return errorResponse("GetServiceLogFilePath requires service name")
		}

		type GetServiceLogFilePathRequest struct {
			ServiceName string
			ErrorLog    bool
		}

		var serviceLogPathRequest GetServiceLogFilePathRequest
		if err := json.Unmarshal([]byte(request.Args[0]), &serviceLogPathRequest); err != nil {
			return errorResponse(fmt.Sprintf("invalid args for GetServiceLogFilePath: %v", err))
		}

		filepath, err := mgr.GetServiceLogFilePath(serviceLogPathRequest.ServiceName, serviceLogPathRequest.ErrorLog)
		if err != nil {
			return errorResponse(err.Error())
		}

		data, err := json.Marshal(map[string]*string{"filepath": filepath})
		if err != nil {
			return errorResponse(fmt.Sprintf("failed to marshal response: %v", err))
		}

		return types.DaemonResponse{Success: true, Data: data}

	default:
		return errorResponse(fmt.Sprintf("unknown method: %s", request.Method))

	}
}

func errorResponse(message string) types.DaemonResponse {
	return types.DaemonResponse{
		Success: false,
		Error:   message,
	}
}

func sendErrorResponse(conn net.Conn, message string, logger *manager.DaemonLogger) {
	response := errorResponse(message)
	encoder := json.NewEncoder(conn)
	err := encoder.Encode(response)
	if err != nil {
		logger.Log(manager.LogLevelError, fmt.Sprintf("Failed to send error response: %v\n", err))
	}
}
