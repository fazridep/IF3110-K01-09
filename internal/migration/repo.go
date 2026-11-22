package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/if3110/tubes-db-platform/internal/platform/pgerr"
)

// querier dipenuhi *pgxpool.Pool dan pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// selectMigration mengambil migration beserta username pembuat dan pelaksananya.
const selectMigration = `
	SELECT m.id, m.database_id, m.version, m.description, m.sql_text, m.down_sql,
		m.irreversible, m.irreversible_reason, m.mode, m.batch_table, m.batch_key,
		m.batch_size, m.batch_backup, m.status, COALESCE(m.error, ''),
		c.username, m.created_at, m.updated_at, COALESCE(e.username, ''),
		m.started_at, m.finished_at, m.up_execution_id
	FROM database_migrations m
	JOIN users c ON c.id = m.created_by
	LEFT JOIN users e ON e.id = m.executed_by`

func scanMigration(row pgx.Row) (*Migration, error) {
	m := &Migration{}
	err := row.Scan(&m.ID, &m.DatabaseID, &m.Version, &m.Description, &m.UpSQL, &m.DownSQL,
		&m.Irreversible, &m.IrreversibleReason, &m.Mode, &m.Batch.Table, &m.Batch.Key,
		&m.Batch.Size, &m.Batch.Backup, &m.Status, &m.Error,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.ExecutedBy,
		&m.StartedAt, &m.FinishedAt, &m.UpExecutionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: baca data: %w", err)
	}
	return m, nil
}

func findByID(ctx context.Context, q querier, id uuid.UUID) (*Migration, error) {
	return scanMigration(q.QueryRow(ctx, selectMigration+` WHERE m.id=$1`, id))
}

// lockByID dipakai di dalam transaction supaya perubahan status nggak balapan.
func lockByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Migration, error) {
	return scanMigration(tx.QueryRow(ctx, selectMigration+` WHERE m.id=$1 FOR UPDATE OF m`, id))
}

func listByDatabase(ctx context.Context, q querier, databaseID uuid.UUID) ([]Migration, error) {
	rows, err := q.Query(ctx, selectMigration+` WHERE m.database_id=$1 ORDER BY m.version DESC`, databaseID)
	if err != nil {
		return nil, fmt.Errorf("migration: daftar: %w", err)
	}
	defer rows.Close()

	var list []Migration
	for rows.Next() {
		m, err := scanMigration(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *m)
	}
	return list, rows.Err()
}

