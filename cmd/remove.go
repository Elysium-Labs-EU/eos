package cmd

import (
	"deploy-cli/internal/manager"

	"github.com/spf13/cobra"
)

func newRemoveCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <service-name>",
		Short: "Unregister a service",
		Long:  `Remove a service from the registry. This does not stop the service if it's running.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.Printf("Error checking service: %v\n", err)
				return
			}

			if !exists {
				cmd.Printf("Service '%s' is not registered\n", serviceName)
				return
			}

			removed, err := mgr.RemoveServiceCatalogEntry(serviceName)
			if err != nil {
				cmd.Printf("Error removing service %v\n", err)
				return
			}

			if !removed {
				cmd.Printf("Service '%s' was not running \n", serviceName)
				return
			}

			cmd.Printf("Successfully removed service '%s'\n", serviceName)

		}}
}
