package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// fakeServer listens on a fresh Unix socket and handles ONE request with handler.
// The socket path is returned; cleanup is registered on t.
func fakeServer(t *testing.T, handler func(req types.DaemonRequest) types.DaemonResponse) string {
	t.Helper()
	dir, mkdirErr := os.MkdirTemp("", "eos-dm-*")
	if mkdirErr != nil {
		t.Fatalf("MkdirTemp: %v", mkdirErr)
	}
	socketPath := filepath.Join(dir, "d.sock")

	lc := net.ListenConfig{}
	ln, listenErr := lc.Listen(t.Context(), "unix", socketPath)
	if listenErr != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("listen: %v", listenErr)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		var req types.DaemonRequest
		if decErr := json.NewDecoder(conn).Decode(&req); decErr != nil {
			return
		}
		resp := handler(req)
		_ = json.NewEncoder(conn).Encode(resp)
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		_ = os.RemoveAll(dir)
	})

	return socketPath
}

func okResponse(data any) types.DaemonResponse {
	raw, _ := json.Marshal(data)
	return types.DaemonResponse{Success: true, Data: raw}
}

func newTestDM(t *testing.T, socketPath string) *DaemonManager {
	t.Helper()
	return &DaemonManager{ctx: t.Context(), socketPath: socketPath}
}

func TestSendRequest_connectionRefused(t *testing.T) {
	dm := &DaemonManager{ctx: t.Context(), socketPath: "/nonexistent/socket.sock"}
	_, err := dm.sendRequest(types.MethodGetAllServiceInstances, nil)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestSendRequest_serverError(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return types.DaemonResponse{Success: false, Error: "something went wrong"}
	})
	dm := newTestDM(t, socketPath)
	_, err := dm.sendRequest(types.MethodGetAllServiceInstances, nil)
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestSendRequest_sentinelError(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return types.DaemonResponse{
			Success:   false,
			ErrorCode: CodeServiceNotRunning,
			Error:     "service not running",
		}
	})
	dm := newTestDM(t, socketPath)
	_, err := dm.sendRequest(types.MethodGetServiceInstance, nil)
	if !errors.Is(err, ErrServiceNotRunning) {
		t.Errorf("expected ErrServiceNotRunning, got %v", err)
	}
}

func TestDaemonManager_GetAllServiceInstances(t *testing.T) {
	socketPath := fakeServer(t, func(req types.DaemonRequest) types.DaemonResponse {
		if req.Method != types.MethodGetAllServiceInstances {
			return types.DaemonResponse{Success: false, Error: "wrong method"}
		}
		return okResponse(types.GetAllServiceInstancesResponse{
			Instances: []types.ServiceInstance{{Name: "svc1"}},
		})
	})
	dm := newTestDM(t, socketPath)
	instances, err := dm.GetAllServiceInstances()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 || instances[0].Name != "svc1" {
		t.Errorf("got %+v", instances)
	}
}

func TestDaemonManager_GetServiceInstance(t *testing.T) {
	socketPath := fakeServer(t, func(req types.DaemonRequest) types.DaemonResponse {
		var args types.GetServiceInstanceArgs
		_ = json.Unmarshal(req.Args, &args)
		if args.Name != "my-svc" {
			return types.DaemonResponse{Success: false, Error: "wrong name"}
		}
		return okResponse(types.GetServiceInstanceResponse{
			Instance: types.ServiceInstance{Name: "my-svc"},
		})
	})
	dm := newTestDM(t, socketPath)
	inst, err := dm.GetServiceInstance("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.Name != "my-svc" {
		t.Errorf("got name %q", inst.Name)
	}
}

func TestDaemonManager_RemoveServiceInstance(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(map[string]bool{"removed": true})
	})
	dm := newTestDM(t, socketPath)
	removed, err := dm.RemoveServiceInstance("svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}
}

func TestDaemonManager_StartService(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(map[string]int{"pid": 1234})
	})
	dm := newTestDM(t, socketPath)
	pid, err := dm.StartService("svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 1234 {
		t.Errorf("expected pid 1234, got %d", pid)
	}
}

func TestDaemonManager_RestartService(t *testing.T) {
	socketPath := fakeServer(t, func(req types.DaemonRequest) types.DaemonResponse {
		var args types.RestartServiceArgs
		_ = json.Unmarshal(req.Args, &args)
		// Durations must survive wire encoding as non-empty strings.
		if args.GracePeriod == "" || args.TickerPeriod == "" {
			return types.DaemonResponse{Success: false, Error: "missing duration args"}
		}
		return okResponse(map[string]int{"pid": 5678})
	})
	dm := newTestDM(t, socketPath)
	pid, err := dm.RestartService("svc", 5*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 5678 {
		t.Errorf("expected pid 5678, got %d", pid)
	}
}

func TestDaemonManager_StopService(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(StopServiceResult{Stopped: map[int]bool{42: true}})
	})
	dm := newTestDM(t, socketPath)
	result, err := dm.StopService("svc", 5*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Stopped[42]; !ok {
		t.Errorf("expected pid 42 in Stopped, got %+v", result)
	}
}

