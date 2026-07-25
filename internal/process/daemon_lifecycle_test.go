package process

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/config"
)

// spawnDisposableChild spawns a child and reaps it in the background as soon
// as it exits, mirroring how a real standalone daemon (reparented to init on
// fork) gets reaped immediately rather than lingering as a zombie visible to
// signal(0) — StopStandaloneDaemon's exit-wait polls signal(0), which would
// otherwise never observe an unreaped zombie as gone.
func spawnDisposableChild(t *testing.T) *exec.Cmd {
	t.Helper()
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to spawn disposable child: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		<-reaped
	})
	return child
}

func deadPID(t *testing.T) int {
	t.Helper()
	child := exec.Command("true")
	if err := child.Start(); err != nil {
		t.Fatalf("failed to spawn short-lived child: %v", err)
	}
	pid := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatalf("failed to wait for short-lived child: %v", err)
	}
	return pid
}

func writePIDFile(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644); err != nil {
		t.Fatalf("writing pid file: %v", err)
	}
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("touching file: %v", err)
	}
}

func TestStopStandaloneDaemon_noPIDFile(t *testing.T) {
	tempDir := t.TempDir()
	stopped, err := StopStandaloneDaemon(filepath.Join(tempDir, "eos.pid"), filepath.Join(tempDir, "eos.sock"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopped {
		t.Error("expected stopped=false when pid file is missing")
	}
}

func TestStopStandaloneDaemon_noSocket(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	writePIDFile(t, pidFile, os.Getpid())

	stopped, err := StopStandaloneDaemon(pidFile, filepath.Join(tempDir, "eos.sock"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopped {
		t.Error("expected stopped=false when socket file is missing")
	}
}

func TestStopStandaloneDaemon_deadProcess(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	socketPath := filepath.Join(tempDir, "eos.sock")
	writePIDFile(t, pidFile, deadPID(t))
	touchFile(t, socketPath)

	stopped, err := StopStandaloneDaemon(pidFile, socketPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopped {
		t.Error("expected stopped=false for a dead process")
	}
}

func TestStopStandaloneDaemon_liveProcess(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	socketPath := filepath.Join(tempDir, "eos.sock")

	child := spawnDisposableChild(t)
	writePIDFile(t, pidFile, child.Process.Pid)
	touchFile(t, socketPath)

	stopped, err := StopStandaloneDaemon(pidFile, socketPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopped {
		t.Error("expected stopped=true for a live process")
	}
}

// spawnSIGTERMIgnoringChild starts a child that traps SIGTERM and ignores it,
// so tests can exercise waitForProcessExit's timeout branch instead of only
// ever hitting the fast-exit path. It blocks until the child's own "ready"
// line confirms the trap is actually installed before returning — sending
// SIGTERM any earlier races the shell's default (terminating) disposition
// for TERM against the trap statement that's supposed to replace it.
func spawnSIGTERMIgnoringChild(t *testing.T) *exec.Cmd {
	t.Helper()
	child := exec.Command("sh", "-c", "trap '' TERM; echo ready; sleep 30")
	stdout, pipeErr := child.StdoutPipe()
	if pipeErr != nil {
		t.Fatalf("creating stdout pipe: %v", pipeErr)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("failed to spawn SIGTERM-ignoring child: %v", err)
	}
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("waiting for child to install its TERM trap: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		<-reaped
	})
	return child
}

func TestWaitForProcessExit_timesOutWhenProcessIgnoresSIGTERM(t *testing.T) {
	child := spawnSIGTERMIgnoringChild(t)

	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}

	err := waitForProcessExit(child.Process, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error when process ignores SIGTERM")
	}
	if !strings.Contains(err.Error(), "did not exit within") {
		t.Errorf("expected timeout message, got: %v", err)
	}
}

func TestStatusStandaloneDaemon_notFound(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.StandaloneDaemonConfig{PIDFile: filepath.Join(tempDir, "eos.pid")}

	status, err := StatusStandaloneDaemon(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Running || status.Pid != nil || status.Process != nil {
		t.Errorf("expected not-found status, got %+v", status)
	}
}

func TestStatusStandaloneDaemon_deadProcess(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	writePIDFile(t, pidFile, deadPID(t))
	cfg := &config.StandaloneDaemonConfig{PIDFile: pidFile}

	status, err := StatusStandaloneDaemon(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Running {
		t.Error("expected Running=false for a dead process")
	}
	if status.Pid == nil {
		t.Error("expected non-nil Pid even for a dead process")
	}
}

func TestStatusStandaloneDaemon_liveProcess(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	writePIDFile(t, pidFile, os.Getpid())
	cfg := &config.StandaloneDaemonConfig{PIDFile: pidFile}

	status, err := StatusStandaloneDaemon(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Running {
		t.Error("expected Running=true for the test process itself")
	}
	if status.Process == nil {
		t.Error("expected non-nil Process for a live process")
	}
}

func TestRemoveStandaloneDaemon_runningRejected(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	writePIDFile(t, pidFile, os.Getpid())
	cfg := &config.StandaloneDaemonConfig{PIDFile: pidFile, SocketPath: filepath.Join(tempDir, "eos.sock")}

	removed, err := RemoveStandaloneDaemon(cfg)
	if err == nil {
		t.Fatal("expected error when daemon is still running")
	}
	if removed {
		t.Error("expected removed=false when daemon is still running")
	}
}

func TestRemoveStandaloneDaemon_removesFiles(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "eos.pid")
	socketPath := filepath.Join(tempDir, "eos.sock")
	writePIDFile(t, pidFile, deadPID(t))
	touchFile(t, socketPath)
	cfg := &config.StandaloneDaemonConfig{PIDFile: pidFile, SocketPath: socketPath}

	removed, err := RemoveStandaloneDaemon(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Error("expected pid file to be removed")
	}
	if _, statErr := os.Stat(socketPath); !os.IsNotExist(statErr) {
		t.Error("expected socket file to be removed")
	}
}

func TestRemoveStandaloneDaemon_missingFilesOK(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.StandaloneDaemonConfig{
		PIDFile:    filepath.Join(tempDir, "eos.pid"),
		SocketPath: filepath.Join(tempDir, "eos.sock"),
	}

	removed, err := RemoveStandaloneDaemon(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Error("expected removed=true when files never existed")
	}
}
