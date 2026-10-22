-- Koneksi ke database target yang dikelola Platform Admin.
-- Tabel ini dirujuk oleh group_database_access (0300) dan database_migrations (0400),
-- jadi harus dibuat lebih dulu.
CREATE TABLE database_connections (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT        NOT NULL UNIQUE,
    host         TEXT        NOT NULL,
    port         INTEGER     NOT NULL DEFAULT 5432 CHECK (port BETWEEN 1 AND 65535),
    db_name      TEXT        NOT NULL,
    username     TEXT        NOT NULL,
    -- Password dienkripsi AES-GCM, lihat internal/dbmgmt/credential.go.
    -- Kuncinya dari CREDENTIAL_ENCRYPTION_KEY, bukan dari database.
    password_enc BYTEA       NOT NULL,
    sslmode      TEXT        NOT NULL DEFAULT 'prefer',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
