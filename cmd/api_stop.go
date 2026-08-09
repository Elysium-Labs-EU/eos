package cmd

import (
	"fmt"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
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

func newAPIStopCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, managerMode localModeFn) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   cmdnames.UseStop,
		Short: "Stop a service; always outputs JSON",
		Long: `Stop all processes for a registered service.

Output schema (stdout, JSON):
  {
    "name":    string  -- service name
    "stopped": int     -- number of processes stopped
    "force":   bool    -- true if --force was used
  }

Error schema (stderr, JSON):
  {
    "error": string  -- human-readable message
    "code":  string  -- present on some failures; a stable, script-matchable
                         reason. "grace_period_exceeded" means the process(es)
                         are still alive and a retry with --force is required
                         to actually kill them.
  }

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

			if err := apiRefuseLocalWrite(cmd, managerMode()); err != nil {
				return err
			}

			exists, err := mgr.IsServiceRegistered(cmd.Context(), serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("checking service: %w", err))
			}
			if !exists {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q not found", serviceName))
			}

			if force {
				forceResult, forceErr := mgr.ForceStopService(cmd.Context(), serviceName)
				if forceErr != nil {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("force stopping service: %w", forceErr))
				}
				// Persist the stop as this service's desired boot state only
				// once every process is confirmed gone: a live process left
				// behind by a failed kill must keep boot recovery armed, or
				// nothing will ever adopt or reap it. See the identical
				// ordering below for the graceful path, and newStopCmd for
				// the interactive counterpart.
				if len(forceResult.Errored) == 0 {
					if err = mgr.SetServiceEnabled(cmd.Context(), serviceName, false); err != nil {
						return helpers.WriteJSONErr(cmd, fmt.Errorf("persisting stopped state: %w", err))
					}
				}
				_, _ = mgr.RemoveServiceInstance(cmd.Context(), serviceName)
				return helpers.WriteJSON(cmd, apiStopResult{
					Name:    serviceName,
					Stopped: len(forceResult.Stopped),
					Failed:  len(forceResult.Errored),
					Force:   true,
				})
			}

			result, err := mgr.StopService(cmd.Context(), serviceName, cfg.Shutdown.GracePeriod, 200*time.Millisecond)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("stopping service: %w", err))
			}
			if len(result.Errored) > 0 {
				return helpers.WriteJSONErrCode(cmd,
					fmt.Errorf("graceful stop failed for %d process(es): exceeded grace period; retry with --force to force kill", len(result.Errored)),
					"grace_period_exceeded")
			}

			// Persist the stop as this service's desired boot state; see the
			// identical call above for the force path and newStopCmd for the
			// interactive counterpart. Deferred to here (rather than up front)
			// so a caller that never reaches this line never records a still-
			// running process as "will not start at boot".
			if err = mgr.SetServiceEnabled(cmd.Context(), serviceName, false); err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("persisting stopped state: %w", err))
			}
			_, _ = mgr.RemoveServiceInstance(cmd.Context(), serviceName)
			return helpers.WriteJSON(cmd, apiStopResult{Name: serviceName, Stopped: len(result.Stopped)})
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force kill immediately")
	return cmd
}
