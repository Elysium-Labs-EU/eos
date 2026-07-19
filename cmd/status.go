package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func newStatusCmd(getManager func() manager.ServiceManager, warnDaemonDown func(*cobra.Command), getConfig func() *config.SystemConfig) *cobra.Command {
	var watch bool
	var interval int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of all services",
		Long:  `Display the current status of all configured services including their running state, process IDs, and health information.`,
		Example: `  eos status
  eos status --watch
  eos status --watch --interval 5`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Probe daemon liveness before getManager: in standalone mode
			// getManager auto-starts the daemon, which would mask an outage.
			warnDaemonDown(cmd)

			mgr := getManager()
			checkInterval := resolveCheckInterval(getConfig)

			if !watch {
				printStatusTable(cmd, mgr, checkInterval)
				return nil
			}
			if interval < 1 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "--interval must be at least 1 second")
				return helpers.ErrCommandFailed
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()

			renderWatchFrame(cmd, mgr, interval, checkInterval)

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					renderWatchFrame(cmd, mgr, interval, checkInterval)
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch mode: refresh status periodically")
	cmd.Flags().IntVarP(&interval, "interval", "i", 2, "refresh interval in seconds (only with --watch)")

	return cmd
}

func renderWatchFrame(cmd *cobra.Command, mgr manager.ServiceManager, interval int, checkInterval time.Duration) {
	cmd.Print("\033[2J\033[H")
	cmd.Printf("Every %ds: eos status    %s\n\n", interval, time.Now().Format("15:04:05"))
	printStatusTable(cmd, mgr, checkInterval)
}

// resolveCheckInterval reads the configured health-check interval at this
// boundary so the pure staleness decision (helpers.IsProcessHistoryStale)
// receives an already-resolved value. Falls back to the default interval when
// config is unavailable or the interval is non-positive, so status never marks
// every row stale off a missing config.
func resolveCheckInterval(getConfig func() *config.SystemConfig) time.Duration {
	checkInterval := time.Duration(config.HealthCheckIntervalMs) * time.Millisecond
	if cfg := getConfig(); cfg != nil && cfg.Health.CheckInterval > 0 {
		checkInterval = cfg.Health.CheckInterval
	}
	return checkInterval
}

// daemonIdentity describes which daemon answered the request, so "no
// services registered" isn't indistinguishable from "wrong user's daemon
// answered" in multi-user setups (each user runs their own daemon against
// their own base dir). Resolution failures degrade gracefully instead of
// hiding the "no services" message behind an unrelated error.
func daemonIdentity() string {
	identity, identityErr := userutil.ResolveIdentity()
	if identityErr != nil {
		return ""
	}

	baseDir, baseDirErr := config.GetBaseDir(identity)
	if baseDirErr != nil {
		return fmt.Sprintf("for user %s", identity.Username())
	}
	return fmt.Sprintf("for user %s (base dir: %s)", identity.Username(), baseDir)
}

