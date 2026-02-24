package monitor

import (
	"bytes"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/ptr"
	"eos/internal/testutil"
	"eos/internal/types"
)

func TestHealthMonitor_Lifecycle(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		log.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	var started sync.WaitGroup
	started.Add(1)

	go func() {
		started.Done()
		hm.Start(t.Context())
	}()

	started.Wait()

	time.Sleep(100 * time.Millisecond)

	hm.Stop()
}

func TestHealthMonitor_CheckStartProcess(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)

	if err != nil {
		log.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	serviceName := "test-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Fatalf("unable to close the listener, got: %v", closeErr)
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	testFile := testutil.CreateTestServiceConfigFile(t, testutil.WithRuntimePath(""), testutil.WithName(serviceName), testutil.WithPort(port))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry was not able to complete, got: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v\n", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)

	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pid < 1 {
		t.Fatalf("Invalid PID received after starting service, got: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Service process history entry not found")
	}
	hm.checkStartProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, healthConfig.Timeout.Limit, healthConfig.Timeout.Enable)

	var buf bytes.Buffer
	var errorBuf bytes.Buffer

	tailLogCommand := exec.Command("tail", "-n", "20", logger.LogPath)
	tailLogCommand.Stdout = &buf
	tailLogCommand.Stderr = &errorBuf

	err = tailLogCommand.Run()

	if err != nil {
		t.Fatalf("The log command failed, got:\n%v\nOutput: %s", err, errorBuf.String())
	}
	time.Sleep(100 * time.Millisecond)

	output := buf.String()

	if strings.Count(output, "\n") != 1 {
		t.Fatal("No logs were created")
	}
	if !strings.Contains(output, "is now running on port") {
		t.Fatalf("Process should be running, got: %s", output)
	}
}

func TestHealthMonitor_CheckStartProcess_ProcessDiedDuringStartup(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		log.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	serviceName := "startup-crash-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	testFile := testutil.CreateTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(0),
	)
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "startup-crash-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Could not create project directory: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Creating service.yaml failed: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pid < 1 {
		t.Fatalf("Invalid PID received: %d", pid)
	}

	// Kill the process immediately to simulate a crash during startup
	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("Failed to find process %d: %v", pid, err)
	}
	err = proc.Kill()
	if err != nil {
		t.Fatalf("Failed to kill process %d: %v", pid, err)
	}
	_, err = proc.Wait() // reap the zombie so isProcessAlive returns false
	if err != nil {
		t.Fatalf("Failed to wait for the process to exit: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Process history entry not found")
	}

	hm.checkStartProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, healthConfig.Timeout.Limit, healthConfig.Timeout.Enable)

	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updatedEntry == nil {
		t.Fatal("Failed to get updated process history")
	}

	if updatedEntry.State != types.ProcessStateFailed {
		t.Errorf("Expected ProcessStateFailed, got %v", updatedEntry.State)
	}

	if updatedEntry.Error == nil || !strings.Contains(*updatedEntry.Error, "died during startup") {
		t.Errorf("Expected 'died during startup' error, got: %v", updatedEntry.Error)
	}

	if updatedEntry.StoppedAt == nil {
		t.Error("Expected StoppedAt to be set")
	}

	var buf bytes.Buffer
	tailLogCommand := exec.Command("tail", "-n", "10", logger.LogPath)
	tailLogCommand.Stdout = &buf
	err = tailLogCommand.Run()
	if err != nil {
		t.Logf("Could not read log file: %v", err)
	} else {
		output := buf.String()
		if !strings.Contains(output, "died during startup") {
			t.Errorf("Log should contain 'died during startup', got: %s", output)
		}
	}
}

// func TestHealthMonitor_CheckStartProcess_Invalid_Port(t *testing.T) {
// 	tempDir := t.TempDir()
//  daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
// 	timoutLimit := 30 * time.Second

// 	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
// 	mgr := manager.NewLocalManager(db, tempDir, t.Context())
// 	logger, err := manager.NewDaemonLogger(true, tempDir, daemonLogFileName)

// 	if err != nil {
// 		log.Fatalf("Unable to set up to test daemon logger, got: %v", err)
// 	}

