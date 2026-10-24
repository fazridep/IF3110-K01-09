-- disabled_at diisi waktu Platform Admin menonaktifkan user. NULL berarti aktif.
ALTER TABLE users ADD COLUMN disabled_at TIMESTAMPTZ;
