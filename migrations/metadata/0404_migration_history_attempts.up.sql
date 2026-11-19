-- Spesifikasi v4: migration history mencatat semua attempt, berhasil maupun
-- gagal, bukan cuma eksekusi yang committed. Satu baris per attempt job, tetap
-- append-only lewat trigger dari 0403.
ALTER TABLE migration_history DROP CONSTRAINT IF EXISTS migration_history_execution_id_key;
ALTER TABLE migration_history RENAME COLUMN committed_at TO finished_at;
ALTER TABLE migration_history
    ADD COLUMN job_id  UUID,
    ADD COLUMN attempt INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN status  TEXT    NOT NULL DEFAULT 'success'
                       CHECK (status IN ('success', 'failed', 'partial', 'uncertain')),
    ADD COLUMN error   TEXT    NOT NULL DEFAULT '';
ALTER TABLE migration_history ALTER COLUMN attempt DROP DEFAULT, ALTER COLUMN status DROP DEFAULT;

-- Baris lama (semuanya sukses) diisi job-nya dari eksekusi.
ALTER TABLE migration_history DISABLE TRIGGER migration_history_append_only;
UPDATE migration_history h SET job_id = x.job_id FROM migration_executions x WHERE x.id = h.execution_id;
ALTER TABLE migration_history ENABLE TRIGGER migration_history_append_only;
ALTER TABLE migration_history ALTER COLUMN job_id SET NOT NULL;

-- Finalize yang dipanggil ulang nggak boleh bikin baris kedua untuk attempt yang sama.
CREATE UNIQUE INDEX idx_migration_history_attempt ON migration_history (job_id, attempt, status);
CREATE INDEX idx_migration_history_execution ON migration_history (execution_id, seq);
