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
				cmd.PrintErrf("Error getting config: %v\n", err)
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
				cmd.PrintErrf("Failed to parse flag: %v\n", err)
				return
			}

			if detached {
				if err := forkDaemon(); err != nil {
					cmd.PrintErrf("Failed to start daemon: %v\n", err)
					return
				}
				cmd.Println("Daemon started in background")
				return
			}

			cmd.Println("Starting daemon in foreground...")
			logToFileAndConsole, _ := cmd.Flags().GetBool("log-to-file-and-console")

			if err := process.StartDaemon(logToFileAndConsole, baseDir, config.Daemon, config.Health); err != nil {
				cmd.PrintErrf("Failed to start daemon: %v\n", err)
			}
		},
	}
	startCmd.Flags().BoolP("detach", "d", false, "Run daemon in background")
	startCmd.Flags().Bool("log-to-file-and-console", false, "")
	err := startCmd.Flags().MarkHidden("log-to-file-and-console")
	if err != nil {
		daemonCmd.PrintErrf("Failed to mark daemon flag as hidden: %v\n", err)
	}

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("Stopping daemon...")
			killed, err := process.StopDaemon(config.Daemon)
			if err != nil {
				cmd.PrintErrf("Failed to stop daemon: %v\n", err)
				return
			}
			if !killed {
				cmd.Println("Daemon was not running")
				return
			}
			cmd.Println("Daemon stopped")
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Status of the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("Reading status of daemon...")

			status, err := process.StatusDaemon(config.Daemon)
			if err != nil {
				cmd.PrintErrf("Failed to get status of daemon: %v\n", err)
				return
			}

			if !status.Running {
				if status.Pid != nil {
					cmd.Println("Daemon is found but not running")
					return
				}
				cmd.Println("Daemon not found")
				return
			}

			cmd.Printf("Daemon is running \n")
			cmd.Printf("PID: %d", *status.Pid)
		},
	}

	var lines int
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Logs of the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("Reading logs of daemon...")

			logPath := filepath.Join(manager.CreateLogDirPath(baseDir), config.Daemon.LogFileName)

			_, err := os.Stat(logPath)
			if err != nil {
				cmd.Printf("An error occurred during getting the log file, got:\n%v", err)
				return
			}

			if lines < 0 || lines > 10000 {
				cmd.Printf("An invalid line count was used, should be between 0 and 10000")
				return
			}
			// #nosec G204 - args are validated above
			tailLogCommand := exec.CommandContext(cmd.Context(), "tail", "-n", fmt.Sprintf("%d", lines), "-f", logPath)
			tailLogCommand.Stdout = cmd.OutOrStdout()
			tailLogCommand.Stderr = cmd.ErrOrStderr()
			// err = tailLogCommand.Run()
			if err := tailLogCommand.Start(); err != nil {
				cmd.PrintErrf("Failed to start log command: %v\n", err)
				return
			}

			if err := tailLogCommand.Wait(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					if exitErr.ExitCode() != 130 { // 130 = Ctrl+C
						cmd.PrintErrf("Log command failed: %v\n", err)
					}
				}
			}

		},
	}
	logsCmd.Flags().IntVar(&lines, "lines", 300, "Number of lines to display")

	daemonCmd.AddCommand(logsCmd)
	daemonCmd.AddCommand(startCmd)
	daemonCmd.AddCommand(statusCmd)
	daemonCmd.AddCommand(stopCmd)

	return daemonCmd
}

// Stay in sync with "startDaemonProcess"
func forkDaemon() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), exePath, "daemon", "start", "--log-to-file-and-console") // #nosec G204 -- exePath is from os.Executable(), not user input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	return nil
}
