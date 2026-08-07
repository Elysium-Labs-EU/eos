package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
)

type apiStatusService struct {
	StartedAt *time.Time          `json:"started_at,omitempty"`
	Error     *string             `json:"error,omitempty"`
	Name      string              `json:"name"`
	MemoryMb  string              `json:"memory_mb"`
	CPU       string              `json:"cpu"`
	Uptime    string              `json:"uptime"`
	Status    types.ServiceStatus `json:"status"`
	// WaitingFor lists the depends_on names this service is currently blocked
	// on, set only when Status is "waiting". Empty/omitted otherwise.
	WaitingFor   []string `json:"waiting_for,omitempty"`
	PGID         int      `json:"pgid"`
	RestartCount int      `json:"restart_count"`
}

type apiStatusResult struct {
	Services []apiStatusService `json:"services"`
}

func newAPIStatusCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Return status of all services as JSON",
		Long: `Return the status of all registered services as a JSON array.

Output schema (stdout, JSON):
  {
    "services": [
      {
        "name":          string           -- service name
        "status":        string           -- current status
        "pgid":          int              -- process group ID (0 if not running)
        "memory_mb":     string           -- memory usage
        "cpu":           string           -- CPU usage percent (e.g. "12.5%")
        "uptime":        string           -- human-readable uptime
        "restart_count": int              -- number of restarts
        "started_at":    string|omitted   -- RFC3339 start time
        "error":         string|omitted   -- last error if any
        "waiting_for":   []string|omitted -- depends_on names still not ready (status "waiting" only)
      }
    ]
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api status
  eos api status | jq '.services[] | select(.status == "running")'
  eos api status | jq '[.services[].name]'`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			services, err := apiStatusCollectServices(cmd.Context(), getManager())
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			return helpers.WriteJSON(cmd, apiStatusResult{Services: services})
		},
	}
}

func apiStatusCollectServices(ctx context.Context, mgr manager.ServiceManager) ([]apiStatusService, error) {
	registeredServices, err := mgr.GetAllServiceCatalogEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting services: %w", err)
	}

	services := make([]apiStatusService, 0, len(registeredServices))

	for _, reg := range registeredServices {
		entry, err := apiStatusBuildServiceEntry(ctx, mgr, &reg)
		if err != nil {
			return nil, err
		}
		services = append(services, entry)
	}

	return services, nil
}

func apiStatusBuildServiceEntry(ctx context.Context, mgr manager.ServiceManager, reg *types.ServiceCatalogEntry) (apiStatusService, error) {
	entry := apiStatusService{Name: reg.Name}

	mostRecentProcess, err := mgr.GetMostRecentProcessHistoryEntry(ctx, reg.Name)
	if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
		return apiStatusService{}, fmt.Errorf("getting process for %q: %w", reg.Name, err)
	}
	apiStatusApplyProcessMetrics(&entry, mostRecentProcess, reg.DirectoryPath, reg.ConfigFileName)

	serviceInstance, err := mgr.GetServiceInstance(ctx, reg.Name)
	if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
		return apiStatusService{}, fmt.Errorf("getting instance for %q: %w", reg.Name, err)
	}
	apiStatusApplyServiceInstance(&entry, serviceInstance)

	// Overrides whatever ProcessHistory-derived status was set above: a
	// service blocked on depends_on has no process yet, so without this
	// it's indistinguishable from one that was simply never started.
	apiStatusApplyDependencyWait(ctx, mgr, reg.Name, &entry)

	return entry, nil
}

func apiStatusApplyProcessMetrics(entry *apiStatusService, mostRecentProcess *types.ProcessHistory, directoryPath, configFileName string) {
	entry.Status = helpers.DetermineServiceStatus(mostRecentProcess)
	entry.Uptime = helpers.DetermineUptimeHuman(mostRecentProcess)

	if mostRecentProcess != nil {
		entry.PGID = mostRecentProcess.PGID
		entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(mostRecentProcess.RssMemoryKb, entry.Status)
		entry.CPU = helpers.DetermineProcessCPUHuman(mostRecentProcess.CPUPercent, entry.Status)
		if mostRecentProcess.Error != nil {
			entry.Error = mostRecentProcess.Error
		}
		return
	}

	configPath := filepath.Join(directoryPath, configFileName)
	_ = configPath
	entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(0, entry.Status)
	entry.CPU = helpers.DetermineProcessCPUHuman(0, entry.Status)
}

func apiStatusApplyServiceInstance(entry *apiStatusService, serviceInstance *types.ServiceInstance) {
	if serviceInstance == nil {
		return
	}
	entry.StartedAt = serviceInstance.StartedAt
	entry.RestartCount = serviceInstance.RestartCount
}

func apiStatusApplyDependencyWait(ctx context.Context, mgr manager.ServiceManager, name string, entry *apiStatusService) {
	pending := helpers.ResolveDependencyWaitStatus(ctx, mgr, name)
	if len(pending) == 0 {
		return
	}
	entry.Status = types.ServiceStatusWaitingForDeps
	entry.WaitingFor = pending
}
