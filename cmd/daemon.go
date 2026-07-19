package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/process"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"codeberg.org/Elysium_Labs/eos/internal/userutil"
	"github.com/spf13/cobra"
)

type DaemonController interface {
	Start(ctx context.Context, detach bool, logToFileAndConsole bool, verbose bool) error
	Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error)
	Remove() error
	Info(cmd *cobra.Command)
	Logs(cmd *cobra.Command, lines int, follow bool)
	LogsHint() string
}

type standaloneDaemonController struct {
	baseDir      string
	identity     userutil.Identity
	telemetry    config.TelemetryConfig
	cfg          config.StandaloneDaemonConfig
	health       config.HealthConfig
	shutdown     config.ShutdownConfig
	underSystemd bool
}

func (c *standaloneDaemonController) Start(ctx context.Context, detach bool, logToFileAndConsole bool, verbose bool) error {
	if detach && !c.underSystemd {
		return forkDaemon(ctx, &c.cfg, verbose, c.identity)
	}
	return process.StartStandaloneDaemon(ctx, logToFileAndConsole, verbose, c.baseDir, &c.cfg, &c.health, c.shutdown, c.telemetry, c.underSystemd)
}

func (c *standaloneDaemonController) Stop(_ context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	helpers.Debugf(cmd, verbose, "reading pid file: %s", c.cfg.PIDFile)
	killed, err := process.StopStandaloneDaemon(c.cfg.PIDFile, c.cfg.SocketPath)
	if err != nil {
		helpers.Debugf(cmd, verbose, "stop failed: %v", err)
		return killed, err
	}
	if killed {
		helpers.Debugf(cmd, verbose, "sent termination signal, removing socket: %s", c.cfg.SocketPath)
	}
	return killed, nil
}

func (c *standaloneDaemonController) Remove() error {
	status, err := process.StatusStandaloneDaemon(&c.cfg)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if status.Running {
		return errors.New("daemon is running — stop it first with 'eos daemon stop'")
	}
	_, err = process.RemoveStandaloneDaemon(&c.cfg)
	return err
}

func (c *standaloneDaemonController) Info(cmd *cobra.Command) {
	status, err := process.StatusStandaloneDaemon(&c.cfg)
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting daemon info: %v", err))
		return
	}
	if !status.Running {
		if status.Pid != nil {
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon found but not running"))
			printStandaloneDaemonDetails(cmd, *status.Pid, &c.cfg)
			return
		}
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon not found"))
		return
	}
	cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("✓"), ui.TextBold.Render("daemon is running"))
	printStandaloneDaemonDetails(cmd, *status.Pid, &c.cfg)
}

func (c *standaloneDaemonController) LogsHint() string {
	return "eos daemon logs"
}

func (c *standaloneDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	tailDaemonLogFile(cmd, c.baseDir, c.cfg.Log.LogFileName, lines, follow)
}

// tailDaemonLogFile tails the daemon's own rotated log file. The daemon writes and
// rotates this file itself regardless of supervisor (standalone, systemd, or launchd),
// so both standaloneDaemonController and launchdDaemonController share this — launchd
// has no persistent unified log like journald, so reusing the daemon's own log file is
// the correct equivalent, not a workaround.
func tailDaemonLogFile(cmd *cobra.Command, baseDir string, logFileName string, lines int, follow bool) {
	logPath := filepath.Join(manager.CreateLogDirPath(baseDir), logFileName)

	if _, err := os.Stat(logPath); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file: %v", err))
		return
	}
	if lines < 0 || lines > 10000 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid line count, should be between 0 and 10000")
		return
	}

	if follow {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "streaming daemon logs")
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "showing daemon logs")
	}

	tailArgs := []string{"-n", fmt.Sprintf("%d", lines)}
	if follow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, logPath)

	// #nosec G204 - args are validated above
	tailCmd := exec.CommandContext(cmd.Context(), "tail", tailArgs...)
	tailCmd.Stderr = cmd.ErrOrStderr()

	stdout, pipeErr := tailCmd.StdoutPipe()
	if pipeErr != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("creating log pipe: %v", pipeErr))
		return
	}
	if err := tailCmd.Start(); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting log command: %v", err))
		return
	}
	renderServiceLogs(cmd.OutOrStdout(), stdout, "")
	if err := tailCmd.Wait(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if exitErr.ExitCode() != 130 { // 130 = Ctrl+C
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("log command failed: %v", err))
			}
		}
	}
}

