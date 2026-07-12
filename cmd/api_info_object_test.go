package cmd

import (
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/types"
)

func TestCompileProcessInfoObject(t *testing.T) {
	t.Run("nil entry returns nil", func(t *testing.T) {
		if got := compileProcessInfoObject(nil); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("stopped process has empty uptime and no memory", func(t *testing.T) {
		got := compileProcessInfoObject(&types.ProcessHistory{
			State:       types.ProcessStateStopped,
			PGID:        123,
			RssMemoryKb: 4096,
		})
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Status != types.ServiceStatusStopped {
			t.Errorf("expected status stopped, got %q", got.Status)
		}
		if got.PGID != 123 {
			t.Errorf("expected PGID 123, got %d", got.PGID)
		}
		if got.Uptime != "" {
			t.Errorf("expected empty uptime for stopped process, got %q", got.Uptime)
		}
		if got.MemoryMb != "" {
			t.Errorf("expected no memory reported for stopped process, got %q", got.MemoryMb)
		}
		if got.Error != nil {
			t.Errorf("expected nil error, got %v", got.Error)
		}
	})

	t.Run("running process reports uptime, memory, and status", func(t *testing.T) {
		startedAt := time.Now()
		got := compileProcessInfoObject(&types.ProcessHistory{
			State:       types.ProcessStateRunning,
			PGID:        456,
			RssMemoryKb: 2048,
			StartedAt:   &startedAt,
		})
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Status != types.ServiceStatusRunning {
			t.Errorf("expected status running, got %q", got.Status)
		}
		if got.Uptime == "" {
			t.Error("expected non-empty uptime for running process")
		}
		if got.MemoryMb == "" {
			t.Error("expected non-empty memory for running process")
		}
	})

	t.Run("errored process surfaces the error", func(t *testing.T) {
		errMsg := "process crashed"
		got := compileProcessInfoObject(&types.ProcessHistory{
			State: types.ProcessStateFailed,
			Error: &errMsg,
		})
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Error == nil || *got.Error != errMsg {
			t.Errorf("expected error %q, got %v", errMsg, got.Error)
		}
	})
}
