package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/spf13/cobra"
)

type apiInfoResult struct {
	CreatedAt    time.Time              `json:"created_at"`
	LogPath      *string                `json:"log_path"`
	ErrorLogPath *string                `json:"error_log_path"`
	Process      *apiInfoProcess        `json:"process,omitempty"`
	Instance     *types.ServiceInstance `json:"instance,omitempty"`
	Config       *types.ServiceConfig   `json:"config,omitempty"`
	Name         string                 `json:"name"`
	Path         string                 `json:"path"`
	ConfigFile   string                 `json:"config_file"`
}

type apiInfoProcess struct {
	Error    *string             `json:"error,omitempty"`
	Status   types.ServiceStatus `json:"status"`
	Uptime   string              `json:"uptime"`
	MemoryMb string              `json:"memory_mb"`
	PGID     int                 `json:"pgid"`
}

func newAPIInfoCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Return service information as JSON",
		Long: `Return detailed information about a registered service including its process state, runtime statistics, log file paths, and full configuration.

Output schema (stdout, JSON):
  {
    "name":          string           -- service name
    "path":          string           -- absolute path to the service directory
    "config_file":   string           -- absolute path to the service config file
    "created_at":    string (RFC3339) -- when the service was registered
    "log_path":      string|null      -- absolute path to the stdout log file
    "error_log_path":string|null      -- absolute path to the stderr log file
    "config": {
      "command":  string  -- command used to start the service
      "port":     int     -- port the service listens on (omitted if unset)
      "runtime": {
        "type": string    -- runtime identifier (e.g. "nodejs")
        "path": string    -- path to the runtime binary
      }
    },
    "instance": { ... } | null        -- present when the service is running
    "process":  { ... } | null        -- most recent process history entry
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api info myservice
  eos api info myservice | jq '.config.port'
  eos api info myservice | jq '.process.status'
  eos api info myservice | jq '{name,path,log_path}'`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()

			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if errors.Is(err, database.ErrServiceNotFound) {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q not found", serviceName))
			}
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting registered service: %w", err))
			}

			configPath := filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName)
			config, err := manager.LoadServiceConfig(configPath)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("loading service config: %w", err))
			}

			serviceInstance, err := mgr.GetServiceInstance(serviceName)
			if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting service instance: %w", err))
			}

			processEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting process history: %w", err))
			}
			logPath, err := mgr.GetServiceLogFilePath(serviceName, false)
			if err != nil && serviceInstance != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting log path: %w", err))
			}
			errorLogPath, err := mgr.GetServiceLogFilePath(serviceName, true)
			if err != nil && serviceInstance != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("getting error log path: %w", err))
			}

			serviceInfo := compileServiceInfoObject(registeredService, serviceInstance, config, logPath, errorLogPath)
			serviceInfo.Process = compileProcessInfoObject(processEntry)

			return helpers.WriteJSON(cmd, serviceInfo)
		}}
}

func compileServiceInfoObject(registeredService types.ServiceCatalogEntry, serviceInstance *types.ServiceInstance, config *types.ServiceConfig, logPath *string, errorLogPath *string) apiInfoResult {
	serviceInfo := apiInfoResult{
		Name:         registeredService.Name,
		Path:         registeredService.DirectoryPath,
		ConfigFile:   filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName),
		CreatedAt:    registeredService.CreatedAt,
		LogPath:      logPath,
		ErrorLogPath: errorLogPath,
		Instance:     serviceInstance,
	}

	if config != nil {
		serviceInfo.Config = &types.ServiceConfig{}
		serviceInfo.Config.Command = config.Command
		if config.Port != 0 {
			serviceInfo.Config.Port = config.Port
		}
		if config.Runtime.Type != "" || config.Runtime.Path != "" {
			serviceInfo.Config.Runtime = config.Runtime
		}
	}

	return serviceInfo
}

func compileProcessInfoObject(processEntry *types.ProcessHistory) *apiInfoProcess {
	if processEntry == nil {
		return nil
	}
	processInfo := &apiInfoProcess{
		Status: helpers.DetermineServiceStatus(processEntry),
		PGID:   processEntry.PGID,
	}

	uptime := helpers.DetermineUptimeAPI(processEntry)
	if uptime != nil {
		processInfo.Uptime = *uptime
	}

	memory := helpers.DetermineProcessMemoryInMbAPI(processEntry.RssMemoryKb)
	if memory != nil {
		processInfo.MemoryMb = *memory
	}

	if processEntry.Error != nil {
		processInfo.Error = processEntry.Error
	}

	return processInfo
}