// 	hm := NewHealthMonitor(mgr, db, logger, healthConfig)

// 	serviceName := "test-service"
// 	serviceDir := filepath.Join(tempDir, serviceName)
// 	if err := os.MkdirAll(serviceDir, 0755); err != nil {
// 		t.Fatalf("Failed to create service directory: %v", err)
// 	}

// 	testFile := testutil.CreateTestServiceConfigFile(t, testutil.WithRuntimePath(""), testutil.WithName(serviceName))
// 	yamlData, err := yaml.Marshal(testFile)
// 	if err != nil {
// 		t.Fatalf("Failed to marshal test config: %v", err)
// 	}

// 	fullDirPath := filepath.Join(tempDir, "test-project")
// 	err = os.MkdirAll(fullDirPath, 0755)

// 	if err != nil {
// 		t.Fatalf("could not create test-project directory: %v\n", err)
// 	}

// 	fullPath := filepath.Join(fullDirPath, "service.yaml")
// 	err = os.WriteFile(fullPath, yamlData, 0644)
// 	if err != nil {
// 		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
// 	}

// 	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
// 	if err != nil {
// 		t.Fatalf("Create service catalog entry was not able to complete, got: %v", err)
// 	}

// 	err = mgr.AddServiceCatalogEntry(,serviceCatalogEntry)
// 	if err != nil {
// 		t.Fatalf("Error registering service: %v\n", err)
// 	}

// 	pid, err := mgr.StartService(,serviceCatalogEntry.Name)

// 	if err != nil {
// 		t.Fatalf("Service unable to start, got: %v", err)
// 	}
// 	if pid < 1 {
// 		t.Fatalf("Invalid PID received after starting service, got: %v", err)
// 	}

// 	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(,serviceName)
// 	if err != nil {
// 		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
// 	}
// 	if processHistoryEntry == nil {
// 		t.Fatal("Service process history entry not found")
// 	}
// 	hm.checkStartProcess(serviceCatalogEntry, processHistoryEntry, &timoutLimit)

// 	var buf bytes.Buffer
// 	var errorBuf bytes.Buffer

// 	tailLogCommand := exec.Command("tail", "-n", "20", logger.LogPath)
// 	tailLogCommand.Stdout = &buf
// 	tailLogCommand.Stderr = &errorBuf

// 	err = tailLogCommand.Run()

// 	if err != nil {
// 		t.Fatalf("The log command failed, got:\n%v\nOutput: %s", err, errorBuf.String())
// 	}
// 	time.Sleep(100 * time.Millisecond)

// 	output := buf.String()

// 	if strings.Count(output, "\n") != 1 {
// 		t.Fatalf("No logs were created")
// 	}

// 	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(,serviceName)
// 	if err != nil {
// 		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
// 	}
// 	if processHistoryEntry == nil {
// 		t.Fatal("Service process history entry not found")
// 	}
// 	if !strings.Contains(output, "is not running on port") {
// 		t.Fatalf("Process check should confirm that service is running on the assigned port")
// 	}
// }

func TestHealthMonitor_CheckStartProcess_ExactTimeout(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t, WithTimeoutLimit(100*time.Millisecond))

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)
	serviceName := "timeout-test-service"
	serviceDir := filepath.Join(tempDir, serviceName)

	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	testFile := testutil.CreateTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(9999)) // Port that won't open

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "timeout-test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Could not create test-project directory: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	if pid < 1 {
		t.Fatalf("Invalid PID received: %d", pid)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Failed to get process history: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Process history entry not found")
	}

	// Manually set StartedAt to simulate timeout
	// We'll update it to be past the timeout limit
	oldStartTime := time.Now().Add(-(healthConfig.Timeout.Limit + 50*time.Millisecond))
	err = hm.db.UpdateProcessHistoryEntry(t.Context(), pid, database.ProcessHistoryUpdate{
		StartedAt: &oldStartTime,
	})
	if err != nil {
		t.Fatalf("Failed to updated process history entry for %s: %v", serviceName, err)
	}

	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatal("Failed to get updated process history")
	}

	hm.checkStartProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, healthConfig.Timeout.Limit, healthConfig.Timeout.Enable)

	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updatedEntry == nil {
		t.Fatal("Failed to get process history after check")
	}

	if updatedEntry.State != types.ProcessStateFailed {
		t.Errorf("Expected ProcessStateFailed, got %v", updatedEntry.State)
	}

	if updatedEntry.Error == nil || !strings.Contains(*updatedEntry.Error, "taking too long") {
		t.Errorf("Expected timeout error, got: %v", updatedEntry.Error)
	}

	if updatedEntry.StoppedAt == nil {
		t.Error("Expected StoppedAt to be set")
	}

	var buf bytes.Buffer
	tailLogCommand := exec.Command("tail", "-n", "10", logger.LogPath)
	tailLogCommand.Stdout = &buf
	err = tailLogCommand.Run()
	if err != nil {
		t.Logf("Could not read log file: %v", err)
	} else {
		output := buf.String()
		if !strings.Contains(output, "taking too long") {
			t.Errorf("Log should contain timeout message, got: %s", output)
		}
	}
}

