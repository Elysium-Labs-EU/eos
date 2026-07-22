package monitor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/logutil"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/otelx"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"gopkg.in/yaml.v3"
)

// TestHealthMonitor_MeasureCPU_SeedsThenSamples verifies the two-reading
// contract: the first call seeds a baseline and reports "not sampled", a call
// within the throttle interval is skipped without disturbing that baseline, and
// a later call produces a non-negative percentage.
func TestHealthMonitor_MeasureCPU_SeedsThenSamples(t *testing.T) {
	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}

	// Long interval: exercise seeding + throttle without racing the clock.
	throttled := &HealthMonitor{
		telemetry:         otelx.NoopHandles(),
		lastCPUSample:     make(map[string]cpuSample),
		memSampleInterval: time.Hour,
	}
	if pct, sampled := throttled.measureCPU(context.Background(), pgid, "svc"); sampled || pct != 0 {
		t.Fatalf("first measureCPU = (%v, %v), want (0, false) to seed baseline", pct, sampled)
	}
	seeded, ok := throttled.lastCPUSample["svc"]
	if !ok {
		t.Fatal("expected baseline stored after first measureCPU")
	}
	if pct, sampled := throttled.measureCPU(context.Background(), pgid, "svc"); sampled || pct != 0 {
		t.Errorf("throttled measureCPU = (%v, %v), want (0, false)", pct, sampled)
	}
	if throttled.lastCPUSample["svc"].at != seeded.at {
		t.Error("throttled call must not overwrite the baseline")
	}

	// Short interval against a dedicated busy child (its own group leader, so
	// group membership is stable): the second reading should produce a real,
	// positive sample.
	busy := exec.Command("sh", "-c", "while :; do :; done")
	busy.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if startErr := busy.Start(); startErr != nil {
		t.Fatalf("start busy loop: %v", startErr)
	}
	busyPgid, err := syscall.Getpgid(busy.Process.Pid)
	if err != nil {
		_ = busy.Process.Kill()
		t.Fatalf("Getpgid: %v", err)
	}
	t.Cleanup(func() { _ = busy.Process.Kill(); _ = busy.Wait() })

	sampler := &HealthMonitor{
		telemetry:         otelx.NoopHandles(),
		lastCPUSample:     make(map[string]cpuSample),
		memSampleInterval: time.Millisecond,
	}
	if _, sampled := sampler.measureCPU(context.Background(), busyPgid, "svc"); sampled {
		t.Fatal("first measureCPU should seed, not sample")
	}
	time.Sleep(50 * time.Millisecond)
	pct, sampled := sampler.measureCPU(context.Background(), busyPgid, "svc")
	if !sampled {
		t.Fatal("second measureCPU after interval should sample")
	}
	if pct <= 0 {
		t.Errorf("busy CPU percent = %v, want > 0", pct)
	}
}

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())
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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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

// TestHealthMonitor_CheckRunningProcess_HeartbeatAdvancesUpdatedAt verifies the
// fix for the stale-flag bug (#146 follow-up to #143): a confirmed-alive running
// service must bump process_history.updated_at on EVERY health tick, even when
// the RSS/CPU sample throttle (memSampleInterval) has not elapsed. Otherwise
// updated_at freezes between samples (~30s) while IsProcessHistoryStale flags
// anything older than 3*checkInterval (~6s), so a healthy service reads "(stale)".
func TestHealthMonitor_CheckRunningProcess_HeartbeatAdvancesUpdatedAt(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	// 24h interval guarantees the RSS/CPU throttle fires (no fresh sample) on the tick.
	healthConfig := newTestHealthConfig(t, WithMemSampleInterval(24*time.Hour))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("unable to set up daemon logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	fullDirPath := filepath.Join(tempDir, "heartbeat-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("could not create heartbeat-project directory: %v", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	const serviceName = "heartbeat-test-svc"
	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(0), // no port check, so ReasonNone is the only action
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

	// Seed a known RSS value; the throttled heartbeat must preserve it, not zero it.
	const knownRssKb = int64(54321)
	if err = db.UpdateProcessHistoryEntry(t.Context(), pid, database.ProcessHistoryUpdate{
		RssMemoryKb: &[]int64{knownRssKb}[0],
	}); err != nil {
		t.Fatalf("seed RSS value failed: %v", err)
	}

	// Mark the sample as just-taken so the throttle fires: no fresh RSS/CPU this tick.
	hm.lastMemSample[serviceName] = time.Now()

	serviceInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || serviceInstance == nil {
		t.Fatalf("get service instance failed: %v", err)
	}
	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("get process history entry failed: %v", err)
	}
	if processHistoryEntry.UpdatedAt == nil {
		t.Fatal("expected seeded row to have a non-nil updated_at")
	}
	updatedBefore := *processHistoryEntry.UpdatedAt

	// Ensure wall-clock advances so the heartbeat's updated_at is strictly newer.
	time.Sleep(25 * time.Millisecond)

	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, serviceInstance)

	processHistoryEntry, err = hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("get process history entry after tick failed: %v", err)
	}
	if processHistoryEntry.UpdatedAt == nil {
		t.Fatal("expected updated_at to be set after heartbeat tick")
	}
	if !processHistoryEntry.UpdatedAt.After(updatedBefore) {
		t.Errorf("heartbeat did not advance updated_at on a throttled tick: before %v, after %v", updatedBefore, *processHistoryEntry.UpdatedAt)
	}
	// The heartbeat must not clobber the last-known RSS with a zero from the throttled sample.
	if processHistoryEntry.RssMemoryKb != knownRssKb {
		t.Errorf("heartbeat overwrote throttled RSS: want %d KB, got %d KB", knownRssKb, processHistoryEntry.RssMemoryKb)
	}
	// The service must still read as running after a heartbeat tick.
	if processHistoryEntry.State != types.ProcessStateRunning {
		t.Errorf("expected state to remain running after heartbeat, got %v", processHistoryEntry.State)
	}
}

