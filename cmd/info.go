package cmd

import (
	"errors"
	"path/filepath"

	"github.com/spf13/cobra"

	"eos/cmd/helpers"
	"eos/internal/database"
	"eos/internal/manager"
)

func newInfoCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Shows info on the service",
		Long:  "Shows info on the service",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if errors.Is(err, database.ErrServiceNotFound) {
				cmd.Printf("There registered service was not found, got:\n%v\n", err)
				return
			}
			if err != nil {
				cmd.Printf("An error occurred when getting the registered service:\n%v\n", err)
				return
			}

			configPath := filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName)
			config, err := manager.LoadServiceConfig(configPath)
			if err != nil {
				cmd.Printf("An error occurred when loading the service config:\n%v\n", err)
			}

			serviceInstance, err := mgr.GetServiceInstance(serviceName)
			if err != nil {
				cmd.Printf("An error occurred when getting the service instance:\n%v\n", err)
			}

			processEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil {
				cmd.Printf("Unable to find a history entry for the service, got:\n%v\n", err)
			}
			logPath, err := mgr.GetServiceLogFilePath(serviceName, false)
			if err != nil {
				cmd.Printf("Unable to find the log path for the service, got:\n%v\n", err)
			}
			errorLogPath, err := mgr.GetServiceLogFilePath(serviceName, true)
			if err != nil {
				cmd.Printf("Unable to find the error log path for the service, got:\n%v\n", err)
			}

			if processEntry != nil {
				cmd.Println("")
				cmd.Printf("Status: %s\n", helpers.DetermineServiceStatus(processEntry.State))
				cmd.Printf("PID: %d\n", processEntry.PID)
				cmd.Printf("Uptime: %s\n", helpers.DetermineUptime(processEntry))
				cmd.Printf("Error: %v\n", processEntry.Error)
			}

			cmd.Println("")
			cmd.Println("# Service information")
			cmd.Printf("Name: %s\n", registeredService.Name)
			cmd.Printf("Path: %s\n", registeredService.DirectoryPath)
			cmd.Printf("Config file name: %s\n", registeredService.ConfigFileName)
			cmd.Printf("Created At: %s\n", registeredService.CreatedAt)

			cmd.Println("")
			cmd.Println("# Logging information")
			if logPath != nil {
				cmd.Printf("Log Path: %s\n", *logPath)
			} else {
				cmd.Printf("Log Path: %s\n", "N/A")
			}
			if errorLogPath != nil {
				cmd.Printf("Error log Path: %s\n", *errorLogPath)
			} else {
				cmd.Printf("Error log Path: %s\n", "N/A")
			}

			cmd.Println("")
			cmd.Println("# Service instance information")
			if serviceInstance != nil {
				cmd.Printf("Restart Count: %d\n", serviceInstance.RestartCount)

				if serviceInstance.LastHealthCheck != nil {
					cmd.Printf("Last Health Check: %s\n", serviceInstance.LastHealthCheck)
				} else {
					cmd.Printf("Last Health Check: %s\n", "N/A")
				}
				if serviceInstance.StartedAt != nil {
					cmd.Printf("Started: %s\n", serviceInstance.StartedAt)
				} else {
					cmd.Printf("Started: %s\n", "N/A")
				}

				cmd.Printf("Created: %s\n", serviceInstance.CreatedAt)

				if serviceInstance.UpdatedAt != nil {
					cmd.Printf("Updated: %s\n", serviceInstance.UpdatedAt)
				} else {
					cmd.Printf("Updated: %s\n", "N/A")
				}
			} else {
				cmd.Println("No service instance found")
			}

			cmd.Println("")
			cmd.Println("# Service config information")
			if config != nil {
				cmd.Printf("Service command: %s\n", config.Command)
				if config.Port != 0 {
					cmd.Printf("Service port: %d\n", config.Port)
				} else {
					cmd.Printf("Service port: %s\n", "N/A")
				}
				cmd.Printf("Runtime: %s\n", config.Runtime.Type)
				cmd.Printf("Runtime path: %s\n", config.Runtime.Path)
			} else {
				cmd.Println("No valid config loaded")
			}
		}}
}
