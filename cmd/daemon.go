package cmd

import (
	"deploy-cli/internal/config"
	"deploy-cli/internal/manager"
	"deploy-cli/internal/process"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the deployment daemon",
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
			logToFileAndConsole := len(args) == 1 && args[0] == "logToFile"
			baseDir, err := config.GetBaseDir()
			if err := process.StartDaemon(logToFileAndConsole, baseDir); err != nil {
				cmd.PrintErrf("Failed to start daemon: %v\n", err)
			}
		},
	}
	startCmd.Flags().BoolP("detach", "d", false, "Run daemon in background")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("Stopping daemon...")
			if err := process.StopDaemon(); err != nil {
				cmd.PrintErrf("Failed to stop daemon: %v\n", err)
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

			status, err := process.StatusDaemon()
			if err != nil {
				cmd.PrintErrf("Failed to get status of daemon: %v\n", err)
				return
			}

			if !status.Running {
				cmd.Println("Daemon is found but not running")
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

			baseDir, err := config.GetBaseDir()
			logDir := manager.CreateLogDir(baseDir)
			if err != nil {
				cmd.Printf("An error occured during getting the log dir, got:\n%v", err)
				return
			}

			fileName := "daemon.log"
			logPath := filepath.Join(logDir, fileName)

			_, err = os.Stat(logPath)
			if err != nil {
				cmd.Printf("An error occured during getting the log file, got:\n%v", err)
				return
			}

			tailLogCommand := exec.Command("tail", "-n", fmt.Sprintf("%d", lines), "-f", logPath)
			tailLogCommand.Stdout = cmd.OutOrStdout()
			tailLogCommand.Stderr = cmd.ErrOrStderr()
			err = tailLogCommand.Run()

			if err != nil {
				cmd.Printf("The log command failed, got:\n%v", err)
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

func forkDaemon() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't find executable path: %w", err)
	}

	cmd := exec.Command(exePath, "daemon", "start", "logToFile")

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	return nil
}
