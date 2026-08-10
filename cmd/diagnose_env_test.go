package cmd

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestDiagnoseRedactEnviron(t *testing.T) {
	got := diagnoseRedactEnviron([]string{
		"PATH=/usr/bin:/bin",
		"SECRET_TOKEN=do-not-leak",
		"HOME=/home/svc",
		"malformed-entry-no-equals",
	})

	byName := map[string]diagnoseEnvVar{}
	for _, v := range got {
		byName[v.Name] = v
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 entries (malformed entry dropped), got %d: %+v", len(got), got)
	}
	if v := byName["PATH"]; v.Withheld || v.Value != "/usr/bin:/bin" {
		t.Errorf("expected PATH allowlisted with its value, got: %+v", v)
	}
	if v := byName["HOME"]; v.Withheld || v.Value != "/home/svc" {
		t.Errorf("expected HOME allowlisted with its value, got: %+v", v)
	}
	if v := byName["SECRET_TOKEN"]; !v.Withheld || v.Value != "" {
		t.Errorf("expected SECRET_TOKEN present but withheld, got: %+v", v)
	}

	for i := 1; i < len(got); i++ {
		if got[i-1].Name > got[i].Name {
			t.Errorf("expected name-sorted output, got: %+v", got)
			break
		}
	}
}

func TestDiagnoseRedactEnviron_Empty(t *testing.T) {
	got := diagnoseRedactEnviron(nil)
	if len(got) != 0 {
		t.Errorf("expected no entries for an empty environment, got: %+v", got)
	}
}

func TestDiagnoseExtractPathVar(t *testing.T) {
	if got := diagnoseExtractPathVar([]string{"HOME=/home/svc", "PATH=/opt/bin:/usr/bin"}); got != "/opt/bin:/usr/bin" {
		t.Errorf("expected extracted PATH, got: %q", got)
	}
	if got := diagnoseExtractPathVar([]string{"HOME=/home/svc"}); got != "" {
		t.Errorf("expected empty string when PATH is unset, got: %q", got)
	}
}

func TestDiagnoseCollectDaemonEnv(t *testing.T) {
	t.Run("nil pid", func(t *testing.T) {
		info, step := diagnoseCollectDaemonEnv(nil)
		if step.Captured {
			t.Error("expected a failed step for a nil daemon pid")
		}
		if !strings.Contains(step.Error, "pid unavailable") {
			t.Errorf("expected a descriptive error, got: %s", step.Error)
		}
		if len(info.Vars) != 0 {
			t.Errorf("expected no vars, got: %+v", info.Vars)
		}
	})

	t.Run("read error", func(t *testing.T) {
		orig := diagnoseReadEnviron
		defer func() { diagnoseReadEnviron = orig }()
		wantErr := errors.New("boom")
		diagnoseReadEnviron = func(int) ([]string, error) { return nil, wantErr }

		pid := 4242
		info, step := diagnoseCollectDaemonEnv(&pid)
		if step.Captured {
			t.Error("expected a failed step on a read error")
		}
		if step.Error != "boom" {
			t.Errorf("expected the read error surfaced, got: %s", step.Error)
		}
		if len(info.Vars) != 0 {
			t.Errorf("expected no vars, got: %+v", info.Vars)
		}
	})

	t.Run("success", func(t *testing.T) {
		orig := diagnoseReadEnviron
		defer func() { diagnoseReadEnviron = orig }()
		diagnoseReadEnviron = func(pid int) ([]string, error) {
			if pid != 4242 {
				t.Errorf("expected pid 4242 passed through, got: %d", pid)
			}
			return []string{"PATH=/usr/bin", "SECRET=leak-me-not"}, nil
		}

		pid := 4242
		info, step := diagnoseCollectDaemonEnv(&pid)
		if !step.Captured {
			t.Fatalf("expected an ok step, got: %+v", step)
		}
		if step.Name != "daemon-env" {
			t.Errorf("expected step name 'daemon-env', got: %s", step.Name)
		}
		if len(info.Vars) != 2 {
			t.Fatalf("expected 2 vars, got: %+v", info.Vars)
		}
	})
}

