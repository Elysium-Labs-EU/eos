package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

// NOTE: In the nearby future we want to enable this to be overwritten by the release process.
// var installDir string

const (
	DaemonLogFileName      = "daemon.log"
	DaemonLogFileSizeLimit = int64(10 * 1024 * 1024)
	DaemonLogMaxFiles      = 5
	DaemonPIDFile          = "eos.pid"
	DaemonSocketPath       = "eos.sock"
	DaemonSocketTimeout    = "5s"
	HealthMaxRestart       = 10
	HealthTimeOutEnable    = true
	HealthTimeOutLimit     = "10s"
	InstallDir             = "/usr/local/bin"
	Name                   = "eos"
	ShutdownGracePeriod    = "5s"
)

type DaemonConfig struct {
	PIDFile       string        `json:"pid_file" yaml:"pidFile"`
	SocketPath    string        `json:"socket_path" yaml:"socketPath"`
	LogDir        string        `json:"log_dir" yaml:"logDir"`
	LogFileName   string        `json:"log_file_name" yaml:"logFileName"`
	SocketTimeout time.Duration `json:"socket_timeout" yaml:"socketTimeout"`
	MaxFiles      int           `json:"max_files" yaml:"maxFiles"`
	FileSizeLimit int64         `json:"file_size_limit" yaml:"fileSizeLimit"`
}

type TimeOutConfig struct {
	Enable bool          `json:"enable" yaml:"enable"`
	Limit  time.Duration `json:"limit" yaml:"limit"`
}

type HealthConfig struct {
	MaxRestart int           `json:"max_restart" yaml:"maxRestart"`
	Timeout    TimeOutConfig `json:"timeout" yaml:"timeout"`
}

type ShutdownConfig struct {
	GracePeriod time.Duration `json:"grace_period" yaml:"gracePeriod"`
}

type SystemConfig struct {
	Daemon   DaemonConfig   `json:"daemon" yaml:"daemon"`
	Health   HealthConfig   `json:"health" yaml:"health"`
	Shutdown ShutdownConfig `json:"shutdown" yaml:"shutdown"`
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
	baseDir, err := GetBaseDir()
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(baseDir, 0750)
	if err != nil {
		return "", fmt.Errorf("could not create eos directory: %w", err)
	}

	return baseDir, nil
}

func GetInstallDir() string {
	if override := os.Getenv("EOS_INSTALL_DIR"); override != "" {
		return override
	}
	return InstallDir
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
