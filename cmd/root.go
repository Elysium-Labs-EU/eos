package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

	getConfig := func() *config.SystemConfig {
		_, _, config, _ := createSystemConfig()
		return config
	}

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRestartCmd(getManager, getConfig))
	rootCmd.AddCommand(newStartCmd(getManager))
	rootCmd.AddCommand(newStatusCmd(getManager))
	rootCmd.AddCommand(newStopCmd(getManager, getConfig))
	rootCmd.AddCommand(newUpdateCmd(getManager))

	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newSystemCmd())

	return rootCmd
}

func newRootCmd() *cobra.Command {
	var mgr manager.ServiceManager
	var cleanup func()
	var cfg *config.SystemConfig

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
			if config == nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "getting system configuration: nil pointer")
				os.Exit(1)
			}
			manager, possibleCleanup, err := getManager(cmd, baseDir, config.Daemon)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting manager: %v", err))
				os.Exit(1)
			}
			mgr = manager
			cleanup = possibleCleanup
			cfg = config
		},

		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if cleanup != nil {
				cleanup()
			}
		},
	}

	rootCmd.PersistentFlags().Bool("no-daemon", false, "run in local mode without daemon")

	getManager := func() manager.ServiceManager {
		return mgr
	}

	getConfig := func() *config.SystemConfig {
		return cfg
	}

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRestartCmd(getManager, getConfig))
	rootCmd.AddCommand(newStartCmd(getManager))
	rootCmd.AddCommand(newStatusCmd(getManager))
	rootCmd.AddCommand(newStopCmd(getManager, getConfig))
	rootCmd.AddCommand(newUpdateCmd(getManager))

	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newSystemCmd())

	return rootCmd
}

// TODO: Centralize "ENV VAR NAMES" somewhere
// TODO: Enable override for all exposed config variables
func createSystemConfig() (installDir string, baseDir string, systemConfig *config.SystemConfig, err error) {
	baseDir, err = config.CreateBaseDir()

	if err != nil {
		return "", "", nil, err
	}

	installDir = config.GetInstallDir()

	daemonConfig := config.DaemonConfig{
		PIDFile:       filepath.Clean(filepath.Join(baseDir, config.DaemonPIDFile)),
		SocketPath:    filepath.Clean(filepath.Join(baseDir, config.DaemonSocketPath)),
		SocketTimeout: safeParseDuration(config.DaemonSocketTimeout, time.Second*5),
		LogDir:        manager.CreateLogDirPath(baseDir),
		LogFileName:   config.DaemonLogFileName,
		MaxFiles:      config.DaemonLogMaxFiles,
		FileSizeLimit: overrideInt64ConfigValue("DAEMON_LOG_FILE_SIZE_LIMIT", config.DaemonLogFileSizeLimit),
	}

	healthConfig := config.HealthConfig{
		MaxRestart: config.HealthMaxRestart,
		Timeout: config.TimeOutConfig{
			Enable: overrideBoolConfigValue("HEALTH_TIMEOUT_ENABLE", config.HealthTimeOutEnable),
			Limit:  safeParseDuration(config.HealthTimeOutLimit, time.Second*10),
		},
	}

	shutdownConfig := config.ShutdownConfig{
		GracePeriod: safeParseDuration(overrideStringConfigValue("SHUTDOWN_GRACE_PERIOD", config.ShutdownGracePeriod), time.Second*5),
	}

	systemConfig = &config.SystemConfig{
		Health:   healthConfig,
		Daemon:   daemonConfig,
		Shutdown: shutdownConfig,
	}

	return installDir, baseDir, systemConfig, nil
}

func overrideStringConfigValue(envKey string, defaultValue string) string {
	if override := os.Getenv(envKey); override != "" {
		return override
	}
	return defaultValue
}

func overrideInt64ConfigValue(envKey string, defaultValue int64) int64 {
	if override := os.Getenv(envKey); override != "" {
		val, err := strconv.ParseInt(override, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %s", ui.LabelError.Render("error"), ui.TextBold.Render(envKey), fmt.Sprintf("overriding int64 config value: %v", err))
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

func overrideBoolConfigValue(envKey string, defaultValue bool) bool {
	if override := os.Getenv(envKey); override != "" {
		val, err := strconv.ParseBool(override)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %s", ui.LabelError.Render("error"), ui.TextBold.Render(envKey), fmt.Sprintf("overriding bool config value: %v", err))
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

func safeParseDuration(durationAsString string, fallback time.Duration) time.Duration {
	limit, err := time.ParseDuration(durationAsString)
	if err != nil {
		return fallback
	}
	return limit
}

func getManager(rootCmd *cobra.Command, baseDir string, daemonConfig config.DaemonConfig) (mgr manager.ServiceManager, cleanUp func(), err error) {
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

	mgr, err = manager.NewDaemonManager(ctx, daemonConfig.SocketPath, daemonConfig.PIDFile, daemonConfig.SocketTimeout)
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
