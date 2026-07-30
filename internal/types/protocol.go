package types

import (
	"encoding/json"
	"fmt"
	"time"
)

type MethodName string

const (
	// MethodGetServiceStatus      = "GetServiceStatus"
	MethodGetServiceInstance     = "GetServiceInstance"
	MethodGetAllServiceInstances = "GetAllServiceInstances"
	MethodRemoveServiceInstance  = "RemoveServiceInstance"

	MethodForceStopService = "ForceStopService"
	MethodReloadService    = "ReloadService"
	MethodRestartService   = "RestartService"
	MethodStartService     = "StartService"
	MethodStopService      = "StopService"

	MethodAddServiceCatalogEntry      = "AddServiceCatalogEntry"
	MethodGetAllServiceCatalogEntries = "GetAllServiceCatalogEntries"
	MethodGetServiceCatalogEntry      = "GetServiceCatalogEntry"
	MethodIsServiceRegistered         = "IsServiceRegistered"
	MethodRemoveServiceCatalogEntry   = "RemoveServiceCatalogEntry"
	MethodUpdateServiceCatalogEntry   = "UpdateServiceCatalogEntry"

	MethodGetMostRecentProcessHistoryEntry = "GetMostRecentProcessHistoryEntry"

	MethodSetDependencyWaitStatus   = "SetDependencyWaitStatus"
	MethodClearDependencyWaitStatus = "ClearDependencyWaitStatus"
	MethodGetDependencyWaitStatus   = "GetDependencyWaitStatus"

	MethodNewServiceLogFiles    = "NewServiceLogFiles"
	MethodGetServiceLogFilePath = "GetServiceLogFilePath"

	MethodGetVersion = "GetVersion"
)

var ValidMethods = map[MethodName]bool{
	MethodGetServiceInstance:     true,
	MethodRemoveServiceInstance:  true,
	MethodGetAllServiceInstances: true,

	MethodForceStopService: true,
	MethodReloadService:    true,
	MethodRestartService:   true,
	MethodStartService:     true,
	MethodStopService:      true,

	MethodAddServiceCatalogEntry:      true,
	MethodGetAllServiceCatalogEntries: true,
	MethodGetServiceCatalogEntry:      true,
	MethodIsServiceRegistered:         true,
	MethodRemoveServiceCatalogEntry:   true,
	MethodUpdateServiceCatalogEntry:   true,

	MethodGetMostRecentProcessHistoryEntry: true,

	MethodSetDependencyWaitStatus:   true,
	MethodClearDependencyWaitStatus: true,
	MethodGetDependencyWaitStatus:   true,

	MethodNewServiceLogFiles:    true,
	MethodGetServiceLogFilePath: true,

	MethodGetVersion: true,
}

type DaemonRequest struct {
	Method MethodName      `json:"method"`
	Args   json.RawMessage `json:"args"`
}

type DaemonResponse struct {
	Error     string          `json:"error,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Success   bool            `json:"success"`
}

func (r *DaemonRequest) Validate() error {
	if !ValidMethods[r.Method] {
		return fmt.Errorf("unknown method: %s", r.Method)
	}
	// NOTE: Could also validate Args count here if needed
	return nil
}

type RemoveServiceInstanceArgs struct {
	Name string `json:"name"`
}

type GetServiceInstanceArgs struct {
	Name string `json:"name"`
}

type GetServiceInstanceResponse struct {
	Instance ServiceInstance `json:"instance"`
}

type GetAllServiceInstancesResponse struct {
	Instances []ServiceInstance `json:"instances"`
}

type GetMostRecentProcessHistoryEntryResponse struct {
	ProcessEntry ProcessHistory `json:"process_entry"`
}

type StartServiceArgs struct {
	Name string `json:"name"`
}

type RestartServiceArgs struct {
	Name         string `json:"name"`
	GracePeriod  string `json:"grace_period"`
	TickerPeriod string `json:"ticker_period"`
}

// ReloadServiceArgs carries the timing knobs for a zero-downtime reload. The
// durations are strings so the wire format stays human-readable and matches the
// stop/restart args; the daemon parses them back with time.ParseDuration.
type ReloadServiceArgs struct {
	Name             string `json:"name"`
	GracePeriod      string `json:"grace_period"`
	TickerPeriod     string `json:"ticker_period"`
	ReadinessTimeout string `json:"readiness_timeout"`
	ProbeInterval    string `json:"probe_interval"`
}

// ReloadServiceResponse reports the process groups the reload swapped between.
type ReloadServiceResponse struct {
	OldPGID int `json:"old_pgid"`
	NewPGID int `json:"new_pgid"`
}

type StopServiceArgs struct {
	Name         string `json:"name"`
	GracePeriod  string `json:"grace_period"`
	TickerPeriod string `json:"ticker_period"`
}

type ForceStopServiceArgs struct {
	Name string `json:"name"`
}

type AddServiceCatalogEntryArgs struct {
	Service *ServiceCatalogEntry `json:"service"`
}

type GetServiceCatalogEntryArgs struct {
	Name string `json:"name"`
}

type IsServiceRegisteredArgs struct {
	Name string `json:"name"`
}

type RemoveServiceCatalogEntryArgs struct {
	Name string `json:"name"`
}

type UpdateServiceCatalogEntryArgs struct {
	Name              string `json:"name"`
	NewDirectoryPath  string `json:"new_directory_path"`
	NewConfigFileName string `json:"new_config_file_name"`
}

type GetMostRecentProcessHistoryEntryArgs struct {
	Name string `json:"name"`
}

// SetDependencyWaitStatusArgs records that ServiceName is currently blocked
// waiting on Pending to become ready (see manager.RecordDependencyWait).
// Deadline is this wait's own resolved max_wait ceiling, used for staleness
// detection rather than a fixed window (see manager.dependencyWaitIsStale).
type SetDependencyWaitStatusArgs struct {
	Deadline    time.Time `json:"deadline"`
	ServiceName string    `json:"service_name"`
	Pending     []string  `json:"pending"`
}

// ClearDependencyWaitStatusArgs clears any recorded wait for ServiceName.
type ClearDependencyWaitStatusArgs struct {
	ServiceName string `json:"service_name"`
}

// GetDependencyWaitStatusArgs queries whether ServiceName currently has a
// recorded depends_on wait.
type GetDependencyWaitStatusArgs struct {
	ServiceName string `json:"service_name"`
}

// GetDependencyWaitStatusResponse carries the recorded wait, or Waiting=false
// when ServiceName has none.
type GetDependencyWaitStatusResponse struct {
	Status  *DependencyWaitStatus `json:"status,omitempty"`
	Waiting bool                  `json:"waiting"`
}

type NewServiceLogFilesArgs struct {
	ServiceName string `json:"service_name"`
}

type GetServiceLogFilePathArgs struct {
	ServiceName string `json:"service_name"`
	ErrorLog    bool   `json:"error_log"`
}

// GetVersionResponse carries the daemon's own buildinfo, letting a CLI query
// the version of the actual running daemon process rather than the on-disk
// binary — the two diverge whenever an update replaces the binary before the
// daemon is restarted.
type GetVersionResponse struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildDate string `json:"build_date"`
}
