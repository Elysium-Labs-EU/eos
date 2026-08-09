// Package types defines shared data structures and interfaces used across eos packages.
package types

import (
	"fmt"
	"os/exec"
	"time"

	"gopkg.in/yaml.v3"
)

type ServiceStatus string

const (
	ServiceStatusUnknown  ServiceStatus = "unknown"
	ServiceStatusStopped  ServiceStatus = "stopped"
	ServiceStatusStarting ServiceStatus = "starting"
	ServiceStatusRunning  ServiceStatus = "running"
	ServiceStatusFailed   ServiceStatus = "failed"
	// ServiceStatusWaitingForDeps is a display-only status: it is never
	// derived from ProcessHistory (no process exists yet), only overlaid at
	// render time (see helpers.ResolveDependencyWaitStatus) while a service's
	// start is gated on its depends_on becoming ready. Without it, a service
	// blocked on a dependency and one that simply hasn't been asked to start
	// both render identically as "stopped" — indistinguishable from a hang.
	ServiceStatusWaitingForDeps ServiceStatus = "waiting"
	// ServiceStatusOrphaned means the most recent process_history row looks
	// inactive (stopped/failed/unknown), but an older row for the same
	// service still has a live process group in the OS process table. A
	// most-recent-row-only view hides this leak entirely; see
	// helpers.DetermineServiceStatus and manager.ServiceManager's
	// GetLiveOrphanProcessGroups.
	ServiceStatusOrphaned ServiceStatus = "orphaned"
)

type Runtime struct {
	Type string `json:"type" yaml:"type"`
	Path string `json:"path" yaml:"path"`
}

// LogSink declares an external log sink plugin for a service.
// eos manages Type, Exec, BufferSize, RestartDelayMs, and Streams.
// Options is an opaque blob passed to the plugin via EOS_SINK_OPTIONS env var.
// Mode and Address are required: mode is "push" or "serve"; address is the
// remote URL (push) or bind address (serve) passed to the plugin via EOS_SINK_ADDRESS.
type LogSink struct {
	Options        map[string]any `json:"options,omitempty"          yaml:"options,omitempty"`
	Type           string         `json:"type"                       yaml:"type"`
	Mode           string         `json:"mode"                       yaml:"mode"`
	Address        string         `json:"address"                    yaml:"address"`
	Exec           string         `json:"exec,omitempty"             yaml:"exec,omitempty"`
	Args           []string       `json:"args,omitempty"             yaml:"args,omitempty"`
	Streams        []string       `json:"streams,omitempty"          yaml:"streams,omitempty"`
	BufferSize     int            `json:"buffer_size,omitempty"      yaml:"buffer_size,omitempty"`
	RestartDelayMs int            `json:"restart_delay_ms,omitempty" yaml:"restart_delay_ms,omitempty"`
}

// LogSinkRef is one entry of ServiceConfig.LogSinks: either a bare name
// referencing a sink registered in the daemon's ~/.eos/config.yaml sink
// registry, or an inline LogSink config. The two modes compose in the same
// log_sinks list.
type LogSinkRef struct {
	Inline *LogSink // set when this entry is an inline sink config
	Name   string   // set when this entry is a name reference into the registry
}

// UnmarshalYAML distinguishes a scalar node (`- prod-loki`, a registry name
// reference) from a mapping node (`- type: file ...`, an inline sink config).
func (r *LogSinkRef) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		r.Name = node.Value
		return nil
	}
	var sink LogSink
	if err := node.Decode(&sink); err != nil {
		return fmt.Errorf("decoding inline log sink: %w", err)
	}
	r.Inline = &sink
	return nil
}

// MarshalYAML is the inverse of UnmarshalYAML: a name reference marshals
// back to a bare scalar, an inline config to a mapping node.
func (r LogSinkRef) MarshalYAML() (any, error) {
	if r.Inline != nil {
		return r.Inline, nil
	}
	return r.Name, nil
}

