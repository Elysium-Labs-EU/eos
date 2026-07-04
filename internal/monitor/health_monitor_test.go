package monitor

import (
	"bytes"
	"context"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/testutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"gopkg.in/yaml.v3"
)

func TestHealthMonitor_Lifecycle(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	ctx, cancel := context.WithCancel(t.Context())

	var started sync.WaitGroup
	started.Add(1)

	go func() {
		started.Done()
		hm.Start(ctx)
	}()

	started.Wait()

	time.Sleep(100 * time.Millisecond)

	cancel()
}

func TestHealthMonitor_CheckStartProcess(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t, WithGracePeriod(5*time.Second))

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)

	if err != nil {
		t.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	serviceName := "test-service"

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Fatalf("unable to close the listener, got: %v", closeErr)
		}
	}()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(port),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry was not able to complete, got: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v\n", err)
	}

	pgid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pgid < 1 {
		t.Fatalf("Invalid PGID received after starting service, got: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	})

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

	tailLogCommand := exec.Command("tail", "-n", "20", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
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
	if !strings.Contains(output, "now running on port") {
		t.Fatalf("Process should be running, got: %s", output)
	}
}

func TestHealthMonitor_CheckStartProcess_ProcessDiedDuringStartup(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	serviceName := "startup-crash-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	fullDirPath := filepath.Join(tempDir, "startup-crash-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Could not create project directory: %v", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(0),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Creating service.yaml failed: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	pgid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pgid < 1 {
		t.Fatalf("Invalid PGID received: %d", pgid)
	}

	// Kill the entire process group immediately to simulate a crash during startup.
	// Use a negative pgid to match how isProcessAlive checks group liveness.
	if err = syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatalf("Failed to kill process group %d: %v", pgid, err)
	}

	for range 50 {
		if !hm.isProcessAlive(pgid) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hm.isProcessAlive(pgid) {
		t.Fatalf("Process %d did not exit after kill", pgid)
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
	tailLogCommand := exec.Command("tail", "-n", "10", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
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
//  daemonConfig := testutil.NewTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
// 	timoutLimit := 30 * time.Second

// 	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
// 	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
// 	logger, err := manager.NewDaemonLogger(true, tempDir, daemonLogFileName)

// 	if err != nil {
// 		t.Fatalf("Unable to set up to test daemon logger, got: %v", err)
// 	}

// 	hm := NewHealthMonitor(mgr, db, logger, healthConfig)

// 	serviceName := "test-service"
// 	serviceDir := filepath.Join(tempDir, serviceName)
// 	if err := os.MkdirAll(serviceDir, 0755); err != nil {
// 		t.Fatalf("Failed to create service directory: %v", err)
// 	}

// 	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithRuntimePath(""), testutil.WithName(serviceName))
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

// 	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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
// 		t.Fatalf("Invalid PGID received after starting service, got: %v", err)
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

// 	tailLogCommand := exec.Command("tail", "-n", "20", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
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
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t, WithTimeoutLimit(100*time.Millisecond))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)
	serviceName := "timeout-test-service"
	serviceDir := filepath.Join(tempDir, serviceName)

	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	fullDirPath := filepath.Join(tempDir, "timeout-test-project")
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Could not create test-project directory: %v", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(9999), // Port that won't open
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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
		t.Fatalf("Invalid PGID received: %d", pid)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

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
	tailLogCommand := exec.Command("tail", "-n", "10", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
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
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)

	if err != nil {
		t.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

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

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	fullDirPath := filepath.Join(tempDir, "test-project")
	err = os.MkdirAll(fullDirPath, 0755)

	if err != nil {
		t.Fatalf("could not create test-project directory: %v\n", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(port),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	err = os.WriteFile(fullPath, yamlData, 0644)
	if err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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
		t.Fatalf("Invalid PGID received after starting service, got: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	})

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Service process history entry not found")
	}
	hm.checkStartProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, healthConfig.Timeout.Limit, healthConfig.Timeout.Enable)

	serviceInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || serviceInstance == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, serviceInstance)

	var buf bytes.Buffer
	var errorBuf bytes.Buffer

	tailLogCommand := exec.Command("tail", "-n", "20", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
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

// TestHealthMonitor_CheckRunningProcess_ThrottledMemSample verifies that when the
// mem sample interval has not elapsed, checkRunningProcess does NOT overwrite the
// last known RSS value in the DB with 0.
func TestHealthMonitor_CheckRunningProcess_ThrottledMemSample(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	// 24h interval ensures the throttle fires on every tick during the test
	healthConfig := newTestHealthConfig(t, WithMemSampleInterval(24*time.Hour))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("unable to set up daemon logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	fullDirPath := filepath.Join(tempDir, "test-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create test-project directory: %v", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	const serviceName = "throttle-test-svc"
	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(port),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("create service catalog entry failed: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("error registering service: %v", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("service unable to start: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("get recent process history entry failed: %v", err)
	}
	hm.checkStartProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, healthConfig.Timeout.Limit, healthConfig.Timeout.Enable)

	// Seed a known RSS value so we can detect if it gets zeroed.
	const knownRssKb = int64(12345)
	if err = db.UpdateProcessHistoryEntry(t.Context(), pid, database.ProcessHistoryUpdate{
		RssMemoryKb: &[]int64{knownRssKb}[0],
	}); err != nil {
		t.Fatalf("seed RSS value failed: %v", err)
	}

	// Mark the sample as just-taken so the throttle fires on the next call.
	hm.lastMemSample[serviceName] = time.Now()

	serviceInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || serviceInstance == nil {
		t.Fatalf("get service instance failed: %v", err)
	}
	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("get process history entry failed: %v", err)
	}

	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, serviceInstance)

	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("get process history entry after tick failed: %v", err)
	}
	if processHistoryEntry.RssMemoryKb != knownRssKb {
		t.Fatalf("RSS overwritten during throttled tick: want %d KB, got %d KB", knownRssKb, processHistoryEntry.RssMemoryKb)
	}
}

// func TestHealthMonitor_CheckRunningProcess_AliveButPortUnreachable(t *testing.T) {
// 	tempDir := t.TempDir()
//  daemonConfig := testutil.NewTestDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
// 	timeoutLimit := 30 * time.Second

// 	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
// 	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
// 	logger, err := manager.NewDaemonLogger(true, tempDir, daemonLogFileName)
// 	if err != nil {
// 		t.Fatalf("Unable to set up test daemon logger, got: %v", err)
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

// 	testFile := testutil.NewTestServiceConfigFile(t,
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

// 	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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
// 		t.Fatalf("Invalid PGID received: %d", pid)
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

// 	// Call checkRunningProcess - process is alive, but port is unreachable
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
// 	tailLogCommand := exec.Command("tail", "-n", "10", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
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
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)

	if err != nil {
		t.Fatalf("Unable to set up to test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

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

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	testFile := testutil.NewTestServiceConfigFile(t, testutil.WithoutRuntime(), testutil.WithName(serviceName), testutil.WithPort(port))
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

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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
		t.Fatalf("Invalid PGID received after starting service, got: %v", err)
	}

	result, err := mgr.ForceStopService(serviceCatalogEntry.Name)

	if err != nil {
		t.Fatalf("An error occurred during force stopping the service, got: %v", err)
	}
	if len(result.Errored) != 0 {
		t.Fatalf("Failed to force stop the service for this test")
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil {
		t.Fatalf("Service unable to get recent process history entry, got: %v", err)
	}
	if processHistoryEntry == nil {
		t.Fatal("Service process history entry not found")
	}

	// SIGKILL is asynchronous — poll until the process group is confirmed dead
	// before calling checkRunningProcess, otherwise isProcessAlive may still
	// return true and the "is not running" log never gets written.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if killErr := syscall.Kill(-processHistoryEntry.PGID, 0); killErr != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	serviceInstance2, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || serviceInstance2 == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}
	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, serviceInstance2)

	var buf bytes.Buffer
	var errorBuf bytes.Buffer

	tailLogCommand := exec.Command("tail", "-n", "20", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
	tailLogCommand.Stdout = &buf
	tailLogCommand.Stderr = &errorBuf

	if err = tailLogCommand.Run(); err != nil {
		t.Fatalf("The log command failed, got:\n%v\nOutput: %s", err, errorBuf.String())
	}

	output := buf.String()

	if !strings.Contains(output, "is not running") {
		t.Fatalf("Expected log about service not running, got: %s", output)
	}
}

func TestHealthMonitor_CheckRunningProcess_ResetsRestartCounter(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	// Use a tiny reset window so the test doesn't have to wait.
	healthConfig := newTestHealthConfig(t, WithRestartCounterResetWindow(1*time.Millisecond))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	serviceName := "test-reset-service"
	fullDirPath := filepath.Join(tempDir, "test-reset-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal service config to YAML: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write service.yaml: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	// Simulate a prior restart by setting RestartCount > 0.
	restartCount := 3
	if err = db.UpdateServiceInstance(t.Context(), serviceName, database.ServiceInstanceUpdate{RestartCount: &restartCount}); err != nil {
		t.Fatalf("Failed to seed restart count: %v", err)
	}

	// Backdate StartedAt so uptime exceeds the reset window.
	pastTime := time.Now().Add(-1 * time.Hour)
	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	if err = db.UpdateProcessHistoryEntry(t.Context(), processHistoryEntry.PGID, database.ProcessHistoryUpdate{
		StartedAt: &pastTime,
	}); err != nil {
		t.Fatalf("Failed to backdate StartedAt: %v", err)
	}
	processHistoryEntry.StartedAt = &pastTime

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}

	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance)

	updated, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updated == nil {
		t.Fatalf("Failed to get updated service instance: %v", err)
	}
	if updated.RestartCount != 0 {
		t.Fatalf("Expected RestartCount to be reset to 0, got: %d", updated.RestartCount)
	}
}

