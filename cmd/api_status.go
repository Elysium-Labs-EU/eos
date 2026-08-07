package cmd

import (
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
			mgr := getManager()

			registeredServices, err := mgr.GetAllServiceCatalogEntries(cmd.Context())
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting services: %w", err))
			}

			services := make([]apiStatusService, 0, len(registeredServices))

			for _, reg := range registeredServices {
				entry := apiStatusService{Name: reg.Name}

				mostRecentProcess, err := mgr.GetMostRecentProcessHistoryEntry(cmd.Context(), reg.Name)
				if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("getting process for %q: %w", reg.Name, err))
				}

				entry.Status = helpers.DetermineServiceStatus(mostRecentProcess)
				entry.Uptime = helpers.DetermineUptimeHuman(mostRecentProcess)

				if mostRecentProcess != nil {
					entry.PGID = mostRecentProcess.PGID
					entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(mostRecentProcess.RssMemoryKb, entry.Status)
					entry.CPU = helpers.DetermineProcessCPUHuman(mostRecentProcess.CPUPercent, entry.Status)
					if mostRecentProcess.Error != nil {
						entry.Error = mostRecentProcess.Error
					}
				} else {
					configPath := filepath.Join(reg.DirectoryPath, reg.ConfigFileName)
					_ = configPath
					entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(0, entry.Status)
					entry.CPU = helpers.DetermineProcessCPUHuman(0, entry.Status)
				}

				serviceInstance, err := mgr.GetServiceInstance(cmd.Context(), reg.Name)
				if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("getting instance for %q: %w", reg.Name, err))
				}
				if serviceInstance != nil {
					entry.StartedAt = serviceInstance.StartedAt
					entry.RestartCount = serviceInstance.RestartCount
				}

				// Overrides whatever ProcessHistory-derived status was set
				// above: a service blocked on depends_on has no process yet,
				// so without this it's indistinguishable from one that was
				// simply never started.
				if pending := helpers.ResolveDependencyWaitStatus(cmd.Context(), mgr, reg.Name); len(pending) > 0 {
					entry.Status = types.ServiceStatusWaitingForDeps
					entry.WaitingFor = pending
				}

				services = append(services, entry)
			}

			return helpers.WriteJSON(cmd, apiStatusResult{Services: services})
		},
	}
}
