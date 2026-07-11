package helpers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/types"
)

func TestDetermineServiceStatus(t *testing.T) {
	tests := []struct {
		name  string
		input *types.ProcessHistory
		want  types.ServiceStatus
	}{
		{"nil", nil, types.ServiceStatusStopped},
		{"stopped", &types.ProcessHistory{State: types.ProcessStateStopped}, types.ServiceStatusStopped},
		{"failed", &types.ProcessHistory{State: types.ProcessStateFailed}, types.ServiceStatusFailed},
		{"running", &types.ProcessHistory{State: types.ProcessStateRunning}, types.ServiceStatusRunning},
		{"starting", &types.ProcessHistory{State: types.ProcessStateStarting}, types.ServiceStatusStarting},
		{"unknown", &types.ProcessHistory{State: types.ProcessStateUnknown}, types.ServiceStatusUnknown},
		{"default", &types.ProcessHistory{State: "bogus"}, types.ServiceStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetermineServiceStatus(tt.input); got != tt.want {
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

func TestDetermineProcessMemoryInMbAPI(t *testing.T) {
	if got := DetermineProcessMemoryInMbAPI(0); got != nil {
		t.Errorf("expected nil for 0 kb, got %v", got)
	}
	if got := DetermineProcessMemoryInMbAPI(-1); got != nil {
		t.Errorf("expected nil for negative kb, got %v", got)
	}
	got := DetermineProcessMemoryInMbAPI(1024)
	if got == nil {
		t.Fatal("expected non-nil for 1024 kb")
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
