package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func newStatusCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show the status of all services",
		Long:    `Display the current status of all configured services including their running state, process IDs, and health information.`,
		Example: `  eos status`,
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
				MemoryMb     string
				Started      string
				Uptime       string
				Error        string
				PGID         int
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
					cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), "service file contains different name than registered.")
					continue
				}

				serviceInstance, err := mgr.GetServiceInstance(regServiceName)

				// NOTE: We check here on both string and error type. String because of daemon serialization.
				if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) && !strings.Contains(err.Error(), manager.ErrServiceNotRunning.Error()) {
					cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), fmt.Sprintf("getting service instance: %v", err))
					continue
				}

				// NOTE: We check here on both string and error type. String because of daemon serialization.
				mostRecentProcess, err := mgr.GetMostRecentProcessHistoryEntry(regServiceName)
				if err != nil && !errors.Is(err, manager.ErrNotFound) && !strings.Contains(err.Error(), manager.ErrNotFound.Error()) {
					fmt.Printf("unable to get most recent process history for %s, got: \n %v\n", regServiceName, err)
					continue
				}

				entry := StatusServiceEntry{
					Name:   config.Name,
					Status: helpers.DetermineServiceStatus(mostRecentProcess),
					Uptime: helpers.DetermineUptime(mostRecentProcess),
				}
				if mostRecentProcess != nil {
					entry.PGID = mostRecentProcess.PGID
					entry.Error = helpers.DetermineError(mostRecentProcess.Error)
					entry.MemoryMb = helpers.DetermineProcessMemoryInMb(mostRecentProcess.RssMemoryKb)
				}
				if serviceInstance != nil && serviceInstance.StartedAt != nil {
					entry.Started = humanize.Time(*serviceInstance.StartedAt)
					entry.RestartCount = serviceInstance.RestartCount
				}
				activeServices = append(activeServices, entry)
			}

			rows := [][]string{}

			if len(activeServices) == 0 {
				rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-"})
			} else {
				for _, svc := range activeServices {
					rows = append(rows, []string{
						svc.Name,
						helpers.PrintStatus(svc.Status),
						fmt.Sprintf("%d", svc.PGID),
						svc.MemoryMb,
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
				Headers("name", "status", "pgid", "memory", "uptime", "restarts", "started", "error").
				Rows(rows...)

			fmt.Println(t)
		},
	}
}
