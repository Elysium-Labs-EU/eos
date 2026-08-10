ALTER TABLE service_instances ADD COLUMN failure_loop_count INTEGER default 0;
ALTER TABLE service_instances ADD COLUMN failure_signature TEXT default '';
