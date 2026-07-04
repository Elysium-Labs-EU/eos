// Package cmd implements the eos CLI commands for registering, starting,
// stopping, and monitoring background services on a VPS.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/buildinfo"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/logutil"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
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
			cmd.Printf("%s\n\n", ui.TextBold.Render("eos - Test version"))
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render("eos help"), ui.TextMuted.Render("→ see available commands"))
		},
	}

	getManager := func() manager.ServiceManager {
		return mgr
	}

	getConfig := func() *config.SystemConfig {
		return &config.SystemConfig{
			Shutdown: config.ShutdownConfig{
				GracePeriod: 5 * time.Second,
			},
			Health: config.HealthConfig{
				MaxRestart:                10,
				RestartCounterResetWindow: 15 * time.Minute,
				Timeout:                   config.TimeOutConfig{Enable: true, Limit: 10 * time.Second},
			},
		}
	}

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRestartCmd(getManager, getConfig))
	rootCmd.AddCommand(newRunCmd(getManager, getConfig))
	rootCmd.AddCommand(newStartCmd(getManager))
	rootCmd.AddCommand(newStatusCmd(getManager))
	rootCmd.AddCommand(newStopCmd(getManager, getConfig))
	rootCmd.AddCommand(newUpdateCmd(getManager))
	rootCmd.AddCommand(newValidateCmd())

	testDaemonConfig := func() (string, *config.SystemConfig, error) {
		testBaseDir := os.TempDir()
		return testBaseDir, &config.SystemConfig{
			Daemon: config.DaemonConfig{
				Standalone: &config.StandaloneDaemonConfig{
					PIDFile:    filepath.Join(testBaseDir, "eos-test.pid"),
					SocketPath: filepath.Join(testBaseDir, "eos-test.sock"),
					Log:        config.DaemonLogConfig{LogFileName: "daemon.log"},
				},
				Systemd: nil,
			},
			Health: config.HealthConfig{
				MaxRestart:                10,
				RestartCounterResetWindow: 15 * time.Minute,
				Timeout:                   config.TimeOutConfig{Enable: true, Limit: 10 * time.Second},
			},
		}, nil
	}
	rootCmd.AddCommand(newDaemonCmd(testDaemonConfig))
	rootCmd.AddCommand(newSystemCmd(getManager, getConfig))
	rootCmd.AddCommand(newAPICmd(getManager, getConfig, testDaemonConfig))

	rootCmd.InitDefaultCompletionCmd()

	return rootCmd
}

func newRootCmd() *cobra.Command {
	// rootCmd declared before assignment so lazyInit can capture the variable.
	// By the time any closure executes (at RunE time), rootCmd is non-nil.
	var rootCmd *cobra.Command

	var once sync.Once
	var mgr manager.ServiceManager
	var cleanup func()
	var cfg *config.SystemConfig

	lazyInit := func() {
		once.Do(func() {
			_, baseDir, c, err := newSystemConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				os.Exit(1)
			}
			m, cl, err := newManager(rootCmd, baseDir, c.Daemon)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting manager: %v", err))
				os.Exit(1)
			}
			mgr = m
			cleanup = cl
			cfg = c
		})
	}

	getManager := func() manager.ServiceManager {
		lazyInit()
		return mgr
	}

	getConfig := func() *config.SystemConfig {
		lazyInit()
		return cfg
	}

	rootCmd = &cobra.Command{
		Use:   "eos",
		Short: "A deployment orchestration CLI tool",
		Long: `eos is a modern deployment orchestration tool.
	It manages services, handles deployments, and provides monitoring
	capabilities for your VPS infrastructure.`,

		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("%s %s\n\n", ui.TextBold.Render("eos"), ui.TextMuted.Render(buildinfo.GetVersionOnly()))
			cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render("eos help"), ui.TextMuted.Render("→ see available commands"))
		},

		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if cleanup != nil {
				cleanup()
			}
		},
	}

	rootCmd.PersistentFlags().Bool("no-daemon", false, "run in local mode without daemon")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable verbose debug logging")

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRestartCmd(getManager, getConfig))
	rootCmd.AddCommand(newRunCmd(getManager, getConfig))
	rootCmd.AddCommand(newStartCmd(getManager))
	rootCmd.AddCommand(newStatusCmd(getManager))
	rootCmd.AddCommand(newStopCmd(getManager, getConfig))
	rootCmd.AddCommand(newUpdateCmd(getManager))
	rootCmd.AddCommand(newValidateCmd())

	getDaemonConfig := func() (string, *config.SystemConfig, error) {
		_, baseDir, c, err := newSystemConfig()
		return baseDir, c, err
	}
	rootCmd.AddCommand(newDaemonCmd(getDaemonConfig))
	rootCmd.AddCommand(newSystemCmd(getManager, getConfig))

	rootCmd.InitDefaultCompletionCmd()

	rootCmd.AddCommand(newAPICmd(getManager, getConfig, getDaemonConfig))

	return rootCmd
}

