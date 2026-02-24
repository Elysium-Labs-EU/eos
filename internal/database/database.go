package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"eos/internal/types"
)

type Database interface {
	CloseDBConnection() error

	GetServiceInstance(ctx context.Context, name string) (types.ServiceRuntime, error)
	RegisterServiceInstance(ctx context.Context, name string) error
	RemoveServiceInstance(ctx context.Context, name string) (bool, error)
	UpdateServiceInstance(ctx context.Context, name string, updates ServiceInstanceUpdate) error

	GetAllServiceCatalogEntries(ctx context.Context) ([]types.ServiceCatalogEntry, error)
	GetServiceCatalogEntry(ctx context.Context, name string) (types.ServiceCatalogEntry, error)
	IsServiceRegistered(ctx context.Context, name string) (bool, error)
	RegisterService(ctx context.Context, name string, directoryPath string, configFileName string) error
	RemoveServiceCatalogEntry(ctx context.Context, name string) (bool, error)
	UpdateServiceCatalogEntry(ctx context.Context, name string, newDirectoryPath string, newConfigFileName string) error

	GetProcessHistoryEntriesByServiceName(ctx context.Context, serviceName string) ([]types.ProcessHistory, error)
	GetProcessHistoryEntryByPid(ctx context.Context, pid int) (types.ProcessHistory, error)
	RegisterProcessHistoryEntry(ctx context.Context, pid int, serviceName string, state types.ProcessState) (types.ProcessHistory, error)
	RemoveProcessHistoryEntryViaPid(ctx context.Context, pid int) (bool, error)
	UpdateProcessHistoryEntry(ctx context.Context, pid int, updates ProcessHistoryUpdate) error

	RunMigrations(migrationsFS embed.FS, migrationsPath string) error
	GetCurrentMigrationVersion(migrationsFS embed.FS, migrationsPath string) (uint, bool, error)
	RunDownMigration(migrationsFS embed.FS, migrationsPath string) error
}

var _ Database = (*DB)(nil)

func NewDB(ctx context.Context, baseDir string) (*DB, error) {
	dbPath := filepath.Join(baseDir, "state.db")

	db, err := openDB(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	if migrationsErr := db.RunMigrations(MigrationsFS, MigrationsPath); migrationsErr != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", migrationsErr)
	}
	_, dirty, err := db.GetCurrentMigrationVersion(MigrationsFS, MigrationsPath)
	if err != nil {
		return nil, fmt.Errorf("warning: Could not get schema version: %w", err)
	}
	if dirty {
		return nil, fmt.Errorf("database is in a dirty state. Manual intervention required")
	}

	return db, nil
}

func NewTestDB(ctx context.Context, dbPath string, testMigrationsFS embed.FS, testMigrationsPath string) (*DB, *sql.DB, error) {
	dir := filepath.Dir(dbPath)
	err := os.MkdirAll(dir, 0750)
	if err != nil {
		return nil, nil, fmt.Errorf("could not create test db directory: %w", err)
	}

	db, err := openDB(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}

	if migrationsErr := db.RunMigrations(testMigrationsFS, testMigrationsPath); migrationsErr != nil {
		return nil, nil, fmt.Errorf("failed to run migrations: %w", migrationsErr)
	}
	version, dirty, err := db.GetCurrentMigrationVersion(testMigrationsFS, testMigrationsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("warning: Could not get schema version: %w", err)
	} else {
		fmt.Printf("Database schema version: %d (dirty: %v)", version, dirty)
		if dirty {
			return nil, nil, fmt.Errorf("database is in a dirty state. Manual intervention required")
		}
	}

	return db, db.conn, nil
}

func openDB(ctx context.Context, dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	if err := conn.PingContext(ctx); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return nil, fmt.Errorf("error closing the connnection to database: %w", err)
		}
		return nil, fmt.Errorf("could not connect to database: %w", err)
	}

	db := &DB{conn: conn}

	return db, nil
}