func printStatusTable(cmd *cobra.Command, mgr manager.ServiceManager, checkInterval time.Duration) {
	registeredServices, err := mgr.GetAllServiceCatalogEntries()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered services: %v", err))
		return
	}

	numberOfRegisteredServices := len(registeredServices)

	if numberOfRegisteredServices == 0 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "no services are registered "+daemonIdentity())
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos add <path>") + ui.TextMuted.Render(" to register a service") + "\n")
		return
	}

	type StatusServiceEntry struct {
		Name         string
		Status       types.ServiceStatus
		MemoryMb     string
		CPU          string
		Started      string
		Uptime       string
		Error        string
		NextRestart  string
		PGID         int
		RestartCount int
		Stale        bool
	}
	var activeServices []StatusServiceEntry
	now := time.Now()

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
			cmd.PrintErrf("  %s %s %s\n",
				ui.TextMuted.Render("run:"),
				ui.TextCommand.Render("eos update <service-name> <new-path>"),
				ui.TextMuted.Render("→ update the service"),
			)
		}

		serviceInstance, err := mgr.GetServiceInstance(regServiceName)

		if err != nil && !errors.Is(err, manager.ErrServiceNotRunning) {
			cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), fmt.Sprintf("getting service instance: %v", err))
			continue
		}

		mostRecentProcess, err := mgr.GetMostRecentProcessHistoryEntry(regServiceName)
		if err != nil && !errors.Is(err, manager.ErrProcessNotFound) {
			cmd.PrintErrf("%s %s: %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(regServiceName), fmt.Sprintf("getting process history: %v", err))
			continue
		}

		entry := StatusServiceEntry{
			Name:     regServiceName,
			Status:   helpers.DetermineServiceStatus(mostRecentProcess),
			Uptime:   helpers.DetermineUptimeHuman(mostRecentProcess),
			MemoryMb: helpers.DetermineProcessMemoryInMbHuman(0, helpers.DetermineServiceStatus(mostRecentProcess)),
			CPU:      helpers.DetermineProcessCPUHuman(0, helpers.DetermineServiceStatus(mostRecentProcess)),
		}
		if mostRecentProcess != nil {
			entry.PGID = mostRecentProcess.PGID
			entry.Error = helpers.DetermineError(mostRecentProcess.Error)
			entry.MemoryMb = helpers.DetermineProcessMemoryInMbHuman(mostRecentProcess.RssMemoryKb, entry.Status)
			entry.CPU = helpers.DetermineProcessCPUHuman(mostRecentProcess.CPUPercent, entry.Status)
			entry.Stale = helpers.IsProcessHistoryStale(mostRecentProcess, checkInterval, now)
		}
		if serviceInstance != nil && serviceInstance.StartedAt != nil {
			entry.Started = humanize.Time(*serviceInstance.StartedAt)
			entry.RestartCount = serviceInstance.RestartCount
		}
		switch {
		case config.CronRestart == "":
			entry.NextRestart = "-"
		case serviceInstance != nil && serviceInstance.NextRestartAt != nil:
			entry.NextRestart = humanize.Time(*serviceInstance.NextRestartAt)
		default:
			entry.NextRestart = "pending"
		}
		activeServices = append(activeServices, entry)
	}

	rows := [][]string{}
	// staleRows[i] tracks whether data row i has a stale process_history row,
	// so the StyleFunc (which only sees row/col indices) can dim it. A stale
	// row is one whose monitor stopped refreshing updated_at; this is
	// independent of the status column's daemon-liveness reading.
	staleRows := []bool{}

	if len(activeServices) == 0 {
		rows = append(rows, []string{"-", "-", "-", "-", "-", "-", "-", "-", "-", "-"})
		staleRows = append(staleRows, false)
	} else {
		for i := range activeServices {
			svc := &activeServices[i]
			status := helpers.PrintStatus(svc.Status)
			if svc.Stale {
				status += " " + ui.TextMuted.Render("(stale)")
			}
			rows = append(rows, []string{
				svc.Name,
				status,
				fmt.Sprintf("%d", svc.PGID),
				svc.MemoryMb,
				svc.CPU,
				svc.Uptime,
				fmt.Sprintf("%d", svc.RestartCount),
				svc.Started,
				svc.NextRestart,
				svc.Error,
			})
			staleRows = append(staleRows, svc.Stale)
		}
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.TableBorderColor)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return ui.TableHeaderStyle
			}
			if row >= 0 && row < len(staleRows) && staleRows[row] {
				return ui.TableStaleRowStyle
			}
			if row%2 == 0 {
				return ui.TableEvenRowStyle
			}
			return ui.TableOddRowStyle
		}).
		Headers("name", "status", "pgid", "memory", "cpu", "uptime", "restarts", "started", "next restart", "error").
		Rows(rows...)

	cmd.Println(t)
}
