package cmd

import (
	"errors"
	"fmt"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/spf13/cobra"
)

type apiRemoveResult struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

func newAPIRemoveCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Unregister a service; always outputs JSON",
		Long: `Unregisters a service and removes its instance record. Does not stop a running process.

Output schema (stdout, JSON):
  {
    "name":    string  -- service name
    "removed": bool    -- true if the catalog entry was removed
  }

Error schema (stderr, JSON):
  { "error": "string" }

Exit codes:
  0  success
  1  error`,
		Example: `  eos api remove myservice
  eos api remove myservice | jq .removed`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("checking service: %w", err))
			}
			if !exists {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("service %q not found", serviceName))
			}

			serviceInstance, err := mgr.GetServiceInstance(serviceName)
			if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("checking service instance: %w", err))
			}
			if serviceInstance != nil {
				_, instanceErr := mgr.RemoveServiceInstance(serviceName)
				if instanceErr != nil {
					return helpers.WriteJSONErr(cmd, fmt.Errorf("removing service instance: %w", instanceErr))
				}
			}

			removed, err := mgr.RemoveServiceCatalogEntry(serviceName)
			if err != nil {
				return helpers.WriteJSONErr(cmd, fmt.Errorf("removing service: %w", err))
			}

			return helpers.WriteJSON(cmd, apiRemoveResult{Name: serviceName, Removed: removed})
		},
	}
}
