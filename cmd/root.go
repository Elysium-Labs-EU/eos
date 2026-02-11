package cmd

import (
	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newTestRootCmd(mgr manager.ServiceManager) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "eos",
		Short: "A deployment orchestration CLI tool",
		Long: `eos is a modern deployment orchestration tool.
			It manages services, handles deployments, and provides monitoring
			capabilities for your VPS infrastructure.`,

		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("eos - Test version")
			cmd.Println("Use 'eos help' to see available commands")
		},
	}

	getManager := func() manager.ServiceManager {
		return mgr
	}

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRestartCmd(getManager))
	rootCmd.AddCommand(newStartCmd(getManager))
	rootCmd.AddCommand(newStatusCmd(getManager))
	rootCmd.AddCommand(newStopCmd(getManager))
	rootCmd.AddCommand(newUpdateCmd(getManager))

	return rootCmd
}

func newRootCmd() *cobra.Command {
	var mgr manager.ServiceManager
	var cleanup func()

	rootCmd := &cobra.Command{
		Use:   "eos",
		Short: "A deployment orchestration CLI tool",
		Long: `eos is a modern deployment orchestration tool.
	It manages services, handles deployments, and provides monitoring
	capabilities for your VPS infrastructure.`,

		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("eos v0.0.1")
			cmd.Println("Use 'eos help' to see available commands")
		},

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if cmd.Parent().Use != "daemon" {
				manager, possibleCleanup, err := getManager(cmd)
				if err != nil {
					fmt.Printf("Error getting manager: %v\n", err)
					os.Exit(1)
				}
				mgr = manager
				cleanup = possibleCleanup
			}
		},

		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if cleanup != nil {
				cleanup()
			}
		},
	}

	rootCmd.PersistentFlags().Bool("no-daemon", false, "Run in local mode without daemon")

	getManager := func() manager.ServiceManager {
		return mgr
	}

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRestartCmd(getManager))
	rootCmd.AddCommand(newStartCmd(getManager))
	rootCmd.AddCommand(newStatusCmd(getManager))
	rootCmd.AddCommand(newStopCmd(getManager))
	rootCmd.AddCommand(newUpdateCmd(getManager))

	rootCmd.AddCommand(newDaemonCmd())

	return rootCmd
}

func getManager(rootCmd *cobra.Command) (manager.ServiceManager, func(), error) {
	noDaemon, err := rootCmd.Flags().GetBool("no-daemon")
	if err != nil {
		return nil, nil, err
	}

	if noDaemon {
		baseDir, err := config.GetBaseDir()
		if err != nil {
			return nil, nil, fmt.Errorf("could not get user home directory for manager: %w", err)
		}

		db, err := database.NewDB()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
		}

		mgr := manager.NewLocalManager(db, baseDir)
		cleanup := func() {
			err = db.CloseDBConnection()
			if err != nil {
				fmt.Printf("Error closing database connection on cleanup: %v\n", err)
				os.Exit(1)
			}
		}
		return mgr, cleanup, nil
	}

	mgr, err := manager.NewDaemonManager()
	if err != nil {
		return nil, nil, err
	}
	return mgr, nil, nil
}

func Execute() {
	rootCmd := newRootCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
