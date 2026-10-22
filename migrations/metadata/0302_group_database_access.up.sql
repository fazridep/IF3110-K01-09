-- Level akses: 'view_only' atau 'view_edit'.
-- Disimpan sebagai TEXT supaya mudah dibaca di query dan nggak perlu enum
-- yang sulit di-alter belakangan.

CREATE TABLE group_database_access (
    group_id    UUID        NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    database_id UUID        NOT NULL,
    level       TEXT        NOT NULL CHECK (level IN ('view_only', 'view_edit')),
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, database_id)
);

CREATE INDEX idx_group_database_access_database_id ON group_database_access (database_id);