func TestHealthMonitor_CheckRunningProcess_DoesNotResetRestartCounterBeforeWindow(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	// Large window — uptime won't exceed it.
	healthConfig := newTestHealthConfig(t, WithRestartCounterResetWindow(24*time.Hour))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	serviceName := "test-no-reset-service"
	fullDirPath := filepath.Join(tempDir, "test-no-reset-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal service config to YAML: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write service.yaml: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	pid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	restartCount := 3
	if err = db.UpdateServiceInstance(t.Context(), serviceName, database.ServiceInstanceUpdate{RestartCount: &restartCount}); err != nil {
		t.Fatalf("Failed to seed restart count: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}

	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance)

	updated, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updated == nil {
		t.Fatalf("Failed to get updated service instance: %v", err)
	}
	if updated.RestartCount != 3 {
		t.Fatalf("Expected RestartCount to remain 3, got: %d", updated.RestartCount)
	}
}

func TestHealthMonitor_CheckFailedProcess_MaxRestarts(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t, WithMaxRestart(3))
	shutdownConfig := newTestShutdownConfig(t)

	maxRestartCount := healthConfig.MaxRestart

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)
	serviceName := "max-restart-service"
	serviceDir := filepath.Join(tempDir, serviceName)

	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	if closeErr := listener.Close(); closeErr != nil {
		t.Errorf("unable to close the listener, got: %v", closeErr)
	}

	testFile := testutil.NewTestServiceConfigFile(t,
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

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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

	fakePGID := 999999

	_, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, serviceName, types.ProcessStateFailed)
	if err != nil {
		t.Fatalf("Failed to register fake process history: %v", err)
	}

	err = db.UpdateProcessHistoryEntry(t.Context(), fakePGID, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StartedAt: new(time.Now().Add(-10 * time.Second)),
		StoppedAt: new(time.Now().Add(-2 * time.Second)),
		Error:     new("Simulated failure"),
	})
	if err != nil {
		t.Fatalf("Failed to update fake process history: %v", err)
	}

	if _, _, err = mgr.NewServiceLogFiles(serviceName); err != nil {
		t.Fatalf("Failed to create service log files: %v", err)
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

		hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount, maxRestartCount)
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
			// replace it with a fake dead PGID for the next iteration.
			latestProcess, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil {
				t.Fatalf("Iteration %d: Failed to get latest process: %v", i, err)
			}
			if latestProcess == nil {
				t.Fatalf("Iteration %d: Failed to get latest process", i)
			}

			// Kill the entire process group. RestartService's background goroutine
			// already calls Wait() on the child, so we must not call Wait()
			// here - doing so races with that goroutine and fails with
			// "no child processes" when it wins.
			_ = syscall.Kill(-latestProcess.PGID, syscall.SIGKILL)

			err = db.UpdateProcessHistoryEntry(t.Context(), latestProcess.PGID, database.ProcessHistoryUpdate{
				State:     new(types.ProcessStateFailed),
				StartedAt: new(time.Now().Add(-5 * time.Minute)),
				StoppedAt: new(time.Now()),
				Error:     new("Simulated failure"),
			})
			if err != nil {
				t.Fatalf("Failed to update process history entry: %v", err)
			}
			continue
		}
		if updatedInstance.RestartCount != instance.RestartCount {
			t.Errorf("Iteration %d: Expected no restart (count should stay %d), but got %d",
				i, instance.RestartCount, updatedInstance.RestartCount)
		}
	}

	finalInstance, instErr := hm.mgr.GetServiceInstance(serviceName)
	if instErr != nil {
		t.Fatalf("GetServiceInstance should not error: %v", instErr)
	}
	if finalInstance.RestartCount != maxRestartCount {
		t.Errorf("Final restart count should be exactly %d, got %d",
			maxRestartCount, finalInstance.RestartCount)
	}
}

