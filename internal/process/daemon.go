// Package process handles OS-level process spawning, signal delivery, and stdin/stdout piping for daemons.
package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // intentional: pprof only exposed when EOS_PPROF_ADDR is set
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/logutil"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/monitor"
	"codeberg.org/Elysium_Labs/eos/internal/types"
)

type daemon struct {
	listener   net.Listener
	ctx        context.Context
	logger     *manager.DaemonLogger
	db         *database.DB
	mgr        *manager.LocalManager
	stop       context.CancelFunc
	sigChan    chan os.Signal
	pidFile    string
	socketPath string
}

func StartStandaloneDaemon(ctx context.Context, logToFileAndConsole bool, baseDir string, standaloneDaemonConfig *config.StandaloneDaemonConfig, healthConfig *config.HealthConfig, shutdownConfig config.ShutdownConfig, underSystemd bool) error {
	d, err := newStandaloneDaemon(ctx, logToFileAndConsole, baseDir, standaloneDaemonConfig)
	if err != nil {
		return err
	}
	defer d.shutdown()

	if addr := os.Getenv("EOS_PPROF_ADDR"); addr != "" {
		go func() { _ = http.ListenAndServe(addr, nil) }() //nolint:gosec // addr is operator-controlled via env var
	}

	if underSystemd {
		err := d.recover()
		if err != nil {
			return err
		}
	}
	d.serve(healthConfig, shutdownConfig)

	d.logger.Log(logutil.LogLevelInfo, "daemon started successfully")

	d.wait()
	return nil
}

func bootPersistedServices(mgr *manager.LocalManager, logger *manager.DaemonLogger) error {
	allRegisteredServices, err := mgr.GetAllServiceCatalogEntries()
	if err != nil {
		errorMessage := fmt.Errorf("getting all service catalog entries: %w", err)
		logger.Log(logutil.LogLevelInfo, errorMessage.Error())
		return errorMessage
	}

	for _, service := range allRegisteredServices {
		_, err := mgr.StartService(service.Name)
		if err != nil {
			errorMessage := fmt.Errorf("starting service: %w", err)
			logger.Log(logutil.LogLevelInfo, errorMessage.Error())
			continue
		}
	}
	return nil
}

func (d *daemon) shutdown() {
	d.stop()
	if err := d.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		d.logger.Log(logutil.LogLevelError, fmt.Sprintf("closing listener: %v", err))
	}
	if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
		d.logger.Log(logutil.LogLevelError, fmt.Sprintf("removing pid file: %v", err))
	}
	if err := os.Remove(d.socketPath); err != nil && !os.IsNotExist(err) {
		d.logger.Log(logutil.LogLevelError, fmt.Sprintf("removing socket: %v", err))
	}
	if err := d.db.CloseDBConnection(); err != nil {
		d.logger.Log(logutil.LogLevelError, fmt.Sprintf("failed to close database: %v", err))
	}
}

func (d *daemon) recover() error {
	return bootPersistedServices(d.mgr, d.logger)
}

func (d *daemon) serve(healthConfig *config.HealthConfig, shutdownConfig config.ShutdownConfig) {
	go handleIncomingCommands(d.listener, d.mgr, d.logger)

	healthMonitor := monitor.NewHealthMonitor(d.mgr, d.db, d.logger, healthConfig, shutdownConfig)
	go healthMonitor.Start(d.ctx)
}

