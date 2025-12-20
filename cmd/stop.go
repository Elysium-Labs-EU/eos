package cmd

import (
	"bufio"
	"eos/internal/manager"
	"strings"

	"github.com/spf13/cobra"
)

func newStopCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <service-name>",
		Short: "Stop all processes for a service",
		Long:  `Stops all the processes for a registered service.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			cmd.Printf("Stopping service '%s'\n", serviceName)

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.Printf("Error checking service: %v\n", err)
				return
			}
			if !exists {
				cmd.Println("The service isn't registered")
				cmd.Println("- Use 'eos add <path>' to register services")
				cmd.Println("- Use 'eos status' to view registered services")
				return
			}

			stopResult, err := mgr.StopService(serviceName)

			if err != nil {
				cmd.Printf("Error occured during gathering service information for graceful stopping, got:\n %v", err)
				return
			}

			if len(stopResult.Stopped) == 0 && len(stopResult.Failed) == 0 {
				cmd.Printf("No operations found for the service '%s'\n", serviceName)

				removed, err := mgr.RemoveServiceInstance(serviceName)

				if err != nil {
					cmd.Printf("Failed to clean up service instance, got: %v\n", err)
					return
				}

				if !removed {
					cmd.Print("Service was not running")
					return
				}

				cmd.Print("Successfully stopped and cleaned up service")
				return
			}

			if len(stopResult.Stopped) > 0 && len(stopResult.Failed) == 0 {
				cmd.Printf("Successfully stopped all processes of the service '%s'\n", serviceName)
				removed, err := mgr.RemoveServiceInstance(serviceName)
				if err != nil {
					cmd.Printf("Failed to clean up service instance, got: %v\n", err)
					return
				}

				if !removed {
					cmd.Print("Service was not running")
					return
				}

				cmd.Print("Successfully stopped and cleaned up service")
				return
			}

			if len(stopResult.Stopped) > 0 {
				cmd.Printf("Successfully stopped %v processes of the service %s", len(stopResult.Stopped), serviceName)
			}

			if len(stopResult.Failed) > 0 {
				cmd.Printf("Failed to gracefully stop the service %s. \n", serviceName)
				cmd.Printf("Would you like to force quit? (y/n): ")

				reader := bufio.NewReader(cmd.InOrStdin())
				response, err := reader.ReadString('\n')

				if err != nil {
					cmd.Printf("Error reading input: %v\n", err)
					return
				}

				response = strings.TrimSpace(strings.ToLower(response))
				if response == "y" || response == "yes" {
					forceStopResult, err := mgr.ForceStopService(serviceName)
					if err != nil {
						cmd.Printf("Error occured during gathering service information for forceful stopping, got: %v\n", err)
						return
					}

					if len(forceStopResult.Stopped) > 0 {
						cmd.Printf("Successfully force stopped %v processes of this service", len(forceStopResult.Stopped))
					}

					if len(forceStopResult.Failed) > 0 {
						cmd.Printf("Failed to forcefully stop the service, manual action is required.")
						return
					}

					removed, err := mgr.RemoveServiceInstance(serviceName)
					if err != nil {
						cmd.Printf("Failed to clean up service instance, got: %v\n", err)
						return
					}

					if !removed {
						cmd.Print("Service was not running")
						return
					}

					cmd.Print("Successfully stopped and cleaned up service")
					return

				} else {
					cmd.Println("Aborted force quit")
				}
			}

			removed, err := mgr.RemoveServiceInstance(serviceName)
			if err != nil {
				cmd.Printf("Failed to clean up service instance, got: %v\n", err)
				return
			}

			if !removed {
				cmd.Print("Service was not running")
				return
			}

			cmd.Print("Successfully stopped and cleaned up service")
		}}
}