// insertDraft memberi versi berikutnya (versi terakhir + 1). Kalau dua request
// bikin draft bareng, salah satunya kena unique constraint dan dapat
// ErrPendingExists.
func (s *Service) insertDraft(ctx context.Context, databaseID, userID uuid.UUID, d Definition) (*Migration, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO database_migrations (database_id, version, description, sql_text, down_sql,
			irreversible, irreversible_reason, mode, batch_table, batch_key, batch_size, batch_backup, created_by)
		SELECT $1, COALESCE(MAX(version), 0) + 1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		FROM database_migrations WHERE database_id=$1
		RETURNING id`,
		databaseID, d.Description, d.UpSQL, d.DownSQL, d.Irreversible, d.IrreversibleReason,
		d.Mode, d.Batch.Table, d.Batch.Key, d.Batch.Size, d.Batch.Backup, userID).Scan(&id)
	if pgerr.IsUniqueViolation(err) {
		return nil, ErrPendingExists
	}
	if err != nil {
		return nil, fmt.Errorf("migration: simpan draft: %w", err)
	}
	return findByID(ctx, s.db, id)
}

// updateDraft mengubah draft. Approval yang belum dipakai untuk draft ini
// dibatalkan, karena yang disetujui leader adalah SQL lama. Kalau sudah ada job
// yang menunggu atau berjalan, draft nggak boleh diubah.
func (s *Service) updateDraft(ctx context.Context, id uuid.UUID, d Definition) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migration: begin update: %w", err)
	}
	defer tx.Rollback(ctx)

	m, err := lockByID(ctx, tx, id)
	if err != nil {
		return err
	}
	if m.Status != StatusDraft {
		return ErrNotDraft
	}
	var busy bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM migration_jobs
		WHERE migration_id = $1 AND status IN ('queued', 'running', 'uncertain'))`, id).Scan(&busy)
	if err != nil {
		return fmt.Errorf("migration: cek job: %w", err)
	}
	if busy {
		return ErrBusy
	}
	if _, err := tx.Exec(ctx, `
		UPDATE approval_requests SET status = 'cancelled', updated_at = now(), resolved_at = now()
		WHERE migration_id = $1 AND status IN ('pending_review', 'approved')
		  AND NOT EXISTS (SELECT 1 FROM migration_jobs j WHERE j.approval_id = approval_requests.id)`, id); err != nil {
		return fmt.Errorf("migration: batalkan approval lama: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE database_migrations SET description=$2, sql_text=$3, down_sql=$4, irreversible=$5,
			irreversible_reason=$6, mode=$7, batch_table=$8, batch_key=$9, batch_size=$10,
			batch_backup=$11, updated_at=$12
		WHERE id=$1`,
		id, d.Description, d.UpSQL, d.DownSQL, d.Irreversible, d.IrreversibleReason, d.Mode,
		d.Batch.Table, d.Batch.Key, d.Batch.Size, d.Batch.Backup, s.now()); err != nil {
		return fmt.Errorf("migration: update draft: %w", err)
	}
	return tx.Commit(ctx)
}

const selectExecution = `
	SELECT x.id, x.migration_id, x.database_id, m.version, m.description, x.action,
		x.reverts_execution_id, x.job_id, x.status, x.sql_text, x.mode, x.batch_size,
		x.batches_committed, x.rows_committed, x.last_cursor, x.error, x.sql_started, u.username,
		x.started_at, x.finished_at, h.finished_at IS NOT NULL, h.finished_at
	FROM migration_executions x
	JOIN database_migrations m ON m.id = x.migration_id
	JOIN users u ON u.id = x.executed_by
	LEFT JOIN LATERAL (
		SELECT finished_at FROM migration_history
		WHERE execution_id = x.id AND status = 'success'
		LIMIT 1
	) h ON true`

func scanExecution(row pgx.Row) (*Execution, error) {
	e := &Execution{}
	err := row.Scan(&e.ID, &e.MigrationID, &e.DatabaseID, &e.Version, &e.Description, &e.Action,
		&e.RevertsExecutionID, &e.JobID, &e.Status, &e.SQL, &e.Mode, &e.BatchSize,
		&e.Batches, &e.Rows, &e.Cursor, &e.Error, &e.SQLStarted, &e.ExecutedBy,
		&e.StartedAt, &e.FinishedAt, &e.Committed, &e.CommittedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: baca eksekusi: %w", err)
	}
	return e, nil
}

func executionByJob(ctx context.Context, q querier, jobID uuid.UUID) (*Execution, error) {
	return scanExecution(q.QueryRow(ctx, selectExecution+` WHERE x.job_id=$1 ORDER BY x.seq DESC LIMIT 1`, jobID))
}

// openExecution eksekusi migration ini yang belum tuntas: masih running atau
// partial, dan belum dibatalkan oleh rollback yang sukses.
func openExecution(ctx context.Context, q querier, migrationID uuid.UUID) (*Execution, error) {
	e, err := scanExecution(q.QueryRow(ctx, selectExecution+`
		WHERE x.migration_id=$1 AND x.status IN ('running', 'partial')
		  AND NOT EXISTS (SELECT 1 FROM migration_executions r
			WHERE r.reverts_execution_id = x.id AND r.status = 'success')
		ORDER BY x.seq DESC LIMIT 1`, migrationID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return e, err
}

type newExecution struct {
	migration *Migration
	action    string
	reverts   *uuid.UUID
	jobID     uuid.UUID
	userID    uuid.UUID
	sql       string
}

func insertExecution(ctx context.Context, tx pgx.Tx, in newExecution) (*Execution, error) {
	var id uuid.UUID
	m := in.migration
	err := tx.QueryRow(ctx, `
		INSERT INTO migration_executions (migration_id, database_id, action, reverts_execution_id,
			job_id, sql_text, mode, batch_size, executed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		m.ID, m.DatabaseID, in.action, in.reverts, in.jobID, in.sql, m.Mode, m.Batch.Size, in.userID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("migration: catat eksekusi: %w", err)
	}
	return scanExecution(tx.QueryRow(ctx, selectExecution+` WHERE x.id=$1`, id))
}

// takeOver memindahkan eksekusi ke job baru (retry dengan job lain atau
// melanjutkan batch).
func takeOver(ctx context.Context, tx pgx.Tx, e *Execution, jobID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE migration_executions SET job_id=$2, status='running', error='', updated_at=now()
		WHERE id=$1`, e.ID, jobID)
	if err != nil {
		return fmt.Errorf("migration: lanjutkan eksekusi: %w", err)
	}
	e.JobID, e.Status = jobID, ExecRunning
	return nil
}

// markSQLStarted dicatat sebelum SQL migration dikirim ke database target, jadi
// kalau worker mati di tengah SQL, tandanya sudah ada.
func (s *Service) markSQLStarted(ctx context.Context, x *Execution) error {
	if x.SQLStarted {
		return nil
	}
	if _, err := s.db.Exec(ctx, `UPDATE migration_executions SET sql_started=true, updated_at=now() WHERE id=$1`, x.ID); err != nil {
		return fmt.Errorf("migration: tandai SQL mulai: %w", err)
	}
	x.SQLStarted = true
	return nil
}

func (s *Service) saveProgress(ctx context.Context, executionID uuid.UUID, batches, rows int64, cursor string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE migration_executions SET batches_committed=$2, rows_committed=$3, last_cursor=$4, updated_at=now()
		WHERE id=$1`, executionID, batches, rows, cursor)
	if err != nil {
		return fmt.Errorf("migration: simpan progres: %w", err)
	}
	return nil
}

