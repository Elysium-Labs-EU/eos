package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"eos/internal/config"
	"eos/internal/manager"
	"eos/internal/process"
	"eos/internal/ui"
)

func newDaemonCmd() *cobra.Command {
	var baseDir string
	var config *config.SystemConfig

	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deployment daemon",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			var err error
			_, baseDir, config, err = createSystemConfig()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting config: %v", err))
				os.Exit(1)
			}
		},
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			detached, err := cmd.Flags().GetBool("detach")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
				return
			}

			if detached {
				if err := forkDaemon(context.Background()); err != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
					return
				}
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "daemon started in background")
				return
			}

			cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), "starting daemon in foreground...")
			logToFileAndConsole, _ := cmd.Flags().GetBool("log-to-file-and-console")

			if err := process.StartDaemon(logToFileAndConsole, baseDir, config.Daemon, config.Health); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting daemon: %v", err))
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
		Short: "Stop the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("%s %s\n", ui.LabelInfo.Render("info"), "stopping daemon...")
			killed, err := process.StopDaemon(config.Daemon)
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

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Info on the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			status, err := process.StatusDaemon(config.Daemon)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting daemon info: %v", err))
				return
			}

			if !status.Running {
				if status.Pid != nil {
					cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"),
						ui.TextMuted.Render("daemon found but not running"))
					printDaemonDetails(cmd, *status.Pid, config.Daemon)
					return
				}
				cmd.Printf("%s %s\n\n", ui.LabelInfo.Render("info"), ui.TextMuted.Render("daemon not found"))
				return
			}

			cmd.Printf("%s %s\n\n", ui.LabelSuccess.Render("✓"), ui.TextBold.Render("daemon is running"))
			printDaemonDetails(cmd, *status.Pid, config.Daemon)
		},
	}

	var lines int
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Logs of the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			logPath := filepath.Join(manager.CreateLogDirPath(baseDir), config.Daemon.LogFileName)

			_, err := os.Stat(logPath)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting log file: %v", err))
				return
			}

			if lines < 0 || lines > 10000 {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "invalid line count, should be between 0 and 10000")
				return
			}
			// #nosec G204 - args are validated above
			tailLogCommand := exec.CommandContext(cmd.Context(), "tail", "-n", fmt.Sprintf("%d", lines), "-f", logPath)
			tailLogCommand.Stdout = cmd.OutOrStdout()
			tailLogCommand.Stderr = cmd.ErrOrStderr()
			if err := tailLogCommand.Start(); err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("starting log command: %v", err))
				return
			}

			if err := tailLogCommand.Wait(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					if exitErr.ExitCode() != 130 { // 130 = Ctrl+C
						cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("log command failed: %v", err))
					}
				}
			}
		},
	}
	logsCmd.Flags().IntVar(&lines, "lines", 300, "number of lines to display")

	daemonCmd.AddCommand(logsCmd)
	daemonCmd.AddCommand(startCmd)
	daemonCmd.AddCommand(infoCmd)
	daemonCmd.AddCommand(stopCmd)

	return daemonCmd
}

// Stay in sync with "startDaemonProcess"
func forkDaemon(ctx context.Context) error {
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

	return nil
}

func printDaemonDetails(cmd *cobra.Command, pid int, cfg config.DaemonConfig) {
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("PID:"), pid)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("socket:"), cfg.SocketPath)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("log dir:"), cfg.LogDir)
	cmd.Printf("  %s %s\n", ui.TextMuted.Render("log file:"), cfg.LogFileName)
	cmd.Printf("  %s %d\n", ui.TextMuted.Render("max files:"), cfg.MaxFiles)
}
