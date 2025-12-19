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
	Method MethodName `json:"method"`
	// Args   json.RawMessage `json:"args"`
	Args []string `json:"args"`
}

type DaemonResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func (r *DaemonRequest) Validate() error {
	if !ValidMethods[r.Method] {
		return fmt.Errorf("unknown method: %s", r.Method)
	}
	// NOTE: Could also validate Args count here if needed
	return nil
}

type GetServiceInstanceResponse struct {
	Instance ServiceRuntime `json:"instance"`
	Found    bool           `json:"found"`
}