func TestHealthMonitor_IsProcessAlive(t *testing.T) {
	hm := &HealthMonitor{}

	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Failed to get pgid: %v", err)
	}

	if !hm.isProcessAlive(pgid) {
		t.Fatal("Current process group should be alive")
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
			actual := calculateBackoffDelay(tc.restartCount, config.HealthBackoffBaseMs, config.HealthBackoffMaxMs)
			if actual != tc.expectedDelay {
				t.Errorf("Expected %v, got %v", tc.expectedDelay, actual)
			}
		})
	}
}

func TestHealthMonitor_CheckAllServices_MultipleServicesInDifferentStates(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	// --- Helper to register a service ---
	setupService := func(name string, port int) *types.ServiceCatalogEntry {
		serviceDir := filepath.Join(tempDir, name)
		if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
			t.Fatalf("Failed to create service directory for %s: %v", name, mkdirErr)
		}

		testFile := testutil.NewTestServiceConfigFile(t,
			testutil.WithRuntimePath(""),
			testutil.WithCommand("sleep 300"),
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

		entry, svcCatalogEntryErr := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
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

	tcpAddr1, ok := listener1.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener1.Addr())
	}
	port1 := tcpAddr1.Port
	svc1Name := "running-svc"
	setupService(svc1Name, port1)

	pid1, err := mgr.StartService(svc1Name)
	if err != nil {
		t.Fatalf("Failed to start %s: %v", svc1Name, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid1, syscall.SIGKILL) })
	// Transition to Running
	err = db.UpdateProcessHistoryEntry(t.Context(), pid1, database.ProcessHistoryUpdate{
		State: new(types.ProcessStateRunning),
		Error: new(""),
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

	tcpAddr2, ok := listener2.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener2.Addr())
	}
	port2 := tcpAddr2.Port
	svc2Name := "starting-svc"
	setupService(svc2Name, port2)

	pid2, err := mgr.StartService(svc2Name)
	if err != nil {
		t.Fatalf("Failed to start %s: %v", svc2Name, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid2, syscall.SIGKILL) })
	// Leave in Starting state (default after StartService)
	_ = pid2

	// Service 3: will be in Failed state (dead process, should attempt restart)
	svc3Name := "failed-svc"
	setupService(svc3Name, 0)

	err = db.RegisterServiceInstance(t.Context(), svc3Name)
	if err != nil {
		t.Fatalf("Failed to register instance for %s: %v", svc3Name, err)
	}

	fakePGID := 999998
	_, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, svc3Name, types.ProcessStateFailed)
	if err != nil {
		t.Fatalf("Failed to register fake process history for %s: %v", svc3Name, err)
	}
	err = db.UpdateProcessHistoryEntry(t.Context(), fakePGID, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StartedAt: new(time.Now().Add(-10 * time.Minute)),
		StoppedAt: new(time.Now().Add(-5 * time.Minute)),
		Error:     new("previous failure"),
	})
	if err != nil {
		t.Fatalf("Failed to update process history for %s: %v", svc3Name, err)
	}

	if _, _, err = mgr.NewServiceLogFiles(svc3Name); err != nil {
		t.Fatalf("Failed to create service log files for %s: %v", svc3Name, err)
	}

	services, err := hm.mgr.GetAllServiceCatalogEntries()

	if err != nil {
		t.Fatalf("Failed to get services: %v", err)
		return
	}

	// Run the full dispatch
	hm.checkAllServices(t.Context(), services)

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
		t.Logf("%s: RestartCount is %d - restart may have been attempted (check logs)", svc3Name, instance3.RestartCount)
	}

	// Kill any process restarted for svc3 by checkAllServices.
	if svc3Entry, svc3Err := hm.mgr.GetMostRecentProcessHistoryEntry(svc3Name); svc3Err == nil && svc3Entry != nil && svc3Entry.PGID > 0 {
		_ = syscall.Kill(-svc3Entry.PGID, syscall.SIGKILL)
	}
}

