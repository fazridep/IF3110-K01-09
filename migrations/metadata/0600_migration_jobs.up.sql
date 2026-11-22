CREATE TABLE migration_jobs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Urutan FIFO per database. created_at bisa kembar kalau dua job masuk bareng.
    seq             BIGINT      GENERATED ALWAYS AS IDENTITY,
    database_id     UUID        NOT NULL REFERENCES database_connections(id),
    migration_id    UUID        NOT NULL REFERENCES database_migrations(id),
    -- UNIQUE: satu approval cuma bisa jadi satu job. Kirim ulang atau pesan
    -- duplikat dapat job yang sama, bukan eksekusi kedua.
    approval_id     UUID        NOT NULL UNIQUE REFERENCES approval_requests(id),
    action          TEXT        NOT NULL,
    resumes_job_id  UUID        REFERENCES migration_jobs(id),
    status          TEXT        NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'success', 'failed', 'partial', 'uncertain', 'superseded')),
    attempt         INTEGER     NOT NULL DEFAULT 0,
    verify_attempts INTEGER     NOT NULL DEFAULT 0,
    max_attempts    INTEGER     NOT NULL,
    run_after       TIMESTAMPTZ NOT NULL DEFAULT now(),
    worker_id       TEXT        NOT NULL DEFAULT '',
    -- Token klaim. Worker yang lease-nya sudah diambil alih nggak bisa nulis lagi.
    lease_token     UUID,
    trace_context   JSONB       NOT NULL DEFAULT '{}',
    error           TEXT        NOT NULL DEFAULT '',
    batches_committed BIGINT    NOT NULL DEFAULT 0,
    rows_committed  BIGINT      NOT NULL DEFAULT 0,
    created_by      UUID        NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);

CREATE INDEX idx_migration_jobs_claim ON migration_jobs (seq)
    WHERE status IN ('queued', 'uncertain');
CREATE INDEX idx_migration_jobs_database ON migration_jobs (database_id, seq DESC);
CREATE INDEX idx_migration_jobs_active ON migration_jobs (database_id, seq)
    WHERE status IN ('queued', 'running', 'uncertain', 'partial');
