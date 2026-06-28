package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/process"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
)

type DaemonController interface {
	Start(ctx context.Context, detach bool, logToFileAndConsole bool) error
	Stop(ctx context.Context) (bool, error)
	Remove() error
	Info(cmd *cobra.Command)
	Logs(cmd *cobra.Command, lines int)
	LogsHint() string
}

type standaloneDaemonController struct {
	baseDir      string
	cfg          config.StandaloneDaemonConfig
	health       config.HealthConfig
	shutdown     config.ShutdownConfig
	underSystemd bool
}

func (c *standaloneDaemonController) Start(ctx context.Context, detach bool, logToFileAndConsole bool) error {
	if detach && !c.underSystemd {
		return forkDaemon(ctx, c.cfg.PIDFile)
	}
	return process.StartStandaloneDaemon(ctx, logToFileAndConsole, c.baseDir, &c.cfg, c.health, c.shutdown, c.underSystemd)
}

func (c *standaloneDaemonController) Stop(_ context.Context) (bool, error) {
	return process.StopStandaloneDaemon(&c.cfg)
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

func (c *standaloneDaemonController) Logs(cmd *cobra.Command, lines int) {
	logPath := filepath.Join(manager.CreateLogDirPath(c.baseDir), c.cfg.Log.LogFileName)

	if _, err := os.Stat(logPath); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file: %v", err))
		return
	}
	if lines < 0 || lines > 10000 {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid line count, should be between 0 and 10000")
		return
	}
	// #nosec G204 - args are validated above
	tailCmd := exec.CommandContext(cmd.Context(), "tail", "-n", fmt.Sprintf("%d", lines), "-f", logPath)
	tailCmd.Stdout = cmd.OutOrStdout()
	tailCmd.Stderr = cmd.ErrOrStderr()
	if err := tailCmd.Start(); err != nil {
		cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting log command: %v", err))
		return
	}
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

func (c systemdDaemonController) Start(ctx context.Context, _ bool, _ bool) error {
	if os.Getuid() != 0 {
		return errors.New("requires root — run with sudo")
	}
	out, err := exec.CommandContext(ctx, "systemctl", "start", "eos").CombinedOutput()
	if err != nil {
		return fmt.Errorf("starting systemd service: %s", out)
	}
	return nil
}

func (c systemdDaemonController) Stop(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "stop", "eos").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("stopping systemd service: %s", out)
	}
	return true, nil
}

func (c systemdDaemonController) Remove() error {
	return os.Remove(c.cfg.SystemdTargetDir + c.cfg.SystemdTargetFileName)
}

func (c systemdDaemonController) Info(cmd *cobra.Command) {
	printSystemdDaemonDetails(cmd)
}

func (c systemdDaemonController) LogsHint() string {
	return "journalctl -u eos -f"
}

func (c systemdDaemonController) Logs(cmd *cobra.Command, lines int) {
	// #nosec G204 - lines is validated by the caller
	journalCmd := exec.CommandContext(cmd.Context(), "journalctl", "-u", "eos", "-n", fmt.Sprintf("%d", lines), "-f")
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

func newDaemonController(cfg config.DaemonConfig, baseDir string, health config.HealthConfig, shutdown config.ShutdownConfig, underSystemd bool) (DaemonController, error) {
	if cfg.Standalone != nil {
		return &standaloneDaemonController{
			cfg:          *cfg.Standalone,
			baseDir:      baseDir,
			health:       health,
			shutdown:     shutdown,
			underSystemd: underSystemd,
		}, nil
	}
	if cfg.Systemd != nil {
		return systemdDaemonController{cfg: *cfg.Systemd}, nil
	}
	return nil, errors.New("invalid daemon config: both standalone and systemd are nil")
}

func newDaemonCmd() *cobra.Command {
	var ctrl DaemonController // closed over by all subcommands below

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deployment daemon",
		Long:  "Commands for controlling and monitoring the long-running deployment daemon process. Use start/stop to control the lifecycle, remove to clean up daemon files, info to inspect its current status, and logs to stream its output.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			_, baseDir, systemConfig, err := newSystemConfig()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
				os.Exit(1)
			}
			ctrl, err = newDaemonController(systemConfig.Daemon, baseDir, systemConfig.Health, systemConfig.Shutdown, systemConfig.UnderSystemd)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("resolving daemon mode: %v", err))
				os.Exit(1)
			}
		},
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon process",
		Long: `Launch the deployment daemon.

If a systemd unit file is installed, delegates to "systemctl start eos" (requires root).

Otherwise, starts the daemon directly. By default runs in the foreground and streams output to the console. Pass --detach (-d) to fork the process into a new session in the background; control returns once the PID file is written (timeout: 5s).`,
		Run: func(cmd *cobra.Command, args []string) {
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

			if err := ctrl.Start(cmd.Context(), detach, logToFileAndConsole); err != nil {
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
			cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
			killed, err := ctrl.Stop(cmd.Context())
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
			ctrl.Info(cmd)
		},
	}

	var lines int
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream the daemon log output",
		Long:  "Tail and follow the daemon's log file in real time. Defaults to the last 300 lines. Use --lines to control how many historical lines are shown before following. Accepts values between 0 and 10,000. Exit with Ctrl+C.",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl.Logs(cmd, lines)
		},
	}
	logsCmd.Flags().IntVar(&lines, "lines", 300, "number of lines to display")

	daemonCmd.AddCommand(infoCmd)
	daemonCmd.AddCommand(logsCmd)
	daemonCmd.AddCommand(removeCmd)
	daemonCmd.AddCommand(startCmd)
	daemonCmd.AddCommand(stopCmd)

	return daemonCmd
}

// Stay in sync with "startDaemonProcess"
func forkDaemon(ctx context.Context, pidFile string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	cmd := exec.CommandContext(ctx, exePath, "daemon", "start", "--log-to-file-and-console") // #nosec G204 -- exePath is from os.Executable(), not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

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

	return fmt.Errorf("timed out waiting for PID file: %s", pidFile)
}

func printSystemdDaemonDetails(cmd *cobra.Command) {
	cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon is systemd managed"))
	cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("systemctl status eos.service") + ui.TextMuted.Render(" → check systemd service status") + "\n")
	cmd.Printf("%s\n\n", ui.TextBold.Render("Logging"))
	cmd.PrintErr(ui.TextMuted.Render("  run: ") + ui.TextCommand.Render("journalctl status eos.service") + ui.TextMuted.Render(" → check journalctl service logs") + "\n")
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
