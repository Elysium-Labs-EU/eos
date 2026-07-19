package cmd

import (
	"fmt"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiStopResult struct {
	Name    string `json:"name"`
	Stopped int    `json:"stopped"`
	Failed  int    `json:"failed,omitempty"`
	Force   bool   `json:"force"`
}

func newAPIStopCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a service; always outputs JSON",
		Long: `Stop all processes for a registered service.

Output schema (stdout, JSON):
  {
    "name":    string  -- service name
    "stopped": int     -- number of processes stopped
    "force":   bool    -- true if --force was used
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api stop myservice
  eos api stop myservice --force
  eos api stop myservice | jq .stopped`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()
			cfg := getConfig()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("checking service: %w", err))
			}
			if !exists {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q not found", serviceName))
			}

			if force {
				forceResult, forceErr := mgr.ForceStopService(serviceName)
				if forceErr != nil {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("force stopping service: %w", forceErr))
				}
				_, _ = mgr.RemoveServiceInstance(serviceName)
				return helpers.WriteJSON(cmd, apiStopResult{
					Name:    serviceName,
					Stopped: len(forceResult.Stopped),
					Failed:  len(forceResult.Errored),
					Force:   true,
				})
			}

			result, err := mgr.StopService(serviceName, cfg.Shutdown.GracePeriod, 200*time.Millisecond)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("stopping service: %w", err))
			}
			if len(result.Errored) > 0 {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("graceful stop failed for %d process(es)", len(result.Errored)))
			}
			_, _ = mgr.RemoveServiceInstance(serviceName)
			return helpers.WriteJSON(cmd, apiStopResult{Name: serviceName, Stopped: len(result.Stopped)})
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force kill immediately")
	return cmd
}
