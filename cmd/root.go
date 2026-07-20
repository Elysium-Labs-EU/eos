// Package cmd implements the eos CLI commands for registering, starting,
// stopping, and monitoring background services on a VPS.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/logutil"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"github.com/spf13/cobra"
)

func newTestRootCmd(mgr manager.ServiceManager) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "eos",
		Short: "A service supervisor CLI tool",
		Long: `eos - Test version

eos is a service supervisor.
			It manages services, handles deployments, and provides monitoring
			capabilities for your VPS infrastructure.`,
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

	// Tests run against an in-memory mock manager, not a real daemon, so the
	// liveness probe is a no-op here — otherwise every status/logs test would
	// probe the host's real daemon and print a spurious banner.
	noopWarnDaemonDown := func(*cobra.Command) {}

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newEnvCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager, noopWarnDaemonDown))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRunCmd(getManager, getConfig))
	rootCmd.AddCommand(newStatusCmd(getManager, noopWarnDaemonDown, getConfig))
	rootCmd.AddCommand(newStopCmd(getManager, getConfig))
	rootCmd.AddCommand(newUpdateCmd(getManager))
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newInitCmd())

	testDaemonConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		testBaseDir := os.TempDir()
		identity, err := userutil.ResolveIdentity()
		if err != nil {
			return "", nil, userutil.Identity{}, err
		}
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
		}, identity, nil
	}
	rootCmd.AddCommand(newDaemonCmd(testDaemonConfig))
	rootCmd.AddCommand(newSystemCmd(getManager, getConfig))
	rootCmd.AddCommand(newAPICmd(getManager, getConfig, testDaemonConfig))
	rootCmd.AddCommand(newCompletionCmd(rootCmd))

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
			_, baseDir, c, _, err := newSystemConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
				os.Exit(1)
			}
			m, cl, err := newManager(rootCmd, baseDir, c.Daemon, c.Sinks)
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
		Short: "A service supervisor CLI tool",
		Long: fmt.Sprintf(`eos %s

eos is a service supervisor.
	It manages services, handles deployments, and provides monitoring
	capabilities for your VPS infrastructure.`, buildinfo.GetVersionOnly()),
		Version: buildinfo.Get(),

		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if cleanup != nil {
				cleanup()
			}
		},
	}
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	rootCmd.PersistentFlags().Bool("no-daemon", false, "run in local mode without daemon")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable verbose debug logging")

	rootCmd.AddCommand(newAddCmd(getManager))
	rootCmd.AddCommand(newInfoCmd(getManager))
	rootCmd.AddCommand(newEnvCmd(getManager))
	rootCmd.AddCommand(newLogsCmd(getManager, warnIfDaemonDown))
	rootCmd.AddCommand(newRemoveCmd(getManager))
	rootCmd.AddCommand(newRunCmd(getManager, getConfig))
	rootCmd.AddCommand(newStatusCmd(getManager, warnIfDaemonDown, getConfig))
	rootCmd.AddCommand(newStopCmd(getManager, getConfig))
	rootCmd.AddCommand(newUpdateCmd(getManager))
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newInitCmd())

	getDaemonConfig := func() (string, *config.SystemConfig, userutil.Identity, error) {
		_, baseDir, c, identity, err := newSystemConfig()
		return baseDir, c, identity, err
	}
	rootCmd.AddCommand(newDaemonCmd(getDaemonConfig))
	rootCmd.AddCommand(newSystemCmd(getManager, getConfig))

	rootCmd.AddCommand(newCompletionCmd(rootCmd))

	rootCmd.AddCommand(newAPICmd(getManager, getConfig, getDaemonConfig))

	return rootCmd
}

func newStandaloneDaemonConfig(baseDir string, logCfg config.EosLogConfig) *config.StandaloneDaemonConfig {
	return &config.StandaloneDaemonConfig{
		PIDFile:       filepath.Clean(filepath.Join(baseDir, config.DaemonPIDFile)),
		SocketPath:    filepath.Clean(filepath.Join(baseDir, config.DaemonSocketPath)),
		SocketTimeout: safeParseDuration(config.DaemonSocketTimeout, time.Second*5),
		Log: config.DaemonLogConfig{
			LogDir:           manager.CreateLogDirPath(baseDir),
			LogFileName:      config.DaemonLogFileName,
			LogMaxFiles:      logCfg.MaxFiles,
			LogFileSizeLimit: logCfg.FileSizeLimitBytes,
		},
	}
}