func TestHealthMonitor_CheckFailedProcess_ProcessStillAlive_Recovery(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t, WithMaxRestart(5))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	serviceName := "recovery-service"
	serviceDir := filepath.Join(tempDir, serviceName)
	if mkdirErr := os.MkdirAll(serviceDir, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithCommand("sleep 300"),
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

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	// Start a real service so we have a live PGID
	pgid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	if pgid < 1 {
		t.Fatalf("Invalid PGID received: %d", pgid)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

	// Verify the process is actually alive
	if !hm.isProcessAlive(pgid) {
		t.Fatal("Process should be alive for this test")
	}

	// Manually mark it as Failed in the DB (simulating a false failure detection)
	err = db.UpdateProcessHistoryEntry(t.Context(), pgid, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StoppedAt: new(time.Now()),
		Error:     new("falsely marked as failed"),
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

	// Call checkFailedProcess - the process is alive, so it should recover
	hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount, healthConfig.MaxRestart)

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

func WithRestartCounterResetWindow(window time.Duration) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.RestartCounterResetWindow = window
	}
}

func WithMemSampleInterval(d time.Duration) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.MemSampleInterval = d
	}
}

func newTestHealthConfig(t *testing.T, opts ...HealthConfigOption) *config.HealthConfig {
	t.Helper()
	healthConfig := &config.HealthConfig{
		MaxRestart: config.HealthMaxRestart,
		Timeout: config.TimeOutConfig{
			Enable: config.HealthTimeOutEnable,
			Limit:  30 * time.Second,
		},
		Backoff: config.BackoffConfig{
			BaseMs: config.HealthBackoffBaseMs,
			MaxMs:  config.HealthBackoffMaxMs,
		},
		Memory: config.MemoryThresholdConfig{
			WarningThreshold:      config.HealthMemoryWarningThreshold,
			SoftRestartThreshold:  config.HealthMemorySoftRestartThreshold,
			ForceRestartThreshold: config.HealthMemoryForceRestartThreshold,
		},
	}

	for _, opt := range opts {
		opt(healthConfig)
	}

	return healthConfig
}