type ServiceConfig struct {
	Runtime     Runtime `json:"runtime"                  yaml:"runtime"`
	Name        string  `json:"name"                     yaml:"name"`
	Command     string  `json:"command"                  yaml:"command"`
	EnvFile     string  `json:"env_file,omitempty"       yaml:"env_file,omitempty"`
	CronRestart string  `json:"cron_restart,omitempty"   yaml:"cron_restart,omitempty"`
	// MaxWait caps how long starting this service blocks on DependsOn becoming
	// ready before failing loud. Empty uses DependencyDefaultMaxWait. It's the
	// ceiling on retry-until-ready, not a fixed per-check timeout: a dependency
	// that comes up slowly still releases the dependent the moment it's ready.
	MaxWait string `json:"max_wait,omitempty" yaml:"max_wait,omitempty"`
	// DependsOn names services that must report healthy (state Running, the
	// health monitor's own readiness signal) before this service is started.
	// Empty means start immediately, exactly as a service with no ordering.
	DependsOn     []string     `json:"depends_on,omitempty"     yaml:"depends_on,omitempty"`
	LogSinks      []LogSinkRef `json:"log_sinks,omitempty"      yaml:"log_sinks,omitempty"`
	Port          int          `json:"port,omitempty"           yaml:"port,omitempty"`
	MemoryLimitMb int          `json:"memory_limit_mb,omitempty" yaml:"memory_limit_mb,omitempty"`
	// LogMaxFiles caps how many rotated stdout/stderr log files this service keeps
	// (active file plus this many rotated siblings). 0 uses the daemon's own default.
	LogMaxFiles int `json:"log_max_files,omitempty" yaml:"log_max_files,omitempty"`
	// LogFileSizeLimitBytes rotates this service's stdout/stderr log once it
	// reaches this size. 0 uses the daemon's own default.
	LogFileSizeLimitBytes int64 `json:"log_file_size_limit_bytes,omitempty" yaml:"log_file_size_limit_bytes,omitempty"`
}

// DependencyWaitStatus records that a service's start is currently gated on
// its depends_on becoming ready. It's persisted to the shared state.db (see
// manager.RecordDependencyWait and database.Database.SetDependencyWaitStatus)
// like ProcessHistory/ServiceInstance, so any process sharing that DB file —
// not just the one that recorded it — can see it; a stale row surviving past
// a hard-killed process is self-healed once past Deadline (see
// manager.dependencyWaitIsStale) and wiped outright on daemon boot (see
// reconcileOrphans).
type DependencyWaitStatus struct {
	Since time.Time `json:"since"        yaml:"since"`
	// Deadline is when THIS wait's own resolved max_wait would give up —
	// computed once when the wait begins and carried unchanged through every
	// subsequent Set as pending narrows (see manager.RecordDependencyWait).
	// Staleness is judged against Deadline, not Since: Since alone would
	// misjudge a wait that legitimately hasn't narrowed in a while (a single
	// slow dependency) as orphaned, particularly once max_wait exceeds a
	// fixed staleness window.
	Deadline    time.Time `json:"deadline"     yaml:"deadline"`
	ServiceName string    `json:"service_name" yaml:"service_name"`
	Pending     []string  `json:"pending"       yaml:"pending"`
}

type ServiceInstance struct {
	CreatedAt       time.Time  `json:"created_at" yaml:"created_at"`
	LastHealthCheck *time.Time `json:"last_health_check,omitempty" yaml:"last_health_check,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty" yaml:"started_at,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	NextRestartAt   *time.Time `json:"next_restart_at,omitempty" yaml:"next_restart_at,omitempty"`
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
	// PeakRssMemoryKb is the highest RssMemoryKb sampled for this PGID since
	// it started. It only ever grows within a PGID's lifetime — a crash or
	// memory-threshold restart does not reset it, only a genuinely new PGID
	// (a fresh row) does.
	PeakRssMemoryKb int64 `json:"peak_rss_memory_kb" yaml:"peak_rss_memory_kb"`
	// CPUPercent is the most recent per-service CPU usage, sampled on a health
	// tick as the delta of accumulated CPU time (utime+stime) across the PGID
	// over the sampling interval. 100.0 means one core fully busy; a service
	// spanning several busy cores can exceed 100.
	CPUPercent float64 `json:"cpu_percent" yaml:"cpu_percent"`
	PGID       int     `json:"pgid" yaml:"pgid"`
	// StartedAtTicks is an opaque, platform-specific process start-time
	// marker (see procutil.StartTime) captured at launch alongside PGID.
	// PGIDs get recycled by the kernel; comparing this value alongside PGID
	// during liveness checks rules out a false match against an unrelated
	// later process that reused the same PGID.
	StartedAtTicks int64 `json:"started_at_ticks" yaml:"started_at_ticks"`
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
	// Enabled is the persisted desired state for this service: true means it
	// should be (re)started on daemon boot, false means it was stopped by hand
	// (eos stop) and must stay down across a daemon restart/reboot until
	// re-enabled (eos run). Defaults to true on registration (see the
	// service_catalog.enabled column default), matching "adding a service
	// means it auto-starts on every future daemon boot".
	Enabled bool `json:"enabled" yaml:"enabled"`
}