type systemdDaemonController struct {
	cfg config.SystemdConfig
}

func (c systemdDaemonController) Start(ctx context.Context, _ bool, _ bool, _ bool) error {
	if !c.cfg.UserUnit && os.Getuid() != 0 {
		return errors.New("requires root — run with sudo")
	}
	out, err := exec.CommandContext(ctx, "systemctl", systemctlArgs(c.cfg.UserUnit, "start", "eos")...).CombinedOutput() // #nosec G204 -- args are a fixed set built from a bool, not external input
	if err != nil {
		return fmt.Errorf("starting systemd service: %s", out)
	}
	return nil
}

func (c systemdDaemonController) Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	args := systemctlArgs(c.cfg.UserUnit, "stop", "eos")
	scope := "system"
	if c.cfg.UserUnit {
		scope = "user"
	}
	helpers.Debugf(cmd, verbose, "resolved scope: %s (unit dir: %s)", scope, c.cfg.SystemdTargetDir)

	if c.cfg.UserUnit {
		effectiveUser, effectiveUserErr := userutil.EffectiveUser()
		if effectiveUserErr != nil {
			return false, fmt.Errorf("getting current user: %w", effectiveUserErr)
		}
		effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser)
		if credErr != nil {
			return false, fmt.Errorf("getting current user credentials: %w", credErr)
		}
		if err := ensureUserBusAvailable(ctx, cmd, verbose, effectiveUser.Username, int(effectiveUID), userRuntimeDir(int(effectiveUID)), execRunCmd); err != nil {
			return false, fmt.Errorf("preparing user bus: %w", err)
		}
	}

	helpers.Debugf(cmd, verbose, "running: systemctl %s", strings.Join(args, " "))
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput() // #nosec G204 -- args are a fixed set built from a bool, not external input
	if err != nil {
		helpers.Debugf(cmd, verbose, "systemctl exited with error: %s", strings.TrimSpace(string(out)))
		return false, fmt.Errorf("stopping systemd service: %s", out)
	}
	helpers.Debugf(cmd, verbose, "systemctl stop succeeded")
	return true, nil
}

func (c systemdDaemonController) Remove() error {
	return os.Remove(c.cfg.SystemdTargetDir + c.cfg.SystemdTargetFileName)
}

func (c systemdDaemonController) Info(cmd *cobra.Command) {
	printSystemdDaemonDetails(cmd, c.cfg.UserUnit)
}

func (c systemdDaemonController) LogsHint() string {
	if c.cfg.UserUnit {
		return "journalctl --user -u eos -f"
	}
	return "journalctl -u eos -f"
}

// buildJournalArgs assembles the journalctl arguments for the daemon unit,
// scoped to the user bus when running a user unit.
func buildJournalArgs(userUnit bool, lines int, follow bool) []string {
	journalArgs := systemctlArgs(userUnit, "-u", "eos", "-n", fmt.Sprintf("%d", lines))
	if follow {
		journalArgs = append(journalArgs, "-f")
	}
	return journalArgs
}

// reportJournalExit prints a failure only for real journalctl errors, treating
// exit code 130 (SIGINT from the user's Ctrl-C while following) as normal.
func reportJournalExit(cmd *cobra.Command, err error) {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() != 130 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("journalctl failed: %v", err))
	}
}

// runJournalStream runs journalctl with the given args, forwarding its output to
// the command's streams.
func runJournalStream(cmd *cobra.Command, journalArgs []string) {
	// #nosec G204 - journalArgs contains only --user, -u, eos, -n, <int>, and optionally -f
	journalCmd := exec.CommandContext(cmd.Context(), "journalctl", journalArgs...)
	journalCmd.Stdout = cmd.OutOrStdout()
	journalCmd.Stderr = cmd.ErrOrStderr()
	if err := journalCmd.Start(); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting journalctl: %v", err))
		return
	}
	if err := journalCmd.Wait(); err != nil {
		reportJournalExit(cmd, err)
	}
}