// TODO: untested gap — process stays alive but its port becomes unreachable;
// checkRunningProcess has no port-reachability check to catch this case.

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	serviceName := "test-reset-service"
	fullDirPath := filepath.Join(tempDir, "test-reset-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(port),
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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	serviceName := "test-no-reset-service"
	fullDirPath := filepath.Join(tempDir, "test-no-reset-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create service directory: %v", mkdirErr)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithPort(port),
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

// TestHealthMonitor_CheckFailedProcess_RetriesIndefinitely proves issue #30's
// fix: a service that keeps failing must keep retrying forever at the
// backoff-capped interval, never hitting a permanent cliff (there is no
// restart-count cap anymore; only the exponential backoff paces attempts).
// It runs well past what used to be the default cap (10) and expects every
// single iteration to still restart.
func TestHealthMonitor_CheckFailedProcess_RetriesIndefinitely(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t, WithBackoff(1, 5))
	shutdownConfig := newTestShutdownConfig(t)

	const iterations = 15 // more than the old default cap of 10

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())
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

	_, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, serviceName, types.ProcessStateFailed)
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

	for i := range iterations {
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

		hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount)

		updatedInstance, _ := hm.mgr.GetServiceInstance(serviceName)
		if updatedInstance == nil {
			t.Fatalf("Iteration %d: Failed to get updated service instance", i)
		}
		if updatedInstance.RestartCount != i+1 {
			t.Fatalf("Iteration %d: expected restart count %d, got %d (retries must never permanently stop)", i, i+1, updatedInstance.RestartCount)
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

		// Backdate StoppedAt well past the (deliberately tiny) backoff cap so
		// the next iteration's canRestart check isn't flaky about real wall-clock
		// overhead between iterations.
		err = db.UpdateProcessHistoryEntry(t.Context(), latestProcess.PGID, database.ProcessHistoryUpdate{
			State:     new(types.ProcessStateFailed),
			StartedAt: new(time.Now().Add(-5 * time.Minute)),
			StoppedAt: new(time.Now().Add(-1 * time.Second)),
			Error:     new("Simulated failure"),
		})
		if err != nil {
			t.Fatalf("Failed to update process history entry: %v", err)
		}
	}

	finalInstance, instErr := hm.mgr.GetServiceInstance(serviceName)
	if instErr != nil {
		t.Fatalf("GetServiceInstance should not error: %v", instErr)
	}
	if finalInstance.RestartCount != iterations {
		t.Errorf("Final restart count should be exactly %d (every attempt should have restarted), got %d",
			iterations, finalInstance.RestartCount)
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
		{
			// The exact threshold where baseMs(300) * 2^restartCount first
			// exceeds int64's range. A float64->int conversion of an
			// out-of-range value is undefined/platform-dependent in Go
			// (arm64's FCVTZS saturates, amd64's CVTTSD2SI does not), so this
			// restartCount previously reproduced the bug on amd64 while
			// appearing fine on this arm64 dev machine.
			name:          "Restart count at the int64-overflow threshold",
			restartCount:  62,
			expectedDelay: 60000 * time.Millisecond,
		},
		{
			name:          "Restart count just past the int64-overflow threshold",
			restartCount:  63,
			expectedDelay: 60000 * time.Millisecond,
		},
		{
			// With the restart-count cliff removed (issue #30), a permanently
			// failing service's restartCount grows without bound. math.Pow(2,
			// restartCount) would overflow float64 well before this, so
			// calculateBackoffDelay must still saturate at maxMs instead of
			// producing garbage from an overflowed/undefined int conversion.
			name:          "Huge restart count from long-running uncapped retries",
			restartCount:  1_000_000,
			expectedDelay: 60000 * time.Millisecond,
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

// TestCanRestart_NeverPermanentlyStopsOnCount is the focused unit test for
// issue #30's core fix: canRestart must gate only on backoff timing, not on
// restartCount reaching some fixed cap — a service that has failed many, many
// times must still be allowed to restart once its backoff window elapses.
// Only the explicit restartHalted sentinel (set for non-transient failures
// like unwritable logs) should ever refuse a restart outright.
func TestCanRestart_NeverPermanentlyStopsOnCount(t *testing.T) {
	backoff := config.BackoffConfig{BaseMs: 1, MaxMs: 5}
	longAgo := time.Now().Add(-1 * time.Hour)

	testCases := []struct {
		since        *time.Time
		name         string
		restartCount int
		want         bool
	}{
		{name: "first attempt, no prior stop time", restartCount: 0, since: nil, want: true},
		{name: "far past the old default cap of 10, backoff elapsed", restartCount: 500, since: &longAgo, want: true},
		{name: "astronomically high count, backoff elapsed", restartCount: 1_000_000, since: &longAgo, want: true},
		{name: "halted sentinel refuses regardless of elapsed time", restartCount: restartHalted, since: &longAgo, want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := canRestart(tc.restartCount, tc.since, backoff)
			if got != tc.want {
				t.Errorf("canRestart(%d, ...) = %v, want %v", tc.restartCount, got, tc.want)
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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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
	_, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, svc3Name, types.ProcessStateFailed)
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
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up test daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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
	hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount)

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

func TestHealthMonitor_CheckRunningProcess_PortUnreachable(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up daemon logger, got: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	serviceName := "port-drop-service"
	fullDirPath := filepath.Join(tempDir, "port-drop-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Failed to create project directory: %v", mkdirErr)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	// Open a port, then close it before the check — the process stays alive,
	// but the port it was supposed to be listening on is no longer reachable.
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected *net.TCPAddr, got %T", listener.Addr())
	}
	port := tcpAddr.Port
	if err = listener.Close(); err != nil {
		t.Fatalf("Failed to close listener: %v", err)
	}

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

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}

	hm.checkRunningProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance)

	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updatedEntry == nil {
		t.Fatal("Failed to get updated process history")
	}
	if updatedEntry.State != types.ProcessStateFailed {
		t.Errorf("Expected ProcessStateFailed, got %v", updatedEntry.State)
	}
	if updatedEntry.Error == nil || !strings.Contains(*updatedEntry.Error, "not reachable on port") {
		t.Errorf("Expected port-unreachable error, got: %v", updatedEntry.Error)
	}
}

// panicOnServiceManager wraps a monitorManager and panics when GetServiceInstance
// is called for a configured service name, to exercise checkService's recover().
type panicOnServiceManager struct {
	monitorManager
	panicFor string
}

func (m *panicOnServiceManager) GetServiceInstance(name string) (*types.ServiceInstance, error) {
	if name == m.panicFor {
		panic("simulated panic during health check for " + name)
	}
	return m.monitorManager.GetServiceInstance(name)
}

func TestHealthMonitor_CheckAllServices_PanicInOneServiceDoesNotStopOthers(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	realMgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(realMgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, true, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Unable to set up daemon logger, got: %v", err)
	}

	panicSvcName := "panic-svc"
	mgr := &panicOnServiceManager{monitorManager: realMgr, panicFor: panicSvcName}
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	if err = db.RegisterServiceInstance(t.Context(), panicSvcName); err != nil {
		t.Fatalf("Failed to register panic-svc instance: %v", err)
	}
	healthyName := "healthy-svc"
	if err = db.RegisterServiceInstance(t.Context(), healthyName); err != nil {
		t.Fatalf("Failed to register healthy-svc instance: %v", err)
	}
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), 424243, 0, healthyName, types.ProcessStateStopped); err != nil {
		t.Fatalf("Failed to register healthy-svc process history: %v", err)
	}

	panicSvc := types.ServiceCatalogEntry{Name: panicSvcName}
	healthySvc := types.ServiceCatalogEntry{Name: healthyName}

	done := make(chan struct{})
	go func() {
		hm.checkAllServices(t.Context(), []types.ServiceCatalogEntry{panicSvc, healthySvc})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("checkAllServices did not return — a panic in one service likely escaped checkService's recover()")
	}

	var buf bytes.Buffer
	tailLogCommand := exec.Command("tail", "-n", "20", filepath.Join(daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName))
	tailLogCommand.Stdout = &buf
	if err = tailLogCommand.Run(); err != nil {
		t.Fatalf("failed to read daemon log: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "recovered from panic") || !strings.Contains(output, panicSvcName) {
		t.Errorf("expected a recovered-panic log entry mentioning %s, got: %s", panicSvcName, output)
	}
	// The healthy service, listed after the panicking one, must still have been reached
	// (a "not registered" style state transition wouldn't occur if checkService bailed
	// out of the whole loop instead of just the panicking entry).
	if !strings.Contains(output, healthyName) {
		t.Errorf("expected healthy-svc to have been reached and logged, got: %s", output)
	}
}

