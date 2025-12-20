package database

import (
	"database/sql"
	"eos/internal/types"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Database interface {
	CloseDBConnection() error

	GetServiceInstance(name string) (types.ServiceRuntime, error)
	RegisterServiceInstance(name string) error
	RemoveServiceInstance(name string) (bool, error)
	UpdateServiceInstance(name string, updates ServiceInstanceUpdate) error

	GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error)
	GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error)
	IsServiceRegistered(name string) (bool, error)
	RegisterService(name string, directoryPath string, configFileName string) error
	RemoveServiceCatalogEntry(name string) (bool, error)
	UpdateServiceCatalogEntry(name string, newDirectoryPath string, newConfigFileName string) error

	GetProcessHistoryEntriesByServiceName(serviceName string) ([]types.ProcessHistory, error)
	GetProcessHistoryEntryByPid(pid int) (types.ProcessHistory, error)
	RegisterProcessHistoryEntry(pid int, serviceName string, state types.ProcessState) (types.ProcessHistory, error)
	RemoveProcessHistoryEntryViaPid(pid int) (bool, error)
	UpdateProcessHistoryEntry(pid int, updates ProcessHistoryUpdate) error
}

var _ Database = (*DB)(nil)

func NewDB() (*DB, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not get user home directory: %w", err)
	}

	deployDir := filepath.Join(homeDir, ".eos")
	err = os.MkdirAll(deployDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("could not create eos directory: %w", err)
	}

	dbPath := filepath.Join(deployDir, "state.db")

	return openDB(dbPath)
}

func NewTestDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, fmt.Errorf("could not create test db directory: %w", err)
	}

	return openDB(dbPath)
}

func openDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	db := &DB{conn: conn}

	err = db.initTables()

	if err != nil {
		return nil, fmt.Errorf("could not initialize tables: %w", err)
	}

	return db, nil
}

func (db *DB) CloseDBConnection() error {
	return db.conn.Close()
}

func (db *DB) initTables() error {
	serviceCatalogSQL := `
	CREATE TABLE IF NOT EXISTS service_catalog (
		name TEXT PRIMARY KEY,
		path TEXT NOT NULL,
  		config_file TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);`

	serviceInstancesSQL := `
	CREATE TABLE IF NOT EXISTS service_instances (
		name TEXT PRIMARY KEY,
		restart_count INTEGER default 0,
		last_health_check DATETIME,
		created_at DATETIME NOT NULL,
		started_at DATETIME,
		updated_at DATETIME
	);`

	processHistorySQL := `
	CREATE TABLE IF NOT EXISTS process_history (
		pid INTEGER DEFAULT 0 PRIMARY KEY,
		service_name TEXT NOT NULL,
		state TEXT NOT NULL DEFAULT 'stopped',
		error TEXT,
		created_at DATETIME NOT NULL,
		started_at DATETIME,
		stopped_at DATETIME,
		updated_at DATETIME,
		FOREIGN KEY (service_name) REFERENCES service_instances(name)
	);`

	indexProcessesLookup := `CREATE INDEX IF NOT EXISTS idx_processes_lookup ON process_history(service_name, stopped_at);`

	_, err := db.conn.Exec(serviceCatalogSQL)
	if err != nil {
		return fmt.Errorf("could not create service_catalog table: %w", err)
	}

	_, err = db.conn.Exec(serviceInstancesSQL)
	if err != nil {
		return fmt.Errorf("could not create service_instances table: %w", err)
	}

	_, err = db.conn.Exec(processHistorySQL)
	if err != nil {
		return fmt.Errorf("could not create process_history table: %w", err)
	}

	_, err = db.conn.Exec(indexProcessesLookup)
	if err != nil {
		return fmt.Errorf("could not create process_history index: %w", err)
	}

	return nil
}

func (db *DB) RegisterService(name string, directoryPath string, configFileName string) error {
	catalogQuery := `
	INSERT OR REPLACE INTO service_catalog (name, path, config_file, created_at)
	VALUES (?, ?, ?, ?)
	`

	_, err := db.conn.Exec(catalogQuery, name, directoryPath, configFileName, time.Now())
	if err != nil {
		return fmt.Errorf("could not register service into service_catalog: %w", err)
	}

	return nil
}