func newStandaloneDaemon(ctx context.Context, logToFileAndConsole bool, baseDir string, standaloneDaemonConfig *config.StandaloneDaemonConfig) (*daemon, error) {
	logger, err := manager.NewDaemonLogger(logToFileAndConsole, standaloneDaemonConfig.Log.LogDir, standaloneDaemonConfig.Log.LogFileName, standaloneDaemonConfig.Log.LogMaxFiles, standaloneDaemonConfig.Log.LogFileSizeLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to setup daemon logger: %w", err)
	}

	logger.Log(logutil.LogLevelInfo, "Started daemon logger")
	pidFile := standaloneDaemonConfig.PIDFile
	socketPath := standaloneDaemonConfig.SocketPath

	if _, pidFileStatErr := os.Stat(pidFile); pidFileStatErr == nil {
		data, _ := os.ReadFile(pidFile) // #nosec G304 -- path sanitized in config.NewDaemonConfig
		oldPid, _ := strconv.Atoi(string(data))

		if process, findProcessErr := os.FindProcess(oldPid); findProcessErr == nil {
			if process.Signal(syscall.Signal(0)) == nil {
				errorMessage := fmt.Errorf("daemon already running with PID %d", oldPid)
				logger.Log(logutil.LogLevelInfo, errorMessage.Error())
				return nil, errorMessage
			}
		}
		if pidRemoveErr := os.Remove(pidFile); pidRemoveErr != nil {
			errorMessage := fmt.Errorf("unable to remove the pid file, got: %w", pidRemoveErr)
			logger.Log(logutil.LogLevelError, errorMessage.Error())
			return nil, errorMessage
		}
	}

	myPID := os.Getpid()
	err = os.WriteFile(pidFile, fmt.Appendf(nil, "%d", myPID), 0600)
	if err != nil {
		errorMessage := fmt.Errorf("failed to write to pid file: %w", err)
		logger.Log(logutil.LogLevelInfo, errorMessage.Error())
		return nil, errorMessage
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGCHLD)

	if _, socketPathStatErr := os.Stat(socketPath); socketPathStatErr == nil {
		if socketPathRemoveErr := os.Remove(socketPath); socketPathRemoveErr != nil {
			errorMessage := fmt.Errorf("unable to remove the socket, got: %w", socketPathRemoveErr)
			logger.Log(logutil.LogLevelError, errorMessage.Error())
			return nil, errorMessage
		}
	}

	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		errorMessage := fmt.Errorf("failed to create socket: %w", err)
		logger.Log(logutil.LogLevelInfo, errorMessage.Error())
		return nil, errorMessage
	}

	db, err := database.NewDB(ctx, baseDir)
	if err != nil {
		errorMessage := fmt.Errorf("failed to connect to database: %w", err)
		logger.Log(logutil.LogLevelInfo, errorMessage.Error())
		return nil, errorMessage
	}

	mgr := manager.NewLocalManager(db, baseDir, ctx, logger)

	return &daemon{
		logger:     logger,
		db:         db,
		mgr:        mgr,
		listener:   listener,
		ctx:        ctx,
		stop:       stop,
		sigChan:    sigChan,
		pidFile:    pidFile,
		socketPath: socketPath,
	}, nil
}

func (d *daemon) wait() {
	for {
		select {
		case sig := <-d.sigChan:
			if sig == syscall.SIGCHLD {
				handleSIGCHLDRequest(d.ctx, d.db, d.logger)
			}
		case <-d.ctx.Done():
			return
		}
	}
}

func handleSIGCHLDRequest(ctx context.Context, db *database.DB, logger *manager.DaemonLogger) {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil {
			logger.Log(logutil.LogLevelError, fmt.Sprintf("cleaning up child process with PID '%d'\n: %v", pid, err))
			break
		}
		if pid == 0 {
			break
		}
		if pid < 0 {
			logger.Log(logutil.LogLevelError, fmt.Sprintf("cleaning up child process with PID '%d'", pid))
			continue
		}

		logger.Log(logutil.LogLevelInfo, fmt.Sprintf("reaped zombie process: %d\n", pid))

		// Check if the process group is still alive (children still running).
		// The reaped PID may be the PGID leader (shell), but actual service
		// processes can still be running in the same group.
		// If the group is alive, skip the state update. The health monitor
		// will handle ongoing liveness tracking.
		if pid > 1 && syscall.Kill(-pid, 0) == nil {
			logger.Log(logutil.LogLevelInfo, fmt.Sprintf("reaped process %d but process group still alive, skipping state update\n", pid))
			continue
		}

		if status.ExitStatus() == 0 {
			updates := database.ProcessHistoryUpdate{
				State:     new(types.ProcessStateStopped),
				StoppedAt: new(time.Now()),
			}
			updateErr := db.UpdateProcessHistoryEntry(ctx, pid, updates)
			if updateErr != nil {
				logger.Log(logutil.LogLevelError, fmt.Sprintf("updating the reaped process in the database: %v", updateErr))
			}
			continue
		}

		updates := database.ProcessHistoryUpdate{
			State:     new(types.ProcessStateFailed),
			StoppedAt: new(time.Now()),
			Error:     new("Zombie process has been reaped"),
		}

		err = db.UpdateProcessHistoryEntry(ctx, pid, updates)
		if err != nil {
			logger.Log(logutil.LogLevelError, fmt.Sprintf("updating the reaped process in the database: %v", err))
		}

		continue
	}
}

func StopStandaloneDaemon(pidFile, socketPath string) (bool, error) {
	_, err := os.Stat(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting stat info on pid of daemon: %w", err)
	}

	_, err = os.Stat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting stat info socket of daemon: %w", err)
	}

	data, readPidErr := os.ReadFile(pidFile) // #nosec G304 -- path sanitized in config.NewDaemonConfig
	if readPidErr != nil {
		return false, fmt.Errorf("reading pid file: %w", readPidErr)
	}

	activePid, err := strconv.Atoi(string(data))
	if err != nil {
		return false, fmt.Errorf("converting pid data to int: %w", err)
	}

	process, err := os.FindProcess(activePid)
	if err != nil {
		return false, fmt.Errorf("finding process matching the pid: %w", err)
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return false, fmt.Errorf("checking active daemon: %w", err)
	}

	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		return false, fmt.Errorf("killing active daemon: %w", err)
	}
	return true, nil
}