func (c systemdDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	if lines < 0 || lines > 10000 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid line count, should be between 0 and 10000")
		return
	}

	if follow {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "streaming daemon logs")
	} else {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "showing daemon logs")
	}

	runJournalStream(cmd, buildJournalArgs(c.cfg.UserUnit, lines, follow))
}

// launchdDaemonController is the macOS analog of systemdDaemonController. It keeps
// baseDir so Logs can tail the daemon's own log file (see tailDaemonLogFile) — launchd,
// unlike systemd/journald, has no persistent unified log to delegate to.
type launchdDaemonController struct {
	baseDir string
	cfg     config.LaunchdConfig
}

// domain returns "system" for a LaunchDaemon, or "gui/<uid>" for a LaunchAgent —
// resolving the target user's uid via userutil.EffectiveUser() since os.Getuid() is 0
// under sudo while the LaunchAgent's gui session belongs to the invoking user.
func (c launchdDaemonController) domain() string {
	if !c.cfg.UserAgent {
		return "system"
	}
	uid := os.Getuid()
	if effectiveUser, err := userutil.EffectiveUser(); err == nil {
		if euid, _, credErr := userutil.UserCredentials(effectiveUser); credErr == nil {
			uid = int(euid)
		}
	}
	return launchdDomain(true, uid)
}

func (c launchdDaemonController) target() string {
	return c.domain() + "/" + launchdLabel(c.cfg.LaunchdPlistFileName)
}

func (c launchdDaemonController) Start(ctx context.Context, _ bool, _ bool, _ bool) error {
	if !c.cfg.UserAgent && os.Getuid() != 0 {
		return errors.New("requires root — run with sudo")
	}
	plistPath := filepath.Join(c.cfg.LaunchdTargetDir, c.cfg.LaunchdPlistFileName)
	out, err := exec.CommandContext(ctx, "launchctl", "bootstrap", c.domain(), plistPath).CombinedOutput() // #nosec G204 -- args are a fixed set built from config, not external input
	if err != nil {
		return fmt.Errorf("starting launchd service: %s", out)
	}
	return nil
}

// Stop uses "launchctl bootout", which stops the job and unloads it — the plist stays
// on disk so it starts again at next boot/login, matching systemd stop's behavior of
// stopping now without disabling the unit. Exit code 3 ("No such process") means the
// job wasn't loaded, i.e. already stopped — treated the same as systemd stop on an
// already-stopped unit (killed=false, no error), verified empirically via launchctl.
func (c launchdDaemonController) Stop(ctx context.Context, cmd *cobra.Command, verbose bool) (bool, error) {
	target := c.target()
	helpers.Debugf(cmd, verbose, "running: launchctl bootout %s", target)
	out, err := exec.CommandContext(ctx, "launchctl", "bootout", target).CombinedOutput() // #nosec G204 -- args are a fixed set built from config, not external input
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			helpers.Debugf(cmd, verbose, "launchctl bootout: job was not loaded")
			return false, nil
		}
		helpers.Debugf(cmd, verbose, "launchctl exited with error: %s", strings.TrimSpace(string(out)))
		return false, fmt.Errorf("stopping launchd service: %s", out)
	}
	helpers.Debugf(cmd, verbose, "launchctl bootout succeeded")
	return true, nil
}

func (c launchdDaemonController) Remove() error {
	return os.Remove(filepath.Join(c.cfg.LaunchdTargetDir, c.cfg.LaunchdPlistFileName))
}

func (c launchdDaemonController) Info(cmd *cobra.Command) {
	printLaunchdDaemonDetails(cmd, c.cfg.UserAgent)
}

func (c launchdDaemonController) LogsHint() string {
	return "eos daemon logs"
}

func (c launchdDaemonController) Logs(cmd *cobra.Command, lines int, follow bool) {
	tailDaemonLogFile(cmd, c.baseDir, config.DaemonLogFileName, lines, follow)
}

