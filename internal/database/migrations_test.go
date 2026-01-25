package database_test

import (
	"database/sql"
	"eos/internal/database"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	db, rawDBConn, err := database.NewTestDB(dbPath, database.MigrationsFS, database.MigrationsPath)
	if err != nil {
		t.Fatalf("Unable to create test database 3: %v", err)
	}

	t.Log("Running up migrations...")
	if err := db.RunMigrations(database.MigrationsFS, database.MigrationsPath); err != nil {
		t.Fatalf("Failed to run up migrations: %v", err)
	}

	expectedVersion := getExpectedVersion(t, database.MigrationsPath)
	version, dirty, err := db.GetCurrentVersion(database.MigrationsFS, database.MigrationsPath)
	if err != nil {
		t.Fatalf("Failed to get current version: %v", err)
	}
	if dirty {
		t.Fatalf("Database is dirty after up migration - this should never happen in a successful migration")
	}
	if version != expectedVersion {
		t.Fatalf("Expected version %d after up migration, got version %d", expectedVersion, version)
	}
	t.Logf("Up migration successful, database at version %d", version)

	t.Log("Verifying tables exist with correct schema...")
	verifyTablesExist(t, rawDBConn)
	verifyTableStructure(t, rawDBConn)
	verifyIndexesExist(t, rawDBConn)
	t.Log("All expected tables exist with correct structure")

	t.Log("Testing constraints and data insertion...")
	testSchemaConstraints(t, rawDBConn)
	t.Log("Schema constraints working correctly")

	t.Log("Running down migration...")
	if err := db.RunDownMigration(database.MigrationsFS, database.MigrationsPath); err != nil {
		t.Fatalf("Failed to run down migration: %v", err)
	}
	t.Log("Down migration successful")

	t.Log("Verifying tables are removed...")
	verifyTablesRemoved(t, rawDBConn)
	t.Log("All tables successfully removed")

	t.Log("Running up migration again to test reversibility...")
	if err := db.RunMigrations(database.MigrationsFS, database.MigrationsPath); err != nil {
		t.Fatalf("Failed to run up migrations second time: %v", err)
	}
	t.Log("Second up migration successful - migrations are fully reversible")

	expectedVersion = getExpectedVersion(t, database.MigrationsPath)
	version, dirty, err = db.GetCurrentVersion(database.MigrationsFS, database.MigrationsPath)
	if err != nil {
		t.Fatalf("Failed to get final version: %v", err)
	}
	if dirty {
		t.Fatalf("Database is dirty after second up migration")
	}
	if version != expectedVersion {
		t.Fatalf("Expected version %d after second up migration, got version %d", expectedVersion, version)
	}
	t.Log("All migration tests passed!")
}

func getExpectedVersion(t *testing.T, migrationsDir string) uint {
	t.Helper()

	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("Failed to read migrations directory: %v", err)
	}

	var migrationCount uint
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".up.sql") {
			migrationCount += 1
		}
	}

	if migrationCount == 0 {
		t.Fatalf("No migrations found in directory - test cannot proceed")
	}

	return migrationCount
}

func verifyTablesExist(t *testing.T, db *sql.DB) {
	t.Helper()

	expectedTables := []string{
		"service_catalog",
		"service_instances",
		"process_history",
	}

	for _, tableName := range expectedTables {
		var count int
		query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
		err := db.QueryRow(query, tableName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for table %s: %v", tableName, err)
		}
		if count != 1 {
			t.Errorf("Expected table %s to exist, but it doesn't", tableName)
		}
	}
}

func verifyTableStructure(t *testing.T, db *sql.DB) {
	t.Helper()

	// Verify service_catalog columns
	t.Run("service_catalog_structure", func(t *testing.T) {
		expectedColumns := map[string]bool{
			"name":        false,
			"path":        false,
			"config_file": false,
			"created_at":  false,
		}

		rows, err := db.Query(`PRAGMA table_info(service_catalog)`)
		if err != nil {
			t.Fatalf("Failed to get table info: %v", err)
		}
		defer rows.Close()

		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue sql.NullString
			err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk)
			if err != nil {
				t.Fatalf("Failed to scan column info: %v", err)
			}
			if _, exists := expectedColumns[name]; exists {
				expectedColumns[name] = true
			}
		}

		for col, found := range expectedColumns {
			if !found {
				t.Errorf("Expected column %s not found in service_catalog", col)
			}
		}
	})

	// Verify service_instances columns
	t.Run("service_instances_structure", func(t *testing.T) {
		expectedColumns := map[string]bool{
			"name":              false,
			"restart_count":     false,
			"last_health_check": false,
			"created_at":        false,
			"started_at":        false,
			"updated_at":        false,
		}

		rows, err := db.Query(`PRAGMA table_info(service_instances)`)
		if err != nil {
			t.Fatalf("Failed to get table info: %v", err)
		}
		defer rows.Close()

		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue sql.NullString
			err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk)
			if err != nil {
				t.Fatalf("Failed to scan column info: %v", err)
			}
			if _, exists := expectedColumns[name]; exists {
				expectedColumns[name] = true
			}
		}

		for col, found := range expectedColumns {
			if !found {
				t.Errorf("Expected column %s not found in service_instances", col)
			}
		}
	})

	// Verify process_history columns
	t.Run("process_history_structure", func(t *testing.T) {
		expectedColumns := map[string]bool{
			"pid":          false,
			"service_name": false,
			"state":        false,
			"error":        false,
			"created_at":   false,
			"started_at":   false,
			"stopped_at":   false,
			"updated_at":   false,
		}

		rows, err := db.Query(`PRAGMA table_info(process_history)`)
		if err != nil {
			t.Fatalf("Failed to get table info: %v", err)
		}
		defer rows.Close()

		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dfltValue sql.NullString
			err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk)
			if err != nil {
				t.Fatalf("Failed to scan column info: %v", err)
			}
			if _, exists := expectedColumns[name]; exists {
				expectedColumns[name] = true
			}
		}

		for col, found := range expectedColumns {
			if !found {
				t.Errorf("Expected column %s not found in process_history", col)
			}
		}
	})
}

