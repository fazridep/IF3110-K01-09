ALTER TABLE database_migrations DROP CONSTRAINT database_migrations_status_check;
ALTER TABLE database_migrations ADD CONSTRAINT database_migrations_status_check
    CHECK (status IN ('draft', 'running', 'success', 'failed'));
ALTER TABLE database_migrations
    DROP COLUMN down_sql, DROP COLUMN irreversible, DROP COLUMN irreversible_reason,
    DROP COLUMN mode, DROP COLUMN batch_table, DROP COLUMN batch_key,
    DROP COLUMN batch_size, DROP COLUMN batch_backup;