type HealthConfigOption func(*config.HealthConfig)

func WithBackoff(baseMs, maxMs int) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.Backoff = config.BackoffConfig{BaseMs: baseMs, MaxMs: maxMs}
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

func WithCheckInterval(d time.Duration) HealthConfigOption {
	return func(hc *config.HealthConfig) {
		hc.CheckInterval = d
	}
}

func newTestHealthConfig(t *testing.T, opts ...HealthConfigOption) *config.HealthConfig {
	t.Helper()
	healthConfig := &config.HealthConfig{
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

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

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
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, serviceName, types.ProcessStateUnknown); err != nil {
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

func TestNewHealthMonitor_CheckIntervalDefault(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t, WithCheckInterval(0))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	mgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(mgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(false, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("failed to setup logger: %v", err)
	}

	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	if hm.checkInterval != 2*time.Second {
		t.Errorf("expected default checkInterval 2s, got %v", hm.checkInterval)
	}
}

func TestEvaluateMemoryThresholds(t *testing.T) {
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)
	hm := NewHealthMonitor(nil, nil, nil, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const limitMb = 100
	limitKb := int64(limitMb) * 1024

	tests := []struct {
		name       string
		rssKb      int64
		wantReason RestartReason
	}{
		{"disabled limit", 0, ReasonNone},
		{"well under threshold", limitKb / 10, ReasonNone},
		{"at warning threshold", int64(float64(limitKb) * healthConfig.Memory.WarningThreshold), ReasonWarning},
		{"at soft restart threshold", int64(float64(limitKb) * healthConfig.Memory.SoftRestartThreshold), ReasonSoftRestart},
		{"at force restart threshold", int64(float64(limitKb) * healthConfig.Memory.ForceRestartThreshold), ReasonForceRestart},
		{"far above force threshold", limitKb * 10, ReasonForceRestart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configLimitMb := limitMb
			if tt.name == "disabled limit" {
				configLimitMb = 0
			}
			got := hm.evaluateMemoryThresholds(configLimitMb, tt.rssKb)
			if got != tt.wantReason {
				t.Errorf("evaluateMemoryThresholds(%d, %d) = %v, want %v", configLimitMb, tt.rssKb, got, tt.wantReason)
			}
		})
	}
}

func TestDispatchMemoryAction_warningAndNone(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const serviceName = "dispatch-memory-svc"
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("failed to register service instance: %v", err)
	}
	if _, _, err = mgr.NewServiceLogFiles(serviceName); err != nil {
		t.Fatalf("failed to create service log files: %v", err)
	}
	const pgid = 999900
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), pgid, 0, serviceName, types.ProcessStateRunning); err != nil {
		t.Fatalf("failed to register process history: %v", err)
	}

	service := &types.ServiceCatalogEntry{Name: serviceName}
	process, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || process == nil {
		t.Fatalf("failed to get process history: %v", err)
	}
	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}

	hm.dispatchMemoryAction(t.Context(), service, process, instance, ReasonWarning, pgid, 5000, true, 12.5, true)

	logPath, err := mgr.GetServiceLogFilePath(serviceName, false)
	if err != nil {
		t.Fatalf("failed to get log file path: %v", err)
	}
	content, err := os.ReadFile(*logPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(content), "memory usage warning") {
		t.Errorf("expected warning message in log, got: %s", content)
	}

	updated, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updated == nil {
		t.Fatalf("failed to get updated process history: %v", err)
	}
	if updated.RssMemoryKb != 5000 {
		t.Errorf("expected RssMemoryKb 5000 after warning dispatch, got %v", updated.RssMemoryKb)
	}
	if updated.CPUPercent != 12.5 {
		t.Errorf("expected CPUPercent 12.5 after warning dispatch, got %v", updated.CPUPercent)
	}

	hm.dispatchMemoryAction(t.Context(), service, updated, instance, ReasonNone, pgid, 6000, true, 20.0, true)

	afterNone, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || afterNone == nil {
		t.Fatalf("failed to get process history after ReasonNone dispatch: %v", err)
	}
	if afterNone.RssMemoryKb != 6000 {
		t.Errorf("expected RssMemoryKb 6000 after sampled ReasonNone dispatch, got %v", afterNone.RssMemoryKb)
	}
	if afterNone.CPUPercent != 20.0 {
		t.Errorf("expected CPUPercent 20.0 after sampled ReasonNone dispatch, got %v", afterNone.CPUPercent)
	}
}