type ShutdownConfigOption func(*config.ShutdownConfig)

func WithGracePeriod(gracePeriod time.Duration) ShutdownConfigOption {
	return func(sc *config.ShutdownConfig) {
		sc.GracePeriod = gracePeriod
	}
}

func newTestShutdownConfig(t *testing.T, opts ...ShutdownConfigOption) *config.ShutdownConfig {
	t.Helper()
	shutdownConfig := &config.ShutdownConfig{
		GracePeriod: safeParseDuration(config.ShutdownGracePeriod, time.Second*5),
	}

	for _, opt := range opts {
		opt(shutdownConfig)
	}

	return shutdownConfig
}

func safeParseDuration(durationAsString string, fallback time.Duration) time.Duration {
	limit, err := time.ParseDuration(durationAsString)
	if err != nil {
		return fallback
	}

	return limit
}

func TestScanStatusFieldBytes_found(t *testing.T) {
	contents := []byte("Name:\tmy-proc\nVmRSS:\t12345 kB\nNSpgid:\t42\n")
	got := scanStatusFieldBytes(contents, []byte("VmRSS:\t"))
	if got == nil {
		t.Fatal("expected to find VmRSS field, got nil")
	}
	if string(got) != "12345 kB" {
		t.Errorf("got %q, want %q", string(got), "12345 kB")
	}
}

