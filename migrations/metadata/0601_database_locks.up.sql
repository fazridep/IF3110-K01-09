-- Satu database cuma boleh menjalankan satu migration. Lock berupa lease:
-- worker memperpanjang expires_at selama job jalan. Kalau worker mati, lease
-- habis sendiri dan job-nya dipulihkan worker lain (ADR-013).
CREATE TABLE database_locks (
    database_id UUID        PRIMARY KEY REFERENCES database_connections(id),
    job_id      UUID        NOT NULL REFERENCES migration_jobs(id),
    worker_id   TEXT        NOT NULL,
    lease_token UUID        NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
