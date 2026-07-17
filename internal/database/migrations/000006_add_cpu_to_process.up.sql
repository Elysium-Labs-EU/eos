ALTER TABLE process_history ADD COLUMN cpu_percent REAL NOT NULL DEFAULT 0 CHECK (cpu_percent >= 0);
