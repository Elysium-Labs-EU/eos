// Package config loads and validates eos service configuration from YAML files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/userutil"
	"gopkg.in/yaml.v3"
)

// NOTE: In the nearby future we want to enable this to be overwritten by the release process.
// var installDir string

const (
	DaemonLogFileName                 = "daemon.log"
	DaemonLogFileSizeLimit            = int64(10 * 1024 * 1024)
	DaemonLogMaxFiles                 = 5
	DaemonPIDFile                     = "eos.pid"
	DaemonSocketPath                  = "eos.sock"
	DaemonSocketTimeout               = "5s"
	EosConfigFileName                 = "config.yaml"
	HealthBackoffBaseMs               = 300
	HealthBackoffMaxMs                = 60000
	HealthCheckIntervalMs             = 2000
	HealthMemSampleIntervalMs         = 30000
	HealthMaxRestart                  = 10
	HealthMemoryForceRestartThreshold = 0.95
	HealthMemorySoftRestartThreshold  = 0.85
	HealthMemoryWarningThreshold      = 0.75
	HealthRestartCounterResetWindow   = "15m"
	HealthTimeOutEnable               = true
	HealthTimeOutLimit                = "10s"
	InstallDir                        = "/usr/local/bin"
	LaunchdLabel                      = "org.elysiumlabs.eos"
	LaunchdPlistFileName              = LaunchdLabel + ".plist"
	LaunchdTargetDir                  = "/Library/LaunchDaemons/"
	Name                              = "eos"
	OpenRCInitDir                     = "/etc/init.d/"
	OpenRCTargetFileName              = "eos"
	ShutdownGracePeriod               = "5s"
	SystemdTargetDir                  = "/etc/systemd/system/"
	SystemdTargetFileName             = "eos.service"
)

type DaemonConfig struct {
	Standalone *StandaloneDaemonConfig `json:"standalone" yaml:"standalone"`
	Systemd    *SystemdConfig          `json:"systemd" yaml:"systemd"`
	Launchd    *LaunchdConfig          `json:"launchd" yaml:"launchd"`
}

type StandaloneDaemonConfig struct {
	PIDFile       string          `json:"pid_file" yaml:"pidFile"`
	SocketPath    string          `json:"socket_path" yaml:"socketPath"`
	Log           DaemonLogConfig `json:"log" yaml:"log"`
	SocketTimeout time.Duration   `json:"socket_timeout" yaml:"socketTimeout"`
}

type DaemonLogConfig struct {
	LogDir           string `json:"log_dir" yaml:"logDir"`
	LogFileName      string `json:"log_file_name" yaml:"logFileName"`
	LogMaxFiles      int    `json:"log_max_files" yaml:"logMaxFiles"`
	LogFileSizeLimit int64  `json:"log_file_size_limit" yaml:"logFileSizeLimit"`
}

type SystemdConfig struct {
	SystemdTargetDir      string `json:"systemd_target_dir" yaml:"systemdTargetDir"`
	SystemdTargetFileName string `json:"systemd_target_file_name" yaml:"systemdTargetFileName"`
	// SocketPath is the Unix socket the systemd-managed daemon listens on for
	// THIS base dir (baseDir/eos.sock). A systemd unit supervises exactly one
	// base dir, so probing this socket is the only base-dir-scoped liveness
	// signal; `systemctl is-active eos` is host-global and says nothing about
	// which EOS_BASE_DIR the active unit serves (issue #12).
	SocketPath string `json:"socket_path" yaml:"socketPath"`
	UserUnit   bool   `json:"user_unit" yaml:"userUnit"`
}

// LaunchdConfig mirrors SystemdConfig for macOS: UserAgent plays the same role as
// SystemdConfig.UserUnit, distinguishing a per-user LaunchAgent (~/Library/LaunchAgents,
// domain gui/<uid>) from a system-wide LaunchDaemon (/Library/LaunchDaemons, domain system).
type LaunchdConfig struct {
	LaunchdTargetDir     string `json:"launchd_target_dir" yaml:"launchdTargetDir"`
	LaunchdPlistFileName string `json:"launchd_plist_file_name" yaml:"launchdPlistFileName"`
	UserAgent            bool   `json:"user_agent" yaml:"userAgent"`
}

type TimeOutConfig struct {
	Enable bool          `json:"enable" yaml:"enable"`
	Limit  time.Duration `json:"limit" yaml:"limit"`
}

type BackoffConfig struct {
	BaseMs int `json:"base_ms" yaml:"baseMs"`
	MaxMs  int `json:"max_ms" yaml:"maxMs"`
}

