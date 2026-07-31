CREATE TABLE IF NOT EXISTS dependency_waits (
	service_name TEXT PRIMARY KEY,
	pending TEXT NOT NULL,
	since DATETIME NOT NULL
);
