package types

import (
	"encoding/json"
	"fmt"
)

type MethodName string

const (
	// MethodGetServiceStatus      = "GetServiceStatus"
	MethodGetServiceInstance     = "GetServiceInstance"
	MethodGetAllServiceInstances = "GetAllServiceInstances"
	MethodRemoveServiceInstance  = "RemoveServiceInstance"

	MethodForceStopService = "ForceStopService"
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

	MethodNewServiceLogFiles    = "NewServiceLogFiles"
	MethodGetServiceLogFilePath = "GetServiceLogFilePath"
)

var ValidMethods = map[MethodName]bool{
	MethodGetServiceInstance:     true,
	MethodRemoveServiceInstance:  true,
	MethodGetAllServiceInstances: true,

	MethodForceStopService: true,
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

	MethodNewServiceLogFiles:    true,
	MethodGetServiceLogFilePath: true,
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

type NewServiceLogFilesArgs struct {
	ServiceName string `json:"service_name"`
}

type GetServiceLogFilePathArgs struct {
	ServiceName string `json:"service_name"`
	ErrorLog    bool   `json:"error_log"`
}
