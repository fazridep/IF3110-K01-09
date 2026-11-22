CREATE TABLE approval_requests (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id     UUID        NOT NULL REFERENCES database_migrations(id) ON DELETE CASCADE,
    database_id      UUID        NOT NULL REFERENCES database_connections(id) ON DELETE CASCADE,
    action           TEXT        NOT NULL DEFAULT 'apply'
                     CHECK (action IN ('apply', 'rollback', 'forward', 'batch')),
    status           TEXT        NOT NULL DEFAULT 'pending_review'
                     CHECK (status IN ('pending_review', 'approved', 'rejected', 'cancelled')),
    requested_by     UUID        NOT NULL REFERENCES users(id),
    rejection_reason TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_approval_requests_one_pending
    ON approval_requests (migration_id)
    WHERE status = 'pending_review';

CREATE INDEX idx_approval_requests_status ON approval_requests (status, created_at DESC);
CREATE INDEX idx_approval_requests_database_id ON approval_requests (database_id);

CREATE TABLE approval_group_decisions (
    request_id UUID        NOT NULL REFERENCES approval_requests(id) ON DELETE CASCADE,
    group_id   UUID        NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    decision   TEXT        NOT NULL DEFAULT 'pending_review'
               CHECK (decision IN ('pending_review', 'approved', 'rejected')),
    decided_by UUID        REFERENCES users(id),
    note       TEXT        NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ,
    PRIMARY KEY (request_id, group_id)
);

CREATE INDEX idx_approval_group_decisions_group_id ON approval_group_decisions (group_id);

