package cmd

import (
	"errors"
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiRunResult struct {
	Name      string `json:"name"`
	PGID      int    `json:"pgid"`
	Restarted bool   `json:"restarted"`
	Skipped   bool   `json:"skipped"`
}

func apiRunResolveServiceName(mgr manager.ServiceManager, serviceFile string, args []string) (string, error) {
	if serviceFile == "" && len(args) == 0 {
		return "", errors.New("must specify either -f <file> or a service name")
	}

	if serviceFile != "" {
		parsed, err := parseServiceFile(serviceFile)
		if err != nil {
			return "", err
		}
		result, err := registerServiceIfNeeded(mgr, parsed.YamlFile, parsed.Config.Name)
		if err != nil {
			return "", err
		}
		return result.Name, nil
	}

	return isServiceRegistered(mgr, args[0])
}

func apiRunShouldSkip(mgr manager.ServiceManager, once bool, serviceName string) (bool, error) {
	if !once {
		return false, nil
	}
	return isServiceRunning(mgr, serviceName)
}

func newAPIRunCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [-f <file>] [--once] [name]",
		Short: "Start or restart a service; always outputs JSON",
		Long: `Start a named service or register-and-start from a service file.

If the service is already running it is restarted, unless --once is set.

Output schema (stdout, JSON):
  {
    "name":      string  -- service name
    "pgid":      int     -- process group ID of the running service
    "restarted": bool    -- true if service was already running and got restarted
    "skipped":   bool    -- true if --once was set and service was already running
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api run myservice
  eos api run -f ./service.yaml
  eos api run --once myservice
  eos api run myservice | jq .pgid`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			cfg := getConfig()

			serviceFile, _ := cmd.Flags().GetString("file")
			once, _ := cmd.Flags().GetBool("once")

			serviceName, err := apiRunResolveServiceName(mgr, serviceFile, args)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			// Persist the run as this service's desired boot state; see the
			// identical call in newRunCmd for why (issue #172).
			if err = mgr.SetServiceEnabled(serviceName, true); err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("persisting run state: %w", err))
			}

			skip, err := apiRunShouldSkip(mgr, once, serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}
			if skip {
				return helpers.WriteJSON(cmd, apiRunResult{Name: serviceName, Skipped: true})
			}

			entry, err := mgr.GetServiceCatalogEntry(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			startResult, err := startOrRestartService(mgr, cfg.Shutdown.GracePeriod, &entry)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			return helpers.WriteJSON(cmd, apiRunResult{
				Name:      serviceName,
				PGID:      startResult.PGID,
				Restarted: startResult.Restarted,
			})
		},
	}

	cmd.Flags().StringP("file", "f", "", "path to service.yaml file")
	cmd.Flags().Bool("once", false, "do nothing if service is already running")
	return cmd
}