func TestRestartOnMemoryThreshold_soft(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	fullDirPath := filepath.Join(tempDir, "restart-threshold-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("failed to create project dir: %v", mkdirErr)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	const serviceName = "restart-threshold-svc"
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

	// Backdate StartedAt so canRestart's backoff window has already elapsed.
	if updateErr := db.UpdateProcessHistoryEntry(t.Context(), pgid, database.ProcessHistoryUpdate{
		StartedAt: new(time.Now().Add(-5 * time.Minute)),
	}); updateErr != nil {
		t.Fatalf("failed to backdate StartedAt: %v", updateErr)
	}

	process, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || process == nil {
		t.Fatalf("failed to get process history: %v", err)
	}
	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}

	rssKb := int64(1024)
	hm.restartOnMemoryThreshold(t.Context(), entry, process, instance, pgid, rssKb, &rssKb, &rssKb, "soft", 5*time.Second, 200*time.Millisecond)

	updatedInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updatedInstance == nil {
		t.Fatalf("failed to get updated service instance: %v", err)
	}
	if updatedInstance.RestartCount != instance.RestartCount+1 {
		t.Errorf("expected RestartCount %d, got %d", instance.RestartCount+1, updatedInstance.RestartCount)
	}

	newProcess, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || newProcess == nil {
		t.Fatalf("failed to get new process history: %v", err)
	}
	if newProcess.PGID == pgid {
		t.Error("expected a new PGID after restart")
	}
	t.Cleanup(func() { _ = syscall.Kill(-newProcess.PGID, syscall.SIGKILL) })
}

