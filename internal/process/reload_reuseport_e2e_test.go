package process

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/monitor"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

// runReusePortServer is the managed service the reload OS-level test drives: a
// bare TCP server that binds its port with SO_REUSEPORT so a second instance can
// listen on the same address during a reload overlap — exactly the contract eos
// requires of a reloadable service. It binds before doing anything else, answers
// every connection with "ok", and on SIGTERM stops accepting but lets in-flight
// handlers finish before exiting, so a draining old instance never resets a
// connection it already accepted. Reached via the test binary re-exec in
// TestMain (see reusePortServerEnv).
func runReusePortServer() int {
	port := os.Getenv("PORT")
	if port == "" {
		fmt.Fprintln(os.Stderr, "reuseport server: PORT not set")
		return 1
	}

	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var sockErr error
		if ctrlErr := c.Control(func(fd uintptr) {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
				sockErr = err
				return
			}
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}); ctrlErr != nil {
			return ctrlErr
		}
		return sockErr
	}}

	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reuseport server listen: %v\n", err)
		return 1
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM)
	go func() {
		<-sigc
		_ = ln.Close() // stop accepting; the Accept loop below breaks out
	}()

	var handlers sync.WaitGroup
	for {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			break // listener closed on SIGTERM
		}
		handlers.Add(1)
		go func(c net.Conn) {
			defer handlers.Done()
			_ = c.SetDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 8)
			_, _ = c.Read(buf)
			_, _ = c.Write([]byte("ok"))
			_ = c.Close()
		}(conn)
	}
	handlers.Wait()
	return 0
}

// freeLoopbackPort grabs an ephemeral loopback port and releases it, returning
// the number for the service to rebind under SO_REUSEPORT.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		t.Fatalf("listener addr is not *net.TCPAddr: %T", ln.Addr())
	}
	_ = ln.Close()
	return addr.Port
}

// probeOnce does one full request/response against the port, returning nil only
// when a connection was accepted and answered "ok". A dial failure is the
// availability gap the reload must never open.
func probeOnce(port int, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte("ping")); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	buf := make([]byte, 2)
	if _, err := conn.Read(buf); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if string(buf) != "ok" {
		return fmt.Errorf("unexpected response %q", buf)
	}
	return nil
}

// waitPortReachable blocks until the port answers a full request or the deadline
// passes, so the load generator starts only once the first instance is serving.
func waitPortReachable(t *testing.T, port int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if probeOnce(port, 200*time.Millisecond) == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("port %d never became reachable within %s", port, within)
}

// runningReusePortService registers and starts one SO_REUSEPORT service (the
// test binary re-exec'd in server mode) under a LocalManager anchored to a temp
// dir, waits for it to serve, and returns the manager, service name, port, and
// the running instance's PGID. Cleanup that reaps every launched process group
// and manager goroutine — required before goleak inspects the process in
// TestMain — is registered on t.
func runningReusePortService(t *testing.T) (mgr *manager.LocalManager, serviceName string, port, pgid int) {
	t.Helper()
	db, _, tempDir := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	// Anchor everything to the test temp dir; never leak into a real ~/.eos.
	t.Setenv("EOS_BASE_DIR", tempDir)
	// Every child the service spawns re-execs this test binary in server mode.
	t.Setenv(reusePortServerEnv, "1")

	port = freeLoopbackPort(t)
	serviceName = "reuseport-svc"

	ctx, cancel := context.WithCancel(context.Background())
	mgr = manager.NewLocalManager(db, tempDir, ctx, testutil.NewTestLogger(t))

	cfg := &types.ServiceConfig{Name: serviceName, Command: os.Args[0], Port: port}
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	dir := filepath.Join(tempDir, serviceName)
	if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
		t.Fatalf("mkdir: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "service.yaml"), yamlData, 0644); writeErr != nil {
		t.Fatalf("write config: %v", writeErr)
	}
	entry, err := manager.NewServiceCatalogEntry(serviceName, dir, "service.yaml")
	if err != nil {
		t.Fatalf("catalog entry: %v", err)
	}
	if regErr := mgr.AddServiceCatalogEntry(entry); regErr != nil {
		t.Fatalf("register service: %v", regErr)
	}

	pgid, err = mgr.StartService(serviceName)
	if err != nil {
		t.Fatalf("StartService: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		mgr.WaitServices()
		mgr.WaitPipes()
	})

	waitPortReachable(t, port, 5*time.Second)
	return mgr, serviceName, port, pgid
}

