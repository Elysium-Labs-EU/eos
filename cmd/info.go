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
		SilenceUsage:      true,
		SilenceErrors:     true,
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := args[0]
			mgr := getManager()

			registeredService, err := infoFetchRegisteredService(cmd, mgr, serviceName)
			if err != nil {
				return err
			}

			configPath := filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName)
			config := infoLoadConfig(cmd, configPath)

			serviceInstance := infoFetchServiceInstance(cmd, mgr, serviceName)
			processEntry := infoFetchProcessEntry(cmd, mgr, serviceName)

			// TODO: Is there a way to make the fact the log files only exist on services that have run once more explicit?
			logPath := infoFetchLogPath(cmd, mgr, serviceName, false, serviceInstance)
			// TODO: Is there a way to make the fact the log files only exist on services that have run once more explicit?
			errorLogPath := infoFetchLogPath(cmd, mgr, serviceName, true, serviceInstance)

			infoPrintProcessSection(cmd, processEntry)
			infoPrintServiceSection(cmd, &registeredService)
			infoPrintLoggingSection(cmd, logPath, errorLogPath, config)
			infoPrintInstanceSection(cmd, serviceInstance)
			infoPrintConfigSection(cmd, config)

			cmd.Println("")
			return nil
		}}
}

func infoFetchRegisteredService(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) (types.ServiceCatalogEntry, error) {
	registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
		return types.ServiceCatalogEntry{}, helpers.ErrCommandFailed
	}
	return registeredService, nil
}

func infoLoadConfig(cmd *cobra.Command, configPath string) *types.ServiceConfig {
	config, err := manager.LoadServiceConfig(configPath)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("loading service config: %v", err))
	}
	return config
}

func infoFetchServiceInstance(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) *types.ServiceInstance {
	serviceInstance, err := mgr.GetServiceInstance(serviceName)
	if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting service instance: %v", err))
	}
	// serviceInstance may be nil if service was never started
	return serviceInstance
}

func infoFetchProcessEntry(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) *types.ProcessHistory {
	processEntry, err := mgr.GetMostRecentProcessHistoryEntry(serviceName)
	if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting process history: %v", err))
	}
	return processEntry
}

func infoFetchLogPath(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string, errorLog bool, serviceInstance *types.ServiceInstance) *string {
	logPath, err := mgr.GetServiceLogFilePath(serviceName, errorLog)
	if err != nil && serviceInstance != nil {
		label := "getting log path"
		if errorLog {
			label = "getting error log path"
		}
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("%s: %v", label, err))
	}
	return logPath
}

func infoPrintProcessSection(cmd *cobra.Command, processEntry *types.ProcessHistory) {
	helpers.PrintSection(cmd, "Process")
	if processEntry == nil {
		cmd.PrintErr(ui.TextMuted.Render("  no process found\n"))
		return
	}
	status := helpers.DetermineServiceStatus(processEntry)
	helpers.PrintKV(cmd, "status", helpers.PrintStatus(status))
	helpers.PrintKV(cmd, "pgid", fmt.Sprintf("%d", processEntry.PGID))
	helpers.PrintKV(cmd, "uptime", helpers.DetermineUptimeHuman(processEntry))
	helpers.PrintKV(cmd, "memory", helpers.DetermineProcessMemoryInMbHuman(processEntry.RssMemoryKb, status))
	helpers.PrintKV(cmd, "peak memory", helpers.DetermineProcessPeakMemoryInMbHuman(processEntry.PeakRssMemoryKb))
	if processEntry.Error == nil {
		helpers.PrintKV(cmd, "error", "N/A")
	} else {
		helpers.PrintKV(cmd, "error", fmt.Sprintf("%v", *processEntry.Error))
	}
}

func infoPrintServiceSection(cmd *cobra.Command, registeredService *types.ServiceCatalogEntry) {
	helpers.PrintSection(cmd, "Service")
	helpers.PrintKV(cmd, "name", registeredService.Name)
	helpers.PrintKV(cmd, "path", registeredService.DirectoryPath)
	helpers.PrintKV(cmd, "config file", filepath.Join(registeredService.DirectoryPath, registeredService.ConfigFileName))
	helpers.PrintKV(cmd, "created at", registeredService.CreatedAt.String())
}

func infoPrintLoggingSection(cmd *cobra.Command, logPath, errorLogPath *string, config *types.ServiceConfig) {
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
	if config != nil && len(config.LogSinks) > 0 {
		infoPrintLogSinks(cmd, config.LogSinks)
	}
}

func infoPrintLogSinks(cmd *cobra.Command, sinks []types.LogSinkRef) {
	for i := range sinks {
		ref := &sinks[i]
		if ref.Inline == nil {
			helpers.PrintKV(cmd, fmt.Sprintf("sink %d", i+1), fmt.Sprintf("%s (registry)", ref.Name))
			continue
		}
		streams := "all"
		if len(ref.Inline.Streams) > 0 {
			streams = strings.Join(ref.Inline.Streams, ", ")
		}
		helpers.PrintKV(cmd, fmt.Sprintf("sink %d", i+1), fmt.Sprintf("%s (%s)", ref.Inline.Type, streams))
	}
}

func infoPrintInstanceSection(cmd *cobra.Command, serviceInstance *types.ServiceInstance) {
	helpers.PrintSection(cmd, "Instance")
	if serviceInstance == nil {
		cmd.PrintErr(ui.TextMuted.Render("  no service instance found\n"))
		return
	}
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
}

func infoPrintConfigSection(cmd *cobra.Command, config *types.ServiceConfig) {
	helpers.PrintSection(cmd, "Config")
	if config == nil {
		cmd.PrintErr(ui.TextMuted.Render("  no config loaded\n"))
		return
	}
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
}