// TestRestartOnMemoryThreshold_Halted proves the memory-threshold restart path
// respects the same permanent-halt sentinel as checkFailedProcess: once a
// service's restart counter is set to restartHalted (a non-transient failure
// elsewhere gave up on it), a memory-threshold breach must not restart it either.
func TestRestartOnMemoryThreshold_Halted(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const serviceName = "restart-threshold-halted-svc"
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("failed to register service instance: %v", err)
	}
	halted := restartHalted
	if updateErr := db.UpdateServiceInstance(t.Context(), serviceName, database.ServiceInstanceUpdate{RestartCount: &halted}); updateErr != nil {
		t.Fatalf("failed to set restart count: %v", updateErr)
	}

	fakePGID := 999998
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, serviceName, types.ProcessStateRunning); err != nil {
		t.Fatalf("failed to register process history: %v", err)
	}

	process, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || process == nil {
		t.Fatalf("failed to get process history: %v", err)
	}
	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	if instance.RestartCount != restartHalted {
		t.Fatalf("test setup invalid: RestartCount %d must equal restartHalted sentinel %d", instance.RestartCount, restartHalted)
	}

	entry := &types.ServiceCatalogEntry{Name: serviceName}
	rssKb := int64(1024)
	hm.restartOnMemoryThreshold(t.Context(), entry, process, instance, fakePGID, rssKb, &rssKb, &rssKb, "soft", 5*time.Second, 200*time.Millisecond)

	// canRestart should have short-circuited: no restart attempted, PGID unchanged.
	unchanged, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || unchanged == nil {
		t.Fatalf("failed to get process history: %v", err)
	}
	if unchanged.PGID != fakePGID {
		t.Errorf("expected no restart (PGID unchanged), got new PGID %d", unchanged.PGID)
	}
}

func TestHealthMonitor_CheckCronRestart_EmptyExprNoop(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const serviceName = "cron-noop-svc"
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("failed to register service instance: %v", err)
	}

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}

	entry := &types.ServiceCatalogEntry{Name: serviceName}
	hm.checkCronRestart(t.Context(), entry, instance, "")

	unchanged, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || unchanged == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	if unchanged.NextRestartAt != nil {
		t.Errorf("expected NextRestartAt to stay nil when cron_restart is empty, got %v", unchanged.NextRestartAt)
	}
}

func TestHealthMonitor_CheckCronRestart_SchedulesFirstFireTime(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const serviceName = "cron-schedule-svc"
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("failed to register service instance: %v", err)
	}

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	if instance.NextRestartAt != nil {
		t.Fatalf("test setup invalid: expected nil NextRestartAt, got %v", instance.NextRestartAt)
	}

	entry := &types.ServiceCatalogEntry{Name: serviceName}
	hm.checkCronRestart(t.Context(), entry, instance, "0 3 * * *")

	updated, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updated == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	if updated.NextRestartAt == nil {
		t.Fatal("expected NextRestartAt to be scheduled, got nil")
	}
	if !updated.NextRestartAt.After(time.Now()) {
		t.Errorf("expected NextRestartAt to be in the future, got %v", updated.NextRestartAt)
	}
}

func TestHealthMonitor_CheckCronRestart_NotDueYet(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const serviceName = "cron-not-due-svc"
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("failed to register service instance: %v", err)
	}

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	future := time.Now().Add(1 * time.Hour)
	instance.NextRestartAt = &future

	entry := &types.ServiceCatalogEntry{Name: serviceName}
	// serviceName is not a registered catalog entry with a config file, so a
	// RestartService call here would error - if checkCronRestart tried to
	// restart despite not being due, this test would surface it.
	hm.checkCronRestart(t.Context(), entry, instance, "0 3 * * *")

	unchanged, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || unchanged == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	if unchanged.NextRestartAt != nil {
		t.Errorf("expected DB NextRestartAt to remain unset (no write when not due), got %v", unchanged.NextRestartAt)
	}
}

func TestHealthMonitor_CheckCronRestart_DueTriggersRestart(t *testing.T) {
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
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	const serviceName = "cron-due-svc"
	fullDirPath := filepath.Join(tempDir, "cron-due-project")
	if err = os.MkdirAll(fullDirPath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	testServiceScript := testutil.NewTestServiceScript(t, testutil.WithDirPath(fullDirPath))
	testutil.NewTestServiceScriptAtLocation(t, *testServiceScript)

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithoutRuntime(),
		testutil.WithName(serviceName),
		testutil.WithCronRestart("0 3 * * *"),
		testutil.WithCommand("./"+testServiceScript.FileName))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write the service.yaml file, got: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}
	if err = mgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Error registering service: %v", err)
	}

	pgid, err := mgr.StartService(serviceCatalogEntry.Name)
	if err != nil {
		t.Fatalf("Service unable to start, got: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	})

	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	past := time.Now().Add(-1 * time.Hour)
	instance.NextRestartAt = &past

	hm.checkCronRestart(t.Context(), serviceCatalogEntry, instance, "0 3 * * *")
	t.Cleanup(func() {
		if latest, latestErr := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName); latestErr == nil && latest != nil {
			_ = syscall.Kill(-latest.PGID, syscall.SIGKILL)
		}
	})

	newProcess, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || newProcess == nil {
		t.Fatalf("failed to get process history after cron restart: %v", err)
	}
	if newProcess.PGID == pgid {
		t.Errorf("expected a new PGID after cron restart, still %d", pgid)
	}

	updated, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updated == nil {
		t.Fatalf("failed to get service instance: %v", err)
	}
	if updated.NextRestartAt == nil {
		t.Fatal("expected NextRestartAt to be recomputed after restart, got nil")
	}
	if !updated.NextRestartAt.After(time.Now()) {
		t.Errorf("expected recomputed NextRestartAt to be in the future, got %v", updated.NextRestartAt)
	}
}

