package cmd

import (
	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiRunResult struct {
	Name      string `json:"name"`
	PGID      int    `json:"pgid"`
	Restarted bool   `json:"restarted"`
	Skipped   bool   `json:"skipped"`
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

			var serviceName string

			if serviceFile != "" {
				parsed, err := parseServiceFile(serviceFile)
				if err != nil {
					return helpers.WriteJSONErr(cmd, err)
				}
				result, err := registerServiceIfNeeded(mgr, parsed.YamlFile, parsed.Config.Name)
				if err != nil {
					return helpers.WriteJSONErr(cmd, err)
				}
				serviceName = result.Name
			} else {
				name, err := isServiceRegistered(mgr, args[0])
				if err != nil {
					return helpers.WriteJSONErr(cmd, err)
				}
				serviceName = name
			}

			if once {
				running, err := isServiceRunning(mgr, serviceName)
				if err != nil {
					return helpers.WriteJSONErr(cmd, err)
				}
				if running {
					return helpers.WriteJSON(cmd, apiRunResult{Name: serviceName, Skipped: true})
				}
			}

			entry, err := mgr.GetServiceCatalogEntry(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, err)
			}

			startResult, err := startOrRestartService(mgr, cfg.Shutdown.GracePeriod, entry)
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