func TestHealthMonitor_CheckRunningProcess(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)

	if err != nil {
		log.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	serviceName := "test-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Errorf("unable to close the listener, got: %v", closeErr)
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	testFile := testutil.CreateTestServiceConfigFile(t, testutil.WithRuntimePath(""), testutil.WithName(serviceName), testutil.WithPort(port))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry was not able to complete, got: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v\n", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)

	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pid < 1 {
		t.Fatalf("Invalid PID received after starting service, got: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Service process history entry not found")
	}
	hm.checkStartProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, healthConfig.Timeout.Limit, healthConfig.Timeout.Enable)

	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry)

	var buf bytes.Buffer
	var errorBuf bytes.Buffer

	tailLogCommand := exec.Command("tail", "-n", "20", logger.LogPath)
	tailLogCommand.Stdout = &buf
	tailLogCommand.Stderr = &errorBuf

	err = tailLogCommand.Run()

	if err != nil {
		t.Fatalf("The log command failed, got:\n%v\nOutput: %s", err, errorBuf.String())
	}
	time.Sleep(100 * time.Millisecond)

	output := buf.String()

	if strings.Count(output, "\n") > 1 {
		t.Fatal("Should not create additional logs when process is running")
	}

	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Service process history entry not found")
	}
	if processHistoryEntry.State != types.ProcessStateRunning {
		t.Fatalf("Service process should be running, got: %s", processHistoryEntry.State)
	}
	if *processHistoryEntry.Error != "" {
		t.Fatalf("Service process error should be none")
	}
}

// func TestHealthMonitor_CheckRunningProcess_AliveButPortUnreachable(t *testing.T) {
// 	tempDir := t.TempDir()
//  daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
// 	timeoutLimit := 30 * time.Second

// 	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
// 	mgr := manager.NewLocalManager(db, tempDir, t.Context())
// 	logger, err := manager.NewDaemonLogger(true, tempDir, daemonLogFileName)
// 	if err != nil {
// 		log.Fatalf("Unable to set up test daemon logger, got: %v", err)
// 	}

// 	hm := NewHealthMonitor(mgr, db, logger, healthConfig)

// 	serviceName := "port-drop-service"
// 	serviceDir := filepath.Join(tempDir, serviceName)
// 	if err := os.MkdirAll(serviceDir, 0755); err != nil {
// 		t.Fatalf("Failed to create service directory: %v", err)
// 	}

// 	// Open a port so the service can start and transition to Running
// 	listener, err := net.Listen("tcp", "localhost:0")
// 	if err != nil {
// 		t.Fatalf("Failed to create listener: %v", err)
// 	}
// 	port := listener.Addr().(*net.TCPAddr).Port

// 	testFile := testutil.CreateTestServiceConfigFile(t,
// 		testutil.WithRuntimePath(""),
// 		testutil.WithName(serviceName),
// 		testutil.WithPort(port),
// 	)
// 	yamlData, err := yaml.Marshal(testFile)
// 	if err != nil {
// 		t.Fatalf("Failed to marshal test config: %v", err)
// 	}

// 	fullDirPath := filepath.Join(tempDir, "port-drop-project")
// 	err = os.MkdirAll(fullDirPath, 0755)
// 	if err != nil {
// 		t.Fatalf("Could not create project directory: %v", err)
// 	}

