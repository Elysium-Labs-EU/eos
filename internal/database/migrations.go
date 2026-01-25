package database

import (
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

const MigrationsPath = "migrations"

func (db *DB) RunMigrations(migrationsFS embed.FS, migrationsPath string) error {
	driver, err := sqlite.WithInstance(db.conn, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("could not create migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, migrationsPath)
	if err != nil {
		return fmt.Errorf("could not create iofs source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("could not create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("could not run migrations: %w", err)
	}

	return nil
}

func (db *DB) GetCurrentVersion(migrationsFS embed.FS, migrationsPath string) (uint, bool, error) {
	driver, err := sqlite.WithInstance(db.conn, &sqlite.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("could not create migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, migrationsPath)
	if err != nil {
		return 0, false, fmt.Errorf("could not create iofs source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
	if err != nil {
		return 0, false, fmt.Errorf("could not create migrate instance: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		return 0, false, fmt.Errorf("could not get migration version: %w", err)
	}

	return version, dirty, nil
}

func (db *DB) RunDownMigration(migrationsFS embed.FS, migrationsPath string) error {
	driver, err := sqlite.WithInstance(db.conn, &sqlite.Config{})
	if err != nil {
		return fmt.Errorf("failed to create sqlite driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+migrationsPath,
		"sqlite",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	if err := m.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run down migration: %w", err)
	}

	return nil
}
