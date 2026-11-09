package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store mengurus tabel migration_jobs dan database_locks. Semua waktu pakai
// now() milik database, supaya jam antar-worker yang beda nggak jadi masalah.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

type NewJob struct {
	DatabaseID   uuid.UUID
	MigrationID  uuid.UUID
	ApprovalID   uuid.UUID
	Action       string
	ResumesJobID *uuid.UUID
	MaxAttempts  int
	CreatedBy    uuid.UUID
	TraceContext map[string]string
}

const jobColumns = `j.id, j.seq, j.database_id, j.migration_id, j.approval_id, j.action, j.resumes_job_id,
	j.status, j.attempt, j.verify_attempts, j.max_attempts, j.run_after, j.worker_id, j.error,
	j.batches_committed, j.rows_committed, j.created_by, j.created_at, j.updated_at, j.started_at, j.finished_at,
	j.trace_context`

func scanJob(row pgx.Row, extra ...any) (*Job, error) {
	j := &Job{}
	var trace []byte
	dest := []any{&j.ID, &j.seq, &j.DatabaseID, &j.MigrationID, &j.ApprovalID, &j.Action, &j.ResumesJobID,
		&j.Status, &j.Attempt, &j.VerifyAttempts, &j.MaxAttempts, &j.RunAfter, &j.WorkerID, &j.Error,
		&j.BatchesDone, &j.RowsDone, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt, &j.StartedAt, &j.FinishedAt,
		&trace}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(trace, &j.traceContext)
	return j, nil
}

// Enqueue memasukkan job. Satu approval cuma jadi satu job: kalau sudah ada, job
// lama yang dikembalikan dan created=false. Kalau job ini melanjutkan job
// partial, job lama ditandai superseded dalam transaction yang sama.
func (s *Store) Enqueue(ctx context.Context, in NewJob) (id uuid.UUID, created bool, err error) {
	trace, err := json.Marshal(in.TraceContext)
	if err != nil {
		return uuid.Nil, false, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("queue: begin enqueue: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO migration_jobs (database_id, migration_id, approval_id, action, resumes_job_id, max_attempts, created_by, trace_context)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (approval_id) DO NOTHING
		RETURNING id`,
		in.DatabaseID, in.MigrationID, in.ApprovalID, in.Action, in.ResumesJobID, in.MaxAttempts, in.CreatedBy, trace,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM migration_jobs WHERE approval_id = $1`, in.ApprovalID).Scan(&id)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("queue: cari job approval: %w", err)
		}
		return id, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("queue: simpan job: %w", err)
	}

	if in.ResumesJobID != nil {
		tag, err := tx.Exec(ctx, `
			UPDATE migration_jobs SET status = 'superseded', updated_at = now()
			WHERE id = $1 AND migration_id = $2 AND status = 'partial'`,
			*in.ResumesJobID, in.MigrationID)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("queue: tandai job lama: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return uuid.Nil, false, ErrInvalidResume
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, false, fmt.Errorf("queue: commit enqueue: %w", err)
	}
	return id, true, nil
}

// PartialJob job partial terbaru milik migration ini, nil kalau nggak ada.
func (s *Store) PartialJob(ctx context.Context, migrationID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT id FROM migration_jobs WHERE migration_id = $1 AND status = 'partial'
		ORDER BY seq DESC LIMIT 1`, migrationID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queue: cari job partial: %w", err)
	}
	return &id, nil
}

const jobDetailFrom = `
	FROM migration_jobs j
	JOIN database_connections dc ON dc.id = j.database_id
	JOIN database_migrations dm ON dm.id = j.migration_id
	JOIN users u ON u.id = j.created_by`

func scanJobDetail(row pgx.Row) (*Job, error) {
	var name, desc, by string
	var version int
	j, err := scanJob(row, &name, &version, &desc, &by)
	if err != nil {
		return nil, err
	}
	j.DatabaseName, j.MigrationVersion, j.Description, j.CreatedByName = name, version, desc, by
	return j, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Job, error) {
	j, err := scanJobDetail(s.db.QueryRow(ctx, `
		SELECT `+jobColumns+`, dc.name, dm.version, dm.description, u.username`+jobDetailFrom+`
		WHERE j.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("queue: ambil job: %w", err)
	}
	return j, nil
}