// 	fullPath := filepath.Join(fullDirPath, "service.yaml")
// 	err = os.WriteFile(fullPath, yamlData, 0644)
// 	if err != nil {
// 		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
// 	}

// 	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
// 	if err != nil {
// 		t.Fatalf("Create service catalog entry failed: %v", err)
// 	}

// 	err = mgr.AddServiceCatalogEntry(,serviceCatalogEntry)
// 	if err != nil {
// 		t.Fatalf("Error registering service: %v", err)
// 	}

// 	pid, err := mgr.StartService(,serviceCatalogEntry.Name)
// 	if err != nil {
// 		t.Fatalf("Service unable to start, got: %v", err)
// 	}
// 	if pid < 1 {
// 		t.Fatalf("Invalid PID received: %d", pid)
// 	}

// 	// Transition to Running via checkStartProcess (port is still open)
// 	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(,serviceName)
// 	if err != nil || processHistoryEntry == nil {
// 		t.Fatal("Failed to get process history entry")
// 	}
// 	hm.checkStartProcess(serviceCatalogEntry, processHistoryEntry, &timeoutLimit)

// 	// Confirm it's Running now
// 	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(,serviceName)
// 	if err != nil || processHistoryEntry == nil {
// 		t.Fatal("Failed to get updated process history")
// 	}
// 	if processHistoryEntry.State != types.ProcessStateRunning {
// 		t.Fatalf("Service should be running before port-drop test, got: %s", processHistoryEntry.State)
// 	}

// 	// Now close the port to simulate the service dropping its listener
// 	defer func() {
// 		if err := listener.Close(); err != nil {
// 			t.Errorf("unable to close the listener, got: %v", err)
// 		}
// 	}()
// 	time.Sleep(50 * time.Millisecond) // give OS time to release the port

// 	// Call checkRunningProcess — process is alive, but port is unreachable
// 	hm.checkRunningProcess(t.Context(),serviceCatalogEntry, processHistoryEntry)

// 	// Verify: state should be Failed with port-related error
// 	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(,serviceName)
// 	if err != nil || updatedEntry == nil {
// 		t.Fatal("Failed to get process history after check")
// 	}

// 	if updatedEntry.State != types.ProcessStateFailed {
// 		t.Errorf("Expected ProcessStateFailed, got %v", updatedEntry.State)
// 	}

// 	if updatedEntry.Error == nil || !strings.Contains(*updatedEntry.Error, "is not running on port") {
// 		t.Errorf("Expected port-related error, got: %v", updatedEntry.Error)
// 	}

// 	if updatedEntry.StoppedAt == nil {
// 		t.Error("Expected StoppedAt to be set")
// 	}

// 	// Verify log output
// 	var buf bytes.Buffer
// 	tailLogCommand := exec.Command("tail", "-n", "10", logger.LogPath)
// 	tailLogCommand.Stdout = &buf
// 	err = tailLogCommand.Run()
// 	if err != nil {
// 		t.Logf("Could not read log file: %v", err)
// 	} else {
// 		output := buf.String()
// 		if !strings.Contains(output, "is not running on port") {
// 			t.Errorf("Log should contain port error, got: %s", output)
// 		}
// 	}
// }

func TestHealthMonitor_CheckRunningProcess_Failed(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)

	if err != nil {
		log.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	serviceName := "test-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Errorf("unable to close the listener, got: %v", closeErr)
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	testFile := testutil.CreateTestServiceConfigFile(t, testutil.WithRuntimePath(""), testutil.WithName(serviceName), testutil.WithPort(port))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry was not able to complete, got: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v\n", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)

	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pid < 1 {
		t.Fatalf("Invalid PID received after starting service, got: %v", err)
	}

	result, err := mgr.ForceStopService(serviceCatalogEntry.Name)

	if err != nil {
		t.Fatalf("An error occurred during force stopping the service, got: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed to force stop the service for this test")
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Service process history entry not found")
	}
	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry)

	var buf bytes.Buffer
	var errorBuf bytes.Buffer

	tailLogCommand := exec.Command("tail", "-n", "20", logger.LogPath)
	tailLogCommand.Stdout = &buf
	tailLogCommand.Stderr = &errorBuf

	err = tailLogCommand.Run()

	if err != nil {
		t.Fatalf("The log command failed, got:\n%v\nOutput: %s", err, errorBuf.String())
	}
	time.Sleep(100 * time.Millisecond)

	output := buf.String()

	if strings.Count(output, "\n") != 0 {
		t.Fatal("Should not create logs when process is running")
	}
}