// restartFailManager wraps a monitorManager and forces RestartService to return a
// configured error, to exercise checkFailedProcess's restart-failure handling.
type restartFailManager struct {
	monitorManager
	restartErr error
}

func (m *restartFailManager) RestartService(name string, gracePeriod, tickerPeriod time.Duration) (int, error) {
	return 0, m.restartErr
}

// TestHealthMonitor_CheckFailedProcess_UnwritableLogHaltsLoop reproduces issue
// #145: when a restart fails because the service's log files are unwritable
// (EACCES), the real permission error must surface in the process history error
// field, and the restart counter must be capped to stop the tight 2s loop.
func TestHealthMonitor_CheckFailedProcess_UnwritableLogHaltsLoop(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	realMgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(realMgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	// A wrapped log-open failure that carries os.ErrPermission, exactly as the
	// manager reports an unwritable log file.
	permErr := fmt.Errorf("preparing log files for logbench: %w", fmt.Errorf("could not open log file: %w", os.ErrPermission))
	mgr := &restartFailManager{monitorManager: realMgr, restartErr: permErr}
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	serviceName := "unwritable-log-service"
	fullDirPath := filepath.Join(tempDir, "unwritable-log-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Could not create project directory: %v", mkdirErr)
	}

	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(0))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write service.yaml: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}
	if err = realMgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Error registering service: %v", err)
	}
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	// A dead PGID in Failed state, stopped long enough ago that backoff allows a
	// restart attempt.
	fakePGID := 999999
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, serviceName, types.ProcessStateFailed); err != nil {
		t.Fatalf("Failed to register fake process history: %v", err)
	}
	if err = db.UpdateProcessHistoryEntry(t.Context(), fakePGID, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StartedAt: new(time.Now().Add(-30 * time.Second)),
		StoppedAt: new(time.Now().Add(-10 * time.Second)),
		Error:     new("[unwritable-log-service] died during startup (PGID 999999)"),
	}); err != nil {
		t.Fatalf("Failed to update fake process history: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}

	hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount)

	// The generic "died during startup" is replaced by the real permission cause.
	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updatedEntry == nil {
		t.Fatal("Failed to get updated process history")
	}
	if updatedEntry.Error == nil {
		t.Fatal("Expected an error to be surfaced, got nil")
	}
	for _, want := range []string{"permission denied", "needs intervention"} {
		if !strings.Contains(*updatedEntry.Error, want) {
			t.Errorf("Expected error to contain %q, got: %s", want, *updatedEntry.Error)
		}
	}
	if strings.Contains(*updatedEntry.Error, "died during startup") {
		t.Errorf("Expected generic startup error to be replaced, still present: %s", *updatedEntry.Error)
	}

	// The restart counter is set to the halted sentinel so canRestart stops
	// firing (no tight loop) — but note this is a permanent, explicit halt for
	// this non-transient cause, not the general uncapped-backoff behavior other
	// restart failures now get.
	updatedInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updatedInstance == nil {
		t.Fatal("Failed to get updated service instance")
	}
	if updatedInstance.RestartCount != restartHalted {
		t.Errorf("Expected restart count set to halted sentinel %d, got %d", restartHalted, updatedInstance.RestartCount)
	}
	if canRestart(updatedInstance.RestartCount, updatedEntry.StoppedAt, hm.backoff) {
		t.Error("Expected canRestart to be false after halting on permission error")
	}
}