func (db *DB) RegisterServiceInstance(name string) error {
	instanceQuery := `
	INSERT OR REPLACE INTO service_instances (name, created_at)
	VALUES (?, ?)
	`

	_, err := db.conn.Exec(instanceQuery, name, time.Now())
	if err != nil {
		return fmt.Errorf("could not create service instance entry: %w", err)
	}

	return nil
}

func (db *DB) RegisterProcessHistoryEntry(pid int, serviceName string, state types.ProcessState) (types.ProcessHistory, error) {
	instanceQuery := `
	INSERT INTO process_history (pid, service_name, state, created_at)
	VALUES (?, ?, ?, ?)
	`
	createdAt := time.Now()

	_, err := db.conn.Exec(instanceQuery, pid, serviceName, state, createdAt)
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

func (db *DB) GetAllServiceCatalogEntries() ([]types.ServiceCatalogEntry, error) {
	query := `
	SELECT name, path, config_file, created_at
	FROM service_catalog
	ORDER BY name
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("could not query registered services: %w", err)
	}
	defer rows.Close()

	var services []types.ServiceCatalogEntry
	for rows.Next() {
		var service types.ServiceCatalogEntry
		err := rows.Scan(&service.Name, &service.DirectoryPath, &service.ConfigFileName, &service.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("could not scan service row: %w", err)
		}
		services = append(services, service)
	}

	return services, nil
}

var ErrServiceNotFound = errors.New("service not found")

func (db *DB) GetServiceCatalogEntry(name string) (types.ServiceCatalogEntry, error) {
	query := `
	SELECT name, path, config_file, created_at
	FROM service_catalog
	WHERE name = ?
	`

	row := db.conn.QueryRow(query, name)
	var svc types.ServiceCatalogEntry

	err := row.Scan(&svc.Name, &svc.DirectoryPath, &svc.ConfigFileName, &svc.CreatedAt)
	if err == sql.ErrNoRows {
		return types.ServiceCatalogEntry{}, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
	} else if err != nil {
		return types.ServiceCatalogEntry{}, fmt.Errorf("could not scan service row: %w", err)
	} else {
		return svc, nil
	}
}

func (db *DB) GetServiceInstance(name string) (types.ServiceRuntime, error) {
	query := `
	SELECT name, restart_count, last_health_check, created_at, started_at, updated_at
	FROM service_instances
	WHERE name = ?
	`

	row := db.conn.QueryRow(query, name)
	var svc types.ServiceRuntime

	err := row.Scan(&svc.Name, &svc.RestartCount, &svc.LastHealthCheck, &svc.CreatedAt, &svc.StartedAt, &svc.UpdatedAt)
	if err == sql.ErrNoRows {
		return types.ServiceRuntime{}, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
	} else if err != nil {
		return types.ServiceRuntime{}, fmt.Errorf("could not scan service row: %w", err)
	} else {
		return svc, nil
	}
}

var ErrProcessHistoryNotFound = errors.New("process history not found")

func (db *DB) GetProcessHistoryEntryByPid(pid int) (types.ProcessHistory, error) {
	query := `
	SELECT pid, service_name, state, error, created_at, started_at, stopped_at
	FROM process_history
	WHERE pid = ?
	`

	row := db.conn.QueryRow(query, pid)
	var processHistory types.ProcessHistory

	err := row.Scan(&processHistory.PID,
		&processHistory.ServiceName,
		&processHistory.State,
		&processHistory.Error,
		&processHistory.CreatedAt,
		&processHistory.StartedAt,
		&processHistory.UpdatedAt)
	if err == sql.ErrNoRows {
		return types.ProcessHistory{}, fmt.Errorf("%w: %v", ErrProcessHistoryNotFound, pid)
	} else if err != nil {
		return types.ProcessHistory{}, fmt.Errorf("could not scan process history row: %w", err)
	} else {
		return processHistory, nil
	}
}

func (db *DB) GetProcessHistoryEntriesByServiceName(serviceName string) ([]types.ProcessHistory, error) {
	query := `
	SELECT pid, service_name, state, error, created_at, started_at, stopped_at
	FROM process_history
	WHERE service_name = ?
	ORDER BY pid
	`

	rows, err := db.conn.Query(query, serviceName)
	if err != nil {
		return nil, fmt.Errorf("could not query registered services: %w", err)
	}
	defer rows.Close()

	var processHistoryEntries []types.ProcessHistory
	for rows.Next() {
		var processHistory types.ProcessHistory
		err := rows.Scan(&processHistory.PID,
			&processHistory.ServiceName,
			&processHistory.State,
			&processHistory.Error,
			&processHistory.CreatedAt,
			&processHistory.StartedAt,
			&processHistory.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("could not scan process history row: %w", err)
		}
		processHistoryEntries = append(processHistoryEntries, processHistory)
	}

	return processHistoryEntries, nil
}

func (db *DB) IsServiceRegistered(name string) (bool, error) {
	query := `
	SELECT COUNT(*) 
	FROM service_catalog 
	WHERE name = ?
	`

	var count int
	err := db.conn.QueryRow(query, name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("could not check if service is registered: %w", err)
	} else {
		return count > 0, nil
	}
}

func (db *DB) RemoveServiceCatalogEntry(name string) (bool, error) {
	result, err := db.conn.Exec("DELETE FROM service_catalog WHERE name = ?", name)
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

func (db *DB) RemoveServiceInstance(name string) (bool, error) {
	result, err := db.conn.Exec("DELETE FROM service_instances WHERE name = ?", name)
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

func (db *DB) RemoveProcessHistoryEntryViaPid(pid int) (bool, error) {
	result, err := db.conn.Exec("DELETE FROM process_history WHERE pid = ?", pid)
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

func (db *DB) UpdateProcessHistoryEntry(pid int, updates ProcessHistoryUpdate) error {
	setParts := []string{}
	args := []any{}

	if updates.Error != nil {
		setParts = append(setParts, "error = ?")
		args = append(args, *updates.Error)
	}

	if updates.StartedAt != nil {
		setParts = append(setParts, "started_at = ?")
		args = append(args, *updates.StartedAt)
	}

	if updates.State != nil {
		setParts = append(setParts, "state = ?")
		args = append(args, *updates.State)
	}

	if updates.StoppedAt != nil {
		setParts = append(setParts, "stopped_at = ?")
		args = append(args, *updates.StoppedAt)
	}

	if len(setParts) == 0 {
		return fmt.Errorf("no fields to update")
	}

	setParts = append(setParts, "updated_at = ?")
	args = append(args, time.Now())

	query := fmt.Sprintf("UPDATE process_history SET %s WHERE pid = ?",
		strings.Join(setParts, ", "))
	args = append(args, pid)

	result, err := db.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("could not update process history entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check update result: %w", err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("process history entry with '%v' not found", pid)
	} else {
		return nil
	}
}

func (db *DB) UpdateServiceCatalogEntry(name string, newDirectoryPath string, newConfigFileName string) error {
	query := `
	UPDATE service_catalog
	SET path = ?, config_file = ?, created_at = ?
	WHERE name = ?
	`

	result, err := db.conn.Exec(query, newDirectoryPath, newConfigFileName, time.Now(), name)
	if err != nil {
		return fmt.Errorf("could not update service catalog entry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check update result: %w", err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("service '%s' not found", name)
	} else {
		return nil
	}
}

type ServiceInstanceUpdate struct {
	RestartCount    *int
	LastHealthCheck *time.Time
	StartedAt       *time.Time
}

func (db *DB) UpdateServiceInstance(name string, updates ServiceInstanceUpdate) error {
	setParts := []string{}
	args := []any{}

	if updates.RestartCount != nil {
		setParts = append(setParts, "restart_count = ?")
		args = append(args, *updates.RestartCount)
	}

	if updates.LastHealthCheck != nil {
		setParts = append(setParts, "last_health_check = ?")
		args = append(args, *updates.LastHealthCheck)
	}

	if updates.StartedAt != nil {
		setParts = append(setParts, "started_at = ?")
		args = append(args, *updates.StartedAt)
	}

	if len(setParts) == 0 {
		return fmt.Errorf("no fields to update")
	}

	setParts = append(setParts, "updated_at = ?")
	args = append(args, time.Now())

	query := fmt.Sprintf("UPDATE service_instances SET %s WHERE name = ?",
		strings.Join(setParts, ", "))
	args = append(args, name)

	result, err := db.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("could not update service	instance: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check update result: %w", err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("service '%s' not found", name)
	} else {
		return nil
	}
}
