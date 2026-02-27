package types

import (
	"encoding/json"
	"fmt"
)

type MethodName string

const (
	// MethodGetServiceStatus      = "GetServiceStatus"
	MethodGetServiceInstance    = "GetServiceInstance"
	MethodRemoveServiceInstance = "RemoveServiceInstance"

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

	MethodCreateServiceLogFiles = "CreateServiceLogFiles"
	MethodGetServiceLogFilePath = "GetServiceLogFilePath"
)

var ValidMethods = map[MethodName]bool{
	MethodGetServiceInstance:    true,
	MethodRemoveServiceInstance: true,

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

	MethodCreateServiceLogFiles: true,
	MethodGetServiceLogFilePath: true,
}

type DaemonRequest struct {
	Method MethodName      `json:"method"`
	Args   json.RawMessage `json:"args"`
}

type DaemonResponse struct {
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Success bool            `json:"success"`
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
	Instance ServiceRuntime `json:"instance"`
}

type GetMostRecentProcessHistoryEntryResponse struct {
	ProcessEntry ProcessHistory `json:"process_entry"`
}

type StartServiceArgs struct {
	Name string `json:"name"`
}

type RestartServiceArgs struct {
	Name         string `json:"name"`
	GracePeriod  string `json:"gracePeriod"`
	TickerPeriod string `json:"tickerPeriod"`
}

type StopServiceArgs struct {
	Name         string `json:"name"`
	GracePeriod  string `json:"gracePeriod"`
	TickerPeriod string `json:"tickerPeriod"`
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
	NewConfigFileName string `json:"new_config_filename"`
}

type GetMostRecentProcessHistoryEntryArgs struct {
	Name string `json:"name"`
}

type CreateServiceLogFilesArgs struct {
	ServiceName string `json:"service_name"`
}

type GetServiceLogFilePathArgs struct {
	ServiceName string `json:"service_name"`
	ErrorLog    bool   `json:"error_log"`
}
