package cmd

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/process"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

func newSystemCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, managerMode localModeFn) *cobra.Command {
	var ctrl DaemonController // closed over by all subcommands below

	systemCmd := &cobra.Command{
		Use:   cmdnames.System,
		Short: "Manage the eos system settings",
		Long:  `Manage eos system settings, check for updates, and inspect runtime configuration.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			ctrl = sysResolveDaemonController(cmd)
		},
	}

	infoCmd := &cobra.Command{
		Use:           cmdnames.SystemInfo,
		Short:         "See active system information and configurations",
		Long:          `Display active EOS configuration including install paths, daemon settings, health check limits, and shutdown grace period.`,
		Example:       `  eos system info`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sysRunInfo(cmd, systemCmd)
		},
	}

	startupCmdDef := &cobra.Command{
		Use:   cmdnames.SystemStartup,
		Short: "Enable eos to start automatically on boot",
		Long: `Install a systemd unit (Linux), OpenRC init script (Linux, non-systemd), or launchd plist (macOS) for eos and enable it to run on boot.

On systemd, auto-detects the unit scope based on how you invoke the command:
  - Run as root (sudo): installs a system unit at /etc/systemd/system/eos.service, a LaunchDaemon at /Library/LaunchDaemons/org.elysiumlabs.eos.plist on macOS, or a system-wide OpenRC init script at /etc/init.d/eos — one per host, daemon runs as the invoking user.
  - Run as a regular user: installs a user unit at ~/.config/systemd/user/eos.service, or a LaunchAgent at ~/Library/LaunchAgents/org.elysiumlabs.eos.plist on macOS — each user gets their own, no root required.

A systemd user unit runs inside your personal --user systemd instance, which
by default only exists while you are logged in: close the SSH session or log
out of the desktop, and systemd tears down that instance along with every
unit running in it — including eos. A system unit has no such dependency; it
runs under the system-wide systemd instance, which starts at boot regardless
of who (if anyone) is logged in.

"linger" (loginctl enable-linger <username>) closes that gap by telling
systemd to keep your user instance running with no one logged in, the way a
system unit always does. This command offers to enable it right after
installing a user unit, and it's what most single-user servers (VPS,
homeserver) want — otherwise the service silently dies the moment you
disconnect. Skip it only if you deliberately want eos to run solely while
you're logged in (e.g. a desktop session). Installing as root/system unit
never needs linger, since it isn't tied to a login session in the first
place.

On OpenRC, installs a system-wide init script at /etc/init.d/eos and requires root — OpenRC has no per-user service scope.

Enabling this also revives every previously-registered service that wasn't
stopped by hand: the daemon starts every service in its catalog on boot,
skipping only those last stopped with "eos stop" — not just the eos daemon
itself.`,
		Example:       "  sudo eos system startup  # system unit (root, one per host)\n       eos system startup  # user unit (no root, per-user, systemd/launchd only)\n       eos system startup --yes  # skip confirmation (non-interactive)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sysRunStartup(cmd, systemCmd)
		},
	}
	startupCmdDef.Flags().BoolP("yes", "y", false, flagDescSkipConfirm)

	unstartupCmdDef := &cobra.Command{
		Use:   cmdnames.SystemUnstartup,
		Short: "Disable eos from starting automatically on boot",
		Long: `Remove the systemd unit (Linux), OpenRC init script (Linux, non-systemd), or launchd plist (macOS) for eos and disable it from running on boot.

On systemd, auto-detects the unit scope based on how you invoke the command:
  - Run as root (sudo): removes the system unit / LaunchDaemon / OpenRC init script.
  - Run as a regular user: removes the user unit / LaunchAgent.

On OpenRC, removes the system-wide init script at /etc/init.d/eos and requires root.`,
		Example:       "  sudo eos system unstartup  # remove system unit\n       eos system unstartup  # remove user unit (systemd/launchd only)\n       eos system unstartup --yes  # skip confirmation (non-interactive)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sysRunUnstartup(cmd, systemCmd)
		},
	}
	unstartupCmdDef.Flags().BoolP("yes", "y", false, flagDescSkipConfirm)

	updateCmd := &cobra.Command{
		Use:           cmdnames.SystemUpdate,
		Short:         "Apply new update if available",
		Long:          `Check GitHub for a newer eos release and optionally download and install it. Uses SHA256 checksum validation and backs up the current binary before replacing it.`,
		Example:       "  eos system update        # check and apply latest stable release\n  eos system update --pre  # include pre-releases",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sysRunUpdate(cmd, systemCmd, ctrl)
		},
	}
	updateCmd.Flags().Bool("pre", false, "includes pre-releases in update check")

	uninstallCmd := &cobra.Command{
		Use:   cmdnames.SystemUninstall,
		Short: "Remove eos from this system",
		Long:  `Stops all running services, removes the eos binary and configuration, and cleans up the install directory. Prompts for confirmation unless --yes is passed.`,
		Example: `  eos system uninstall        # interactive uninstall with confirmation prompt
  eos system uninstall --yes  # skip confirmation (non-interactive)`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sysRunUninstall(cmd, systemCmd, getManager, getConfig, ctrl, managerMode)
		},
	}
	uninstallCmd.Flags().BoolP("yes", "y", false, flagDescSkipConfirm)

	versionCmd := &cobra.Command{
		Use:     cmdnames.SystemVersion,
		Short:   "Get version of system",
		Long:    `Print the current eos version, git commit hash, and build date. Also flags version drift against the running daemon and the latest published release, and suggests the fix.`,
		Example: `  eos system version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sysRunVersion(cmd)
			return nil
		},
	}

	systemCmd.AddCommand(infoCmd)
	systemCmd.AddCommand(startupCmdDef)
	systemCmd.AddCommand(unstartupCmdDef)
	systemCmd.AddCommand(updateCmd)
	systemCmd.AddCommand(uninstallCmd)
	systemCmd.AddCommand(versionCmd)

	return systemCmd
}

// sysResolveDaemonController implements the "system" command group's
// PersistentPreRun: resolve config and daemon mode, or print an error and
// exit(1) — cobra's PersistentPreRun has no error return, so a hard failure
// here has always meant terminating the process directly.
func sysResolveDaemonController(cmd *cobra.Command) DaemonController {
	_, baseDir, systemConfig, identity, err := newSystemConfig()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
		os.Exit(1)
	}
	ctrl, err := newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.Telemetry, systemConfig.UnderSystemd, identity)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving daemon mode: %v", err))
		os.Exit(1)
	}
	return ctrl
}

// loadSystemConfigAndFlags loads the system config and resolves the shared
// verbose/--yes flags used by the startup/unstartup commands, printing to
// printCmd and returning helpers.ErrCommandFailed on failure so callers only
// need to check err != nil.
func loadSystemConfigAndFlags(cmd *cobra.Command, printCmd *cobra.Command) (installDir string, systemConfig *config.SystemConfig, identity userutil.Identity, verbose, flagYes bool, err error) {
	installDir, _, systemConfig, identity, err = newSystemConfig()
	if err != nil {
		printCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
		return installDir, systemConfig, identity, verbose, flagYes, helpers.ErrCommandFailed
	}
	verbose, _ = cmd.Flags().GetBool("verbose")
	flagYes, err = cmd.Flags().GetBool("yes")
	if err != nil {
		printCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
		return installDir, systemConfig, identity, verbose, flagYes, helpers.ErrCommandFailed
	}
	return installDir, systemConfig, identity, verbose, flagYes, nil
}

func unitScope(userUnit bool) string {
	if userUnit {
		return "user unit"
	}
	return "system unit"
}

// unitName derives the systemctl unit name (e.g. "eos") from a unit filename
// (e.g. "eos.service"). Production always uses config.SystemdTargetFileName
// ("eos.service" -> "eos"); tests pass an isolated name so systemctl
// enable/disable/stop calls target a throwaway unit instead of the real one.
func unitName(systemdFile string) string {
	return strings.TrimSuffix(systemdFile, ".service")
}

func systemctlArgs(userUnit bool, args ...string) []string {
	if userUnit {
		return append([]string{"--user"}, args...)
	}
	return args
}

// ensureRuntime verifies the host is running the given init system (want),
// printing and returning ErrCommandFailed otherwise. Shared by the systemd
// and OpenRC startup/unstartup paths.
func ensureRuntime(cmd *cobra.Command, verbose bool, detectRuntime func() (string, error), want string) error {
	runtime, err := detectRuntime()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system command: %v", err))
		return helpers.ErrCommandFailed
	}
	helpers.Debugf(cmd, verbose, "detected runtime: %s", runtime)
	if runtime != want {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("managing startup file not supported for this runtime: %v", runtime))
		return helpers.ErrCommandFailed
	}
	return nil
}

// confirmOrDecline prompts unless flagYes is set. When declined it prints
// declineMsg (unless empty) and reports the decline so callers can return
// early.
func confirmOrDecline(cmd *cobra.Command, flagYes bool, prompt, declineMsg string) bool {
	if flagYes || helpers.PromptConfirm(cmd, prompt) {
		return true
	}
	if declineMsg != "" {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), declineMsg)
	}
	return false
}

// prepareUserBus makes the invoking user's systemd bus reachable before any
// --user systemctl call.
func prepareUserBus(ctx context.Context, cmd *cobra.Command, verbose bool, effectiveUser *user.User, run runCmdFn) error {
	effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
	if credErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting current user credentials: %v", credErr))
		return helpers.ErrCommandFailed
	}
	if err := ensureUserBusAvailable(ctx, cmd, verbose, effectiveUser.Username, int(effectiveUID), userRuntimeDir(int(effectiveUID)), run, isAccessibleDir); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("preparing user bus: %v", err))
		return helpers.ErrCommandFailed
	}
	return nil
}

// stopStandaloneForRestart stops a running standalone daemon before handing off
// to the init system, printing progress. Returns ErrCommandFailed on a stop
// error; a not-running daemon is not an error.
func stopStandaloneForRestart(cmd *cobra.Command, daemonConfig *config.StandaloneDaemonConfig) error {
	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "stopping daemon...")
	if daemonConfig == nil {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(msgDaemonWasNotRunning))
		return nil
	}
	killed, killErr := process.StopStandaloneDaemon(daemonConfig.PIDFile, daemonConfig.SocketPath)
	if killErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", killErr))
		return helpers.ErrCommandFailed
	}
	if !killed {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), ui.TextMuted.Render(msgDaemonWasNotRunning))
		return nil
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "daemon stopped")
	return nil
}

// launchdLabel derives the launchctl job label (e.g. "org.elysiumlabs.eos") from a
// plist filename (e.g. "org.elysiumlabs.eos.plist"). Mirrors unitName for systemd;
// tests pass an isolated name so launchctl calls target a throwaway job.
func launchdLabel(plistFileName string) string {
	return strings.TrimSuffix(plistFileName, ".plist")
}

// launchdDomain returns the launchctl target domain: "system" for a LaunchDaemon,
// or "gui/<uid>" for a LaunchAgent running in the given user's GUI session.
func launchdDomain(userAgent bool, uid int) string {
	if userAgent {
		return fmt.Sprintf("gui/%d", uid)
	}
	return "system"
}

func launchdScope(userAgent bool) string {
	if userAgent {
		return "launch agent"
	}
	return "launch daemon"
}

// resolveLaunchdUID returns the uid used to build the launchctl target domain:
// the invoking user's uid for a LaunchAgent, or the current process uid.
func resolveLaunchdUID(cmd *cobra.Command, userAgent bool, effectiveUser *user.User) (int, error) {
	if !userAgent {
		return os.Getuid(), nil
	}
	effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
	if credErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting current user credentials: %v", credErr))
		return 0, helpers.ErrCommandFailed
	}
	return int(effectiveUID), nil
}

func checkWritable(cmd *cobra.Command, dir string) error {
	file, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("directory %q does not appear to be writable: %w", dir, err)
	}

	if closeErr := file.Close(); closeErr != nil {
		return fmt.Errorf("closing temp file: %w", closeErr)
	}

	if removeErr := os.Remove(file.Name()); removeErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelWarning.Render("warning"), fmt.Sprintf("could not remove temp file %s: %v\n", file.Name(), removeErr))
	}

	return nil
}
