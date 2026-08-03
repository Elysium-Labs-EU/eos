package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// fakeDaemonServer listens on a fresh unix socket and answers every connection
// it receives (one DaemonRequest/DaemonResponse per connection, matching how
// DaemonManager.sendRequest dials fresh each call) with handle. It keeps
// accepting until the listener is closed on test cleanup, so a single server
// can service the several IPC calls one cobra command invocation makes
// (e.g. IsServiceRegistered followed by ReloadService).
func fakeDaemonServer(t *testing.T, handle func(req types.DaemonRequest) types.DaemonResponse) string {
	t.Helper()
	// A short-lived, top-level temp dir rather than t.TempDir(): unix socket
	// paths are capped at ~104 bytes on macOS (sockaddr_un.sun_path), and
	// t.TempDir() nests under the (often long) test function name, which
	// blows that budget and fails with "bind: invalid argument".
	dir, err := os.MkdirTemp("", "eos-dm-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "d.sock")

	lc := net.ListenConfig{}
	ln, err := lc.Listen(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			func() {
				defer func() { _ = conn.Close() }()
				var req types.DaemonRequest
				if decErr := json.NewDecoder(conn).Decode(&req); decErr != nil {
					return
				}
				resp := handle(req)
				_ = json.NewEncoder(conn).Encode(resp)
			}()
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	return socketPath
}

// respondingTo builds a fakeDaemonServer handler that answers each method with
// a pre-scripted response. A method not present in responses fails loudly
// (rather than hanging or panicking) so a test that forgot to script a call
// notices immediately instead of getting a confusing zero-value result.
func respondingTo(responses map[types.MethodName]types.DaemonResponse) func(types.DaemonRequest) types.DaemonResponse {
	return func(req types.DaemonRequest) types.DaemonResponse {
		if resp, ok := responses[req.Method]; ok {
			return resp
		}
		return types.DaemonResponse{Success: false, Error: fmt.Sprintf("unscripted method in test: %s", req.Method)}
	}
}

// okDaemonResponse marshals data into a successful DaemonResponse envelope,
// mirroring the real daemon's response shape for a given IPC method.
func okDaemonResponse(t *testing.T, data any) types.DaemonResponse {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal fake daemon response data: %v", err)
	}
	return types.DaemonResponse{Success: true, Data: raw}
}

// newFakeDaemonManager builds a real *manager.DaemonManager wired to a fake
// unix-socket daemon peer that answers exactly the scripted responses. This
// exercises the actual DaemonManager IPC client code (used by reload/remove/logs
// when a real standalone daemon is in play), only the "daemon" on the other end
// of the socket is a test-controlled stub instead of a live eos daemon process.
//
// It writes this test process's own PID to a fresh PID file so
// NewDaemonManager's liveness check (isDaemonRunning) is satisfied without
// actually forking a daemon process.
func newFakeDaemonManager(t *testing.T, responses map[types.MethodName]types.DaemonResponse) manager.ServiceManager {
	t.Helper()
	socketPath := fakeDaemonServer(t, respondingTo(responses))

	pidFile := filepath.Join(t.TempDir(), "eos.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("write fake pid file: %v", err)
	}

	dm, err := manager.NewDaemonManager(t.Context(), socketPath, pidFile, time.Second, false)
	if err != nil {
		t.Fatalf("NewDaemonManager: %v", err)
	}
	return dm
}

// isServiceRegisteredOK is the canned IsServiceRegistered response every
// reload test below needs, since ensureServiceRegistered calls it before
// reload does anything else.
func isServiceRegisteredOK(t *testing.T) types.DaemonResponse {
	return okDaemonResponse(t, map[string]bool{"exists": true})
}

// TestReloadCommandServiceNotRunning checks that a daemon-reported
// ErrServiceNotRunning surfaces as a clear "is not running" message with a
// hint to start the service, rather than a generic failure.
func TestReloadCommandServiceNotRunning(t *testing.T) {
	mgr := newFakeDaemonManager(t, map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodReloadService: {
			Success:   false,
			ErrorCode: manager.CodeServiceNotRunning,
			Error:     "service not running",
		},
	})
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"reload", "cms"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "is not running") {
		t.Errorf("expected 'is not running', got: %s", out)
	}
	if !strings.Contains(out, "eos run cms") {
		t.Errorf("expected hint to run the service, got: %s", out)
	}
}

// TestReloadCommandNotReady checks that a daemon-reported ErrReloadNotReady
// (the new instance never passed its health check) surfaces the "kept the old
// instance running" reassurance plus a hint to check logs, not a generic error.
func TestReloadCommandNotReady(t *testing.T) {
	mgr := newFakeDaemonManager(t, map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodReloadService: {
			Success:   false,
			ErrorCode: manager.CodeReloadNotReady,
			Error:     "reload aborted: new instance not ready",
		},
	})
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"reload", "cms"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "new instance never became healthy") {
		t.Errorf("expected 'new instance never became healthy', got: %s", out)
	}
	if !strings.Contains(out, "kept the old instance running") {
		t.Errorf("expected reassurance that the old instance is kept, got: %s", out)
	}
	if !strings.Contains(out, "eos logs cms") {
		t.Errorf("expected hint to check logs, got: %s", out)
	}
}

// TestReloadCommandGenericError checks that any other ReloadService failure
// (not ErrServiceNotRunning, not ErrReloadNotReady) falls through to the
// generic "reloading service" error rather than being silently swallowed.
func TestReloadCommandGenericError(t *testing.T) {
	mgr := newFakeDaemonManager(t, map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodReloadService: {
			Success: false,
			Error:   "unexpected daemon-side failure",
		},
	})
	cmd := newTestRootCmd(mgr)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"reload", "cms"})

	err := cmd.ExecuteContext(t.Context())
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected ErrCommandFailed, got: %v", err)
	}
	if !strings.Contains(errBuf.String(), "reloading service") {
		t.Errorf("expected 'reloading service' error, got: %s", errBuf.String())
	}
}

// TestReloadCommandSuccess checks the happy path prints the swapped PGIDs.
func TestReloadCommandSuccess(t *testing.T) {
	mgr := newFakeDaemonManager(t, map[types.MethodName]types.DaemonResponse{
		types.MethodIsServiceRegistered: isServiceRegisteredOK(t),
		types.MethodReloadService: okDaemonResponse(t, types.ReloadServiceResponse{
			OldPGID: 111,
			NewPGID: 222,
		}),
	})
	cmd := newTestRootCmd(mgr)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"reload", "cms"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v\nerr output: %s", err, errBuf.String())
	}
	out := outBuf.String()
	if !strings.Contains(out, "111") || !strings.Contains(out, "222") {
		t.Errorf("expected reloaded PGIDs 111 -> 222 in output, got: %s", out)
	}
}