// HistoryFilter: MigrationID opsional buat riwayat satu migration saja.
type HistoryFilter struct {
	MigrationID  *uuid.UUID
	HideRollback bool
}

func listExecutions(ctx context.Context, q querier, databaseID uuid.UUID, f HistoryFilter) ([]Execution, error) {
	rows, err := q.Query(ctx, selectExecution+`
		WHERE x.database_id=$1 AND ($2::uuid IS NULL OR x.migration_id=$2)
		ORDER BY x.seq`, databaseID, f.MigrationID)
	if err != nil {
		return nil, fmt.Errorf("migration: daftar eksekusi: %w", err)
	}
	defer rows.Close()

	var list []Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *e)
	}
	return list, rows.Err()
}

// insertHistory menambah satu baris attempt ke migration_history. Pelaksana
// attempt adalah pembuat job-nya, bukan pembuat eksekusi pertama.
func insertHistory(ctx context.Context, q querier, executionID uuid.UUID, a attemptRecord) error {
	_, err := q.Exec(ctx, `
		INSERT INTO migration_history (execution_id, migration_id, database_id, version, action,
			reverts_execution_id, sql_text, mode, batches, rows_affected, executed_by, started_at,
			finished_at, job_id, attempt, status, error)
		SELECT x.id, x.migration_id, x.database_id, m.version, x.action, x.reverts_execution_id,
			x.sql_text, x.mode, x.batches_committed, x.rows_committed, $2, x.started_at,
			$3, $4, $5, $6, $7
		FROM migration_executions x
		JOIN database_migrations m ON m.id = x.migration_id
		WHERE x.id = $1
		ON CONFLICT (job_id, attempt, status) DO NOTHING`,
		executionID, a.userID, a.at, a.jobID, a.attempt, a.status, a.err)
	if err != nil {
		return fmt.Errorf("migration: catat history: %w", err)
	}
	return nil
}

type attemptRecord struct {
	jobID   uuid.UUID
	attempt int
	userID  uuid.UUID
	status  AttemptStatus
	err     string
	at      time.Time
}

// listAttempts mengambil attempt dari migration_history untuk eksekusi-eksekusi ini.
func listAttempts(ctx context.Context, q querier, executionIDs []uuid.UUID) (map[uuid.UUID][]Attempt, error) {
	out := map[uuid.UUID][]Attempt{}
	if len(executionIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT h.execution_id, h.job_id, h.attempt, h.status, h.error, h.batches, h.rows_affected,
			u.username, h.finished_at
		FROM migration_history h
		JOIN users u ON u.id = h.executed_by
		WHERE h.execution_id = ANY($1)
		ORDER BY h.seq`, executionIDs)
	if err != nil {
		return nil, fmt.Errorf("migration: daftar attempt: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var execID uuid.UUID
		var a Attempt
		if err := rows.Scan(&execID, &a.JobID, &a.Attempt, &a.Status, &a.Error, &a.Batches, &a.Rows,
			&a.ExecutedBy, &a.FinishedAt); err != nil {
			return nil, fmt.Errorf("migration: baca attempt: %w", err)
		}
		out[execID] = append(out[execID], a)
	}
	return out, rows.Err()
}