type MemoryThresholdConfig struct {
	WarningThreshold      float64 `json:"warning_threshold" yaml:"warningThreshold"`
	SoftRestartThreshold  float64 `json:"soft_restart_threshold" yaml:"softRestartThreshold"`
	ForceRestartThreshold float64 `json:"force_restart_threshold" yaml:"forceRestartThreshold"`
}

type HealthConfig struct {
	CheckInterval             time.Duration         `json:"check_interval" yaml:"checkInterval"`
	MemSampleInterval         time.Duration         `json:"mem_sample_interval" yaml:"memSampleInterval"`
	MaxRestart                int                   `json:"max_restart" yaml:"maxRestart"`
	RestartCounterResetWindow time.Duration         `json:"restart_counter_reset_window" yaml:"restartCounterResetWindow"`
	Timeout                   TimeOutConfig         `json:"timeout" yaml:"timeout"`
	Backoff                   BackoffConfig         `json:"backoff" yaml:"backoff"`
	Memory                    MemoryThresholdConfig `json:"memory" yaml:"memory"`
}

type ShutdownConfig struct {
	GracePeriod time.Duration `json:"grace_period" yaml:"gracePeriod"`
}

// TelemetryConfig controls the daemon's OTLP export of metrics and traces.
// Disabled by default: with Enable false, the daemon never dials a collector
// and uses no-op tracer/meter providers (see otelx.NewProvider).
type TelemetryConfig struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Enable   bool   `json:"enable" yaml:"enable"`
	Insecure bool   `json:"insecure" yaml:"insecure"`
}

type SystemConfig struct {
	Daemon       DaemonConfig             `json:"daemon" yaml:"daemon"`
	Sinks        map[string]types.LogSink `json:"sinks" yaml:"sinks"`
	Telemetry    TelemetryConfig          `json:"telemetry" yaml:"telemetry"`
	Health       HealthConfig             `json:"health" yaml:"health"`
	Shutdown     ShutdownConfig           `json:"shutdown" yaml:"shutdown"`
	UnderSystemd bool                     `json:"under_systemd" yaml:"underSystemd"`
	Verbose      bool                     `json:"verbose" yaml:"verbose"`
}

func UserSystemdDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "systemd", "user") + "/", nil
}

// GetBaseDir takes an already-resolved Identity rather than deriving one itself —
// see userutil.ResolveIdentity, the single authority for the sudo/root distinction
// that broke this function before: `sudo -u <non-root-user>` also sets SUDO_USER even
// though the process isn't running as root, and honoring it there would
// redirect data to the invoking user's home instead of the target user's.
func GetBaseDir(id userutil.Identity) (string, error) {
	if override := os.Getenv("EOS_BASE_DIR"); override != "" {
		return override, nil
	}
	return filepath.Join(id.HomeDir(), fmt.Sprintf(".%s", Name)), nil
}

// CreateBaseDir takes an already-resolved Identity rather than deriving one itself;
// see GetBaseDir.
func CreateBaseDir(id userutil.Identity) (string, error) {
	if os.Getuid() == 0 && os.Getenv("SUDO_USER") == "" && os.Getenv("EOS_BASE_DIR") == "" {
		return "", fmt.Errorf("do not run eos as root: invoke as the target user directly")
	}

	baseDir, err := GetBaseDir(id)
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(baseDir, 0750)
	if err != nil {
		return "", fmt.Errorf("could not create eos directory: %w", err)
	}

	if os.Getuid() == 0 {
		if err := os.Chown(baseDir, int(id.UID()), int(id.GID())); err != nil {
			return "", fmt.Errorf("chown %s to %s: %w", baseDir, id.Username(), err)
		}
	}

	return baseDir, nil
}

func GetInstallDir() string {
	if override := os.Getenv("EOS_INSTALL_DIR"); override != "" {
		return override
	}
	return InstallDir
}

func IsUnderSystemd() bool {
	return os.Getenv("INVOCATION_ID") != ""
}

func IsSystemdManaged(systemdTargetDir string, systemdTargetFileName string) (bool, error) {
	_, err := os.Stat(filepath.Join(systemdTargetDir, systemdTargetFileName))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking systemd unit file: %w", err)
}

