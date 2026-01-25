CREATE TABLE IF NOT EXISTS service_catalog (
	name TEXT PRIMARY KEY,
	path TEXT NOT NULL,
	config_file TEXT NOT NULL,
	created_at DATETIME NOT NULL
);


CREATE TABLE IF NOT EXISTS service_instances (
	name TEXT PRIMARY KEY,
	restart_count INTEGER default 0,
	last_health_check DATETIME,
	created_at DATETIME NOT NULL,
	started_at DATETIME,
	updated_at DATETIME
);

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
);

CREATE INDEX IF NOT EXISTS idx_processes_lookup ON process_history(service_name, stopped_at);