func newDaemonConfig(baseDir string, isSystemdManaged bool, underSystemd bool, systemdDir string, logCfg config.EosLogConfig) config.DaemonConfig {
	// When invoked BY systemd (INVOCATION_ID set) we ARE the daemon process —
	// run standalone in the foreground. Only delegate to systemctl when a human
	// calls "eos daemon start" from outside systemd.
	if isSystemdManaged && !underSystemd {
		return config.DaemonConfig{
			Standalone: nil,
			Systemd: &config.SystemdConfig{
				SystemdTargetDir:      systemdDir,
				SystemdTargetFileName: config.SystemdTargetFileName,
			},
		}
	}

	return config.DaemonConfig{
		Standalone: &config.StandaloneDaemonConfig{
			PIDFile:       filepath.Clean(filepath.Join(baseDir, config.DaemonPIDFile)),
			SocketPath:    filepath.Clean(filepath.Join(baseDir, config.DaemonSocketPath)),
			SocketTimeout: safeParseDuration(config.DaemonSocketTimeout, time.Second*5),
			Log: config.DaemonLogConfig{
				LogDir:           manager.CreateLogDirPath(baseDir),
				LogFileName:      config.DaemonLogFileName,
				LogMaxFiles:      logCfg.MaxFiles,
				LogFileSizeLimit: logCfg.FileSizeLimitBytes,
			}},
		Systemd: nil,
	}
}