func newDaemonController(cfg config.DaemonConfig, baseDir string, health *config.HealthConfig, shutdown config.ShutdownConfig, telemetry config.TelemetryConfig, underSystemd bool, identity userutil.Identity) (DaemonController, error) {
	if cfg.Standalone != nil {
		return &standaloneDaemonController{
			cfg:          *cfg.Standalone,
			baseDir:      baseDir,
			health:       *health,
			shutdown:     shutdown,
			telemetry:    telemetry,
			underSystemd: underSystemd,
			identity:     identity,
		}, nil
	}
	if cfg.Systemd != nil {
		return systemdDaemonController{cfg: *cfg.Systemd}, nil
	}
	if cfg.Launchd != nil {
		return launchdDaemonController{cfg: *cfg.Launchd, baseDir: baseDir}, nil
	}
	return nil, errors.New("invalid daemon config: standalone, systemd, and launchd are all nil")
}

// buildDaemonSubcmds attaches all daemon subcommands to daemonCmd.
// getCtrl is called at Run time; in production it returns the controller set by
// PersistentPreRun, and in tests it returns a mock.
func buildDaemonSubcmds(daemonCmd *cobra.Command, getCtrl func() DaemonController) {
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon process",
		Long: `Launch the deployment daemon.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).

Otherwise, starts the daemon detached in the background by default; control returns once the PID file is written (timeout: 5s). --detach (-d) is accepted for backward compatibility but is now a no-op. Pass --foreground (-f) to run in the foreground and stream output to the console instead — Ctrl-C will then stop the daemon.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl := getCtrl()
			foreground, err := cmd.Flags().GetBool("foreground")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return helpers.ErrCommandFailed
			}
			detachFlag, err := cmd.Flags().GetBool("detach")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return helpers.ErrCommandFailed
			}
			if foreground && detachFlag {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "cannot use --foreground and --detach together")
				return helpers.ErrCommandFailed
			}
			detach := !foreground
			logToFileAndConsole, _ := cmd.Flags().GetBool("log-to-file-and-console")

			if detach {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "starting daemon in background...")
			} else {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "starting daemon in foreground — press Ctrl-C to stop this daemon...")
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			if err := ctrl.Start(cmd.Context(), detach, logToFileAndConsole, verbose); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
				return helpers.ErrCommandFailed
			}

			if detach {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon started in background")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos daemon info"), ui.TextMuted.Render("to check daemon service status"))
			}
			return nil
		},
	}
	startCmd.Flags().BoolP("foreground", "f", false, "run daemon in foreground and stream output (Ctrl-C stops it)")
	startCmd.Flags().BoolP("detach", "d", false, "run daemon in background (default; kept for backward compatibility)")
	startCmd.Flags().Bool("log-to-file-and-console", false, "")
	err := startCmd.Flags().MarkHidden("log-to-file-and-console")
	if err != nil {
		daemonCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("marking daemon flag as hidden: %v", err))
	}

	stopCmd := &cobra.Command{
		Use:           "stop",
		Short:         "Stop the running daemon",
		Long:          "Stop the running daemon process. If managed by systemd, delegates to systemctl stop (requires root). Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl := getCtrl()
			verbose, _ := cmd.Flags().GetBool("verbose")
			cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
			killed, err := ctrl.Stop(cmd.Context(), cmd, verbose)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", err))
				return helpers.ErrCommandFailed
			}
			if !killed {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
				return nil
			}
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon stopped")
			return nil
		},
	}

	removeCmd := &cobra.Command{
		Use:           "remove",
		Short:         "Remove a stopped daemon",
		Long:          "Remove daemon files. If managed by systemd, removes the unit file only (run 'eos system unstartup' to fully undo startup). Otherwise removes all daemon files; the daemon must be stopped first.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl := getCtrl()
			cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "removing daemon...")
			if err := ctrl.Remove(); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing daemon: %v", err))
				return helpers.ErrCommandFailed
			}
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon removed")
			cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos system unstartup"), ui.TextMuted.Render("to undo systemd startup"))
			return nil
		},
	}

	var allUsers bool
	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show daemon status and configuration",
		Long:  "Display daemon status and configuration. For systemd-managed daemons, shows configuration only (use 'systemctl status eos.service' for runtime state). For standalone daemons, shows whether the process is running, its PID, socket path, log directory, log file name, max file count, and file size limit. Reports clearly if the daemon is stopped or not found.\n\nPass --all (root only) to enumerate every user's standalone daemon on the host instead of just the invoking user's, flagging any still running against a since-replaced binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if allUsers {
				return printAllDaemons(cmd)
			}
			ctrl := getCtrl()
			ctrl.Info(cmd)
			return nil
		},
	}
	infoCmd.Flags().BoolVar(&allUsers, "all", false, "list every user's standalone daemon on this host (root only)")

	var lines int
	var follow bool
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "View daemon log output",
		Long:  "Display or stream the daemon's log file. Defaults to the last 300 lines. Use --follow to tail in real time, --lines to control history depth. Accepts values between 0 and 10,000. Exit with Ctrl+C.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctrl := getCtrl()
			ctrl.Logs(cmd, lines, follow)
			return nil
		},
	}
	logsCmd.Flags().IntVar(&lines, "lines", 300, "number of lines to display")
	logsCmd.Flags().BoolVar(&follow, "follow", false, "follow log output")

	daemonCmd.AddCommand(infoCmd)
	daemonCmd.AddCommand(logsCmd)
	daemonCmd.AddCommand(removeCmd)
	daemonCmd.AddCommand(startCmd)
	daemonCmd.AddCommand(stopCmd)
}

func newDaemonCmd(getConfig func() (string, *config.SystemConfig, userutil.Identity, error)) *cobra.Command {
	var ctrl DaemonController

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deployment daemon",
		Long:  "Commands for controlling and monitoring the long-running deployment daemon process. Use start/stop to control the lifecycle, remove to clean up daemon files, info to inspect its current status, and logs to stream its output.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			baseDir, systemConfig, identity, err := getConfig()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
				os.Exit(1)
			}
			ctrl, err = newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.Telemetry, systemConfig.UnderSystemd, identity)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving daemon mode: %v", err))
				os.Exit(1)
			}
		},
	}

	buildDaemonSubcmds(daemonCmd, func() DaemonController { return ctrl })

	return daemonCmd
}

// envWithout returns env with any entries for key removed.
func envWithout(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// Stay in sync with "startDaemonProcess"
// buildForkCommand constructs the detached `eos daemon start` command. When the
// parent runs as root it drops the child to identity's uid/gid and strips root's
// HOME so the child resolves paths under its own home, not /root.
//
// The child's stderr is wired to a real file (manager.OpenForkStderrLog), not an
// in-process io.Writer: os/exec creates a real OS pipe for any Stderr that isn't
// an *os.File, and the pipe's read end lives in this process. Once this process
// exits — moments after reporting success back to the shell — that pipe is
// orphaned, and the detached child gets SIGPIPE'd on its next stderr write (see
// issue #156). A real file has no reader to lose: fork/exec gives the child its
// own independent file descriptor regardless of what this process does with it.
func buildForkCommand(ctx context.Context, exePath string, verbose bool, identity userutil.Identity, pidFile string) (*exec.Cmd, *os.File, error) {
	// --foreground is required here: the child inherits no flags, and "daemon start"
	// now defaults to detach=true, so without it the child would fork again and again.
	args := []string{"daemon", "start", "--foreground", "--log-to-file-and-console"}
	if verbose {
		args = append(args, "--verbose")
	}
	cmd := exec.CommandContext(ctx, exePath, args...) // #nosec G204 -- exePath is from os.Executable(), not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil

	stderrFile, err := manager.OpenForkStderrLog(pidFile)
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = stderrFile

	if os.Getuid() == 0 {
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: identity.UID(), Gid: identity.GID()}

		// The child drops to identity's uid/gid, but without this it would inherit
		// root's HOME from the parent's environment (sudo sets HOME=/root).
		// os.UserHomeDir() and friends would then resolve paths under /root,
		// which the dropped-privilege child can't even stat (root's home is 0700).
		cmd.Env = append(envWithout(os.Environ(), "HOME"), "HOME="+identity.HomeDir())
	}
	return cmd, stderrFile, nil
}

// waitForForkPIDFile blocks until the forked daemon is confirmed alive via its
// PID file, or the 5s deadline elapses. It re-checks process liveness (not
// just file existence): a PID file can exist for an instant before the
// process that wrote it dies.
func waitForForkPIDFile(pidFile string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := process.StatusStandaloneDaemon(&config.StandaloneDaemonConfig{PIDFile: pidFile})
		if err == nil && status.Running {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for PID file: %s", pidFile)
}

// waitForForkSocket blocks until the forked daemon's Unix socket accepts a
// connection, or the 5s deadline elapses. A PID file can exist — and its
// process still be alive — for a brief window before an unrelated startup
// failure (e.g. a socket bind error) kills it moments later; confirming the
// socket answers is what actually proves the daemon reached a running state,
// not just that it forked (issue #156).
func waitForForkSocket(ctx context.Context, socketPath string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if socketResponds(ctx, socketPath) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon socket: %s", socketPath)
}

// forkDaemon starts a new detached daemon process: refuse if one is already
// running, spawn the child, then confirm it is durably alive before
// reporting success (issue #156). Each step is a small, independently tested
// helper so this orchestrator stays a plain three-step sequence.
func forkDaemon(ctx context.Context, cfg *config.StandaloneDaemonConfig, verbose bool, identity userutil.Identity) error {
	if err := ensureDaemonNotRunning(cfg); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	if err := spawnForkedDaemon(ctx, exePath, verbose, identity, cfg.PIDFile); err != nil {
		return err
	}

	return confirmForkAlive(ctx, cfg)
}

// ensureDaemonNotRunning errors if a live daemon already holds cfg's PID
// file. A fork while one is running spawns a child that fails to bind and
// exits quietly, which previously looked identical to success (issue #156).
func ensureDaemonNotRunning(cfg *config.StandaloneDaemonConfig) error {
	status, err := process.StatusStandaloneDaemon(cfg)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if status.Running {
		return fmt.Errorf("daemon already running (pid %d)", *status.Pid)
	}
	return nil
}

// spawnForkedDaemon starts the detached daemon child. Once cmd.Start()
// returns, this process's copy of the child's stderr fd is no longer needed:
// fork/exec already gave the child its own independent one (see
// buildForkCommand).
func spawnForkedDaemon(ctx context.Context, exePath string, verbose bool, identity userutil.Identity, pidFile string) error {
	cmd, stderrFile, err := buildForkCommand(ctx, exePath, verbose, identity, pidFile)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stderrFile.Close()
		return fmt.Errorf("failed to start daemon process: %w", err)
	}
	_ = stderrFile.Close()
	return nil
}

// confirmForkAlive waits for the forked child to become durably alive: PID
// file liveness first (required for Type=forking: systemd reads the PID file
// immediately after this process exits), then the daemon's socket actually
// answering — a PID file can exist, and its process still be alive, for a
// brief window before an unrelated startup failure (e.g. a socket bind
// error) kills it moments later (issue #156).
func confirmForkAlive(ctx context.Context, cfg *config.StandaloneDaemonConfig) error {
	if err := waitForForkPIDFile(cfg.PIDFile); err != nil {
		return forkStartupErr(err, cfg.PIDFile)
	}
	if err := waitForForkSocket(ctx, cfg.SocketPath); err != nil {
		return forkStartupErr(err, cfg.PIDFile)
	}
	return nil
}

// forkStartupErr folds any captured child stderr into a fork startup failure,
// whichever of the two readiness waits (PID file, socket) timed out first.
func forkStartupErr(err error, pidFile string) error {
	if output := manager.ReadForkStderr(pidFile); output != "" {
		return fmt.Errorf("%w\nchild stderr: %s", err, output)
	}
	return err
}

func printAllDaemons(cmd *cobra.Command) error {
	if os.Getuid() != 0 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "--all requires root — run with sudo")
		return helpers.ErrCommandFailed
	}

	daemons, err := process.DiscoverDaemons()
	if err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("discovering daemons: %v", err))
		return helpers.ErrCommandFailed
	}

	renderDaemonSummaries(cmd, daemons)
	return nil
}

// renderDaemonSummaries prints one line per discovered daemon plus a trailing restart hint
// if any are stale. Split out from printAllDaemons so it can be tested without root.
func renderDaemonSummaries(cmd *cobra.Command, daemons []process.DaemonSummary) {
	if len(daemons) == 0 {
		cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("no standalone daemons found on this host"))
		return
	}

	staleCount := 0
	for _, d := range daemons {
		if d.Err != nil {
			cmd.Printf("%s %s\n", ui.LabelError.Render("✗"), d.Username)
			cmd.Printf("  %s %v\n\n", ui.TextMuted.Render("error:"), d.Err)
			continue
		}
		if !d.Status.Running {
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("○"), d.Username, ui.TextMuted.Render("not running"))
			continue
		}
		if d.StaleBinary {
			staleCount++
			cmd.Printf("%s %s %s\n", ui.LabelWarning.Render("⚠"), d.Username, ui.TextMuted.Render(fmt.Sprintf("running (pid %d) — on a since-replaced binary, restart needed", *d.Status.Pid)))
			continue
		}
		cmd.Printf("%s %s %s\n", ui.LabelSuccess.Render("✓"), d.Username, ui.TextMuted.Render(fmt.Sprintf("running (pid %d)", *d.Status.Pid)))
	}
	cmd.Println()

	if staleCount > 0 {
		cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("%d daemon(s) still running the pre-update binary", staleCount))
		cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("sudo -u <user> eos daemon stop && sudo -u <user> eos daemon start") + ui.TextMuted.Render(" → restart each") + "\n\n")
	}
}

func printSystemdDaemonDetails(cmd *cobra.Command, userUnit bool) {
	statusCmd := "systemctl status eos.service"
	logsCmd := "journalctl -u eos.service"
	if userUnit {
		statusCmd = "systemctl --user status eos.service"
		logsCmd = "journalctl --user -u eos.service"

		if effectiveUser, effectiveUserErr := userutil.EffectiveUser(); effectiveUserErr == nil {
			if effectiveUID, _, credErr := userutil.UserCredentials(effectiveUser); credErr == nil {
				uid := int(effectiveUID)
				if !isAccessibleDir(os.Getenv("XDG_RUNTIME_DIR"), uid) && !isAccessibleDir(userRuntimeDir(uid), uid) {
					cmd.Printf("%s %s\n\n", ui.LabelWarning.Render("warning"), "no active systemd user bus — the commands below will fail with \"Failed to connect to bus\"")
					cmd.Printf("%s %s\n\n", ui.TextMuted.Render("hint:"), fmt.Sprintf("run %s, then start a fresh login session (or export XDG_RUNTIME_DIR=/run/user/%d in this shell)", ui.TextCommand.Render("sudo loginctl enable-linger "+effectiveUser.Username), uid))
				}
			}
		}
	}
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon is systemd managed"))
	cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render(statusCmd) + ui.TextMuted.Render(" → check systemd service status") + "\n")
	cmd.Printf("%s\n\n", ui.TextBold.Render("Logging"))
	cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render(logsCmd) + ui.TextMuted.Render(" → check journalctl service logs") + "\n")
	cmd.Println()
}

func printLaunchdDaemonDetails(cmd *cobra.Command, userAgent bool) {
	statusCmd := "sudo launchctl print system/" + config.LaunchdLabel
	scope := "launch daemon"
	if userAgent {
		scope = "launch agent"
		statusCmd = "launchctl print gui/$(id -u)/" + config.LaunchdLabel
	}
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render(fmt.Sprintf("daemon is launchd managed (%s)", scope)))
	cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render(statusCmd) + ui.TextMuted.Render(" → check launchd service status") + "\n")
	cmd.Printf("%s\n\n", ui.TextBold.Render("Logging"))
	cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("eos daemon logs") + ui.TextMuted.Render(" → tail daemon log file") + "\n")
	cmd.Println()
}

func printStandaloneDaemonDetails(cmd *cobra.Command, pid int, cfg *config.StandaloneDaemonConfig) {
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("PID:"), pid)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("pid file:"), cfg.PIDFile)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket path:"), cfg.SocketPath)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket timeout:"), cfg.SocketTimeout)
	cmd.Printf("%s\n\n", ui.TextBold.Render("Logging"))
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("log dir:"), cfg.Log.LogDir)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("log file:"), cfg.Log.LogFileName)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("log max files:"), cfg.Log.LogMaxFiles)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("log file size limit:"), cfg.Log.LogFileSizeLimit)
}
