package cmd

import (
	"eos/internal/database"
	"eos/internal/manager"
	"errors"

	"github.com/spf13/cobra"
)

func newStartCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Starts a registered service",
		Long:  "Starts up a registered service",
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

			pid, err := mgr.StartService(registeredService.Name)

			if err != nil {
				cmd.Printf("The start command failed, got:\n%v", err)
			} else {
				cmd.Printf("Started with PID: %d\n", pid)
			}
		}}
}