func TestDiagnoseCollectServiceEnv(t *testing.T) {
	t.Run("no process history", func(t *testing.T) {
		mgr := &apiStatusFakeManager{processErr: errors.New("no process found")}
		catalog := []types.ServiceCatalogEntry{{Name: "svc"}}
		files, steps := diagnoseCollectServiceEnv(t.Context(), mgr, catalog)

		if len(files) != 1 || files[0].Name != "service-env.json" {
			t.Fatalf("expected a single service-env.json file, got: %+v", files)
		}
		if len(steps) != 1 || steps[0].Captured || !strings.Contains(steps[0].Error, "no process found") {
			t.Errorf("expected a failed 'service-env:svc' step, got: %+v", steps)
		}
	})

	t.Run("pgid not alive", func(t *testing.T) {
		mgr := &apiStatusFakeManager{processEntry: &types.ProcessHistory{PGID: 999999999, StartedAtTicks: 0}}
		catalog := []types.ServiceCatalogEntry{{Name: "svc"}}
		_, steps := diagnoseCollectServiceEnv(t.Context(), mgr, catalog)

		if len(steps) != 1 || steps[0].Captured || !strings.Contains(steps[0].Error, "not running") {
			t.Errorf("expected a failed 'not running' step, got: %+v", steps)
		}
	})

	t.Run("read error", func(t *testing.T) {
		orig := diagnoseReadEnviron
		defer func() { diagnoseReadEnviron = orig }()

		cmd := startRealSleepProcess(t)
		startedAtTicks, err := procutil.StartTime(cmd.Process.Pid)
		if err != nil {
			t.Fatalf("StartTime: %v", err)
		}
		diagnoseReadEnviron = func(int) ([]string, error) { return nil, errors.New("read boom") }

		mgr := &apiStatusFakeManager{processEntry: &types.ProcessHistory{PGID: cmd.Process.Pid, StartedAtTicks: startedAtTicks}}
		catalog := []types.ServiceCatalogEntry{{Name: "svc"}}
		_, steps := diagnoseCollectServiceEnv(t.Context(), mgr, catalog)

		if len(steps) != 1 || steps[0].Captured || steps[0].Error != "read boom" {
			t.Errorf("expected the read error surfaced, got: %+v", steps)
		}
	})

	t.Run("success with a real live process", func(t *testing.T) {
		orig := diagnoseReadEnviron
		defer func() { diagnoseReadEnviron = orig }()

		cmd := startRealSleepProcess(t)
		startedAtTicks, err := procutil.StartTime(cmd.Process.Pid)
		if err != nil {
			t.Fatalf("StartTime: %v", err)
		}
		diagnoseReadEnviron = func(pid int) ([]string, error) {
			if pid != cmd.Process.Pid {
				t.Errorf("expected the live process's pgid passed through, got: %d", pid)
			}
			return []string{"PATH=/custom/bin:/usr/bin"}, nil
		}

		mgr := &apiStatusFakeManager{processEntry: &types.ProcessHistory{PGID: cmd.Process.Pid, StartedAtTicks: startedAtTicks}}
		catalog := []types.ServiceCatalogEntry{{Name: "svc"}}
		files, steps := diagnoseCollectServiceEnv(t.Context(), mgr, catalog)

		if len(steps) != 1 || !steps[0].Captured {
			t.Fatalf("expected an ok step, got: %+v", steps)
		}
		entries := unmarshalOrFatal[[]diagnoseServiceEnvInfo](t, files[0].Data)
		if len(entries) != 1 || entries[0].Name != "svc" || entries[0].Path != "/custom/bin:/usr/bin" {
			t.Errorf("expected one resolved entry for svc, got: %+v", entries)
		}
	})

	t.Run("no services", func(t *testing.T) {
		mgr := &apiStatusFakeManager{}
		files, steps := diagnoseCollectServiceEnv(t.Context(), mgr, nil)
		if len(files) != 1 {
			t.Fatalf("expected service-env.json even with no services, got: %+v", files)
		}
		if len(steps) != 0 {
			t.Errorf("expected no steps with no services, got: %+v", steps)
		}
	})
}

// startRealSleepProcess launches a real, detached child process (its own
// process group leader, mirroring how eos launches every service) so
// procutil.IsAliveMatching and procutil.StartTime have a genuine live PID to
// check -- IsAliveMatching's kernel-level PGID-recycle guard can't be
// exercised against a faked one. The process is killed and reaped on
// cleanup.
func startRealSleepProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting real sleep process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}
