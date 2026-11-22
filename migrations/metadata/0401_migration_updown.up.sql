-- Milestone 2: SQL down, tanda irreversible, mode batch, dan status rollback.
ALTER TABLE database_migrations
    ADD COLUMN down_sql            TEXT    NOT NULL DEFAULT '',
    ADD COLUMN irreversible        BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN irreversible_reason TEXT    NOT NULL DEFAULT '',
    ADD COLUMN mode                TEXT    NOT NULL DEFAULT 'normal'
                                   CHECK (mode IN ('normal', 'batch')),
    -- Mode batch: tabel yang dipecah, kolom kursor (unik, nggak berubah), ukuran
    -- batch, dan apakah isi row disimpan dulu buat rollback per batch.
    ADD COLUMN batch_table         TEXT    NOT NULL DEFAULT '',
    ADD COLUMN batch_key           TEXT    NOT NULL DEFAULT '',
    ADD COLUMN batch_size          INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN batch_backup        BOOLEAN NOT NULL DEFAULT false;

-- Migration M1 yang sudah jalan nggak punya SQL down, jadi nggak bisa di-rollback.
UPDATE database_migrations
SET irreversible = true, irreversible_reason = 'Dibuat sebelum ada SQL down.'
WHERE status <> 'draft';

ALTER TABLE database_migrations DROP CONSTRAINT database_migrations_status_check;
ALTER TABLE database_migrations ADD CONSTRAINT database_migrations_status_check
    CHECK (status IN ('draft', 'running', 'success', 'failed', 'rolled_back', 'partial', 'partially_reverted'));