func (db *DB) CloseDBConnection() error {
	if err := db.conn.Close(); err != nil {
		return fmt.Errorf("error closing database: %w", err)
	}
	return nil
}

func (db *DB) RegisterService(ctx context.Context, name string, directoryPath string, configFileName string) error {
	catalogQuery := `
	INSERT OR REPLACE INTO service_catalog (name, path, config_file, created_at)
	VALUES (?, ?, ?, ?)
	`

	_, err := db.conn.ExecContext(ctx, catalogQuery, name, directoryPath, configFileName, time.Now())
	if err != nil {
		return fmt.Errorf("could not register service into service_catalog: %w", err)
	}

	return nil
}

func (db *DB) RegisterServiceInstance(ctx context.Context, name string) error {
	instanceQuery := `
	INSERT OR REPLACE INTO service_instances (name, created_at)
	VALUES (?, ?)
	`

	_, err := db.conn.ExecContext(ctx, instanceQuery, name, time.Now())
	if err != nil {
		return fmt.Errorf("could not create service instance entry: %w", err)
	}

	return nil
}

func (db *DB) RegisterProcessHistoryEntry(ctx context.Context, pid int, serviceName string, state types.ProcessState) (types.ProcessHistory, error) {
	instanceQuery := `
	INSERT INTO process_history (pid, service_name, state, created_at)
	VALUES (?, ?, ?, ?)
	`
	createdAt := time.Now()

	_, err := db.conn.ExecContext(ctx, instanceQuery, pid, serviceName, state, createdAt)
	if err != nil {
		return types.ProcessHistory{}, fmt.Errorf("could not create process history entry: %w", err)
	}

	return types.ProcessHistory{
		PID:         pid,
		ServiceName: serviceName,
		State:       types.ProcessStateUnknown,
		CreatedAt:   createdAt,
	}, nil
}

func (db *DB) GetAllServiceCatalogEntries(ctx context.Context) ([]types.ServiceCatalogEntry, error) {
	query := `
	SELECT name, path, config_file, created_at
	FROM service_catalog
	ORDER BY name
	`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("could not query registered services: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var services []types.ServiceCatalogEntry
	for rows.Next() {
		var service types.ServiceCatalogEntry
		err := rows.Scan(&service.Name, &service.DirectoryPath, &service.ConfigFileName, &service.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("could not scan service row: %w", err)
		}
		services = append(services, service)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating service rows: %w", err)
	}

	return services, nil
}

var ErrServiceNotFound = errors.New("service not found")

func (db *DB) GetServiceCatalogEntry(ctx context.Context, name string) (types.ServiceCatalogEntry, error) {
	query := `
	SELECT name, path, config_file, created_at
	FROM service_catalog
	WHERE name = ?
	`

	row := db.conn.QueryRowContext(ctx, query, name)
	var svc types.ServiceCatalogEntry

	err := row.Scan(&svc.Name, &svc.DirectoryPath, &svc.ConfigFileName, &svc.CreatedAt)
	if err == sql.ErrNoRows {
		return types.ServiceCatalogEntry{}, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
	}
	if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("could not scan service row: %w", err)
	}
	return svc, nil
}

func (db *DB) GetServiceInstance(ctx context.Context, name string) (types.ServiceRuntime, error) {
	query := `
	SELECT name, restart_count, last_health_check, created_at, started_at, updated_at
	FROM service_instances
	WHERE name = ?
	`

	row := db.conn.QueryRowContext(ctx, query, name)
	var svc types.ServiceRuntime

	err := row.Scan(&svc.Name, &svc.RestartCount, &svc.LastHealthCheck, &svc.CreatedAt, &svc.StartedAt, &svc.UpdatedAt)
	if err == sql.ErrNoRows {
		return types.ServiceRuntime{}, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
	}
	if err != nil {
		return types.ServiceRuntime{}, fmt.Errorf("could not scan service row: %w", err)
	}
	return svc, nil
}

