package types

import (
	"os/exec"
	"time"
)

type ServiceStatus string

const (
	ServiceStatusUnknown  ServiceStatus = "unknown"
	ServiceStatusStopped  ServiceStatus = "stopped"
	ServiceStatusStarting ServiceStatus = "starting"
	ServiceStatusRunning  ServiceStatus = "running"
	ServiceStatusFailed   ServiceStatus = "failed"
)

type Runtime struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

type ServiceConfig struct {
	Name    string  `json:"name" yaml:"name"`
	Command string  `json:"command" yaml:"command"`
	Port    int     `json:"port,omitempty" yaml:"port,omitempty"`
	EnvFile string  `json:"env_file,omitempty" yaml:"env_file,omitempty"`
	Runtime Runtime `yaml:"runtime"`
}

type ServiceRuntime struct {
	Name            string     `json:"name" yaml:"name"`
	RestartCount    int        `json:"restart_count,omitempty" yaml:"restart_count"`
	LastHealthCheck *time.Time `json:"last_health_check" yaml:"last_health_check,omitzero"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitzero"`
	UpdatedAt       *time.Time `json:"updated_at,omitzero"`
}

type ProcessState string

const (
	ProcessStateUnknown  ProcessState = "unknown"
	ProcessStateStopped  ProcessState = "stopped"
	ProcessStateStarting ProcessState = "starting"
	ProcessStateRunning  ProcessState = "running"
	ProcessStateFailed   ProcessState = "failed"
)

type ProcessHistory struct {
	ServiceName string       `json:"service_name"`
	State       ProcessState `json:"state"`
	PID         int          `json:"pid"`
	Error       *string      `json:"error,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	StartedAt   *time.Time   `json:"started_at,omitzero"`
	StoppedAt   *time.Time   `json:"stopped_at,omitzero"`
	UpdatedAt   *time.Time   `json:"updated_at,omitzero"`
}

type RunningProcess struct {
	PID int
	Cmd *exec.Cmd // The live handle
}

type Service struct {
	Config  ServiceConfig
	Runtime ServiceRuntime
}

type ServiceCatalogEntry struct {
	Name           string    `json:"name" yaml:"name"`
	DirectoryPath  string    `json:"path" yaml:"path"`
	ConfigFileName string    `json:"config_file" yaml:"config_file"`
	CreatedAt      time.Time `json:"created_at" yaml:"created_at"`
}
