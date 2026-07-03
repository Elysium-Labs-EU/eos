// Package config loads and validates eos service configuration from YAML files.
package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/userutil"
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
	Name                              = "eos"
	ShutdownGracePeriod               = "5s"
	SystemdTargetDir                  = "/etc/systemd/system/"
	SystemdTargetFileName             = "eos.service"
)

type DaemonConfig struct {
	Standalone *StandaloneDaemonConfig `json:"standalone" yaml:"standalone"`
	Systemd    *SystemdConfig          `json:"systemd" yaml:"systemd"`
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

type SystemConfig struct {
	Daemon       DaemonConfig   `json:"daemon" yaml:"daemon"`
	Health       HealthConfig   `json:"health" yaml:"health"`
	Shutdown     ShutdownConfig `json:"shutdown" yaml:"shutdown"`
	UnderSystemd bool           `json:"under_systemd" yaml:"underSystemd"`
}

func UserSystemdDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "systemd", "user") + "/", nil
}

func GetBaseDir() (string, error) {
	if override := os.Getenv("EOS_BASE_DIR"); override != "" {
		return override, nil
	}

	// When invoked via sudo, use the original user's home directory
	// instead of root's, so data always lives in the invoking user's home.
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		u, err := user.Lookup(sudoUser)
		if err == nil {
			return filepath.Join(u.HomeDir, fmt.Sprintf(".%s", Name)), nil
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine base directory: %w", err)
	}

	return filepath.Join(homeDir, fmt.Sprintf(".%s", Name)), nil
}

func CreateBaseDir() (string, error) {
	if os.Getuid() == 0 && os.Getenv("SUDO_USER") == "" && os.Getenv("EOS_BASE_DIR") == "" {
		return "", fmt.Errorf("do not run eos as root: invoke as the target user directly")
	}

	baseDir, err := GetBaseDir()
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(baseDir, 0750)
	if err != nil {
		return "", fmt.Errorf("could not create eos directory: %w", err)
	}

	if os.Getuid() == 0 {
		u, err := userutil.EffectiveUser()
		if err != nil {
			return "", fmt.Errorf("resolving effective user for chown: %w", err)
		}
		uid, gid, err := userutil.UserCredentials(u)
		if err != nil {
			return "", fmt.Errorf("resolving credentials for chown: %w", err)
		}
		if err := os.Chown(baseDir, int(uid), int(gid)); err != nil {
			return "", fmt.Errorf("chown %s to %s: %w", baseDir, u.Username, err)
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

// EosConfig is the shape of ~/.eos/config.yaml.
type EosConfig struct {
	Health EosHealthConfig `yaml:"health"`
	Log    EosLogConfig    `yaml:"log"`
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
func (c EosConfig) Validate() error {
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