func TestDaemonManager_ForceStopService(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(StopServiceResult{Stopped: map[int]bool{99: true}})
	})
	dm := newTestDM(t, socketPath)
	result, err := dm.ForceStopService("svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Stopped[99]; !ok {
		t.Errorf("expected pid 99 in Stopped, got %+v", result)
	}
}

func TestDaemonManager_AddServiceCatalogEntry(t *testing.T) {
	socketPath := fakeServer(t, func(req types.DaemonRequest) types.DaemonResponse {
		var args types.AddServiceCatalogEntryArgs
		_ = json.Unmarshal(req.Args, &args)
		if args.Service == nil || args.Service.Name != "new-svc" {
			return types.DaemonResponse{Success: false, Error: "wrong args"}
		}
		return okResponse(nil)
	})
	dm := newTestDM(t, socketPath)
	if err := dm.AddServiceCatalogEntry(&types.ServiceCatalogEntry{Name: "new-svc"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonManager_GetAllServiceCatalogEntries(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse([]types.ServiceCatalogEntry{{Name: "svc-a"}, {Name: "svc-b"}})
	})
	dm := newTestDM(t, socketPath)
	entries, err := dm.GetAllServiceCatalogEntries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestDaemonManager_GetServiceCatalogEntry(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(types.ServiceCatalogEntry{Name: "my-svc", DirectoryPath: "/opt/svc"})
	})
	dm := newTestDM(t, socketPath)
	entry, err := dm.GetServiceCatalogEntry("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Name != "my-svc" {
		t.Errorf("got name %q", entry.Name)
	}
}

func TestDaemonManager_IsServiceRegistered(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(map[string]bool{"exists": true})
	})
	dm := newTestDM(t, socketPath)
	registered, err := dm.IsServiceRegistered("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !registered {
		t.Error("expected registered=true")
	}
}

func TestDaemonManager_RemoveServiceCatalogEntry(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(map[string]bool{"removed": true})
	})
	dm := newTestDM(t, socketPath)
	removed, err := dm.RemoveServiceCatalogEntry("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}
}

func TestDaemonManager_UpdateServiceCatalogEntry(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(nil)
	})
	dm := newTestDM(t, socketPath)
	if err := dm.UpdateServiceCatalogEntry("my-svc", "/new/path", "service.yaml"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonManager_GetMostRecentProcessHistoryEntry(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(types.GetMostRecentProcessHistoryEntryResponse{
			ProcessEntry: types.ProcessHistory{
				PGID:  42,
				State: types.ProcessStateRunning,
			},
		})
	})
	dm := newTestDM(t, socketPath)
	entry, err := dm.GetMostRecentProcessHistoryEntry("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.PGID != 42 {
		t.Errorf("expected PGID 42, got %d", entry.PGID)
	}
}

func TestDaemonManager_NewServiceLogFiles(t *testing.T) {
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(ServiceLogFilesResult{
			LogFilePath:      "/logs/svc.log",
			ErrorLogFilePath: "/logs/svc.error.log",
		})
	})
	dm := newTestDM(t, socketPath)
	logPath, errLogPath, err := dm.NewServiceLogFiles("my-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logPath != "/logs/svc.log" || errLogPath != "/logs/svc.error.log" {
		t.Errorf("got logPath=%q errLogPath=%q", logPath, errLogPath)
	}
}

func TestDaemonManager_GetServiceLogFilePath(t *testing.T) {
	path := "/logs/svc.log"
	socketPath := fakeServer(t, func(_ types.DaemonRequest) types.DaemonResponse {
		return okResponse(map[string]*string{"filepath": &path})
	})
	dm := newTestDM(t, socketPath)
	result, err := dm.GetServiceLogFilePath("my-svc", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || *result != path {
		t.Errorf("got %v", result)
	}
}

// TestNewDaemonManager_daemonAlreadyRunning verifies NewDaemonManager succeeds
// (does not error) when a live daemon is already listening on the socket.
func TestNewDaemonManager_daemonAlreadyRunning(t *testing.T) {
	dir, mkdirErr := os.MkdirTemp("", "eos-dm-nr-*")
	if mkdirErr != nil {
		t.Fatalf("MkdirTemp: %v", mkdirErr)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	pidFile := filepath.Join(dir, "eos.pid")
	socketPath := filepath.Join(dir, "eos.sock")

	if err := os.WriteFile(pidFile, fmt.Appendf(nil, "%d", os.Getpid()), 0644); err != nil {
		t.Fatalf("WriteFile pid: %v", err)
	}

	lc := net.ListenConfig{}
	ln, err := lc.Listen(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	dm, err := NewDaemonManager(t.Context(), socketPath, pidFile, time.Second, false)
	if err != nil {
		t.Fatalf("NewDaemonManager: %v", err)
	}
	if dm == nil {
		t.Fatal("expected non-nil DaemonManager")
	}
}
