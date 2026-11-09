-- Satu baris per eksekusi (apply, forward, rollback), termasuk yang gagal atau
-- masih partial. Eksekusi batch bisa dikerjakan lebih dari satu job (retry atau
-- dilanjutkan), jadi ID eksekusi beda dengan ID job.
CREATE TABLE migration_executions (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    seq                  BIGINT      GENERATED ALWAYS AS IDENTITY,
    migration_id         UUID        NOT NULL REFERENCES database_migrations(id),
    database_id          UUID        NOT NULL REFERENCES database_connections(id),
    action               TEXT        NOT NULL CHECK (action IN ('apply', 'forward', 'rollback')),
    -- Rollback menunjuk eksekusi up yang dibatalkannya.
    reverts_execution_id UUID        REFERENCES migration_executions(id),
    -- Job terakhir yang mengerjakan. Tanpa FK karena migration_jobs dibuat di 0600.
    job_id               UUID        NOT NULL,
    status               TEXT        NOT NULL DEFAULT 'running'
                         CHECK (status IN ('running', 'success', 'failed', 'partial')),
    sql_text             TEXT        NOT NULL,
    mode                 TEXT        NOT NULL,
    batch_size           INTEGER     NOT NULL DEFAULT 0,
    batches_committed    BIGINT      NOT NULL DEFAULT 0,
    rows_committed       BIGINT      NOT NULL DEFAULT 0,
    last_cursor          TEXT        NOT NULL DEFAULT '',
    error                TEXT        NOT NULL DEFAULT '',
    -- true sejak SQL migration pertama kali dikirim ke database target. Apply
    -- yang gagal sebelum itu mengembalikan migration ke draft.
    sql_started          BOOLEAN     NOT NULL DEFAULT false,
    executed_by          UUID        NOT NULL REFERENCES users(id),
    started_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at          TIMESTAMPTZ
);

CREATE INDEX idx_migration_executions_database ON migration_executions (database_id, seq);
CREATE INDEX idx_migration_executions_job ON migration_executions (job_id);

-- Eksekusi up yang sedang berlaku. Ini yang dibatalkan kalau migration di-rollback.
ALTER TABLE database_migrations
    ADD COLUMN up_execution_id UUID REFERENCES migration_executions(id);
