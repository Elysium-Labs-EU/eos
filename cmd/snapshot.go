package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func newSnapshotCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Save and restore the set of currently running services",
		Long: `Save every currently running service to a small state file, and restore them
all in one shot later — after a host reboot, an eos upgrade that requires a
restart, or planned maintenance where services were stopped by hand.

This is independent of boot persistence (eos system startup), which restarts
the eos daemon itself; snapshot restores the services that were running
under it, and also covers hosts with no systemd/launchd access.`,
	}

	snapshotCmd.AddCommand(newSnapshotSaveCmd(getManager, getConfig))
	snapshotCmd.AddCommand(newSnapshotRestoreCmd(getManager, getConfig))

	return snapshotCmd
}

func newSnapshotSaveCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	return &cobra.Command{
		Use:           "save",
		Short:         "Save the set of currently running services to a snapshot file",
		Long:          `Records the name of every currently running service, so a later "eos snapshot restore" can bring them all back.`,
		Example:       `  eos snapshot save`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			cfg := getConfig()

			instances, err := mgr.GetAllServiceInstances()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting running services: %v", err))
				return helpers.ErrCommandFailed
			}

			snap := manager.BuildSnapshot(instances, time.Now())
			path := manager.SnapshotFilePath(cfg.BaseDir)
			if err := manager.SaveSnapshot(path, snap); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("saving snapshot: %v", err))
				return helpers.ErrCommandFailed
			}

			if len(snap.Services) == 0 {
				cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "no services are currently running - saved an empty snapshot")
				return nil
			}

			cmd.Printf("%s %s\n", ui.LabelSuccess.Render("success"), fmt.Sprintf("saved snapshot of %d running service(s)", len(snap.Services)))
			for _, name := range snap.Services {
				cmd.Printf("  %s\n", ui.TextBold.Render(name))
			}
			cmd.Println()
			cmd.Printf("%s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos snapshot restore"), ui.TextMuted.Render("→ bring them all back later"))
			return nil
		},
	}
}

func newSnapshotRestoreCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "restore",
		Short: "Start every service recorded in the last snapshot",
		Long: `Reads the snapshot written by "eos snapshot save" and starts each service
in it, in dependency order. A service already running is left alone; a
service no longer registered is skipped with a warning.`,
		Example:       `  eos snapshot restore`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			cfg := getConfig()

			path := manager.SnapshotFilePath(cfg.BaseDir)
			snap, err := manager.LoadSnapshot(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "no snapshot found")
					cmd.PrintErrf("  %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos snapshot save"))
					return helpers.ErrCommandFailed
				}
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("loading snapshot: %v", err))
				return helpers.ErrCommandFailed
			}

			if len(snap.Services) == 0 {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "snapshot is empty - nothing to restore")
				return nil
			}

			// mgr is already built (getManager ran above), so in standalone mode
			// the daemon has been auto-started and this probe no longer fires; see
			// newRunCmd's identical call for why restore — post-reboot recovery,
			// precisely when the daemon is most likely still down — must never
			// silently leave a restored service pinned in 'starting'.
			warnDaemonDownBeforeStart(cmd, &cfg.Daemon)

			ordered := manager.OrderByDependencies(snap.Services, loadDependsOnMap(mgr, snap.Services))

			var started, restarted, skipped, missing, failed []string
			for _, name := range ordered {
				outcome, restoreErr := restoreSnapshotService(cmd, mgr, cfg, name)
				switch {
				case restoreErr != nil:
					cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(name), restoreErr.Error())
					failed = append(failed, name)
				case outcome == restoreOutcomeMissing:
					cmd.Printf("%s %s %s\n\n", ui.LabelWarning.Render("warning"), ui.TextBold.Render(name), "was running at snapshot time but is no longer registered - skipped")
					missing = append(missing, name)
				case outcome == restoreOutcomeAlreadyRunning:
					cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(name), "already running - skipped")
					skipped = append(skipped, name)
				case outcome == restoreOutcomeRestarted:
					restarted = append(restarted, name)
				default:
					started = append(started, name)
				}
			}

			cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("success"), fmt.Sprintf(
				"restore complete: %d started, %d restarted, %d already running, %d no longer registered, %d failed",
				len(started), len(restarted), len(skipped), len(missing), len(failed),
			))

			if len(failed) > 0 {
				return helpers.ErrCommandFailed
			}
			return nil
		},
	}
}

type restoreOutcome int

const (
	restoreOutcomeStarted restoreOutcome = iota
	restoreOutcomeRestarted
	restoreOutcomeAlreadyRunning
	restoreOutcomeMissing
)

// restoreSnapshotService brings a single snapshotted service back up,
// mirroring "eos run <name>": skip if it's no longer registered, skip if
// it's already running (a restore is meant to be safely re-runnable, not a
// forced bounce of services someone deliberately started since the
// snapshot), otherwise gate on its dependencies and start or restart it.
func restoreSnapshotService(cmd *cobra.Command, mgr manager.ServiceManager, cfg *config.SystemConfig, name string) (restoreOutcome, error) {
	registeredName, err := isServiceRegistered(mgr, name)
	if errors.Is(err, ErrServiceNonExistent) {
		return restoreOutcomeMissing, nil
	}
	if err != nil {
		return 0, fmt.Errorf("checking registration: %w", err)
	}

	running, err := isServiceRunning(mgr, registeredName)
	if err != nil {
		return 0, fmt.Errorf("checking running status: %w", err)
	}
	if running {
		return restoreOutcomeAlreadyRunning, nil
	}

	registeredService, err := mgr.GetServiceCatalogEntry(registeredName)
	if err != nil {
		return 0, fmt.Errorf("getting registered service: %w", err)
	}

	if depErr := gateDependencies(cmd.Context(), cmd, mgr, registeredService); depErr != nil {
		return 0, depErr
	}

	result, err := startOrRestartService(mgr, cfg.Shutdown.GracePeriod, registeredService)
	if err != nil {
		return 0, fmt.Errorf("starting service: %w", err)
	}
	if result.Restarted {
		printRestartedSuccessOuput(cmd, registeredService.Name, result.PGID)
		return restoreOutcomeRestarted, nil
	}
	printStartedSuccessOuput(cmd, registeredService.Name, result.PGID)
	return restoreOutcomeStarted, nil
}

// loadDependsOnMap resolves each name's configured DependsOn, for ordering
// restore with OrderByDependencies. A name that's no longer registered, or
// whose config fails to load, is simply left out of the map — it has no
// known deps to order by, and restoreSnapshotService reports its own
// registration/load failure when it's actually processed.
func loadDependsOnMap(mgr manager.ServiceManager, names []string) map[string][]string {
	depsOf := make(map[string][]string, len(names))
	for _, name := range names {
		entry, err := mgr.GetServiceCatalogEntry(name)
		if err != nil {
			continue
		}
		cfg, err := manager.LoadServiceConfig(filepath.Join(entry.DirectoryPath, entry.ConfigFileName))
		if err != nil {
			continue
		}
		depsOf[name] = cfg.DependsOn
	}
	return depsOf
}
