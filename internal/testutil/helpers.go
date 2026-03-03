package testutil

import (
	"database/sql"
	"embed"
	"os"
	"path/filepath"
	"testing"

	"eos/internal/config"
	"eos/internal/database"
	"eos/internal/types"
)

func SetupTestDB(t *testing.T, migrationsFS embed.FS, migrationsPath string) (*database.DB, *sql.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, dbConn, err := database.NewTestDB(t.Context(), dbPath, migrationsFS, migrationsPath)
	if err != nil {
		t.Fatalf("Unable to create test database 3: %v", err)
	}
	return db, dbConn, tempDir
}

type ServiceConfigOption func(*types.ServiceConfig)

func WithRuntime(runtimeType string, path string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Runtime.Type = runtimeType
		sc.Runtime.Path = path
	}
}

func WithRuntimeType(runtimeType string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Runtime.Type = runtimeType
	}
}

func WithRuntimePath(path string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Runtime.Path = path
	}
}

func WithoutRuntime() ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Runtime = types.Runtime{}
	}
}

func WithName(name string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Name = name
	}
}

func WithCommand(command string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Command = command
	}
}

func WithPort(port int) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.Port = port
	}
}

func WithEnvFile(envFile string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.EnvFile = envFile
	}
}

func CreateTestServiceConfigFile(t *testing.T, opts ...ServiceConfigOption) *types.ServiceConfig {
	t.Helper()

	config := &types.ServiceConfig{
		Name:    "cms",
		Command: "/home/user/start-script.sh",
		Port:    1337,
		Runtime: types.Runtime{
			Type: "nodejs",
			Path: "/path/to/node",
		},
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}

type ServiceScriptSetup struct {
	Script   string `yaml:"script"`
	DirPath  string `json:"dir_path" yaml:"dir_path"`
	FileName string `json:"file_name" yaml:"file_name"`
}

type ServiceScriptOption func(*ServiceScriptSetup)

func WithScript(script string) ServiceScriptOption {
	return func(sss *ServiceScriptSetup) {
		sss.Script = script
	}
}

func WithDirPath(dirPath string) ServiceScriptOption {
	return func(sss *ServiceScriptSetup) {
		sss.DirPath = dirPath
	}
}

func WithFileName(fileName string) ServiceScriptOption {
	return func(sss *ServiceScriptSetup) {
		sss.FileName = fileName
	}
}

func CreateTestServiceScript(t *testing.T, opts ...ServiceScriptOption) *ServiceScriptSetup {
	testServiceConfig := &ServiceScriptSetup{
		Script: `#!/bin/bash
trap 'exit 0' SIGTERM SIGINT
while true; do
    sleep 1
done`,
		DirPath:  "/",
		FileName: "test-script.sh",
	}

	for _, opt := range opts {
		opt(testServiceConfig)
	}

	return testServiceConfig
}

func CreateTestServiceScriptAtLocation(t *testing.T, testServiceScript ServiceScriptSetup) {
	t.Helper()

	fullPathScript := filepath.Join(testServiceScript.DirPath, testServiceScript.FileName)
	err := os.WriteFile(fullPathScript, []byte(testServiceScript.Script), 0755) // #nosec G306 -- test files should be readable by other users/tools
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}
}

type DaemonConfigOption func(*config.DaemonConfig)

func WithPIDFile(pidFile string) DaemonConfigOption {
	return func(dc *config.DaemonConfig) {
		dc.PIDFile = pidFile
	}
}

func WithSocketPath(socketPath string) DaemonConfigOption {
	return func(dc *config.DaemonConfig) {
		dc.SocketPath = socketPath
	}
}

func WithLogDir(logDir string) DaemonConfigOption {
	return func(dc *config.DaemonConfig) {
		dc.LogDir = logDir
	}
}

func WithLogFilename(logFilename string) DaemonConfigOption {
	return func(dc *config.DaemonConfig) {
		dc.LogFileName = logFilename
	}
}

func WithMaxFiles(maxFiles int) DaemonConfigOption {
	return func(dc *config.DaemonConfig) {
		dc.MaxFiles = maxFiles
	}
}

func WithFileSizeLimit(fileSizeLimit int64) DaemonConfigOption {
	return func(dc *config.DaemonConfig) {
		dc.FileSizeLimit = fileSizeLimit
	}
}

func CreateTestDaemonConfig(t *testing.T, baseDir string, opts ...DaemonConfigOption) *config.DaemonConfig {
	t.Helper()

	daemonConfig := &config.DaemonConfig{
		PIDFile:       filepath.Join(baseDir, config.DaemonPIDFile),
		SocketPath:    filepath.Join(baseDir, config.DaemonSocketPath),
		LogDir:        filepath.Join(baseDir, "logs"),
		LogFileName:   config.DaemonLogFileName,
		MaxFiles:      config.DaemonLogMaxFiles,
		FileSizeLimit: config.DaemonLogFileSizeLimit,
	}

	for _, opt := range opts {
		opt(daemonConfig)
	}

	return daemonConfig
}
