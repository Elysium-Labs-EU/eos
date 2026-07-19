package database

import (
	"embed"
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// ErrSchemaVersionAhead is returned when the recorded schema version in the
// state database is newer than the highest migration embedded in this binary.
// This happens on a downgrade/rollback: an older eos is started against a
// database already migrated by a newer eos. It is not recoverable by running
// migrations, so it is surfaced as a distinct, actionable error rather than the
// raw golang-migrate "no migration found for version N" message.
var ErrSchemaVersionAhead = errors.New("database schema version is newer than this eos binary supports")

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

	// Detect the "database newer than binary" case before attempting Up().
	// golang-migrate's Up() would otherwise fail trying to read the down file
	// for the current (unknown) version, producing a cryptic internal error
	// with no recovery guidance. This is a downgrade/rollback situation and is
	// not fixable by migrating, so surface a clear, actionable error.
	dbVersion, dirty, verr := m.Version()
	if verr != nil && !errors.Is(verr, migrate.ErrNilVersion) {
		return fmt.Errorf("could not read current schema version: %w", verr)
	}
	if verr == nil { // a schema version is recorded
		latest, herr := highestMigrationVersion(sourceDriver)
		if herr != nil {
			return fmt.Errorf("could not determine latest known migration: %w", herr)
		}
		if dbVersion > latest {
			return fmt.Errorf(
				"%w: state database is at version %d (dirty=%v) but this binary only knows migrations up to version %d. "+
					"This eos is older than the one that last wrote the database (a downgrade/rollback). "+
					"To recover: upgrade eos to a build that includes migration %d, "+
					"or back up and remove the state database (state.db in EOS_BASE_DIR, default ~/.eos) so this version can recreate it",
				ErrSchemaVersionAhead, dbVersion, dirty, latest, dbVersion)
		}
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("could not run migrations: %w", err)
	}

	return nil
}

// highestMigrationVersion returns the largest migration version available in
// the given source (the newest migration embedded in this binary).
func highestMigrationVersion(src source.Driver) (uint, error) {
	version, err := src.First()
	if err != nil {
		// No migrations at all in the source.
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading first migration: %w", err)
	}

	for {
		next, err := src.Next(version)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return version, nil // no newer migration, this is the highest
			}
			return 0, fmt.Errorf("reading migration after version %d: %w", version, err)
		}
		version = next
	}
}

func (db *DB) GetCurrentMigrationVersion(migrationsFS embed.FS, migrationsPath string) (uint, bool, error) {
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

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run down migration: %w", err)
	}

	return nil
}
