package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"github.com/spf13/cobra"
)

type apiStatusService struct {
	StartedAt    *time.Time          `json:"started_at,omitempty"`
	Error        *string             `json:"error,omitempty"`
	Name         string              `json:"name"`
	MemoryMb     string              `json:"memory_mb"`
	Uptime       string              `json:"uptime"`
	Status       types.ServiceStatus `json:"status"`
	PGID         int                 `json:"pgid"`
	RestartCount int                 `json:"restart_count"`
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
        "uptime":        string           -- human-readable uptime
        "restart_count": int              -- number of restarts
        "started_at":    string|omitted   -- RFC3339 start time
        "error":         string|omitted   -- last error if any
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

			registeredServices, err := mgr.GetAllServiceCatalogEntries()
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting services: %w", err))
			}

			services := make([]apiStatusService, 0, len(registeredServices))

			for _, reg := range registeredServices {
				entry := apiStatusService{Name: reg.Name}

				mostRecentProcess, err := mgr.GetMostRecentProcessHistoryEntry(reg.Name)
				if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("getting process for %q: %w", reg.Name, err))
				}

				entry.Status = helpers.DetermineServiceStatus(mostRecentProcess)
				entry.Uptime = helpers.DetermineUptimeHuman(mostRecentProcess)

				if mostRecentProcess != nil {
					entry.PGID = mostRecentProcess.PGID
					entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(mostRecentProcess.RssMemoryKb, entry.Status)
					if mostRecentProcess.Error != nil {
						entry.Error = mostRecentProcess.Error
					}
				} else {
					configPath := filepath.Join(reg.DirectoryPath, reg.ConfigFileName)
					_ = configPath
					entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(0, entry.Status)
				}

				serviceInstance, err := mgr.GetServiceInstance(reg.Name)
				if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("getting instance for %q: %w", reg.Name, err))
				}
				if serviceInstance != nil {
					entry.StartedAt = serviceInstance.StartedAt
					entry.RestartCount = serviceInstance.RestartCount
				}

				services = append(services, entry)
			}

			return helpers.WriteJSON(cmd, apiStatusResult{Services: services})
		},
	}
}