func verifyIndexesExist(t *testing.T, db *sql.DB) {
	t.Helper()

	query := `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`

	var count int
	err := db.QueryRow(query, "idx_processes_lookup").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for index: %v", err)
	}
	if count != 1 {
		t.Error("Expected index idx_processes_lookup to exist, but it doesn't")
	}
}

func testSchemaConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	// Test primary key constraint on service_catalog
	t.Run("service_catalog_pk_constraint", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO service_catalog (name, path, config_file, created_at) VALUES (?, ?, ?, datetime('now'))`,
			"test-service", "/path", "config.yaml")
		if err != nil {
			t.Fatalf("Failed to insert first record: %v", err)
		}

		// Try to insert duplicate - should fail
		_, err = db.Exec(`INSERT INTO service_catalog (name, path, config_file, created_at) VALUES (?, ?, ?, datetime('now'))`,
			"test-service", "/other/path", "other.yaml")
		if err == nil {
			t.Error("Expected primary key constraint violation, but insert succeeded")
		}
	})

	// Test NOT NULL constraints
	t.Run("service_catalog_not_null", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO service_catalog (name, path, config_file, created_at) VALUES (?, NULL, ?, datetime('now'))`,
			"null-test", "config.yaml")
		if err == nil {
			t.Error("Expected NOT NULL constraint violation for path, but insert succeeded")
		}
	})

	// Test default values on service_instances
	t.Run("service_instances_defaults", func(t *testing.T) {
		_, err := db.Exec(`INSERT INTO service_instances (name, created_at) VALUES (?, datetime('now'))`, "default-test")
		if err != nil {
			t.Fatalf("Failed to insert service instance: %v", err)
		}

		var restartCount int
		err = db.QueryRow(`SELECT restart_count FROM service_instances WHERE name = ?`, "default-test").Scan(&restartCount)
		if err != nil {
			t.Fatalf("Failed to query restart_count: %v", err)
		}
		if restartCount != 0 {
			t.Errorf("Expected default restart_count of 0, got %d", restartCount)
		}
	})

	// Test foreign key constraint on process_history
	t.Run("process_history_fk_constraint", func(t *testing.T) {
		// Enable foreign keys for this test
		_, err := db.Exec(`PRAGMA foreign_keys = ON`)
		if err != nil {
			t.Fatalf("Failed to enable foreign keys: %v", err)
		}

		// Try to insert process history for non-existent service - should fail with FK enabled
		_, err = db.Exec(`INSERT INTO process_history (pid, service_name, state, created_at) VALUES (?, ?, ?, datetime('now'))`,
			99999, "nonexistent-service", "running")
		if err == nil {
			t.Error("Expected foreign key constraint violation, but insert succeeded")
		}

		// Now insert a valid service instance first
		_, err = db.Exec(`INSERT INTO service_instances (name, created_at) VALUES (?, datetime('now'))`, "fk-test-service")
		if err != nil {
			t.Fatalf("Failed to insert service instance: %v", err)
		}

		// Now process history insert should succeed
		_, err = db.Exec(`INSERT INTO process_history (pid, service_name, state, created_at) VALUES (?, ?, ?, datetime('now'))`,
			88888, "fk-test-service", "running")
		if err != nil {
			t.Errorf("Expected insert to succeed with valid FK, but got error: %v", err)
		}
	})
}

func verifyTablesRemoved(t *testing.T, db *sql.DB) {
	t.Helper()

	expectedTables := []string{
		"service_catalog",
		"service_instances",
		"process_history",
	}

	for _, tableName := range expectedTables {
		var count int
		query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
		err := db.QueryRow(query, tableName).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to check for table %s: %v", tableName, err)
		}
		if count != 0 {
			t.Errorf("Expected table %s to be removed, but it still exists", tableName)
		}
	}

	// Verify indexes are also removed
	var count int
	query := `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`
	err := db.QueryRow(query, "idx_processes_lookup").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check for index: %v", err)
	}
	if count != 0 {
		t.Error("Expected index idx_processes_lookup to be removed, but it still exists")
	}
}