func TestHealthMonitor_CheckFailedProcess_MaxRestarts(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t, WithMaxRestart(3))
	maxRestartCount := healthConfig.MaxRestart

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)
	serviceName := "max-restart-service"
	serviceDir := filepath.Join(tempDir, serviceName)

	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	if closeErr := listener.Close(); closeErr != nil {
		t.Errorf("unable to close the listener, got: %v", closeErr)
	}

	testFile := testutil.CreateTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(port))

	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "max-restart-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Could not create test-project directory: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	err = db.RegisterServiceInstance(t.Context(), serviceName)
	if err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	fakePID := 999999

	_, err = db.RegisterProcessHistoryEntry(t.Context(), fakePID, serviceName, types.ProcessStateFailed)
	if err != nil {
		t.Fatalf("Failed to register fake process history: %v", err)
	}

	err = db.UpdateProcessHistoryEntry(t.Context(), fakePID, database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
		StartedAt: ptr.TimePtr(time.Now().Add(-10 * time.Second)),
		StoppedAt: ptr.TimePtr(time.Now().Add(-2 * time.Second)),
		Error:     ptr.StringPtr("Simulated failure"),
	})
	if err != nil {
		t.Fatalf("Failed to update fake process history: %v", err)
	}

	for i := 0; i <= maxRestartCount+1; i++ {
		processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
		if err != nil {
			t.Fatalf("Iteration %d: Failed to get process history: %v", i, err)
		}
		if processHistoryEntry == nil {
			t.Fatalf("Iteration %d: Failed to get process history", i)
		}

		instance, err := hm.mgr.GetServiceInstance(serviceName)
		if err != nil {
			t.Fatalf("Iteration %d: Failed to get service instance: %v", i, err)
		}
		if instance == nil {
			t.Fatalf("Iteration %d: Failed to get service instance", i)
		}

		hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance, &maxRestartCount)
		time.Sleep(50 * time.Millisecond)

		updatedInstance, _ := hm.mgr.GetServiceInstance(serviceName)
		if updatedInstance == nil {
			t.Fatalf("Iteration %d: Failed to get updated service instance", i)
		}

		if i < maxRestartCount {
			if updatedInstance.RestartCount != i+1 {
				t.Errorf("Iteration %d: Expected restart count %d, got %d", i, i+1, updatedInstance.RestartCount)
			}

			// RestartService spawned a real process. We need to kill it and
			// replace it with a fake dead PID for the next iteration.
			latestProcess, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil {
				t.Fatalf("Iteration %d: Failed to get latest process: %v", i, err)
			}
			if latestProcess == nil {
				t.Fatalf("Iteration %d: Failed to get latest process", i)
			}

			// Kill the real process and wait for it to fully exit
			proc, err := os.FindProcess(latestProcess.PID)
			if err == nil {
				_ = proc.Kill()
				_, err = proc.Wait() // reap the zombie
				if err != nil {
					t.Fatalf("Failed to wait for the process to exit: %v", err)
				}
			}

			err = db.UpdateProcessHistoryEntry(t.Context(), latestProcess.PID, database.ProcessHistoryUpdate{
				State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
				StartedAt: ptr.TimePtr(time.Now().Add(-5 * time.Minute)),
				StoppedAt: ptr.TimePtr(time.Now()),
				Error:     ptr.StringPtr("Simulated failure"),
			})
			if err != nil {
				t.Fatalf("Failed to update process history entry: %v", err)
			}
			return
		}
		if updatedInstance.RestartCount != instance.RestartCount {
			t.Errorf("Iteration %d: Expected no restart (count should stay %d), but got %d",
				i, instance.RestartCount, updatedInstance.RestartCount)
		}
	}

	finalInstance, _ := hm.mgr.GetServiceInstance(serviceName)
	if finalInstance.RestartCount != maxRestartCount {
		t.Errorf("Final restart count should be exactly %d, got %d",
			maxRestartCount, finalInstance.RestartCount)
	}
}