func newDaemonConfig(baseDir string, isSystemdManaged bool, underSystemd bool, systemdDir string, userUnit bool, logCfg config.EosLogConfig) config.DaemonConfig {
	// When invoked BY systemd (INVOCATION_ID set) we ARE the daemon process —
	// run standalone in the foreground. Only delegate to systemctl when a human
	// calls "eos daemon start" from outside systemd.
	if isSystemdManaged && !underSystemd {
		return config.DaemonConfig{
			Standalone: nil,
			Systemd: &config.SystemdConfig{
				SystemdTargetDir:      systemdDir,
				SystemdTargetFileName: config.SystemdTargetFileName,
				SocketPath:            filepath.Clean(filepath.Join(baseDir, config.DaemonSocketPath)),
				UserUnit:              userUnit,
			},
		}
	}

	return config.DaemonConfig{
		Standalone: newStandaloneDaemonConfig(baseDir, logCfg),
		Systemd:    nil,
	}
}

func newDaemonConfigLaunchd(baseDir string, isLaunchdManaged bool, underLaunchd bool, launchdDir string, userAgent bool, logCfg config.EosLogConfig) config.DaemonConfig {
	// When invoked BY launchd (XPC_SERVICE_NAME set) we ARE the daemon process —
	// run standalone in the foreground. Only delegate to launchctl when a human
	// calls "eos daemon start" from outside launchd.
	if isLaunchdManaged && !underLaunchd {
		return config.DaemonConfig{
			Standalone: nil,
			Launchd: &config.LaunchdConfig{
				LaunchdTargetDir:     launchdDir,
				LaunchdPlistFileName: config.LaunchdPlistFileName,
				UserAgent:            userAgent,
			},
		}
	}

	return config.DaemonConfig{
		Standalone: newStandaloneDaemonConfig(baseDir, logCfg),
		Launchd:    nil,
	}
}

// newDaemonConfigOpenRC is the OpenRC analog of newDaemonConfig. When invoked BY
// OpenRC's supervise-daemon (underOpenRC) we ARE the daemon process, so run
// standalone in the foreground; only delegate to rc-service when a human calls
// "eos daemon start/stop" from outside the supervisor.
func newDaemonConfigOpenRC(baseDir string, isOpenRCManaged bool, underOpenRC bool, initDir string, logCfg config.EosLogConfig) config.DaemonConfig {
	if isOpenRCManaged && !underOpenRC {
		return config.DaemonConfig{
			OpenRC: &config.OpenRCConfig{
				InitDir:      initDir,
				InitFileName: config.OpenRCTargetFileName,
			},
		}
	}

	return config.DaemonConfig{
		Standalone: newStandaloneDaemonConfig(baseDir, logCfg),
	}
}

// resolveLinuxDaemonConfig picks the daemon supervisor on Linux. systemd wins
// when its unit is installed; otherwise, if an OpenRC init script is installed,
// delegate to OpenRC; otherwise fall back to standalone. This mirrors
// detectActiveSystemRuntime's systemd-before-OpenRC preference.
func resolveLinuxDaemonConfig(baseDir string, logCfg config.EosLogConfig) (config.DaemonConfig, error) {
	systemdDir, isSystemdManaged, userUnit, systemdErr := config.ResolveSystemdScope(overrideStringConfigValue("EOS_SYSTEMD_TARGET_DIR", config.SystemdTargetDir))
	if systemdErr != nil {
		return config.DaemonConfig{}, fmt.Errorf("resolving systemd scope: %w", systemdErr)
	}
	if isSystemdManaged {
		return newDaemonConfig(baseDir, true, config.IsUnderSystemd(), systemdDir, userUnit, logCfg), nil
	}

	initDir := overrideStringConfigValue("EOS_OPENRC_INIT_DIR", config.OpenRCInitDir)
	isOpenRCManaged, openrcErr := config.IsOpenRCManaged(initDir, config.OpenRCTargetFileName)
	if openrcErr != nil {
		return config.DaemonConfig{}, fmt.Errorf("resolving OpenRC scope: %w", openrcErr)
	}
	if isOpenRCManaged {
		return newDaemonConfigOpenRC(baseDir, true, config.IsUnderOpenRC(), initDir, logCfg), nil
	}

	return newDaemonConfig(baseDir, false, config.IsUnderSystemd(), systemdDir, userUnit, logCfg), nil
}

