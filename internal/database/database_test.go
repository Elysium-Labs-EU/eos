package database

import (
	"deploy-cli/internal/util"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateServiceInstance(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := NewTestDB(dbPath)
	if err != nil {
		t.Fatalf("Unable to create test database: %v", err)
	}

	db.conn.Exec(`INSERT INTO service_instances (name, created_at) VALUES ('cms', ?)`, time.Now())

	updates := ServiceInstanceUpdate{
		RestartCount: util.IntPtr(5),
		StartedAt:    util.TimePtr(time.Now()),
	}
	err = db.UpdateServiceInstance("cms", updates)

	if err != nil {
		t.Fatalf("updateServiceInstance failed got: %v\n", err)
	}
}