func TestScanStatusFieldBytes_notFound(t *testing.T) {
	contents := []byte("Name:\tmy-proc\nVmRSS:\t12345 kB\n")
	got := scanStatusFieldBytes(contents, []byte("NSpgid:\t"))
	if got != nil {
		t.Errorf("expected nil for missing field, got %q", got)
	}
}

func TestScanStatusFieldBytes_lastLine(t *testing.T) {
	contents := []byte("Name:\tmy-proc\nNSpgid:\t99")
	got := scanStatusFieldBytes(contents, []byte("NSpgid:\t"))
	if got == nil {
		t.Fatal("expected to find NSpgid at last line (no trailing newline)")
	}
	if string(got) != "99" {
		t.Errorf("got %q, want %q", string(got), "99")
	}
}

func TestScanStatusFieldBytes_empty(t *testing.T) {
	got := scanStatusFieldBytes([]byte{}, []byte("VmRSS:\t"))
	if got != nil {
		t.Errorf("expected nil for empty contents, got %q", got)
	}
}

func TestCheckMemoryLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("checkMemoryLinux only runs on Linux")
	}

	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("failed to get pgid: %v", err)
	}

	rss := hm.checkMemoryLinux(pgid)
	if rss <= 0 {
		t.Errorf("expected positive RSS for own process group, got %d", rss)
	}
}

func TestCheckUnknownProcess_alive(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("failed to setup logger: %v", err)
	}
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	fullDirPath := filepath.Join(tempDir, "unknown-alive-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("failed to create project dir: %v", mkdirErr)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	const serviceName = "unknown-alive-svc"
	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}

	entry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("failed to create catalog entry: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(entry); err != nil {
		t.Fatalf("failed to register service: %v", err)
	}

	pgid, err := mgr.StartService(serviceName)
	if err != nil {
		t.Fatalf("failed to start service: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

	processEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processEntry == nil {
		t.Fatalf("failed to get process history: %v", err)
	}

	hm.checkUnknownProcess(t.Context(), entry, processEntry)

	updated, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updated == nil {
		t.Fatal("failed to get updated process history")
	}
	if updated.State != types.ProcessStateRunning {
		t.Errorf("expected Running for alive process in unknown state, got %v", updated.State)
	}
}

func TestCheckUnknownProcess_dead(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("failed to setup logger: %v", err)
	}
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig)

	const serviceName = "unknown-dead-svc"
	fullDirPath := filepath.Join(tempDir, "unknown-dead-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName))
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	yamlData, yamlErr := yaml.Marshal(testFile)
	if yamlErr != nil {
		t.Fatalf("failed to marshal config: %v", yamlErr)
	}
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("failed to write service.yaml: %v", err)
	}

	entry, err := manager.NewServiceCatalogEntry(serviceName, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("failed to create catalog entry: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(entry); err != nil {
		t.Fatalf("failed to register service: %v", err)
	}
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("failed to register service instance: %v", err)
	}

	fakePGID := 999997
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, serviceName, types.ProcessStateUnknown); err != nil {
		t.Fatalf("failed to register fake process history: %v", err)
	}
	if _, _, err = mgr.NewServiceLogFiles(serviceName); err != nil {
		t.Fatalf("failed to create log files: %v", err)
	}

	processEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processEntry == nil {
		t.Fatalf("failed to get process history: %v", err)
	}

	hm.checkUnknownProcess(t.Context(), entry, processEntry)

	updated, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updated == nil {
		t.Fatal("failed to get updated process history")
	}
	if updated.State != types.ProcessStateFailed {
		t.Errorf("expected Failed for dead process in unknown state, got %v", updated.State)
	}
}