func newSystemConfig() (installDir string, baseDir string, systemConfig *config.SystemConfig, identity userutil.Identity, err error) {
	identity, err = userutil.ResolveIdentity()
	if err != nil {
		return "", "", nil, userutil.Identity{}, fmt.Errorf("resolving identity: %w", err)
	}

	baseDir, err = config.CreateBaseDir(identity)
	if err != nil {
		return "", "", nil, userutil.Identity{}, err
	}

	installDir = config.GetInstallDir()

	eosCfg, err := config.LoadEosConfig(baseDir)
	if err != nil {
		return "", "", nil, userutil.Identity{}, fmt.Errorf("loading eos config: %w", err)
	}

	// Env vars win over config.yaml values.
	logCfg := config.EosLogConfig{
		MaxFiles:           overrideIntConfigValue("DAEMON_LOG_MAX_FILES", eosCfg.Log.MaxFiles),
		FileSizeLimitBytes: overrideInt64ConfigValue("DAEMON_LOG_FILE_SIZE_LIMIT", eosCfg.Log.FileSizeLimitBytes),
	}

	var daemonConfig config.DaemonConfig
	if runtime.GOOS == "darwin" {
		launchdDir, isLaunchdManaged, userAgent, launchdErr := config.ResolveLaunchdScope(overrideStringConfigValue("EOS_LAUNCHD_TARGET_DIR", config.LaunchdTargetDir))
		if launchdErr != nil {
			return "", "", nil, userutil.Identity{}, fmt.Errorf("resolving launchd scope: %w", launchdErr)
		}
		daemonConfig = newDaemonConfigLaunchd(baseDir, isLaunchdManaged, config.IsUnderLaunchd(), launchdDir, userAgent, logCfg)
	} else {
		daemonConfig, err = resolveLinuxDaemonConfig(baseDir, logCfg)
		if err != nil {
			return "", "", nil, userutil.Identity{}, err
		}
	}

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

	telemetryConfig := config.TelemetryConfig{
		Enable:   overrideBoolConfigValue("EOS_OTEL_ENABLE", eosCfg.Telemetry.Enable),
		Endpoint: overrideStringConfigValue("EOS_OTEL_ENDPOINT", eosCfg.Telemetry.Endpoint),
		Insecure: overrideBoolConfigValue("EOS_OTEL_INSECURE", eosCfg.Telemetry.Insecure),
	}
	if telemetryConfig.Enable && telemetryConfig.Endpoint == "" {
		return "", "", nil, userutil.Identity{}, fmt.Errorf("telemetry is enabled but no OTLP endpoint is configured (set telemetry.endpoint in config.yaml or EOS_OTEL_ENDPOINT)")
	}

	systemConfig = &config.SystemConfig{
		Sinks:        eosCfg.Sinks,
		Health:       healthConfig,
		Daemon:       daemonConfig,
		Shutdown:     shutdownConfig,
		Telemetry:    telemetryConfig,
		UnderSystemd: config.IsUnderSystemd(),
		Verbose:      overrideBoolConfigValue("EOS_VERBOSE", false),
	}

	return installDir, baseDir, systemConfig, identity, nil
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

// newLocalManagerWithCleanup opens the database directly and returns an
// in-process manager plus a cleanup that closes the connection. Used for both
// --no-daemon and the "no standalone daemon configured" cases.
func newLocalManagerWithCleanup(ctx context.Context, baseDir string, verbose bool, sinkRegistry map[string]types.LogSink) (manager.ServiceManager, func(), error) {
	db, err := database.NewDB(ctx, baseDir)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to database: %w", err)
	}
	mgr := manager.NewLocalManager(db, baseDir, ctx, logutil.NewTextLogger(os.Stderr, verbose), manager.WithSinkRegistry(sinkRegistry))
	cleanup := func() {
		if closeErr := db.CloseDBConnection(); closeErr != nil {
			fmt.Printf("closing database connection on cleanup: %v\n", closeErr)
			os.Exit(1)
		}
	}
	return mgr, cleanup, nil
}

func newManager(rootCmd *cobra.Command, baseDir string, daemonConfig config.DaemonConfig, sinkRegistry map[string]types.LogSink) (mgr manager.ServiceManager, cleanUp func(), err error) {
	ctx := rootCmd.Context()
	noDaemon, err := rootCmd.Flags().GetBool("no-daemon")
	if err != nil {
		return nil, nil, err
	}
	verbose, _ := rootCmd.Flags().GetBool("verbose")

	if noDaemon || daemonConfig.Standalone == nil {
		return newLocalManagerWithCleanup(ctx, baseDir, verbose, sinkRegistry)
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
		// Commands that already printed a human-readable error return
		// helpers.ErrCommandFailed/ErrAPICommandFailed purely to signal a
		// non-zero exit; printing err here would just repeat "command failed".
		if !errors.Is(err, helpers.ErrCommandFailed) && !errors.Is(err, helpers.ErrAPICommandFailed) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