func newSystemConfig() (installDir string, baseDir string, systemConfig *config.SystemConfig, err error) {
	baseDir, err = config.CreateBaseDir()
	if err != nil {
		return "", "", nil, err
	}

	installDir = config.GetInstallDir()

	eosCfg, err := config.LoadEosConfig(baseDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("loading eos config: %w", err)
	}

	// Env vars win over config.yaml values.
	logCfg := config.EosLogConfig{
		MaxFiles:           overrideIntConfigValue("DAEMON_LOG_MAX_FILES", eosCfg.Log.MaxFiles),
		FileSizeLimitBytes: overrideInt64ConfigValue("DAEMON_LOG_FILE_SIZE_LIMIT", eosCfg.Log.FileSizeLimitBytes),
	}

	systemdDir := overrideStringConfigValue("EOS_SYSTEMD_TARGET_DIR", config.SystemdTargetDir)
	if os.Getuid() != 0 {
		userDir, userDirErr := config.UserSystemdDir()
		if userDirErr != nil {
			return "", "", nil, fmt.Errorf("resolving user systemd dir: %w", userDirErr)
		}
		systemdDir = userDir
	}

	isSystemdManaged, err := config.IsSystemdManaged(systemdDir, config.SystemdTargetFileName)
	if err != nil {
		return "", "", nil, fmt.Errorf("checking systemd managed state: %w", err)
	}
	daemonConfig := newDaemonConfig(baseDir, isSystemdManaged, config.IsUnderSystemd(), systemdDir, logCfg)

	restartCounterResetWindow := safeParseDuration(overrideStringConfigValue("HEALTH_RESTART_COUNTER_RESET_WINDOW", config.HealthRestartCounterResetWindow), 15*time.Minute)
	if restartCounterResetWindow <= 0 {
		restartCounterResetWindow = 15 * time.Minute
	}

	healthConfig := config.HealthConfig{
		MaxRestart:                config.HealthMaxRestart,
		RestartCounterResetWindow: restartCounterResetWindow,
		Timeout: config.TimeOutConfig{
			Enable: overrideBoolConfigValue("HEALTH_TIMEOUT_ENABLE", config.HealthTimeOutEnable),
			Limit:  safeParseDuration(config.HealthTimeOutLimit, time.Second*10),
		},
		CheckInterval:     time.Duration(overrideIntConfigValue("HEALTH_CHECK_INTERVAL_MS", eosCfg.Health.CheckIntervalMs)) * time.Millisecond,
		MemSampleInterval: time.Duration(overrideIntConfigValue("HEALTH_MEM_SAMPLE_INTERVAL_MS", eosCfg.Health.MemSampleIntervalMs)) * time.Millisecond,
		Backoff: config.BackoffConfig{
			BaseMs: overrideIntConfigValue("HEALTH_BACKOFF_BASE_MS", eosCfg.Health.Backoff.BaseMs),
			MaxMs:  overrideIntConfigValue("HEALTH_BACKOFF_MAX_MS", eosCfg.Health.Backoff.MaxMs),
		},
		Memory: config.MemoryThresholdConfig{
			WarningThreshold:      overrideFloat64ConfigValue("HEALTH_MEMORY_WARNING_THRESHOLD", eosCfg.Health.Memory.WarningThreshold),
			SoftRestartThreshold:  overrideFloat64ConfigValue("HEALTH_MEMORY_SOFT_RESTART_THRESHOLD", eosCfg.Health.Memory.SoftRestartThreshold),
			ForceRestartThreshold: overrideFloat64ConfigValue("HEALTH_MEMORY_FORCE_RESTART_THRESHOLD", eosCfg.Health.Memory.ForceRestartThreshold),
		},
	}

	shutdownConfig := config.ShutdownConfig{
		GracePeriod: safeParseDuration(overrideStringConfigValue("SHUTDOWN_GRACE_PERIOD", config.ShutdownGracePeriod), time.Second*5),
	}

	systemConfig = &config.SystemConfig{
		Health:       healthConfig,
		Daemon:       daemonConfig,
		Shutdown:     shutdownConfig,
		UnderSystemd: config.IsUnderSystemd(),
		Verbose:      overrideBoolConfigValue("EOS_VERBOSE", false),
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
			fmt.Fprintf(os.Stderr, "%s %s: %s\n", ui.LabelError.Render("error"), ui.TextBold.Render(envKey), fmt.Sprintf("overriding int64 config value: %v", err))
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
			fmt.Fprintf(os.Stderr, "%s %s: %s\n", ui.LabelError.Render("error"), ui.TextBold.Render(envKey), fmt.Sprintf("overriding bool config value: %v", err))
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

func overrideIntConfigValue(envKey string, defaultValue int) int {
	if override := os.Getenv(envKey); override != "" {
		val, err := strconv.Atoi(override)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %s\n", ui.LabelError.Render("error"), ui.TextBold.Render(envKey), fmt.Sprintf("overriding int config value: %v", err))
			os.Exit(1)
		}
		return val
	}
	return defaultValue
}

func overrideFloat64ConfigValue(envKey string, defaultValue float64) float64 {
	if override := os.Getenv(envKey); override != "" {
		val, err := strconv.ParseFloat(override, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %s\n", ui.LabelError.Render("error"), ui.TextBold.Render(envKey), fmt.Sprintf("overriding float64 config value: %v", err))
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

func newManager(rootCmd *cobra.Command, baseDir string, daemonConfig config.DaemonConfig) (mgr manager.ServiceManager, cleanUp func(), err error) {
	ctx := rootCmd.Context()
	noDaemon, err := rootCmd.Flags().GetBool("no-daemon")
	if err != nil {
		return nil, nil, err
	}
	verbose, _ := rootCmd.Flags().GetBool("verbose")

	if noDaemon {
		db, dbErr := database.NewDB(ctx, baseDir)
		if dbErr != nil {
			return nil, nil, fmt.Errorf("connecting to database: %w", dbErr)
		}

		mgr := manager.NewLocalManager(db, baseDir, ctx, logutil.NewTextLogger(os.Stderr, verbose))
		cleanup := func() {
			err = db.CloseDBConnection()
			if err != nil {
				fmt.Printf("closing database connection on cleanup: %v\n", err)
				os.Exit(1)
			}
		}
		return mgr, cleanup, nil
	}

	if daemonConfig.Standalone == nil {
		db, dbErr := database.NewDB(ctx, baseDir)
		if dbErr != nil {
			return nil, nil, fmt.Errorf("connecting to database: %w", dbErr)
		}
		mgr := manager.NewLocalManager(db, baseDir, ctx, logutil.NewTextLogger(os.Stderr, verbose))
		cleanup := func() {
			err = db.CloseDBConnection()
			if err != nil {
				fmt.Printf("closing database connection on cleanup: %v\n", err)
				os.Exit(1)
			}
		}
		return mgr, cleanup, nil
	}

	mgr, err = manager.NewDaemonManager(ctx, daemonConfig.Standalone.SocketPath, daemonConfig.Standalone.PIDFile, daemonConfig.Standalone.SocketTimeout, verbose)
	if err != nil {
		return nil, nil, err
	}
	return mgr, nil, nil
}

// Execute is the entry point for the eos CLI.
// It builds the root command tree and exits with code 1 on error.
func Execute() {
	rootCmd := newRootCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
