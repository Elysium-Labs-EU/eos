package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"eos/cmd/helpers"
	"eos/internal/manager"
	"eos/internal/types"
	"eos/internal/ui"
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
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered services: %v", err))
				return
			}

			numberOfRegisteredServices := len(registeredServices)

			if numberOfRegisteredServices == 0 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "no services are registered")
				cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos add <path>") + ui.TextMuted.Render(" to register a service") + "\n")
				return
			}

			type StatusServiceEntry struct {
				Name         string
				Status       types.ServiceStatus
				Started      string
				Uptime       string
				Error        string
				PID          int
				RestartCount int
			}
			var activeServices []StatusServiceEntry

			for _, regService := range registeredServices {
				configPath := filepath.Join(regService.DirectoryPath, regService.ConfigFileName)
				config, err := manager.LoadServiceConfig(configPath)
				regServiceName := regService.Name

				if err != nil {
					cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), fmt.Sprintf("loading service config: %v", err))
					continue
				}
				if config.Name != regServiceName {
					cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), fmt.Sprintf("name of services drifted: %v", err))
					continue
				}

				serviceInstance, err := mgr.GetServiceInstance(regServiceName)
				if err != nil {
					cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), fmt.Sprintf("getting service instance: %v", err))
					continue
				}
				if serviceInstance == nil {
					cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(regServiceName), "not yet started")
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

			rows := [][]string{}

			if len(activeServices) == 0 {
				rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
			} else {
				for _, svc := range activeServices {
					rows = append(rows, []string{
						svc.Name,
						helpers.PrintStatus(svc.Status),
						fmt.Sprintf("%d", svc.PID),
						svc.Uptime,
						fmt.Sprintf("%d", svc.RestartCount),
						svc.Started,
						svc.Error,
					})
				}
			}

			t := table.New().
				Border(lipgloss.RoundedBorder()).
				BorderStyle(lipgloss.NewStyle().Foreground(ui.TableBorderColor)).
				StyleFunc(func(row, col int) lipgloss.Style {
					if row == table.HeaderRow {
						return ui.TableHeaderStyle
					}
					if row%2 == 0 {
						return ui.TableEvenRowStyle
					}
					return ui.TableOddRowStyle
				}).
				Headers("NAME", "STATUS", "PID", "UPTIME", "RESTARTS", "STARTED", "ERROR").
				Rows(rows...)

			fmt.Println(t)
		},
	}
}
