ALTER TABLE process_history ADD COLUMN peak_rss_memory_kb BIGINT NOT NULL DEFAULT 0 CHECK (peak_rss_memory_kb >= 0);
