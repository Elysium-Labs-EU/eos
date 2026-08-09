package cmd

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/process"
)

// TestRunningExeVersion_Resolved covers the fixed happy path: exec'ing a live
// process's own /proc/<pid>/exe, unaffected by whatever readlink of that path
// would report.
func TestRunningExeVersion_Resolved(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/exe resolution is Linux-only")
	}
	child := spawnDisposableChild(t)

	version, err := runningExeVersion(t.Context(), child.Process.Pid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version == "" {
		t.Error("expected a non-empty version string from GNU coreutils sleep --version")
	}
}

// TestRunningExeVersion_ProcessGone is the regression case for the install.sh
// `mv -f` bug: a pid whose /proc/<pid>/exe no longer exists (the process has
// exited) must classify as errVersionUnresolvable, not a surfaced failure —
// this is the expected "cannot resolve" case, same as before this fix.
func TestRunningExeVersion_ProcessGone(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/exe resolution is Linux-only")
	}
	pid := deadPID(t)

	_, err := runningExeVersion(t.Context(), pid)
	if err == nil {
		t.Fatal("expected an error for a dead pid")
	}
	if !errors.Is(err, errVersionUnresolvable) {
		t.Errorf("expected errVersionUnresolvable, got: %v", err)
	}
}

// TestRunningExeVersion_RealFailure covers a resolution failure that is NOT
// the benign "process gone" case: the pid is alive and /proc/<pid>/exe
// exists, but the exec itself fails for another reason (here, a context
// that is already done). This must NOT classify as errVersionUnresolvable,
// since the operator should be told about it rather than see silence.
func TestRunningExeVersion_RealFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/exe resolution is Linux-only")
	}
	child := spawnDisposableChild(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runningExeVersion(ctx, child.Process.Pid)
	if err == nil {
		t.Fatal("expected an error from an already-canceled context")
	}
	if errors.Is(err, errVersionUnresolvable) {
		t.Errorf("a live process's context-canceled exec failure must not be classified as unresolvable, got: %v", err)
	}
}

func TestRenderSystemdVersion(t *testing.T) {
	t.Run("success prints the version line", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		renderSystemdVersion(cmd, "v9.9.9", nil)

		if !strings.Contains(out.String(), "running version: v9.9.9") {
			t.Errorf("expected 'running version: v9.9.9', got: %s", out.String())
		}
	})

	t.Run("unresolvable prints nothing", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		renderSystemdVersion(cmd, "", errVersionUnresolvable)

		if out.Len() != 0 {
			t.Errorf("expected no output for an unresolvable error, got: %s", out.String())
		}
	})

	t.Run("a real failure prints a warning", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		renderSystemdVersion(cmd, "", errors.New("permission denied"))

		got := out.String()
		if !strings.Contains(got, "warning") {
			t.Errorf("expected a warning label, got: %s", got)
		}
		if !strings.Contains(got, "permission denied") {
			t.Errorf("expected the underlying error text, got: %s", got)
		}
	})
}

func TestSystemdStaleBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/exe resolution is Linux-only")
	}
	child := spawnDisposableChild(t)
	pid := child.Process.Pid
	realIno := process.RunningExeInode(pid)
	if realIno == 0 {
		t.Fatal("could not resolve inode of spawned child's executable")
	}

	if systemdStaleBinary(pid, realIno) {
		t.Error("matching inode should not be flagged stale")
	}
	if !systemdStaleBinary(pid, realIno+1) {
		t.Error("mismatched inode should be flagged stale")
	}
	if systemdStaleBinary(pid, 0) {
		t.Error("currentIno=0 (unknown) should never flag stale")
	}
}

func TestRenderSystemdRunState(t *testing.T) {
	t.Run("stale prints the restart warning", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		renderSystemdRunState(cmd, 1234, true)

		got := out.String()
		if !strings.Contains(got, "running (pid 1234)") {
			t.Errorf("expected the pid rendered, got: %s", got)
		}
		if !strings.Contains(got, "since-replaced binary, restart needed") {
			t.Errorf("expected the stale-binary warning, got: %s", got)
		}
	})

	t.Run("fresh prints a plain running line", func(t *testing.T) {
		var out bytes.Buffer
		cmd := newTestRootCmd(nil)
		cmd.SetOut(&out)

		renderSystemdRunState(cmd, 1234, false)

		got := out.String()
		if !strings.Contains(got, "running (pid 1234)") {
			t.Errorf("expected the pid rendered, got: %s", got)
		}
		if strings.Contains(got, "restart needed") {
			t.Errorf("expected no stale-binary warning, got: %s", got)
		}
	})
}