// ResolveSystemdScope finds which systemd instance (system or user) actually
// has the eos unit installed, checking the on-disk unit file rather than the
// invoking process's uid — a system unit installed via sudo must stay
// discoverable to a later non-root invocation, and vice versa.
//
// If no unit file exists in either location (not installed yet), it falls
// back to the caller's privilege level: root defaults to the system scope,
// non-root defaults to the user scope.
// resolveScope implements the shared systemd/launchd scope resolution: it prefers
// whichever domain (system or per-user) actually has the eos unit installed on
// disk, and otherwise falls back to the caller's privilege level (root→system,
// non-root→user). isManaged reports whether the unit exists in a given dir;
// userDir resolves the per-user directory (already error-wrapped by the caller).
func resolveScope(systemDir string, userDir func() (string, error), isManaged func(dir string) (bool, error)) (dir string, managed bool, userScope bool, err error) {
	systemManaged, err := isManaged(systemDir)
	if err != nil {
		return "", false, false, err
	}
	if systemManaged {
		return systemDir, true, false, nil
	}

	uDir, err := userDir()
	if err != nil {
		return "", false, false, err
	}
	userManaged, err := isManaged(uDir)
	if err != nil {
		return "", false, false, err
	}
	if userManaged {
		return uDir, true, true, nil
	}

	if os.Getuid() != 0 {
		return uDir, false, true, nil
	}
	return systemDir, false, false, nil
}

func ResolveSystemdScope(systemDir string) (dir string, isManaged bool, userUnit bool, err error) {
	return resolveScope(systemDir,
		func() (string, error) {
			d, dirErr := UserSystemdDir()
			if dirErr != nil {
				return "", fmt.Errorf("resolving user systemd dir: %w", dirErr)
			}
			return d, nil
		},
		func(dir string) (bool, error) { return IsSystemdManaged(dir, SystemdTargetFileName) },
	)
}

// UserLaunchAgentsDir returns the per-user LaunchAgents directory, e.g. ~/Library/LaunchAgents/.
func UserLaunchAgentsDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents") + "/", nil
}

// IsUnderLaunchd reports whether the current process was launched by launchd.
// launchd sets XPC_SERVICE_NAME on every job it starts (LaunchAgent or LaunchDaemon) —
// the macOS analog of systemd's INVOCATION_ID.
func IsUnderLaunchd() bool {
	return os.Getenv("XPC_SERVICE_NAME") != ""
}

func IsLaunchdManaged(launchdTargetDir string, launchdPlistFileName string) (bool, error) {
	_, err := os.Stat(filepath.Join(launchdTargetDir, launchdPlistFileName))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking launchd plist file: %w", err)
}

// ResolveLaunchdScope is the launchd analog of ResolveSystemdScope: it finds which
// domain (system LaunchDaemon or per-user LaunchAgent) actually has the eos plist
// installed, checking the on-disk file rather than the invoking process's uid.
//
// If no plist exists in either location (not installed yet), it falls back to the
// caller's privilege level: root defaults to the system scope, non-root to the user scope.
func ResolveLaunchdScope(systemDir string) (dir string, isManaged bool, userAgent bool, err error) {
	return resolveScope(systemDir,
		func() (string, error) {
			d, dirErr := UserLaunchAgentsDir()
			if dirErr != nil {
				return "", fmt.Errorf("resolving user launch agents dir: %w", dirErr)
			}
			return d, nil
		},
		func(dir string) (bool, error) { return IsLaunchdManaged(dir, LaunchdPlistFileName) },
	)
}

// EosConfig is the shape of ~/.eos/config.yaml.
type EosConfig struct {
	Sinks     map[string]types.LogSink `yaml:"sinks"`
	Telemetry EosTelemetryConfig       `yaml:"telemetry"`
	Health    EosHealthConfig          `yaml:"health"`
	Log       EosLogConfig             `yaml:"log"`
}

// EosTelemetryConfig is the config.yaml shape of TelemetryConfig.
type EosTelemetryConfig struct {
	Endpoint string `yaml:"endpoint"`
	Enable   bool   `yaml:"enable"`
	Insecure bool   `yaml:"insecure"`
}

type EosHealthConfig struct {
	CheckIntervalMs     int              `yaml:"checkIntervalMs"`
	MemSampleIntervalMs int              `yaml:"memSampleIntervalMs"`
	Backoff             EosBackoffConfig `yaml:"backoff"`
	Memory              EosMemoryConfig  `yaml:"memory"`
}

type EosBackoffConfig struct {
	BaseMs int `yaml:"baseMs"`
	MaxMs  int `yaml:"maxMs"`
}

type EosMemoryConfig struct {
	WarningThreshold      float64 `yaml:"warningThreshold"`
	SoftRestartThreshold  float64 `yaml:"softRestartThreshold"`
	ForceRestartThreshold float64 `yaml:"forceRestartThreshold"`
}

type EosLogConfig struct {
	MaxFiles           int   `yaml:"maxFiles"`
	FileSizeLimitBytes int64 `yaml:"fileSizeLimitBytes"`
}

