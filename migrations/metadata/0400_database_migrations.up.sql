CREATE TABLE database_migrations (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID        NOT NULL REFERENCES database_connections(id),
    version     INTEGER     NOT NULL,
    description TEXT        NOT NULL,
    sql_text    TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'running', 'success', 'failed')),
    error       TEXT,
    created_by  UUID        NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    executed_by UUID        REFERENCES users(id),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    UNIQUE (database_id, version)
);

-- Per database cuma boleh ada satu migration yang belum selesai, biar riwayatnya linear.
CREATE UNIQUE INDEX idx_database_migrations_one_pending
    ON database_migrations (database_id)
    WHERE status IN ('draft', 'running');