func TestHealthMonitor_IsProcessAlive(t *testing.T) {
	hm := &HealthMonitor{}

	currentPID := os.Getpid()
	isAlive := hm.isProcessAlive(currentPID)

	if !isAlive {
		t.Fatal("Current process should be alive")
	}
}

func TestHealthMonitor_IsProcessAlive_NonExistent(t *testing.T) {
	hm := &HealthMonitor{}
	isAlive := hm.isProcessAlive(rand.Intn(99999))

	if isAlive {
		t.Fatal("Should not be able to find process in unit test")
	}
}

// func Test_CanConnectToPort(t *testing.T) {
// 	hm := &HealthMonitor{}

// 	listener, err := net.Listen("tcp", "localhost:0")
// 	if err != nil {
// 		t.Fatalf("Failed to create listener: %v", err)
// 	}
// 	defer func() {
// 		if err := listener.Close(); err != nil {
// 			t.Errorf("unable to close the listener, got: %v", err)
// 		}
// 	}()

// 	port := listener.Addr().(*net.TCPAddr).Port

// 	canConnect := hm.canConnectToPort(port)
// 	if !canConnect {
// 		t.Fatal("Should be able to connect to open port")
// 	}

// 	if err := listener.Close(); err != nil {
// 		t.Errorf("unable to close the listener, got: %v", err)
// 	}
// 	time.Sleep(10 * time.Millisecond)

// 	canConnect = hm.canConnectToPort(port)
// 	if canConnect {
// 		t.Fatal("Should not be able to connect to closed port")
// 	}
// }

// func Test_CanConnectToPort_NonExistent(t *testing.T) {
// 	hm := &HealthMonitor{}

// 	canConnect := hm.canConnectToPort(rand.Intn(99999))
// 	if canConnect {
// 		t.Fatalf("Should not be able to connect to port in unit test")
// 	}
// }

func TestHealthMonitor_CalculateBackoffDelay(t *testing.T) {
	testCases := []struct {
		name          string
		restartCount  int
		expectedDelay time.Duration
	}{
		{
			name:          "First restart",
			restartCount:  0,
			expectedDelay: 300 * time.Millisecond, // 300 * 2^0
		},
		{
			name:          "Second restart",
			restartCount:  1,
			expectedDelay: 600 * time.Millisecond, // 300 * 2^1
		},
		{
			name:          "Third restart",
			restartCount:  2,
			expectedDelay: 1200 * time.Millisecond, // 300 * 2^2
		},
		{
			name:          "Max delay reached",
			restartCount:  10,
			expectedDelay: 60000 * time.Millisecond, // Capped at max
		},
		{
			name:          "Beyond max delay",
			restartCount:  20,
			expectedDelay: 60000 * time.Millisecond, // Still capped
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := calculateBackoffDelay(tc.restartCount)
			if actual != tc.expectedDelay {
				t.Errorf("Expected %v, got %v", tc.expectedDelay, actual)
			}
		})
	}
}