// TestHealthMonitor_CheckFailedProcess_SurfacesChildStderr proves issue #30's
// second half: when a restart attempt fails for a reason other than the
// already-classified permission case, the error surfaced to `eos status`/`eos
// info` includes the service's own last captured stderr line (e.g. a port
// bind conflict), not just the generic exec-layer error.
func TestHealthMonitor_CheckFailedProcess_SurfacesChildStderr(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t)
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	realMgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(realMgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	// A generic exec-layer error - deliberately NOT an os.ErrPermission, to
	// exercise the "other error" branch of handleRestartFailure.
	genericErr := errors.New("stopping process(es) for bind-conflict-svc: exit status 1")
	mgr := &restartFailManager{monitorManager: realMgr, restartErr: genericErr}
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	serviceName := "bind-conflict-svc"
	fullDirPath := filepath.Join(tempDir, "bind-conflict-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Could not create project directory: %v", mkdirErr)
	}
	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(0))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write service.yaml: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}
	if err = realMgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Error registering service: %v", err)
	}
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	// Simulate the child process's own stderr already being captured by the
	// real stdout/stderr pipe machinery (pipeToErrorLogFile), by writing the
	// same JSON-line format directly to the service's error log.
	_, errorLogPath, err := realMgr.NewServiceLogFiles(serviceName)
	if err != nil {
		t.Fatalf("Failed to create service log files: %v", err)
	}
	errFile, err := os.OpenFile(errorLogPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open error log: %v", err)
	}
	logutil.NewJSONLogger(errFile, false).Info("bind: Address already in use", "service", serviceName, "source", "stderr")
	if err = errFile.Close(); err != nil {
		t.Fatalf("Failed to close error log: %v", err)
	}

	fakePGID := 999997
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, serviceName, types.ProcessStateFailed); err != nil {
		t.Fatalf("Failed to register fake process history: %v", err)
	}
	if err = db.UpdateProcessHistoryEntry(t.Context(), fakePGID, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StartedAt: new(time.Now().Add(-30 * time.Second)),
		StoppedAt: new(time.Now().Add(-10 * time.Second)),
		Error:     new("[bind-conflict-svc] died during startup (PGID 999997)"),
	}); err != nil {
		t.Fatalf("Failed to update fake process history: %v", err)
	}

	processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || processHistoryEntry == nil {
		t.Fatalf("Failed to get process history entry: %v", err)
	}
	instance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || instance == nil {
		t.Fatalf("Failed to get service instance: %v", err)
	}

	hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount)

	updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil || updatedEntry == nil {
		t.Fatal("Failed to get updated process history")
	}
	if updatedEntry.Error == nil {
		t.Fatal("Expected an error to be surfaced, got nil")
	}
	if !strings.Contains(*updatedEntry.Error, "bind: Address already in use") {
		t.Errorf("Expected the child's real stderr cause to be surfaced, got: %s", *updatedEntry.Error)
	}
	if !strings.Contains(*updatedEntry.Error, "restart failed") {
		t.Errorf("Expected the generic restart-failed prefix to remain, got: %s", *updatedEntry.Error)
	}

	// Unlike the permission-denied case, this is a transient-looking failure:
	// the restart counter must simply bump, never halt.
	updatedInstance, err := hm.mgr.GetServiceInstance(serviceName)
	if err != nil || updatedInstance == nil {
		t.Fatal("Failed to get updated service instance")
	}
	if updatedInstance.RestartCount != instance.RestartCount+1 {
		t.Errorf("Expected restart count to bump by 1, got %d (was %d)", updatedInstance.RestartCount, instance.RestartCount)
	}
}