type DaemonStatus struct {
	Pid     *int
	Process *os.Process
	Running bool
}

func StatusStandaloneDaemon(daemonConfig *config.StandaloneDaemonConfig) (*DaemonStatus, error) {
	pidFile := filepath.Clean(daemonConfig.PIDFile)

	_, err := os.Stat(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &DaemonStatus{
				Running: false,
				Pid:     nil,
				Process: nil,
			}, nil
		}
		return nil, fmt.Errorf("describing pid file: %w", err)
	}

	data, readPidErr := os.ReadFile(pidFile)
	if readPidErr != nil {
		return nil, fmt.Errorf("reading pid file: %w", readPidErr)
	}

	activePid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("converting pid data to string: %w", err)
	}

	process, err := os.FindProcess(activePid)
	if err != nil {
		return nil, fmt.Errorf("finding process matching the pid: %w", err)
	}

	if err := process.Signal(syscall.Signal(0)); err != nil {
		// signal(0) failing means the process isn't alive - this is not an error
		// in the function's own operation, so we return a valid status with nil error
		return &DaemonStatus{Running: false, Pid: &activePid, Process: nil}, nil //lint:ignore nilerr intentional
	}

	return &DaemonStatus{
		Running: true,
		Pid:     &activePid,
		Process: process,
	}, nil
}

func RemoveStandaloneDaemon(daemonConfig *config.StandaloneDaemonConfig) (bool, error) {
	status, err := StatusStandaloneDaemon(daemonConfig)
	if err != nil {
		return false, err
	}
	if status.Running {
		return false, fmt.Errorf("standalone daemon is running; stop it before removing daemon files")
	}

	pidFile := daemonConfig.PIDFile
	socketPath := daemonConfig.SocketPath

	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("removing pid file: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("removing socket file: %w", err)
	}

	return true, nil
}

func handleIncomingCommands(listener net.Listener, mgr manager.ServiceManager, logger *manager.DaemonLogger) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logger.Log(logutil.LogLevelInfo, "listener closed, shutting down gracefully")
				return
			}
			logger.Log(logutil.LogLevelError, fmt.Sprintf("accepting the connection: %v", err))
			return
		}

		go handleConnection(conn, mgr, logger)
	}
}

func handleConnection(conn net.Conn, mgr manager.ServiceManager, logger *manager.DaemonLogger) {
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Log(logutil.LogLevelError, fmt.Sprintf("closing daemon socket: %v", err))
		}
	}()

	var request types.DaemonRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		sendErrorResponse(conn, fmt.Sprintf("decoding request: %v", err), logger)
		return
	}

	response := executeRequest(mgr, request)

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(response); err != nil {
		logger.Log(logutil.LogLevelError, fmt.Sprintf("sending response: %v\n", err))
	}
}