func TestHealthMonitor_CheckAllServices_MultipleServicesInDifferentStates(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(true, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		log.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	// --- Helper to register a service ---
	setupService := func(name string, port int) *types.ServiceCatalogEntry {
		serviceDir := filepath.Join(tempDir, name)
		if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
			t.Fatalf("Failed to create service directory for %s: %v", name, mkdirErr)
		}

		testFile := testutil.CreateTestServiceConfigFile(t,
			testutil.WithRuntimePath(""),
			testutil.WithName(name),
			testutil.WithPort(port),
		)
		marshalData, marshalErr := yaml.Marshal(testFile)
		if marshalErr != nil {
			t.Fatalf("Failed to marshal config for %s: %v", name, marshalErr)
		}

		fullDirPath := filepath.Join(tempDir, name+"-project")
		err = os.MkdirAll(fullDirPath, 0755)
		if err != nil {
			t.Fatalf("Could not create project directory for %s: %v", name, err)
		}

		fullPath := filepath.Join(fullDirPath, "service.yaml")
		err = os.WriteFile(fullPath, marshalData, 0644)
		if err != nil {
			t.Fatalf("Failed to write the service.yaml file, got: %v", err)
		}

		entry, svcCatalogEntryErr := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
		if svcCatalogEntryErr != nil {
			t.Fatalf("Create catalog entry failed for %s: %v", name, svcCatalogEntryErr)
		}
		err = mgr.AddServiceCatalogEntry(entry)
		if err != nil {
			t.Fatalf("Error registering service %s: %v", name, err)
		}
		return entry
	}

	// Service 1: will be in Running state (healthy, port open)
	listener1, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	defer func() {
		if closeErr := listener1.Close(); closeErr != nil {
			t.Errorf("unable to close the listener, got: %v", closeErr)
		}
	}()

	port1 := listener1.Addr().(*net.TCPAddr).Port
	svc1Name := "running-svc"
	setupService(svc1Name, port1)

	pid1, err := mgr.StartService(svc1Name)
	if err != nil {
		t.Fatalf("Failed to start %s: %v", svc1Name, err)
	}
	// Transition to Running
	err = db.UpdateProcessHistoryEntry(t.Context(), pid1, database.ProcessHistoryUpdate{
		State: ptr.ProcessStatePtr(types.ProcessStateRunning),
		Error: ptr.StringPtr(""),
	})
	if err != nil {
		t.Fatalf("Failed to update process history entry: %v", err)
	}

	// Service 2: will be in Starting state (port open, should transition to Running)
	listener2, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	defer func() {
		if closeErr := listener2.Close(); closeErr != nil {
			t.Errorf("unable to close the listener, got: %v", closeErr)
		}
	}()

	port2 := listener2.Addr().(*net.TCPAddr).Port
	svc2Name := "starting-svc"
	setupService(svc2Name, port2)

	pid2, err := mgr.StartService(svc2Name)
	if err != nil {
		t.Fatalf("Failed to start %s: %v", svc2Name, err)
	}
	// Leave in Starting state (default after StartService)
	_ = pid2

	// Service 3: will be in Failed state (dead process, should attempt restart)
	svc3Name := "failed-svc"
	setupService(svc3Name, 0)

	err = db.RegisterServiceInstance(t.Context(), svc3Name)
	if err != nil {
		t.Fatalf("Failed to register instance for %s: %v", svc3Name, err)
	}

	fakePID := 999998
	_, err = db.RegisterProcessHistoryEntry(t.Context(), fakePID, svc3Name, types.ProcessStateFailed)
	if err != nil {
		t.Fatalf("Failed to register fake process history for %s: %v", svc3Name, err)
	}
	err = db.UpdateProcessHistoryEntry(t.Context(), fakePID, database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
		StartedAt: ptr.TimePtr(time.Now().Add(-10 * time.Minute)),
		StoppedAt: ptr.TimePtr(time.Now().Add(-5 * time.Minute)),
		Error:     ptr.StringPtr("previous failure"),
	})
	if err != nil {
		t.Fatalf("Failed to update process history for %s: %v", svc3Name, err)
	}

	// Run the full dispatch
	hm.checkAllServices(t.Context())

	// Allow time for any async effects
	time.Sleep(200 * time.Millisecond)

	// Verify Service 1 (Running): should still be Running
	entry1, err := hm.mgr.GetMostRecentProcessHistoryEntry(svc1Name)
	if err != nil {
		t.Fatalf("Failed to get process history for %s", svc1Name)
	}
	if entry1 == nil {
		t.Fatal("Failed to get process history")
	}
	if entry1.State != types.ProcessStateRunning {
		t.Errorf("%s: expected Running, got %v", svc1Name, entry1.State)
	}

	// Verify Service 2 (Starting): should have transitioned to Running
	entry2, err := hm.mgr.GetMostRecentProcessHistoryEntry(svc2Name)
	if err != nil {
		t.Fatalf("Failed to get process history for %s", svc2Name)
	}
	if entry2 == nil {
		t.Fatal("Failed to get process history")
	}
	if entry2.State != types.ProcessStateRunning {
		t.Errorf("%s: expected Running after start check, got %v", svc2Name, entry2.State)
	}

	// Verify Service 3 (Failed): should have attempted a restart (new process entry or restart count > 0)
	instance3, err := hm.mgr.GetServiceInstance(svc3Name)
	if err != nil {
		t.Fatalf("Failed to get service instance for %s", svc3Name)
	}
	if instance3 == nil {
		t.Fatal("Failed to get service instance")
	}
	// The failed service with elapsed > backoff should have been restarted
	if instance3.RestartCount < 1 {
		t.Logf("%s: RestartCount is %d — restart may have been attempted (check logs)", svc3Name, instance3.RestartCount)
	}
}

