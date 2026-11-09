-- Migration history: cuma eksekusi yang berhasil di-commit. Isinya salinan, jadi
-- tetap utuh walaupun data lain berubah. Baris yang sudah masuk nggak bisa
-- diubah atau dihapus.
CREATE TABLE migration_history (
    seq                  BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    execution_id         UUID        NOT NULL UNIQUE REFERENCES migration_executions(id),
    migration_id         UUID        NOT NULL REFERENCES database_migrations(id),
    database_id          UUID        NOT NULL REFERENCES database_connections(id),
    version              INTEGER     NOT NULL,
    action               TEXT        NOT NULL,
    reverts_execution_id UUID        REFERENCES migration_executions(id),
    sql_text             TEXT        NOT NULL,
    mode                 TEXT        NOT NULL,
    batches              BIGINT      NOT NULL DEFAULT 0,
    rows_affected        BIGINT      NOT NULL DEFAULT 0,
    executed_by          UUID        NOT NULL REFERENCES users(id),
    started_at           TIMESTAMPTZ NOT NULL,
    committed_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_migration_history_database ON migration_history (database_id, seq);

CREATE FUNCTION migration_history_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'migration_history nggak boleh diubah atau dihapus';
END
$$;

CREATE TRIGGER migration_history_append_only
    BEFORE UPDATE OR DELETE ON migration_history
    FOR EACH ROW EXECUTE FUNCTION migration_history_append_only();
