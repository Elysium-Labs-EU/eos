package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"eos/internal/manager"
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

			cmd.Printf("INFO: This does not stop the service if it's running.\n")
			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.Printf("Error checking service: %v\n", err)
				return
			}

			if !exists {
				cmd.Printf("Service '%s' is not registered\n", serviceName)
				return
			}

			serviceInstance, err := mgr.GetServiceInstance(serviceName)
			if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
				cmd.Printf("Error checking for service instance %v\n", err)
				return
			}

			if serviceInstance != nil {
				removedInstance, removeInstanceErr := mgr.RemoveServiceInstance(serviceName)
				if removeInstanceErr != nil {
					cmd.Printf("Error removing service instance %v\n", removeInstanceErr)
					return
				}
				if !removedInstance {
					cmd.Println("Unable to remove service instance")
					return
				}
				cmd.Println("Successfully removed service instance")
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
			cmd.Printf("Successfully removed service registration '%s'\n", serviceName)
		}}
}
