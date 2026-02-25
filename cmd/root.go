package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"eos/internal/buildinfo"
	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/manager"
	"eos/internal/ui"
)

func newTestRootCmd(mgr manager.ServiceManager) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "eos",
		Short: "A deployment orchestration CLI tool",
		Long: `eos is a modern deployment orchestration tool.
			It manages services, handles deployments, and provides monitoring
			capabilities for your VPS infrastructure.`,

		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("%s\n\n", ui.TextBold.Render("eos - Test version"))
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render("eos help"), ui.TextMuted.Render("→ see available commands"))
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

	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newSystemCmd())

	return rootCmd
}

func newRootCmd() *cobra.Command {
	var mgr manager.ServiceManager
	var cleanup func()

	skipManagerInit := func(cmd *cobra.Command) bool {
		if cmd.Parent() == nil {
			return true
		}
		for c := cmd; c != nil; c = c.Parent() {
			if c.Use == "daemon" {
				return true
			}
		}
		return false
	}

	rootCmd := &cobra.Command{
		Use:   "eos",
		Short: "A deployment orchestration CLI tool",
		Long: `eos is a modern deployment orchestration tool.
	It manages services, handles deployments, and provides monitoring
	capabilities for your VPS infrastructure.`,

		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("%s %s\n\n", ui.TextBold.Render("eos"), ui.TextMuted.Render(buildinfo.GetVersionOnly()))
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render("eos help"), ui.TextMuted.Render("→ see available commands"))
		},

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if skipManagerInit(cmd) {
				return
			}
			_, baseDir, config, err := createSystemConfig()
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				os.Exit(1)
			}
			manager, possibleCleanup, err := getManager(cmd, baseDir, config.Daemon)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting manager: %v", err))
				os.Exit(1)
			}
			mgr = manager
			cleanup = possibleCleanup
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
	rootCmd.AddCommand(newSystemCmd())

	return rootCmd
}

func createSystemConfig() (installDir string, baseDir string, systemConfig *config.SystemConfig, err error) {
	baseDir, err = config.CreateBaseDir()

	if err != nil {
		return "", "", nil, err
	}

	installDir = config.GetInstallDir()

	daemonConfig := config.DaemonConfig{
		PIDFile:       filepath.Clean(filepath.Join(baseDir, config.DaemonPIDFile)),
		SocketPath:    filepath.Clean(filepath.Join(baseDir, config.DaemonSocketPath)),
		LogDir:        manager.CreateLogDirPath(baseDir),
		LogFileName:   config.DaemonLogFileName,
		MaxFiles:      config.DaemonLogMaxFiles,
		FileSizeLimit: config.DaemonLogFileSizeLimit,
	}

	healthConfig := config.HealthConfig{
		MaxRestart: config.HealthMaxRestart,
		Timeout: config.TimeOutConfig{
			Enable: config.HealthTimeOutEnable,
			Limit:  safeParseHealthTimeoutLimit(),
		},
	}

	systemConfig = &config.SystemConfig{
		Health: healthConfig,
		Daemon: daemonConfig,
	}

	return installDir, baseDir, systemConfig, nil
}

func safeParseHealthTimeoutLimit() time.Duration {
	limit, err := time.ParseDuration(config.HealthTimeOutLimit)
	if err != nil {
		return time.Second * 10
	}

	return limit
}

func getManager(rootCmd *cobra.Command, baseDir string, daemonConfig config.DaemonConfig) (manager.ServiceManager, func(), error) {
	ctx := rootCmd.Context()
	noDaemon, err := rootCmd.Flags().GetBool("no-daemon")
	if err != nil {
		return nil, nil, err
	}

	if noDaemon {
		db, dbErr := database.NewDB(ctx, baseDir)
		if dbErr != nil {
			return nil, nil, fmt.Errorf("connecting to database: %w", dbErr)
		}

		mgr := manager.NewLocalManager(db, baseDir, ctx)
		cleanup := func() {
			err = db.CloseDBConnection()
			if err != nil {
				fmt.Printf("closing database connection on cleanup: %v\n", err)
				os.Exit(1)
			}
		}
		return mgr, cleanup, nil
	}

	mgr, err := manager.NewDaemonManager(ctx, daemonConfig.SocketPath, daemonConfig.PIDFile)
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
