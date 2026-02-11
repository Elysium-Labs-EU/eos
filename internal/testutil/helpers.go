package testutil

import (
	"database/sql"
	"embed"
	"eos/internal/database"
	"eos/internal/types"
	"path/filepath"
	"testing"
)

func SetupTestDB(t *testing.T, migrationsFS embed.FS, migrationsPath string) (*database.DB, *sql.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, dbConn, err := database.NewTestDB(dbPath, migrationsFS, migrationsPath)
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
