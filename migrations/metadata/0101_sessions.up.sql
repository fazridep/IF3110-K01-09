-- Tabel sessions: session stateful server-side dan menyimpan token hash SHA-256
CREATE TABLE sessions (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash           BYTEA       NOT NULL UNIQUE,
    previous_token_hash  BYTEA,
    rotated_at           TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_accessed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at           TIMESTAMPTZ NOT NULL,
    revoked_at           TIMESTAMPTZ
);

CREATE INDEX idx_sessions_token_hash          ON sessions (token_hash);
CREATE INDEX idx_sessions_previous_token_hash ON sessions (previous_token_hash) WHERE previous_token_hash IS NOT NULL;
CREATE INDEX idx_sessions_user_id             ON sessions (user_id);
CREATE INDEX idx_sessions_expires_at          ON sessions (expires_at);