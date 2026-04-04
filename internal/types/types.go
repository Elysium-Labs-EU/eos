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
	Type string `json:"type" yaml:"type"`
	Path string `json:"path" yaml:"path"`
}

type ServiceConfig struct {
	Runtime       Runtime `json:"runtime" yaml:"runtime"`
	Name          string  `json:"name" yaml:"name"`
	Command       string  `json:"command" yaml:"command"`
	EnvFile       string  `json:"env_file,omitempty" yaml:"env_file,omitempty"`
	Port          int     `json:"port,omitempty" yaml:"port,omitempty"`
	MemoryLimitMb int     `json:"memory_limit_mb,omitempty" yaml:"memory_limit_mb,omitempty"`
}

type ServiceInstance struct {
	CreatedAt       time.Time  `json:"created_at" yaml:"created_at"`
	LastHealthCheck *time.Time `json:"last_health_check,omitempty" yaml:"last_health_check,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty" yaml:"started_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	Name            string     `json:"name" yaml:"name"`
	RestartCount    int        `json:"restart_count,omitempty" yaml:"restart_count,omitempty"`
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
	CreatedAt   time.Time    `json:"created_at" yaml:"created_at"`
	Error       *string      `json:"error,omitempty" yaml:"error,omitempty"`
	StartedAt   *time.Time   `json:"started_at,omitempty" yaml:"started_at,omitempty"`
	StoppedAt   *time.Time   `json:"stopped_at,omitempty" yaml:"stopped_at,omitempty"`
	UpdatedAt   *time.Time   `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	ServiceName string       `json:"service_name" yaml:"service_name"`
	State       ProcessState `json:"state" yaml:"state"`
	RssMemoryKb int64        `json:"rss_memory_kb" yaml:"rss_memory_kb"`
	PGID        int          `json:"pgid" yaml:"pgid"`
}

type RunningProcess struct {
	Cmd  *exec.Cmd `json:"-" yaml:"-"`
	PGID int       `json:"pgid" yaml:"pgid"`
}

type Service struct {
	Instance ServiceInstance `json:"instance" yaml:"instance"`
	Config   ServiceConfig   `json:"config" yaml:"config"`
}

type ServiceCatalogEntry struct {
	CreatedAt      time.Time `json:"created_at" yaml:"created_at"`
	Name           string    `json:"name" yaml:"name"`
	DirectoryPath  string    `json:"path" yaml:"path"`
	ConfigFileName string    `json:"config_file" yaml:"config_file"`
}
