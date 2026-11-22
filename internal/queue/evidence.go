package queue

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/platform/pgerr"
)

// Bukti commit disimpan di database target, bukan metadata, supaya bisa masuk
// transaction yang sama dengan SQL migration. Kalau transaction-nya commit,
// buktinya ada. Kalau rollback, buktinya juga hilang. Ini satu-satunya cara
// menjawab "sudah ke-commit atau belum" setelah koneksi putus saat COMMIT.
// Schema ini disembunyikan dari viewer (dbmgmt.Schemas).
const evidenceTable = "_dbplatform.job_commits"

// RecordCommit dipanggil Executor di dalam transaction SQL migration, sebelum
// commit. Kalau job yang sama sudah pernah commit, hasilnya ErrAlreadyCommitted
// dan transaction harus dibatalkan.
func RecordCommit(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) error {
	if err := ensureEvidenceTable(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO `+evidenceTable+` (job_id) VALUES ($1)`, jobID)
	if pgerr.IsUniqueViolation(err) {
		return ErrAlreadyCommitted
	}
	if err != nil {
		return fmt.Errorf("queue: catat bukti commit: %w", err)
	}
	return nil
}

// ensureEvidenceTable pakai advisory lock karena CREATE ... IF NOT EXISTS yang
// jalan bareng tetap bisa kena unique violation di catalog.
func ensureEvidenceTable(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, evidenceTable).Scan(&exists); err != nil {
		return fmt.Errorf("queue: cek tabel bukti: %w", err)
	}
	if exists {
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('_dbplatform.setup'))`)
	if err == nil {
		_, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS _dbplatform`)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+evidenceTable+` (
			job_id       UUID        PRIMARY KEY,
			committed_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	}
	if err != nil {
		return fmt.Errorf("queue: siapkan tabel bukti: %w", err)
	}
	return nil
}

func CommitRecorded(ctx context.Context, db *pgxpool.Pool, jobID uuid.UUID) (bool, error) {
	var found bool
	err := db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, evidenceTable).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("queue: cek tabel bukti: %w", err)
	}
	if !found {
		return false, nil
	}
	err = db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+evidenceTable+` WHERE job_id = $1)`, jobID).Scan(&found)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("queue: baca bukti commit: %w", err)
	}
	return found, nil
}