// TestReloadZeroDowntimeSOReusePort is the OS-level proof: a real SO_REUSEPORT
// server is reloaded while a load generator hammers the port throughout, and no
// connection is dropped and no availability gap opens. It exercises the exact
// production gate (monitor.ProbeReady) and the manager's real launch→probe→drain
// path against actual processes and sockets, not mocks.
func TestReloadZeroDowntimeSOReusePort(t *testing.T) {
	mgr, serviceName, port, oldPGID := runningReusePortService(t)

	// Hammer the port continuously across the reload. Any dial/read failure is a
	// dropped connection — the exact thing the SO_REUSEPORT overlap must prevent.
	var (
		stop      = make(chan struct{})
		loadWG    sync.WaitGroup
		mu        sync.Mutex
		successes int
		failures  []string
	)
	loadWG.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if probeErr := probeOnce(port, time.Second); probeErr != nil {
				mu.Lock()
				failures = append(failures, probeErr.Error())
				mu.Unlock()
			} else {
				mu.Lock()
				successes++
				mu.Unlock()
			}
			time.Sleep(2 * time.Millisecond)
		}
	})

	result, err := mgr.ReloadService(serviceName, monitor.ProbeReady, manager.ReloadConfig{
		GracePeriod:      3 * time.Second,
		TickerPeriod:     50 * time.Millisecond,
		ReadinessTimeout: 5 * time.Second,
		ProbeInterval:    100 * time.Millisecond,
	})
	if err != nil {
		close(stop)
		loadWG.Wait()
		t.Fatalf("ReloadService: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-result.NewPGID, syscall.SIGKILL) })

	// Keep hammering briefly after the cutover so a post-drain gap would surface.
	time.Sleep(200 * time.Millisecond)
	close(stop)
	loadWG.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(failures) > 0 {
		t.Errorf("reload dropped %d/%d connections; first failures: %v", len(failures), len(failures)+successes, firstN(failures, 5))
	}
	if successes == 0 {
		t.Errorf("load generator recorded no successful connections; test proved nothing")
	}

	if result.OldPGID != oldPGID {
		t.Errorf("OldPGID = %d, want %d", result.OldPGID, oldPGID)
	}
	if result.NewPGID == 0 || result.NewPGID == oldPGID {
		t.Errorf("NewPGID = %d, want a fresh pgid distinct from %d", result.NewPGID, oldPGID)
	}
	if procutil.IsAlive(oldPGID) {
		t.Errorf("old instance pgid %d should have been drained after cutover", oldPGID)
	}
	if !procutil.IsAlive(result.NewPGID) {
		t.Errorf("new instance pgid %d should be serving after cutover", result.NewPGID)
	}
	t.Logf("reload served %d connections with zero drops (pgid %d → %d)", successes, oldPGID, result.NewPGID)
}

func firstN(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// TestReloadDaemonDispatch drives the reload through the daemon's IPC dispatch
// path (executeRequest → handleReloadService), the entry point the CLI reaches
// over the socket, proving the handler parses the durations, wires
// monitor.ProbeReady as the gate, runs the real cutover, and reports the swapped
// PGIDs back in the response.
func TestReloadDaemonDispatch(t *testing.T) {
	mgr, serviceName, _, oldPGID := runningReusePortService(t)

	args, err := json.Marshal(types.ReloadServiceArgs{
		Name:             serviceName,
		GracePeriod:      (3 * time.Second).String(),
		TickerPeriod:     (50 * time.Millisecond).String(),
		ReadinessTimeout: (5 * time.Second).String(),
		ProbeInterval:    (100 * time.Millisecond).String(),
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	resp := executeRequest(mgr, types.DaemonRequest{Method: types.MethodReloadService, Args: args})
	if !resp.Success {
		t.Fatalf("reload dispatch failed: %s", resp.Error)
	}

	var result types.ReloadServiceResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result.OldPGID != oldPGID {
		t.Errorf("OldPGID = %d, want %d", result.OldPGID, oldPGID)
	}
	if result.NewPGID == 0 || result.NewPGID == oldPGID {
		t.Errorf("NewPGID = %d, want a fresh pgid distinct from %d", result.NewPGID, oldPGID)
	}
	t.Cleanup(func() { _ = syscall.Kill(-result.NewPGID, syscall.SIGKILL) })

	if procutil.IsAlive(oldPGID) {
		t.Errorf("old instance pgid %d should have been drained", oldPGID)
	}
	if !procutil.IsAlive(result.NewPGID) {
		t.Errorf("new instance pgid %d should be serving", result.NewPGID)
	}
}

// TestReloadDispatchInvalidDuration checks the handler rejects a malformed
// duration arg with a plain failure response rather than attempting a cutover.
func TestReloadDispatchInvalidDuration(t *testing.T) {
	mgr, serviceName, _, _ := runningReusePortService(t)

	args, err := json.Marshal(types.ReloadServiceArgs{
		Name:             serviceName,
		GracePeriod:      "not-a-duration",
		TickerPeriod:     (50 * time.Millisecond).String(),
		ReadinessTimeout: (5 * time.Second).String(),
		ProbeInterval:    (100 * time.Millisecond).String(),
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	resp := executeRequest(mgr, types.DaemonRequest{Method: types.MethodReloadService, Args: args})
	if resp.Success {
		t.Fatal("reload dispatch should reject an invalid grace period")
	}
	if !strings.Contains(resp.Error, "grace period") {
		t.Errorf("error should name the bad grace period, got: %s", resp.Error)
	}
}
