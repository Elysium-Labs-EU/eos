package cmd

import (
	"eos/cmd/helpers"
	"eos/internal/manager"
	"eos/internal/types"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dustin/go-humanize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func newStatusCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of all services",
		Long:  `Display the current status of all configured services including their running state, process IDs, and health information.`,
		Run: func(cmd *cobra.Command, args []string) {
			mgr := getManager()
			registeredServices, err := mgr.GetAllServiceCatalogEntries()
			if err != nil {
				cmd.Printf("error getting registered services: %v\n", err)
				return
			}

			numberOfRegisteredServices := len(registeredServices)

			if numberOfRegisteredServices == 0 {
				cmd.Println("No services registered")
				cmd.Println("Use 'eos add <path>' to register services")
				return
			}

			type StatusServiceEntry struct {
				Name         string
				Status       types.ServiceStatus
				PID          int
				Started      string
				Uptime       string
				RestartCount int
				Error        string
			}
			var activeServices []StatusServiceEntry

			for _, regService := range registeredServices {
				configPath := filepath.Join(regService.DirectoryPath, regService.ConfigFileName)
				config, err := manager.LoadServiceConfig(configPath)
				regServiceName := regService.Name

				if err != nil {
					cmd.Printf("%s: Error reading config '(%v)'\n", regServiceName, err)
					continue
				}
				if config.Name != regServiceName {
					cmd.Printf("%s: Name of services drifted\n", regServiceName)
					continue
				}

				serviceInstance, err := mgr.GetServiceInstance(regServiceName)
				if err != nil {
					cmd.Printf("%s: Unable to get service instance '(%v)'\n", regServiceName, err)
					continue
				}
				if serviceInstance == nil {
					cmd.Printf("%s: No active service instance found\n", regServiceName)
					continue
				}

				mostRecentProcess, err := mgr.GetMostRecentProcessHistoryEntry(regServiceName)
				if err != nil {
					fmt.Printf("Unable to get most recent process history for %s, got: \n %v\n", regServiceName, err)
					continue
				}

				if mostRecentProcess == nil {
					fmt.Printf("No process history found for %s\n", regServiceName)
					continue
				}

				activeServices = append(activeServices, StatusServiceEntry{
					Name:         config.Name,
					Status:       helpers.DetermineServiceStatus(mostRecentProcess.State),
					PID:          mostRecentProcess.PID,
					Started:      humanize.Time(*serviceInstance.StartedAt),
					Uptime:       helpers.DetermineUptime(mostRecentProcess),
					RestartCount: serviceInstance.RestartCount,
					Error:        helpers.DetermineError(mostRecentProcess.Error),
				})
			}

			t := table.NewWriter()
			t.SetOutputMirror(os.Stdout)
			t.SetStyle(table.StyleRounded)
			t.AppendHeader(table.Row{
				"Name", "Status", "PID", "Uptime", "Restart Count", "Started", "Error",
			})
			t.SetColumnConfigs([]table.ColumnConfig{
				{Number: 1, WidthMin: 25},
				{Number: 2, WidthMin: 15},
				{Number: 3, WidthMin: 15},
				{Number: 4, WidthMin: 12},
				{Number: 5, WidthMin: 20},
				{Number: 6, WidthMin: 20},
			})

			if len(activeServices) == 0 {
				t.AppendRow(table.Row{
					"-",
					"-",
					"-",
					"-",
					"-",
					"-",
					"-",
				})
			}

			for _, svc := range activeServices {
				t.AppendRow(table.Row{
					svc.Name,
					svc.Status,
					svc.PID,
					svc.Uptime,
					svc.RestartCount,
					svc.Started,
					svc.Error,
				})
			}
			t.Render()
		},
	}
}
