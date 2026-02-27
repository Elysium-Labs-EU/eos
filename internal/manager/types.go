package manager

import (
	"time"

	"eos/internal/types"
)

type ServiceManager interface {
	// AddService(service *types.ServiceConfig) error
	// GetServiceStatus(name string) (types.ServiceStatus, error)
	GetServiceInstance(name string) (*types.ServiceRuntime, error)
	// RemoveService(name string) error
	// GetService(name string) (types.Service, error)
	// GetServices() []types.Service
	RemoveServiceInstance(name string) (bool, error)

	ForceStopService(name string) (StopServiceResult, error)
	RestartService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (int, error)
	StartService(name string) (int, error)
	StopService(name string, gracePeriod time.Duration, tickerPeriod time.Duration) (StopServiceResult, error)

	AddServiceCatalogEntry(service *types.ServiceCatalogEntry) error
	GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error)
	GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error)
	IsServiceRegistered(name string) (bool, error)
	RemoveServiceCatalogEntry(name string) (bool, error)
	UpdateServiceCatalogEntry(name string, newDirectoryPath string, newConfigFileName string) error

	GetMostRecentProcessHistoryEntry(name string) (*types.ProcessHistory, error)

	CreateServiceLogFiles(serviceName string) (logPath string, errorLogPath string, err error)
	GetServiceLogFilePath(serviceName string, errorLog bool) (*string, error)
}
