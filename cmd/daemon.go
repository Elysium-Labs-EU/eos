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
	cfg          config.StandaloneDaemonConfig
	health       config.HealthConfig
	shutdown     config.ShutdownConfig
	underSystemd bool
}

func (c *standaloneDaemonController) Start(ctx context.Context, detach bool, logToFileAndConsole bool, verbose bool) error {
	if detach && !c.underSystemd {
		return forkDaemon(ctx, c.cfg.PIDFile, verbose)
	}
	return process.StartStandaloneDaemon(ctx, logToFileAndConsole, verbose, c.baseDir, &c.cfg, &c.health, c.shutdown, c.underSystemd)
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
	logPath := filepath.Join(manager.CreateLogDirPath(c.baseDir), c.cfg.Log.LogFileName)

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

	// #nosec G204 - lines is validated above
	journalArgs := systemctlArgs(c.cfg.UserUnit, "-u", "eos", "-n", fmt.Sprintf("%d", lines))
	if follow {
		journalArgs = append(journalArgs, "-f")
	}
	// #nosec G204 - lines is validated above; journalArgs contains only --user, -u, eos, -n, <int>, and optionally -f
	journalCmd := exec.CommandContext(cmd.Context(), "journalctl", journalArgs...)
	journalCmd.Stdout = cmd.OutOrStdout()
	journalCmd.Stderr = cmd.ErrOrStderr()
	if err := journalCmd.Start(); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting journalctl: %v", err))
		return
	}
	if err := journalCmd.Wait(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if exitErr.ExitCode() != 130 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("journalctl failed: %v", err))
			}
		}
	}
}

func newDaemonController(cfg config.DaemonConfig, baseDir string, health *config.HealthConfig, shutdown config.ShutdownConfig, underSystemd bool) (DaemonController, error) {
	if cfg.Standalone != nil {
		return &standaloneDaemonController{
			cfg:          *cfg.Standalone,
			baseDir:      baseDir,
			health:       *health,
			shutdown:     shutdown,
			underSystemd: underSystemd,
		}, nil
	}
	if cfg.Systemd != nil {
		return systemdDaemonController{cfg: *cfg.Systemd}, nil
	}
	return nil, errors.New("invalid daemon config: both standalone and systemd are nil")
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

Otherwise, starts the daemon directly. By default runs in the foreground and streams output to the console. Pass --detach (-d) to fork the process into a new session in the background; control returns once the PID file is written (timeout: 5s).`,
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := getCtrl()
			detach, err := cmd.Flags().GetBool("detach")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return
			}
			logToFileAndConsole, _ := cmd.Flags().GetBool("log-to-file-and-console")

			if detach {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "starting daemon in background...")
			} else {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "starting daemon in foreground...")
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			if err := ctrl.Start(cmd.Context(), detach, logToFileAndConsole, verbose); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
				return
			}

			if detach {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon started in background")
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos daemon info"), ui.TextMuted.Render("-> check daemon service status"))
			}
		},
	}
	startCmd.Flags().BoolP("detach", "d", false, "run daemon in background")
	startCmd.Flags().Bool("log-to-file-and-console", false, "")
	err := startCmd.Flags().MarkHidden("log-to-file-and-console")
	if err != nil {
		daemonCmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("marking daemon flag as hidden: %v", err))
	}

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		Long:  "Stop the running daemon process. If managed by systemd, delegates to systemctl stop (requires root). Otherwise sends a termination signal directly. Exits cleanly if the daemon is not running.",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := getCtrl()
			verbose, _ := cmd.Flags().GetBool("verbose")
			cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
			killed, err := ctrl.Stop(cmd.Context(), cmd, verbose)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", err))
				return
			}
			if !killed {
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon was not running"))
				return
			}
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon stopped")
		},
	}

	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a stopped daemon",
		Long:  "Remove daemon files. If managed by systemd, removes the unit file only (run 'eos system unstartup' to fully undo startup). Otherwise removes all daemon files; the daemon must be stopped first.",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := getCtrl()
			cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "removing daemon...")
			if err := ctrl.Remove(); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("removing daemon: %v", err))
				return
			}
			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon removed")
			cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos system unstartup"), ui.TextMuted.Render("-> undo systemd startup"))
		},
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show daemon status and configuration",
		Long:  "Display daemon status and configuration. For systemd-managed daemons, shows configuration only (use 'systemctl status eos.service' for runtime state). For standalone daemons, shows whether the process is running, its PID, socket path, log directory, log file name, max file count, and file size limit. Reports clearly if the daemon is stopped or not found.",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := getCtrl()
			ctrl.Info(cmd)
		},
	}

	var lines int
	var follow bool
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "View daemon log output",
		Long:  "Display or stream the daemon's log file. Defaults to the last 300 lines. Use --follow to tail in real time, --lines to control history depth. Accepts values between 0 and 10,000. Exit with Ctrl+C.",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := getCtrl()
			ctrl.Logs(cmd, lines, follow)
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

func newDaemonCmd(getConfig func() (string, *config.SystemConfig, error)) *cobra.Command {
	var ctrl DaemonController

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deployment daemon",
		Long:  "Commands for controlling and monitoring the long-running deployment daemon process. Use start/stop to control the lifecycle, remove to clean up daemon files, info to inspect its current status, and logs to stream its output.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			baseDir, systemConfig, err := getConfig()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
				os.Exit(1)
			}
			ctrl, err = newDaemonController(systemConfig.Daemon, baseDir, &systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd)
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
func forkDaemon(ctx context.Context, pidFile string, verbose bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	args := []string{"daemon", "start", "--log-to-file-and-console"}
	if verbose {
		args = append(args, "--verbose")
	}
	cmd := exec.CommandContext(ctx, exePath, args...) // #nosec G204 -- exePath is from os.Executable(), not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	stderr := &manager.CapturedWriter{}
	cmd.Stderr = stderr

	if os.Getuid() == 0 {
		u, err := userutil.EffectiveUser()
		if err != nil {
			return fmt.Errorf("resolving effective user: %w", err)
		}
		uid, gid, err := userutil.UserCredentials(u)
		if err != nil {
			return fmt.Errorf("resolving user credentials: %w", err)
		}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uid, Gid: gid}

		// The child drops to u's uid/gid, but without this it would inherit
		// root's HOME from the parent's environment (sudo sets HOME=/root).
		// os.UserHomeDir() and friends would then resolve paths under /root,
		// which the dropped-privilege child can't even stat (root's home is 0700).
		cmd.Env = append(envWithout(os.Environ(), "HOME"), "HOME="+u.HomeDir)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	// Wait for child to write PID file before parent exits.
	// Required for Type=forking: systemd reads PID file immediately after parent exits.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	if output := strings.TrimSpace(stderr.String()); output != "" {
		return fmt.Errorf("timed out waiting for PID file: %s\nchild stderr: %s", pidFile, output)
	}
	return fmt.Errorf("timed out waiting for PID file: %s", pidFile)
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