// ListFilter: Databases wajib diisi daftar database yang boleh dilihat user.
type ListFilter struct {
	Databases []uuid.UUID
	Status    string
	BeforeSeq int64
	Limit     int
}

func (s *Store) List(ctx context.Context, f ListFilter) ([]Job, int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+jobColumns+`, dc.name, dm.version, dm.description, u.username`+jobDetailFrom+`
		WHERE j.database_id = ANY($1)
		  AND ($2 = '' OR j.status = $2)
		  AND ($3 = 0 OR j.seq < $3)
		ORDER BY j.seq DESC
		LIMIT $4`,
		f.Databases, f.Status, f.BeforeSeq, f.Limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("queue: daftar job: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		j, err := scanJobDetail(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("queue: baca job: %w", err)
		}
		out = append(out, *j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(out) > f.Limit {
		out = out[:f.Limit]
		next = out[f.Limit-1].seq
	}
	return out, next, nil
}

// Claim mengambil job berikutnya yang boleh jalan sekaligus memegang lock
// databasenya, dalam satu transaction. Hasil nil berarti belum ada yang bisa
// dikerjakan. Job dilewati kalau:
//   - database-nya sedang dipegang worker lain (lock lintas worker), atau
//   - ada job lebih lama di database yang sama yang belum tuntas. Ini yang
//     menahan migration berikutnya selama hasilnya belum pasti atau partial.
func (s *Store) Claim(ctx context.Context, workerID string, ttl time.Duration) (*Job, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("queue: begin klaim: %w", err)
	}
	defer tx.Rollback(ctx)

	j, err := scanJob(tx.QueryRow(ctx, `
		SELECT `+jobColumns+`
		FROM migration_jobs j
		WHERE (j.status = 'queued' OR (j.status = 'uncertain' AND j.verify_attempts < j.max_attempts))
		  AND j.run_after <= now()
		  AND NOT EXISTS (
			SELECT 1 FROM database_locks l
			WHERE l.database_id = j.database_id AND l.expires_at > now())
		  AND NOT EXISTS (
			SELECT 1 FROM migration_jobs o
			WHERE o.database_id = j.database_id AND o.seq < j.seq
			  AND o.status IN ('queued', 'running', 'uncertain', 'partial'))
		ORDER BY j.seq
		LIMIT 1
		FOR UPDATE OF j SKIP LOCKED`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queue: cari job: %w", err)
	}

	j.leaseToken = uuid.New()
	j.WorkerID = workerID
	tag, err := tx.Exec(ctx, `
		INSERT INTO database_locks (database_id, job_id, worker_id, lease_token, expires_at)
		VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		ON CONFLICT (database_id) DO UPDATE
		SET job_id = EXCLUDED.job_id, worker_id = EXCLUDED.worker_id, lease_token = EXCLUDED.lease_token,
		    acquired_at = now(), expires_at = EXCLUDED.expires_at
		WHERE database_locks.expires_at <= now()`,
		j.DatabaseID, j.ID, workerID, j.leaseToken, ttl.Seconds())
	if err != nil {
		return nil, fmt.Errorf("queue: ambil lock database: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, nil // keduluan worker lain, coba lagi di putaran berikutnya
	}

	// Attempt naik kalau job langsung dieksekusi. Yang uncertain diverifikasi
	// dulu, BeginExecute yang menaikkan attempt kalau ternyata perlu diulang.
	verifying := j.Status == StatusUncertain
	err = tx.QueryRow(ctx, `
		UPDATE migration_jobs
		SET status = 'running', worker_id = $2, lease_token = $3, updated_at = now(),
		    started_at = COALESCE(started_at, now()),
		    attempt = attempt + CASE WHEN $4 THEN 0 ELSE 1 END,
		    verify_attempts = verify_attempts + CASE WHEN $4 THEN 1 ELSE 0 END
		WHERE id = $1
		RETURNING attempt, verify_attempts`,
		j.ID, workerID, j.leaseToken, verifying).Scan(&j.Attempt, &j.VerifyAttempts)
	if err != nil {
		return nil, fmt.Errorf("queue: tandai running: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("queue: commit klaim: %w", err)
	}
	if !verifying {
		j.Status = StatusRunning
	}
	return j, nil
}

func (s *Store) BeginExecute(ctx context.Context, j *Job) error {
	err := s.db.QueryRow(ctx, `
		UPDATE migration_jobs SET attempt = attempt + 1, updated_at = now()
		WHERE id = $1 AND lease_token = $2
		RETURNING attempt`, j.ID, j.leaseToken).Scan(&j.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return fmt.Errorf("queue: naikkan attempt: %w", err)
	}
	return nil
}

func (s *Store) Heartbeat(ctx context.Context, j *Job, ttl time.Duration) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE database_locks SET expires_at = now() + make_interval(secs => $3)
		WHERE database_id = $1 AND lease_token = $2`,
		j.DatabaseID, j.leaseToken, ttl.Seconds())
	if err != nil {
		return fmt.Errorf("queue: perpanjang lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) Report(ctx context.Context, j *Job, batches, rows int64) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE migration_jobs SET batches_committed = $3, rows_committed = $4, updated_at = now()
		WHERE id = $1 AND lease_token = $2`, j.ID, j.leaseToken, batches, rows)
	if err != nil {
		return fmt.Errorf("queue: simpan progres: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

type transition struct {
	status   Status
	errText  string
	delay    time.Duration
	terminal bool
}

func (s *Store) finish(ctx context.Context, j *Job, t transition) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("queue: begin selesai: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE migration_jobs
		SET status = $3, error = $4, run_after = now() + make_interval(secs => $5),
		    lease_token = NULL, worker_id = '', updated_at = now(),
		    finished_at = CASE WHEN $6 THEN now() END
		WHERE id = $1 AND lease_token = $2`,
		j.ID, j.leaseToken, t.status, t.errText, t.delay.Seconds(), t.terminal)
	if err != nil {
		return fmt.Errorf("queue: simpan hasil job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	if _, err := tx.Exec(ctx, `DELETE FROM database_locks WHERE database_id = $1 AND lease_token = $2`, j.DatabaseID, j.leaseToken); err != nil {
		return fmt.Errorf("queue: lepas lock: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("queue: commit hasil job: %w", err)
	}
	return nil
}

// RecoverExpired memulihkan job running yang lease-nya habis (worker mati). Job
// dibuat uncertain, karena bisa saja SQL-nya sudah ke-commit sebelum worker mati.
// Worker berikutnya memeriksa bukti dulu sebelum menjalankan ulang.
func (s *Store) RecoverExpired(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `
		WITH expired AS (
			SELECT j.id
			FROM migration_jobs j
			LEFT JOIN database_locks l ON l.job_id = j.id
			WHERE j.status = 'running' AND (l.job_id IS NULL OR l.expires_at <= now())
			FOR UPDATE OF j SKIP LOCKED
		), released AS (
			DELETE FROM database_locks WHERE job_id IN (SELECT id FROM expired)
		)
		UPDATE migration_jobs j
		SET status = 'uncertain', run_after = now(), lease_token = NULL, worker_id = '',
		    error = 'Worker berhenti di tengah eksekusi, hasilnya sedang diperiksa.', updated_at = now()
		FROM expired
		WHERE j.id = expired.id
		RETURNING j.id`)
	if err != nil {
		return nil, fmt.Errorf("queue: pulihkan job: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("queue: pulihkan job: %w", err)
	}
	return ids, nil
}

func (s *Store) Recheck(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE migration_jobs SET verify_attempts = 0, run_after = now(), updated_at = now()
		WHERE id = $1 AND status = 'uncertain'`, id)
	if err != nil {
		return fmt.Errorf("queue: periksa ulang: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}