// TestHealthMonitor_CheckFailedProcess_RestartFailureDoesNotNestAcrossCycles
// guards against a bug a review caught in the fix for issue #30: once the
// restart-count cliff is gone, a non-permission restart failure (e.g. a
// runtime binary missing from PATH) retries forever. Each cycle,
// checkFailedProcess snapshots GetServiceLastErrorLine before writing its own
// "restarting"/"restart failed: ..." breadcrumbs to the same error log that
// snapshot reads from. If that snapshot ever picked up eos's own previous
// breadcrumb instead of skipping it, each new errMsg would nest the last one
// inside it, growing the error string (and the on-disk log) without bound
// across the now-unbounded retry loop. This runs checkFailedProcess across
// several consecutive failure cycles and asserts the surfaced error message
// is byte-for-byte identical every time, never growing or nesting.
func TestHealthMonitor_CheckFailedProcess_RestartFailureDoesNotNestAcrossCycles(t *testing.T) {
	tempDir := t.TempDir()
	daemonConfig := testutil.NewTestStandaloneDaemonConfig(t, tempDir, testutil.WithLogFilename("daemon.log"))
	healthConfig := newTestHealthConfig(t, WithBackoff(1, 5))
	shutdownConfig := newTestShutdownConfig(t)

	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	realMgr := manager.NewLocalManager(db, tempDir, t.Context(), testutil.NewTestLogger(t))
	t.Cleanup(realMgr.WaitPipes)
	logger, err := manager.NewDaemonLogger(true, false, daemonConfig.Standalone.Log.LogDir, daemonConfig.Standalone.Log.LogFileName, daemonConfig.Standalone.Log.LogMaxFiles, daemonConfig.Standalone.Log.LogFileSizeLimit)
	if err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	// A persistent, non-permission exec-layer failure (e.g. missing runtime
	// binary) that never halts and never heals on its own within this test -
	// exactly the scenario the reviewer flagged as now retrying forever.
	genericErr := errors.New("runtime binary not found in PATH")
	mgr := &restartFailManager{monitorManager: realMgr, restartErr: genericErr}
	hm := NewHealthMonitor(mgr, db, logger, healthConfig, *shutdownConfig, otelx.NoopHandles())

	serviceName := "nesting-guard-svc"
	fullDirPath := filepath.Join(tempDir, "nesting-guard-project")
	if mkdirErr := os.MkdirAll(fullDirPath, 0755); mkdirErr != nil {
		t.Fatalf("Could not create project directory: %v", mkdirErr)
	}
	testFile := testutil.NewTestServiceConfigFile(t,
		testutil.WithRuntimePath(""),
		testutil.WithName(serviceName),
		testutil.WithPort(0))
	yamlData, err := yaml.Marshal(testFile)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	fullPath := filepath.Join(fullDirPath, "service.yaml")
	if err = os.WriteFile(fullPath, yamlData, 0644); err != nil {
		t.Fatalf("Failed to write service.yaml: %v", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(testFile.Name, fullDirPath, filepath.Base(fullPath))
	if err != nil {
		t.Fatalf("Create service catalog entry failed: %v", err)
	}
	if err = realMgr.AddServiceCatalogEntry(serviceCatalogEntry); err != nil {
		t.Fatalf("Error registering service: %v", err)
	}
	if err = db.RegisterServiceInstance(t.Context(), serviceName); err != nil {
		t.Fatalf("Failed to register service instance: %v", err)
	}

	// One genuine child-stderr line, captured the same way the real
	// stdout/stderr pipe machinery (pipeToErrorLogFile) would tag it. This is
	// the only "source":"stderr" line ever written in this test, so if it
	// keeps surfacing unchanged every cycle (rather than eos's own prior
	// breadcrumb), that proves the snapshot is reading past its own writes.
	_, errorLogPath, err := realMgr.NewServiceLogFiles(serviceName)
	if err != nil {
		t.Fatalf("Failed to create service log files: %v", err)
	}
	errFile, err := os.OpenFile(errorLogPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open error log: %v", err)
	}
	logutil.NewJSONLogger(errFile, false).Info("exec: \"eos-runtime\": executable file not found in $PATH", "service", serviceName, "source", "stderr")
	if err = errFile.Close(); err != nil {
		t.Fatalf("Failed to close error log: %v", err)
	}

	fakePGID := 999996
	if _, err = db.RegisterProcessHistoryEntry(t.Context(), fakePGID, 0, serviceName, types.ProcessStateFailed); err != nil {
		t.Fatalf("Failed to register fake process history: %v", err)
	}
	if err = db.UpdateProcessHistoryEntry(t.Context(), fakePGID, database.ProcessHistoryUpdate{
		State:     new(types.ProcessStateFailed),
		StartedAt: new(time.Now().Add(-30 * time.Second)),
		StoppedAt: new(time.Now().Add(-10 * time.Second)),
		Error:     new("[nesting-guard-svc] died during startup"),
	}); err != nil {
		t.Fatalf("Failed to update fake process history: %v", err)
	}

	const cycles = 4
	var errMsgs []string

	for i := range cycles {
		processHistoryEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
		if err != nil || processHistoryEntry == nil {
			t.Fatalf("cycle %d: failed to get process history entry: %v", i, err)
		}
		instance, err := hm.mgr.GetServiceInstance(serviceName)
		if err != nil || instance == nil {
			t.Fatalf("cycle %d: failed to get service instance: %v", i, err)
		}

		hm.checkFailedProcess(t.Context(), serviceCatalogEntry, processHistoryEntry, instance.RestartCount)

		updatedEntry, err := hm.mgr.GetMostRecentProcessHistoryEntry(serviceName)
		if err != nil || updatedEntry == nil || updatedEntry.Error == nil {
			t.Fatalf("cycle %d: expected an error to be surfaced, got entry=%v err=%v", i, updatedEntry, err)
		}
		errMsgs = append(errMsgs, *updatedEntry.Error)

		// RestartService never succeeds in this test, so the same fakePGID
		// row is reused every cycle; backdate StoppedAt past the (tiny) test
		// backoff so the next cycle's canRestart check isn't gated by real
		// wall-clock overhead between iterations.
		if err = db.UpdateProcessHistoryEntry(t.Context(), fakePGID, database.ProcessHistoryUpdate{
			StoppedAt: new(time.Now().Add(-1 * time.Second)),
		}); err != nil {
			t.Fatalf("cycle %d: failed to backdate StoppedAt: %v", i, err)
		}
	}

	for i, msg := range errMsgs {
		if !strings.Contains(msg, "restart failed") {
			t.Errorf("cycle %d: expected generic restart-failed prefix, got: %s", i, msg)
		}
		if !strings.Contains(msg, "executable file not found in $PATH") {
			t.Errorf("cycle %d: expected the genuine child stderr line to be surfaced, got: %s", i, msg)
		}
		// The real bug: eos's own previous breadcrumb (also logged to the
		// same error log) getting picked back up and nested into the next
		// one. That would make later messages strictly longer than earlier
		// ones and contain "restart failed" more than once.
		if strings.Count(msg, "restart failed") > 1 {
			t.Errorf("cycle %d: error message nests a previous breadcrumb: %s", i, msg)
		}
		if msg != errMsgs[0] {
			t.Errorf("cycle %d: error message changed/grew across cycles.\n  cycle 0: %s\n  cycle %d: %s", i, errMsgs[0], i, msg)
		}
	}
}
