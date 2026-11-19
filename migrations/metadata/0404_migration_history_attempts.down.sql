DROP INDEX idx_migration_history_execution;
DROP INDEX idx_migration_history_attempt;
ALTER TABLE migration_history DISABLE TRIGGER migration_history_append_only;
DELETE FROM migration_history WHERE status <> 'success';
ALTER TABLE migration_history ENABLE TRIGGER migration_history_append_only;
ALTER TABLE migration_history DROP COLUMN job_id, DROP COLUMN attempt, DROP COLUMN status, DROP COLUMN error;
ALTER TABLE migration_history RENAME COLUMN finished_at TO committed_at;
ALTER TABLE migration_history ADD CONSTRAINT migration_history_execution_id_key UNIQUE (execution_id);
