package cmd

import (
	"deploy-cli/internal/database"
	"deploy-cli/internal/manager"
	"errors"

	"github.com/spf13/cobra"
)

func newRestartCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restarts an active service",
		Long:  "Restarts an active service",
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
			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if errors.Is(err, database.ErrServiceNotFound) {
				cmd.Printf("There registered service was not found, got:\n%v", err)
				return
			} else if err != nil {
				cmd.Printf("An error occured when getting the registered service:\n%v", err)
				return
			}

			pid, err := mgr.RestartService(registeredService.Name)

			if err != nil {
				cmd.Printf("The restart command failed, got:\n%v", err)
			} else {
				cmd.Printf("Restarted with PID: %d\n", pid)
			}
		}}
}
