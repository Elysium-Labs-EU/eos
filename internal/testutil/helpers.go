// Package testutil provides shared test helpers for database setup and fixture loading.
package testutil

import (
	"database/sql"
	"embed"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/database"
	"codeberg.org/Elysium_Labs/eos/internal/logutil"
	"codeberg.org/Elysium_Labs/eos/internal/types"
)

func SetupTestDB(t testing.TB, migrationsFS embed.FS, migrationsPath string) (*database.DB, *sql.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, dbConn, err := database.NewTestDB(t.Context(), dbPath, migrationsFS, migrationsPath)
	if err != nil {
		t.Fatalf("Unable to create test database 3: %v", err)
	}
	t.Cleanup(func() { _ = db.CloseDBConnection() })
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

func WithLogSinks(sinks ...types.LogSink) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.LogSinks = sinks
	}
}

func WithCronRestart(cronExpr string) ServiceConfigOption {
	return func(sc *types.ServiceConfig) {
		sc.CronRestart = cronExpr
	}
}

func NewTestServiceConfigFile(t *testing.T, opts ...ServiceConfigOption) *types.ServiceConfig {
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

func NewTestServiceScript(t *testing.T, opts ...ServiceScriptOption) *ServiceScriptSetup {
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

func NewTestServiceScriptAtLocation(t *testing.T, testServiceScript ServiceScriptSetup) {
	t.Helper()

	fullPathScript := filepath.Join(testServiceScript.DirPath, testServiceScript.FileName)
	err := os.WriteFile(fullPathScript, []byte(testServiceScript.Script), 0755) // #nosec G306 -- test files should be readable by other users/tools
	if err != nil {
		t.Fatalf("error occurred during writing the start script file, got: %v\n", err)
	}
}

type StandaloneDaemonConfigOption func(*config.StandaloneDaemonConfig)
type SystemdConfigOption func(*config.SystemdConfig)

func WithPIDFile(pidFile string) StandaloneDaemonConfigOption {
	return func(dc *config.StandaloneDaemonConfig) {
		dc.PIDFile = pidFile
	}
}

func WithSocketPath(socketPath string) StandaloneDaemonConfigOption {
	return func(dc *config.StandaloneDaemonConfig) {
		dc.SocketPath = socketPath
	}
}

func WithLogDir(logDir string) StandaloneDaemonConfigOption {
	return func(dc *config.StandaloneDaemonConfig) {
		dc.Log.LogDir = logDir
	}
}

func WithLogFilename(logFilename string) StandaloneDaemonConfigOption {
	return func(dc *config.StandaloneDaemonConfig) {
		dc.Log.LogFileName = logFilename
	}
}

func WithLogMaxFiles(logMaxFiles int) StandaloneDaemonConfigOption {
	return func(dc *config.StandaloneDaemonConfig) {
		dc.Log.LogMaxFiles = logMaxFiles
	}
}

func WithLogFileSizeLimit(logFileSizeLimit int64) StandaloneDaemonConfigOption {
	return func(dc *config.StandaloneDaemonConfig) {
		dc.Log.LogFileSizeLimit = logFileSizeLimit
	}
}

func WithSystemdTargetDir(systemdTargetDir string) SystemdConfigOption {
	return func(dc *config.SystemdConfig) {
		dc.SystemdTargetDir = systemdTargetDir
	}
}

func WithSystemdTargetFileName(systemdTargetFileName string) SystemdConfigOption {
	return func(dc *config.SystemdConfig) {
		dc.SystemdTargetFileName = systemdTargetFileName
	}
}

func safeParseDuration(durationAsString string, fallback time.Duration) time.Duration {
	limit, err := time.ParseDuration(durationAsString)
	if err != nil {
		return fallback
	}
	return limit
}

func IsSystemdManaged() (bool, error) {
	return config.IsSystemdManaged(config.SystemdTargetDir, config.SystemdTargetFileName)
}

func NewTestSystemdDaemonConfig(t *testing.T, opts ...SystemdConfigOption) config.DaemonConfig {
	t.Helper()

	systemdConfig := config.SystemdConfig{
		SystemdTargetDir:      config.SystemdTargetDir,
		SystemdTargetFileName: config.SystemdTargetFileName,
	}

	for _, opt := range opts {
		opt(&systemdConfig)
	}

	return config.DaemonConfig{Standalone: nil, Systemd: &systemdConfig}
}

func NewTestStandaloneDaemonConfig(t *testing.T, baseDir string, opts ...StandaloneDaemonConfigOption) config.DaemonConfig {
	t.Helper()

	standaloneDaemonConfig := config.StandaloneDaemonConfig{
		PIDFile:       filepath.Join(baseDir, config.DaemonPIDFile),
		SocketPath:    filepath.Join(baseDir, config.DaemonSocketPath),
		SocketTimeout: safeParseDuration(config.DaemonSocketTimeout, time.Second*5),
		Log: config.DaemonLogConfig{
			LogDir:           filepath.Join(baseDir, "logs"),
			LogFileName:      config.DaemonLogFileName,
			LogMaxFiles:      config.DaemonLogMaxFiles,
			LogFileSizeLimit: config.DaemonLogFileSizeLimit,
		},
	}

	for _, opt := range opts {
		opt(&standaloneDaemonConfig)
	}

	return config.DaemonConfig{Standalone: &standaloneDaemonConfig, Systemd: nil}
}

type testWriter struct {
	t    *testing.T
	done chan struct{}
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	select {
	case <-w.done:
	default:
		w.t.Log(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}

// NewTestLogger returns a verbose *slog.Logger that writes to t.Log.
func NewTestLogger(t *testing.T) *slog.Logger {
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	return logutil.NewTextLogger(&testWriter{t: t, done: done}, true)
}
