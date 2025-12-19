package testutil

import (
	"deploy-cli/internal/database"
	"path/filepath"
	"testing"
)

func SetupTestDB(t *testing.T) (*database.DB, string) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewTestDB(dbPath)
	if err != nil {
		t.Fatalf("Unable to create test database: %v", err)
	}
	return db, tempDir
}
