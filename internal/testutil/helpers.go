package testutil

import (
	"database/sql"
	"embed"
	"eos/internal/database"
	"path/filepath"
	"testing"
)

func SetupTestDB(t *testing.T, migrationsFS embed.FS, migrationsPath string) (*database.DB, *sql.DB, string) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, dbConn, err := database.NewTestDB(dbPath, migrationsFS, migrationsPath)
	if err != nil {
		t.Fatalf("Unable to create test database 3: %v", err)
	}
	return db, dbConn, tempDir
}
