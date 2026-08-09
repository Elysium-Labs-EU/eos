package manager

import (
	"context"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

type ServiceManager interface {
	GetServiceInstance(ctx context.Context, name string) (*types.ServiceInstance, error)
	GetAllServiceInstances(ctx context.Context) ([]types.ServiceInstance, error)
	RemoveServiceInstance(ctx context.Context, name string) (bool, error)

	ForceStopService(ctx context.Context, name string) (StopServiceResult, error)
	RestartService(ctx context.Context, name string, gracePeriod time.Duration, tickerPeriod time.Duration) (int, error)
	StartService(ctx context.Context, name string) (int, error)
	StopService(ctx context.Context, name string, gracePeriod time.Duration, tickerPeriod time.Duration) (StopServiceResult, error)

	AddServiceCatalogEntry(ctx context.Context, service *types.ServiceCatalogEntry) error
	GetAllServiceCatalogEntries(ctx context.Context) ([]types.ServiceCatalogEntry, error)
	GetServiceCatalogEntry(ctx context.Context, name string) (types.ServiceCatalogEntry, error)
	IsServiceRegistered(ctx context.Context, name string) (bool, error)
	RemoveServiceCatalogEntry(ctx context.Context, name string) (bool, error)
	UpdateServiceCatalogEntry(ctx context.Context, name, newDirectoryPath, newConfigFileName string) error
	// SetServiceEnabled persists a service's desired boot state: false once
	// stopped by hand (eos stop), true again once re-run (eos run). The
	// daemon's boot recovery skips a disabled service instead of restarting
	// every registered service unconditionally (see issue #172).
	SetServiceEnabled(ctx context.Context, name string, enabled bool) error

	GetMostRecentProcessHistoryEntry(ctx context.Context, name string) (*types.ProcessHistory, error)
	// GetLiveOrphanProcessGroups returns every process_history row for name
	// other than the most recent one whose process group is still alive in
	// the OS process table. status/info read paths have historically only
	// ever consulted the most recent row, which hides a still-running
	// process pinned to an older row entirely — even though the stop path
	// already walks the whole history (see LocalManager.stopServiceWithSignal).
	GetLiveOrphanProcessGroups(ctx context.Context, name string) ([]types.ProcessHistory, error)

	NewServiceLogFiles(ctx context.Context, serviceName string) (logPath string, errorLogPath string, err error)
	GetServiceLogFilePath(ctx context.Context, serviceName string, errorLog bool) (*string, error)

	GetVersion(ctx context.Context) (types.GetVersionResponse, error)
}