var ErrProcessHistoryNotFound = errors.New("process history not found")

func (db *DB) GetProcessHistoryEntryByPid(ctx context.Context, pid int) (types.ProcessHistory, error) {
	query := `
	SELECT pid, service_name, state, error, created_at, started_at, stopped_at, updated_at
	FROM process_history
	WHERE pid = ?
	`

	row := db.conn.QueryRowContext(ctx, query, pid)
	var processHistory types.ProcessHistory

	err := row.Scan(&processHistory.PID,
		&processHistory.ServiceName,
		&processHistory.State,
		&processHistory.Error,
		&processHistory.CreatedAt,
		&processHistory.StartedAt,
		&processHistory.StoppedAt,
		&processHistory.UpdatedAt)
	if err == sql.ErrNoRows {
		return types.ProcessHistory{}, fmt.Errorf("%w: %v", ErrProcessHistoryNotFound, pid)
	}
	if err != nil {
		return types.ProcessHistory{}, fmt.Errorf("could not scan process history row: %w", err)
	}
	return processHistory, nil
}

func (db *DB) GetProcessHistoryEntriesByServiceName(ctx context.Context, serviceName string) ([]types.ProcessHistory, error) {
	query := `
	SELECT pid, service_name, state, error, created_at, started_at, stopped_at, updated_at
	FROM process_history
	WHERE service_name = ?
	ORDER BY pid
	`

	rows, err := db.conn.QueryContext(ctx, query, serviceName)
	if err != nil {
		return nil, fmt.Errorf("could not query registered services: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var processHistoryEntries []types.ProcessHistory
	for rows.Next() {
		var processHistory types.ProcessHistory
		err := rows.Scan(&processHistory.PID,
			&processHistory.ServiceName,
			&processHistory.State,
			&processHistory.Error,
			&processHistory.CreatedAt,
			&processHistory.StartedAt,
			&processHistory.StoppedAt,
			&processHistory.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("could not scan process history row: %w", err)
		}
		processHistoryEntries = append(processHistoryEntries, processHistory)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating process history rows: %w", err)
	}

	return processHistoryEntries, nil
}

func (db *DB) IsServiceRegistered(ctx context.Context, name string) (bool, error) {
	query := `
	SELECT COUNT(*) 
	FROM service_catalog 
	WHERE name = ?
	`

	var count int
	err := db.conn.QueryRowContext(ctx, query, name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("could not check if service is registered: %w", err)
	} else {
		return count > 0, nil
	}
}

func (db *DB) RemoveServiceCatalogEntry(ctx context.Context, name string) (bool, error) {
	result, err := db.conn.ExecContext(ctx, "DELETE FROM service_catalog WHERE name = ?", name)
	if err != nil {
		return false, fmt.Errorf("could not remove from service_catalog: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("could not check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return false, nil
	}

	return true, nil
}

func (db *DB) RemoveServiceInstance(ctx context.Context, name string) (bool, error) {
	result, err := db.conn.ExecContext(ctx, "DELETE FROM service_instances WHERE name = ?", name)
	if err != nil {
		return false, fmt.Errorf("could not remove from service_instances: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("could not check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return false, nil
	}

	return true, nil
}

func (db *DB) RemoveProcessHistoryEntryViaPid(ctx context.Context, pid int) (bool, error) {
	result, err := db.conn.ExecContext(ctx, "DELETE FROM process_history WHERE pid = ?", pid)
	if err != nil {
		return false, fmt.Errorf("could not remove from service_instances: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("could not check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return false, nil
	}

	return true, nil
}

type ProcessHistoryUpdate struct {
	Error     *string
	StartedAt *time.Time
	State     *types.ProcessState
	StoppedAt *time.Time
}

func (db *DB) UpdateProcessHistoryEntry(ctx context.Context, pid int, updates ProcessHistoryUpdate) error {
	setParts := []string{}
	args := []any{}
	requestedColumns := []string{}
	validColumns := map[string]bool{"error": true, "started_at": true, "state": true, "stopped_at": true, "updated_at": true}

	if updates.Error != nil {
		requestedColumns = append(requestedColumns, "error")
		setParts = append(setParts, "error = ?")
		args = append(args, *updates.Error)
	}

	if updates.StartedAt != nil {
		requestedColumns = append(requestedColumns, "started_at")
		setParts = append(setParts, "started_at = ?")
		args = append(args, *updates.StartedAt)
	}

	if updates.State != nil {
		requestedColumns = append(requestedColumns, "state")
		setParts = append(setParts, "state = ?")
		args = append(args, *updates.State)
	}

	if updates.StoppedAt != nil {
		requestedColumns = append(requestedColumns, "stopped_at")
		setParts = append(setParts, "stopped_at = ?")
		args = append(args, *updates.StoppedAt)
	}

	if len(setParts) == 0 {
		return fmt.Errorf("no fields to update")
	}

	requestedColumns = append(requestedColumns, "updated_at")
	setParts = append(setParts, "updated_at = ?")
	args = append(args, time.Now())

	for _, col := range requestedColumns {
		if !validColumns[col] {
			return fmt.Errorf("invalid column: %s", col)
		}
	}

	// #nosec G201 - column names are from a validated allowlist
	query := fmt.Sprintf("UPDATE process_history SET %s WHERE pid = ?",
		strings.Join(setParts, ", "))
	args = append(args, pid)

	result, err := db.conn.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("could not update process history entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("process history entry with '%v' not found", pid)
	}
	return nil
}

func (db *DB) UpdateServiceCatalogEntry(ctx context.Context, name string, newDirectoryPath string, newConfigFileName string) error {
	query := `
	UPDATE service_catalog
	SET path = ?, config_file = ?, created_at = ?
	WHERE name = ?
	`

	result, err := db.conn.ExecContext(ctx, query, newDirectoryPath, newConfigFileName, time.Now(), name)
	if err != nil {
		return fmt.Errorf("could not update service catalog entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("service '%s' not found", name)
	}
	return nil
}

type ServiceInstanceUpdate struct {
	RestartCount    *int
	LastHealthCheck *time.Time
	StartedAt       *time.Time
}

func (db *DB) UpdateServiceInstance(ctx context.Context, name string, updates ServiceInstanceUpdate) error {
	setParts := []string{}
	args := []any{}
	requestedColumns := []string{}
	validColumns := map[string]bool{"restart_count": true, "last_health_check": true, "started_at": true, "updated_at": true}

	if updates.RestartCount != nil {
		requestedColumns = append(requestedColumns, "restart_count")
		setParts = append(setParts, "restart_count = ?")
		args = append(args, *updates.RestartCount)
	}

	if updates.LastHealthCheck != nil {
		requestedColumns = append(requestedColumns, "last_health_check")
		setParts = append(setParts, "last_health_check = ?")
		args = append(args, *updates.LastHealthCheck)
	}

	if updates.StartedAt != nil {
		requestedColumns = append(requestedColumns, "started_at")
		setParts = append(setParts, "started_at = ?")
		args = append(args, *updates.StartedAt)
	}

	if len(setParts) == 0 {
		return fmt.Errorf("no fields to update")
	}

	requestedColumns = append(requestedColumns, "updated_at")
	setParts = append(setParts, "updated_at = ?")
	args = append(args, time.Now())

	for _, col := range requestedColumns {
		if !validColumns[col] {
			return fmt.Errorf("invalid column: %s", col)
		}
	}

	// #nosec G201 - column names are from a validated allowlis
	query := fmt.Sprintf("UPDATE service_instances SET %s WHERE name = ?",
		strings.Join(setParts, ", "))
	args = append(args, name)

	result, err := db.conn.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("could not update service	instance: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("service '%s' not found", name)
	}
	return nil
}
