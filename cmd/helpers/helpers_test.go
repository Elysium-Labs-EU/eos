package helpers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestDetermineServiceStatus(t *testing.T) {
	tests := []struct {
		input         *types.ProcessHistory
		name          string
		want          types.ServiceStatus
		hasLiveOrphan bool
	}{
		{name: "nil", input: nil, hasLiveOrphan: false, want: types.ServiceStatusStopped},
		{name: "stopped", input: &types.ProcessHistory{State: types.ProcessStateStopped}, hasLiveOrphan: false, want: types.ServiceStatusStopped},
		{name: "failed", input: &types.ProcessHistory{State: types.ProcessStateFailed}, hasLiveOrphan: false, want: types.ServiceStatusFailed},
		{name: "running", input: &types.ProcessHistory{State: types.ProcessStateRunning}, hasLiveOrphan: false, want: types.ServiceStatusRunning},
		{name: "starting", input: &types.ProcessHistory{State: types.ProcessStateStarting}, hasLiveOrphan: false, want: types.ServiceStatusStarting},
		{name: "unknown", input: &types.ProcessHistory{State: types.ProcessStateUnknown}, hasLiveOrphan: false, want: types.ServiceStatusUnknown},
		{name: "default", input: &types.ProcessHistory{State: "bogus"}, hasLiveOrphan: false, want: types.ServiceStatusUnknown},
		{name: "nil with live orphan", input: nil, hasLiveOrphan: true, want: types.ServiceStatusOrphaned},
		{name: "stopped with live orphan", input: &types.ProcessHistory{State: types.ProcessStateStopped}, hasLiveOrphan: true, want: types.ServiceStatusOrphaned},
		{name: "failed with live orphan", input: &types.ProcessHistory{State: types.ProcessStateFailed}, hasLiveOrphan: true, want: types.ServiceStatusOrphaned},
		{name: "unknown with live orphan", input: &types.ProcessHistory{State: types.ProcessStateUnknown}, hasLiveOrphan: true, want: types.ServiceStatusOrphaned},
		{name: "running with live orphan stays running", input: &types.ProcessHistory{State: types.ProcessStateRunning}, hasLiveOrphan: true, want: types.ServiceStatusRunning},
		{name: "starting with live orphan stays starting", input: &types.ProcessHistory{State: types.ProcessStateStarting}, hasLiveOrphan: true, want: types.ServiceStatusStarting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetermineServiceStatus(tt.input, tt.hasLiveOrphan); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetermineUptimeHuman(t *testing.T) {
	now := time.Now()
	tests := []struct {
		input *types.ProcessHistory
		name  string
		dash  bool
	}{
		{name: "nil", input: nil, dash: true},
		{name: "stopped", input: &types.ProcessHistory{State: types.ProcessStateStopped}, dash: true},
		{name: "failed", input: &types.ProcessHistory{State: types.ProcessStateFailed}, dash: true},
		{name: "unknown", input: &types.ProcessHistory{State: types.ProcessStateUnknown}, dash: true},
		{name: "running no startedAt", input: &types.ProcessHistory{State: types.ProcessStateRunning}, dash: true},
		{name: "running with startedAt", input: &types.ProcessHistory{State: types.ProcessStateRunning, StartedAt: &now}, dash: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineUptimeHuman(tt.input)
			if tt.dash && got != "-" {
				t.Errorf("expected dash, got %q", got)
			}
			if !tt.dash && got == "-" {
				t.Errorf("expected uptime string, got dash")
			}
		})
	}
}

func TestDetermineUptimeAPI(t *testing.T) {
	now := time.Now()
	tests := []struct {
		input *types.ProcessHistory
		name  string
		nilOk bool
	}{
		{name: "nil", input: nil, nilOk: true},
		{name: "stopped", input: &types.ProcessHistory{State: types.ProcessStateStopped}, nilOk: true},
		{name: "failed", input: &types.ProcessHistory{State: types.ProcessStateFailed}, nilOk: true},
		{name: "unknown", input: &types.ProcessHistory{State: types.ProcessStateUnknown}, nilOk: true},
		{name: "running no startedAt", input: &types.ProcessHistory{State: types.ProcessStateRunning}, nilOk: true},
		{name: "running with startedAt", input: &types.ProcessHistory{State: types.ProcessStateRunning, StartedAt: &now}, nilOk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineUptimeAPI(tt.input)
			if tt.nilOk && got != nil {
				t.Errorf("expected nil, got %q", *got)
			}
			if !tt.nilOk && got == nil {
				t.Error("expected non-nil uptime string, got nil")
			}
			if !tt.nilOk && got != nil {
				// Must match DetermineUptimeHuman's format (relative, e.g. "12 seconds ago"),
				// not an absolute timestamp restatement of started_at — see issue #68.
				if *got == tt.input.StartedAt.String() {
					t.Errorf("expected relative duration string like DetermineUptimeHuman, got absolute timestamp %q", *got)
				}
				if want := DetermineUptimeHuman(tt.input); *got != want {
					t.Errorf("expected api uptime %q to match human uptime %q", *got, want)
				}
			}
		})
	}
}

func TestDetermineProcessMemoryInMbHuman(t *testing.T) {
	tests := []struct {
		name   string
		status types.ServiceStatus
		want   string
		kb     int64
	}{
		{name: "failed status", kb: 1024, status: types.ServiceStatusFailed, want: "-"},
		{name: "stopped status", kb: 1024, status: types.ServiceStatusStopped, want: "-"},
		{name: "orphaned status", kb: 1024, status: types.ServiceStatusOrphaned, want: "-"},
		{name: "zero kb", kb: 0, status: types.ServiceStatusRunning, want: "-"},
		{name: "negative kb", kb: -1, status: types.ServiceStatusRunning, want: "-"},
		{name: "1 MB", kb: 1024, status: types.ServiceStatusRunning, want: "1.0 MB"},
		{name: "1.5 MB", kb: 1536, status: types.ServiceStatusRunning, want: "1.5 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetermineProcessMemoryInMbHuman(tt.kb, tt.status); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetermineProcessCPUHuman(t *testing.T) {
	tests := []struct {
		name    string
		status  types.ServiceStatus
		want    string
		percent float64
	}{
		{name: "failed status", percent: 50, status: types.ServiceStatusFailed, want: "-"},
		{name: "stopped status", percent: 50, status: types.ServiceStatusStopped, want: "-"},
		{name: "orphaned status", percent: 50, status: types.ServiceStatusOrphaned, want: "-"},
		{name: "negative percent", percent: -1, status: types.ServiceStatusRunning, want: "-"},
		{name: "idle running", percent: 0, status: types.ServiceStatusRunning, want: "0.0%"},
		{name: "partial core", percent: 12.5, status: types.ServiceStatusRunning, want: "12.5%"},
		{name: "over one core", percent: 143.2, status: types.ServiceStatusRunning, want: "143.2%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetermineProcessCPUHuman(tt.percent, tt.status); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetermineProcessMemoryInMbAPI(t *testing.T) {
	if got := DetermineProcessMemoryInMbAPI(0, types.ServiceStatusRunning); got != nil {
		t.Errorf("expected nil for 0 kb, got %v", got)
	}
	if got := DetermineProcessMemoryInMbAPI(-1, types.ServiceStatusRunning); got != nil {
		t.Errorf("expected nil for negative kb, got %v", got)
	}
	if got := DetermineProcessMemoryInMbAPI(1024, types.ServiceStatusStopped); got != nil {
		t.Errorf("expected nil for stopped status, got %v", got)
	}
	if got := DetermineProcessMemoryInMbAPI(1024, types.ServiceStatusFailed); got != nil {
		t.Errorf("expected nil for failed status, got %v", got)
	}
	if got := DetermineProcessMemoryInMbAPI(1024, types.ServiceStatusOrphaned); got != nil {
		t.Errorf("expected nil for orphaned status, got %v", got)
	}
	got := DetermineProcessMemoryInMbAPI(1024, types.ServiceStatusRunning)
	if got == nil {
		t.Fatal("expected non-nil for 1024 kb")
		return
	}
	if *got != "1.0 MB" {
		t.Errorf("got %q, want %q", *got, "1.0 MB")
	}
}

func TestDetermineError(t *testing.T) {
	if got := DetermineError(nil); got != "-" {
		t.Errorf("expected dash for nil, got %q", got)
	}
	if got := DetermineError(new("")); got != "-" {
		t.Errorf("expected dash for empty string, got %q", got)
	}
	msg := "connection refused"
	if got := DetermineError(&msg); got != "connection refused" {
		t.Errorf("got %q, want %q", got, "connection refused")
	}
}

func TestExtractPGIDs(t *testing.T) {
	if got := ExtractPGIDs(nil); len(got) != 0 {
		t.Errorf("expected empty slice for nil input, got %v", got)
	}
	history := []types.ProcessHistory{{PGID: 111}, {PGID: 222}, {PGID: 333}}
	got := ExtractPGIDs(history)
	want := []int{111, 222, 333}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestDetermineOrphanedPGIDsHuman(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		pgids []int
	}{
		{name: "nil", pgids: nil, want: "-"},
		{name: "empty", pgids: []int{}, want: "-"},
		{name: "single", pgids: []int{1763}, want: "1763"},
		{name: "multiple", pgids: []int{1763, 2001}, want: "1763, 2001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetermineOrphanedPGIDsHuman(tt.pgids); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsProcessHistoryStale(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	interval := 2 * time.Second // threshold = 3 * interval = 6s
	updated := func(d time.Duration) *types.ProcessHistory {
		ts := now.Add(-d)
		return &types.ProcessHistory{UpdatedAt: &ts}
	}

	tests := []struct {
		proc     *types.ProcessHistory
		name     string
		interval time.Duration
		want     bool
	}{
		{nil, "nil process", interval, false},
		{&types.ProcessHistory{}, "nil updated_at", interval, false},
		{updated(1 * time.Second), "fresh", interval, false},
		{updated(6 * time.Second), "at threshold not stale", interval, false},
		{updated(6*time.Second + time.Millisecond), "just past threshold", interval, true},
		{updated(1 * time.Hour), "very stale", interval, true},
		{updated(1 * time.Hour), "zero interval never stale", 0, false},
		{updated(1 * time.Hour), "negative interval never stale", -time.Second, false},
		{updated(20 * time.Second), "threshold scales with interval", 10 * time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProcessHistoryStale(tt.proc, tt.interval, now); got != tt.want {
				t.Errorf("IsProcessHistoryStale = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindServiceFileInDirectory(t *testing.T) {
	dir := t.TempDir()

	if got := findServiceFileInDirectory(dir); got != "" {
		t.Errorf("expected empty in empty dir, got %q", got)
	}

	yamlPath := filepath.Join(dir, "service.yaml")
	_ = os.WriteFile(yamlPath, []byte("name: test"), 0644)
	if got := findServiceFileInDirectory(dir); got != yamlPath {
		t.Errorf("got %q, want %q", got, yamlPath)
	}
}

func TestFindServiceFileInDirectoryYml(t *testing.T) {
	dir := t.TempDir()
	ymlPath := filepath.Join(dir, "service.yml")
	_ = os.WriteFile(ymlPath, []byte("name: test"), 0644)
	if got := findServiceFileInDirectory(dir); got != ymlPath {
		t.Errorf("got %q, want %q", got, ymlPath)
	}
}

func TestDetermineYamlFile(t *testing.T) {
	dir := t.TempDir()

	_, err := DetermineYamlFile(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Error("expected error for non-existent path")
	}

	_, err = DetermineYamlFile(dir)
	if err == nil {
		t.Error("expected error for dir without service.yaml")
	}

	yamlPath := filepath.Join(dir, "service.yaml")
	_ = os.WriteFile(yamlPath, []byte("name: test"), 0644)
	got, err := DetermineYamlFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != yamlPath {
		t.Errorf("got %q, want %q", got, yamlPath)
	}

	got, err = DetermineYamlFile(yamlPath)
	if err != nil {
		t.Fatalf("unexpected error for direct yaml path: %v", err)
	}
	if got != yamlPath {
		t.Errorf("got %q, want %q", got, yamlPath)
	}

	txtPath := filepath.Join(dir, "config.txt")
	_ = os.WriteFile(txtPath, []byte(""), 0644)
	_, err = DetermineYamlFile(txtPath)
	if err == nil {
		t.Error("expected error for non-yaml file")
	}
}