func DefaultEosConfig() EosConfig {
	return EosConfig{
		Health: EosHealthConfig{
			CheckIntervalMs:     HealthCheckIntervalMs,
			MemSampleIntervalMs: HealthMemSampleIntervalMs,
			Backoff: EosBackoffConfig{
				BaseMs: HealthBackoffBaseMs,
				MaxMs:  HealthBackoffMaxMs,
			},
			Memory: EosMemoryConfig{
				WarningThreshold:      HealthMemoryWarningThreshold,
				SoftRestartThreshold:  HealthMemorySoftRestartThreshold,
				ForceRestartThreshold: HealthMemoryForceRestartThreshold,
			},
		},
		Log: EosLogConfig{
			MaxFiles:           DaemonLogMaxFiles,
			FileSizeLimitBytes: DaemonLogFileSizeLimit,
		},
	}
}

// Validate checks that all EosConfig values are within acceptable ranges.
func (c *EosConfig) Validate() error {
	m := c.Health.Memory
	if m.WarningThreshold <= 0 || m.WarningThreshold >= 1 {
		return fmt.Errorf("health.memory.warningThreshold must be between 0 and 1, got %.2f", m.WarningThreshold)
	}
	if m.SoftRestartThreshold <= 0 || m.SoftRestartThreshold >= 1 {
		return fmt.Errorf("health.memory.softRestartThreshold must be between 0 and 1, got %.2f", m.SoftRestartThreshold)
	}
	if m.ForceRestartThreshold <= 0 || m.ForceRestartThreshold >= 1 {
		return fmt.Errorf("health.memory.forceRestartThreshold must be between 0 and 1, got %.2f", m.ForceRestartThreshold)
	}
	if !(m.WarningThreshold < m.SoftRestartThreshold && m.SoftRestartThreshold < m.ForceRestartThreshold) {
		return fmt.Errorf("health.memory thresholds must be ascending: warning < softRestart < forceRestart")
	}
	if c.Health.CheckIntervalMs <= 0 {
		return fmt.Errorf("health.checkIntervalMs must be positive, got %d", c.Health.CheckIntervalMs)
	}
	if c.Health.Backoff.BaseMs <= 0 {
		return fmt.Errorf("health.backoff.baseMs must be positive, got %d", c.Health.Backoff.BaseMs)
	}
	if c.Health.Backoff.MaxMs <= c.Health.Backoff.BaseMs {
		return fmt.Errorf("health.backoff.maxMs must be greater than baseMs")
	}
	for name := range c.Sinks {
		if name == "" {
			return fmt.Errorf("sinks: registry entry has an empty name")
		}
		if c.Sinks[name].Type == "" {
			return fmt.Errorf("sinks.%s: type is required", name)
		}
	}
	if c.Telemetry.Enable && c.Telemetry.Endpoint == "" {
		return fmt.Errorf("telemetry.enable is true but telemetry.endpoint is empty")
	}
	return nil
}

// LoadEosConfig reads ~/.eos/config.yaml, returning defaults when absent.
func LoadEosConfig(baseDir string) (EosConfig, error) {
	cfg := DefaultEosConfig()
	path := filepath.Join(baseDir, EosConfigFileName)
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from trusted baseDir
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid eos config: %w", err)
	}
	return cfg, nil
}

// func CreateConfigFile(baseDir string) (*os.File, error) {
// 	configFilePath := filepath.Join(baseDir, ConfigFile)
// 	if _, err := os.Stat(configFilePath); err != nil {
// 		return nil, fmt.Errorf("config file already exists")
// 	}

// 	file, err := os.Create(configFilePath)
// 	if err != nil {
// 		return nil, fmt.Errorf("creating a config file errored, got: %w", err)
// 	}
// 	return file, nil
// }

// func ReadConfigFile(baseDir string) ([]byte, error) {
// 	configFilePath := filepath.Join(baseDir, ConfigFile)

// 	if _, err := os.Stat(configFilePath); err != nil {
// 		return nil, fmt.Errorf("config file doesn't exist")
// 	}

// 	data, err := os.ReadFile(configFilePath)
// 	if err != nil {
// 		return nil, fmt.Errorf("reading config file errored, got: %w", err)
// 	}

// 	var config SystemConfig
// 	err = yaml.Unmarshal(data, &config)
// 	if err != nil {
// 		return nil, fmt.Errorf("error parsing Config YAML: %w", err)
// 	}

// 	return data, nil
// }

// func UpdateConfigFile(baseDir string, data []byte) error {
// 	configFilePath := filepath.Join(baseDir, ConfigFile)
// 	err := os.WriteFile(configFilePath, data, 0644)
// 	if err != nil {
// 		return fmt.Errorf("updating config file errored, got: %w", err)
// 	}
// 	return nil
// }

// func RemoveConfigFile(baseDir string) error {
// 	configFilePath := filepath.Join(baseDir, ConfigFile)
// 	err := os.Remove(configFilePath)
// 	if err != nil {
// 		return fmt.Errorf("removing config file errored, got: %w", err)
// 	}
// 	return nil
// }