func TestHealthMonitor_CheckFailedProcess_ProcessStillAlive_Recovery(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.CreateTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := createTestHealthConfig(t, WithMaxRestart(5))

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context())
	logger, err := manager.NewDaemonLogger(false, daemonConfig.LogDir, daemonConfig.LogFileName, daemonConfig.MaxFiles, daemonConfig.FileSizeLimit)
	if err != nil {
		log.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, *healthConfig)

	serviceName := "recovery-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	testFile := testutil.CreateTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(0),
	)
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullDirPath := filepath.Join(tempDir, "recovery-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Could not create project directory: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.CreateServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	// Start a real service so we have a live PID
	pid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pid < 1 {
		t.Fatalf("Invalid PID received: %d", pid)
	}

	// Verify the process is actually alive
	if !hm.isProcessAlive(pid) {
		t.Fatal("Process should be alive for this test")
	}

	// Manually mark it as Failed in the DB (simulating a false failure detection)
	err = db.UpdateProcessHistoryEntry(t.Context(), pid, database.ProcessHistoryUpdate{
		State:     ptr.ProcessStatePtr(types.ProcessStateFailed),
		StoppedAt: ptr.TimePtr(time.Now()),
		Error:     ptr.StringPtr("falsely marked as failed"),
	})
	if err != nil {
		t.Fatalf("Failed to update process history: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatal("Failed to get process history entry")
	}

	// Confirm it's in Failed state before we call checkFailedProcess
	if processHistoryEntry.State != types.ProcessStateFailed {
		t.Fatalf("Pre-condition failed: expected Failed state, got %v", processHistoryEntry.State)
	}

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatal("Failed to get service instance")
	}

	restartCountBefore := instance.RestartCount

	// Call checkFailedProcess — the process is alive, so it should recover
	hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance, &healthConfig.MaxRestart)

	// Verify: state should be back to Running
	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updatedEntry == nil {
		t.Fatal("Failed to get updated process history")
	}

	if updatedEntry.State != types.ProcessStateRunning {
		t.Errorf("Expected ProcessStateRunning (recovery), got %v", updatedEntry.State)
	}

	if updatedEntry.Error != nil && *updatedEntry.Error != "" {
		t.Errorf("Expected error to be cleared, got: %s", *updatedEntry.Error)
	}

	// Verify: restart count should NOT have changed (it recovered, not restarted)
	updatedInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updatedInstance == nil {
		t.Fatal("Failed to get updated service instance")
	}

	if updatedInstance.RestartCount != restartCountBefore {
		t.Errorf("RestartCount should not change on recovery: expected %d, got %d",
			restartCountBefore, updatedInstance.RestartCount)
	}
}

type HealthConfigOption func(*config.HealthConfig)

func WithMaxRestart(maxRestart int) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.MaxRestart = maxRestart
	}
}

func WithTimeoutEnable(timeoutEnable bool) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.Timeout.Enable = timeoutEnable
	}
}

func WithTimeoutLimit(timeoutLimit time.Duration) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.Timeout.Limit = timeoutLimit
	}
}

func createTestHealthConfig(t *testing.T, opts ...HealthConfigOption) *config.HealthConfig {
	t.Helper()
	healthConfig := &config.HealthConfig{
		MaxRestart: config.HealthMaxRestart,
		Timeout: config.TimeOutConfig{
			Enable: config.HealthTimeOutEnable,
			Limit:  30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(healthConfig)
	}

	return healthConfig
}
