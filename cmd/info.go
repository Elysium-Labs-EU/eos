package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newInfoCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:               "info",
		Short:             "Shows info on the service",
		Long:              `Show detailed information about a registered service including its process state, runtime statistics, log file paths, and full configuration.`,
		Example:           `  eos info cms`,
		ValidArgsFunction: helpers.ServiceNameCompletions(getManager),
		Args:              cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			mgr := getManager()

			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if errors.Is(err, database.ErrServiceNotFound) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return
			}
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return
			}

			configPath := filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName)
			config, err := manager.LoadServiceConfig(configPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("loading service config: %v", err))
			}

			serviceInstance, err := mgr.GetServiceInstance(serviceName)
			if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting service instance: %v", err))
			}
			// serviceInstance may be nil if service was never started

			processEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
			if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting process history: %v", err))
			}

			// TODO: Is there a way to make the fact the log files only exist on services that have run once more explicit?
			logPath, err := mgr.GetServiceLogFilePath(serviceName, false)
			if err != nil && serviceInstance != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log path: %v", err))
			}
			// TODO: Is there a way to make the fact the log files only exist on services that have run once more explicit?
			errorLogPath, err := mgr.GetServiceLogFilePath(serviceName, true)
			if err != nil && serviceInstance != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting error log path: %v", err))
			}

			helpers.PrintSection(cmd, "Process")
			if processEntry != nil {
				helpers.PrintKV(cmd, "status", helpers.PrintStatus(helpers.DetermineServiceStatus(processEntry)))
				helpers.PrintKV(cmd, "pgid", fmt.Sprintf("%d", processEntry.PGID))
				helpers.PrintKV(cmd, "uptime", helpers.DetermineUptimeHuman(processEntry))
				helpers.PrintKV(cmd, "memory", helpers.DetermineProcessMemoryInMbHuman(processEntry.RssMemoryKb))
				if processEntry.Error == nil {
					helpers.PrintKV(cmd, "error", "N/A")
				} else {
					helpers.PrintKV(cmd, "error", fmt.Sprintf("%v", *processEntry.Error))
				}
			} else {
				cmd.PrintErr(ui.TextMuted.Render("  no process found\n"))
			}

			helpers.PrintSection(cmd, "Service")
			helpers.PrintKV(cmd, "name", registeredService.Name)
			helpers.PrintKV(cmd, "path", registeredService.DirectoryPath)
			helpers.PrintKV(cmd, "config file", filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName))
			helpers.PrintKV(cmd, "created at", registeredService.CreatedAt.String())

			helpers.PrintSection(cmd, "Logging")
			if logPath != nil {
				helpers.PrintKV(cmd, "log path", *logPath)
			} else {
				helpers.PrintKV(cmd, "log path", "N/A")
			}
			if errorLogPath != nil {
				helpers.PrintKV(cmd, "error log path", *errorLogPath)
			} else {
				helpers.PrintKV(cmd, "error log path", "N/A")
			}

			helpers.PrintSection(cmd, "Instance")
			if serviceInstance != nil {
				helpers.PrintKV(cmd, "restarts", fmt.Sprintf("%d", serviceInstance.RestartCount))
				if serviceInstance.LastHealthCheck != nil {
					helpers.PrintKV(cmd, "last health check", serviceInstance.LastHealthCheck.String())
				} else {
					helpers.PrintKV(cmd, "last health check", "N/A")
				}
				if serviceInstance.StartedAt != nil {
					helpers.PrintKV(cmd, "started", serviceInstance.StartedAt.String())
				} else {
					helpers.PrintKV(cmd, "started", "N/A")
				}
				helpers.PrintKV(cmd, "created", serviceInstance.CreatedAt.String())
				if serviceInstance.UpdatedAt != nil {
					helpers.PrintKV(cmd, "updated", serviceInstance.UpdatedAt.String())
				} else {
					helpers.PrintKV(cmd, "updated", "N/A")
				}
			} else {
				cmd.PrintErr(ui.TextMuted.Render("  no service instance found\n"))
			}

			helpers.PrintSection(cmd, "Config")
			if config != nil {
				helpers.PrintKV(cmd, "command", config.Command)
				if config.Port != 0 {
					helpers.PrintKV(cmd, "port", fmt.Sprintf("%d", config.Port))
				} else {
					helpers.PrintKV(cmd, "port", "N/A")
				}
				if config.Runtime.Type == "" {
					helpers.PrintKV(cmd, "runtime", "N/A")
				} else {
					helpers.PrintKV(cmd, "runtime", config.Runtime.Type)
				}
				if config.Runtime.Path == "" {
					helpers.PrintKV(cmd, "runtime path", "N/A")
				} else {
					helpers.PrintKV(cmd, "runtime path", config.Runtime.Path)
				}
			} else {
				cmd.PrintErr(ui.TextMuted.Render("  no config loaded\n"))
			}

			cmd.Println("")
		}}
}
