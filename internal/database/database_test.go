package database_test

import (
	"eos/internal/database"
	"eos/internal/testutil"
	"eos/internal/util"
	"testing"
	"time"
)

func TestUpdateServiceInstance(t *testing.T) {
	db, rawDBConn, _ := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	rawDBConn.Exec(`INSERT INTO service_instances (name, created_at) VALUES ('cms', ?)`, time.Now())

	updates := database.ServiceInstanceUpdate{
		RestartCount: util.IntPtr(5),
		StartedAt:    util.TimePtr(time.Now()),
	}
	err := db.UpdateServiceInstance("cms", updates)

	if err != nil {
		t.Fatalf("updateServiceInstance failed got: %v\n", err)
	}
}