func executeRequest(mgr manager.ServiceManager, request types.DaemonRequest) types.DaemonResponse {
	switch request.Method {
	case types.MethodGetAllServiceInstances:
		result, err := mgr.GetAllServiceInstances()
		if err != nil {
			return sentinelErrorResponse(err)
		}
		if result == nil {
			result = []types.ServiceInstance{}
		}
		data, err := json.Marshal(types.GetAllServiceInstancesResponse{
			Instances: result,
		})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodGetServiceInstance:
		var args types.GetServiceInstanceArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodGetServiceInstance args: %v", err))
		}

		result, err := mgr.GetServiceInstance(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		if result == nil {
			return errorResponse("result returned nil")
		}
		data, err := json.Marshal(types.GetServiceInstanceResponse{
			Instance: *result,
		})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodRemoveServiceInstance:
		var args types.RemoveServiceInstanceArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodRemoveServiceInstance args: %v", err))
		}
		removed, err := mgr.RemoveServiceInstance(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(map[string]bool{"removed": removed})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{Success: true, Data: data}

	case types.MethodStartService:
		var args types.StartServiceArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodStartService args: %v", err))
		}
		pid, err := mgr.StartService(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(map[string]int{"pid": pid})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodRestartService:
		var args types.RestartServiceArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse("invalid MethodRestartService args")
		}
		gracePeriod, err := time.ParseDuration(args.GracePeriod)
		if err != nil {
			return errorResponse(fmt.Sprintf("invalid grace period: %s", args.GracePeriod))
		}
		tickerPeriod, err := time.ParseDuration(args.TickerPeriod)
		if err != nil {
			return errorResponse(fmt.Sprintf("invalid ticker period: %s", args.TickerPeriod))
		}
		pid, err := mgr.RestartService(args.Name, gracePeriod, tickerPeriod)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(map[string]int{"pid": pid})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodStopService:
		var args types.StopServiceArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse("invalid MethodStopService args")
		}
		gracePeriod, err := time.ParseDuration(args.GracePeriod)
		if err != nil {
			return errorResponse(fmt.Sprintf("invalid grace period: %s", args.GracePeriod))
		}
		tickerPeriod, err := time.ParseDuration(args.TickerPeriod)
		if err != nil {
			return errorResponse(fmt.Sprintf("invalid ticker period: %s", args.TickerPeriod))
		}
		result, err := mgr.StopService(args.Name, gracePeriod, tickerPeriod)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(result)

		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodForceStopService:
		var args types.ForceStopServiceArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodForceStopService args: %v", err))
		}
		result, err := mgr.ForceStopService(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodAddServiceCatalogEntry:
		var args types.AddServiceCatalogEntryArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodAddServiceCatalogEntry args: %v", err))
		}
		err := mgr.AddServiceCatalogEntry(args.Service)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		return types.DaemonResponse{Success: true}

	case types.MethodGetAllServiceCatalogEntries:
		result, err := mgr.GetAllServiceCatalogEntries()
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodGetServiceCatalogEntry:
		var args types.GetServiceCatalogEntryArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodGetServiceCatalogEntry args: %v", err))
		}
		result, err := mgr.GetServiceCatalogEntry(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodIsServiceRegistered:
		var args types.IsServiceRegisteredArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodIsServiceRegistered args: %v", err))
		}
		result, err := mgr.IsServiceRegistered(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(map[string]bool{"exists": result})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}
		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodRemoveServiceCatalogEntry:
		var args types.RemoveServiceCatalogEntryArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodRemoveServiceCatalogEntry args: %v", err))
		}
		removed, err := mgr.RemoveServiceCatalogEntry(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		data, err := json.Marshal(map[string]bool{"removed": removed})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}

		return types.DaemonResponse{Success: true, Data: data}

	case types.MethodUpdateServiceCatalogEntry:
		var args types.UpdateServiceCatalogEntryArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodUpdateServiceCatalogEntry args: %v", err))
		}
		err := mgr.UpdateServiceCatalogEntry(args.Name, args.NewDirectoryPath, args.NewConfigFileName)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		return types.DaemonResponse{Success: true}

	case types.MethodGetMostRecentProcessHistoryEntry:
		var args types.GetMostRecentProcessHistoryEntryArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodGetMostRecentProcessHistoryEntry args: %v", err))
		}
		result, err := mgr.GetMostRecentProcessHistoryEntry(args.Name)
		if err != nil {
			return sentinelErrorResponse(err)
		}
		if result == nil {
			return errorResponse("no process history entry found")
		}
		data, err := json.Marshal(types.GetMostRecentProcessHistoryEntryResponse{
			ProcessEntry: *result,
		})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}

		return types.DaemonResponse{
			Success: true,
			Data:    data,
		}

	case types.MethodNewServiceLogFiles:
		var args types.NewServiceLogFilesArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodNewServiceLogFiles args: %v", err))
		}

		logPath, errorLogPath, err := mgr.NewServiceLogFiles(args.ServiceName)
		if err != nil {
			return sentinelErrorResponse(err)
		}

		data, err := json.Marshal(map[string]string{"logPath": logPath, "errorLogPath": errorLogPath})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
		}

		return types.DaemonResponse{Success: true, Data: data}

	case types.MethodGetServiceLogFilePath:
		var args types.GetServiceLogFilePathArgs
		if err := json.Unmarshal(request.Args, &args); err != nil {
			return errorResponse(fmt.Sprintf("invalid MethodGetServiceLogFilePath args: %v", err))
		}

		filepath, err := mgr.GetServiceLogFilePath(args.ServiceName, args.ErrorLog)
		if err != nil {
			return sentinelErrorResponse(err)
		}

		data, err := json.Marshal(map[string]*string{"filepath": filepath})
		if err != nil {
			return errorResponse(fmt.Sprintf("marshaling response: %v", err))
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

func sentinelErrorResponse(err error) types.DaemonResponse {
	return types.DaemonResponse{
		Success:   false,
		Error:     err.Error(),
		ErrorCode: manager.ErrorCode(err),
	}
}

func sendErrorResponse(conn net.Conn, message string, logger *manager.DaemonLogger) {
	response := errorResponse(message)
	encoder := json.NewEncoder(conn)
	err := encoder.Encode(response)
	if err != nil {
		logger.Log(logutil.LogLevelError, fmt.Sprintf("sending error response: %v\n", err))
	}
}
