ALTER TABLE groups
    ADD COLUMN leader_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_groups_leader_id ON groups (leader_id) WHERE leader_id IS NOT NULL;

