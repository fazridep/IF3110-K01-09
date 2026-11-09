package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"

	"github.com/if3110/tubes-db-platform/internal/platform/obs"
	"github.com/if3110/tubes-db-platform/internal/queue"
)

// Executor menyambungkan worker ke migration (kontrak queue.Executor, ADR-013).
// Aksi apply, forward, rollback, dan batch (lanjutkan) semuanya lewat sini.
type Executor struct {
	svc *Service
}

func NewExecutor(svc *Service) *Executor {
	return &Executor{svc: svc}
}

func (e *Executor) Execute(ctx context.Context, job queue.Job, report queue.Reporter) error {
	m, err := findByID(ctx, e.svc.db, job.MigrationID)
	if err != nil {
		return err
	}
	// Revalidasi: izin, SQL, dan posisi migration bisa berubah selama job antre.
	if err := e.svc.require(ctx, e.svc.perms.CanEdit, job.CreatedBy, m.DatabaseID); err != nil {
		return err
	}
	x, err := e.svc.startExecution(ctx, job)
	if err != nil {
		return err
	}

	ctx = obs.WithAttrs(ctx, obs.KeyExecutionID, x.ID.String(), obs.KeyAttempt, job.Attempt,
		"migration_version", m.Version, "mode", string(m.Mode))
	ctx, span := obs.Start(ctx, "migration.execute",
		attribute.String("migration.execution_id", x.ID.String()),
		attribute.String("migration.action", x.Action),
		attribute.Int("migration.version", m.Version),
	)
	attrs := []any{"action", x.Action, "sql", obs.RedactString(x.SQL)}
	if x.RevertsExecutionID != nil {
		attrs = append(attrs, "reverts_execution_id", x.RevertsExecutionID.String())
	}
	obs.Info(ctx, "migration.execute.start", "eksekusi migration mulai", attrs...)

	pool, err := e.svc.targets.Pool(ctx, m.DatabaseID)
	switch {
	case err != nil:
		err = queue.Transient(fmt.Errorf("koneksi ke database target gagal: %w", err))
	case m.Mode == ModeBatch:
		err = e.runBatches(ctx, pool, job, m, x, report)
	default:
		err = e.runOnce(ctx, pool, job, x)
	}

	if err != nil {
		obs.Warn(ctx, "migration.execute.result", "eksekusi migration gagal", obs.KeyStatus, "error", "error", err.Error())
	} else {
		obs.Info(ctx, "migration.execute.result", "eksekusi migration selesai", obs.KeyStatus, "ok")
	}
	obs.End(span, err)
	return err
}

// startExecution membuat eksekusi baru atau melanjutkan yang sudah ada, dalam
// satu transaction metadata yang mengunci baris migration.
func (s *Service) startExecution(ctx context.Context, job queue.Job) (*Execution, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration: begin eksekusi: %w", err)
	}
	defer tx.Rollback(ctx)

	m, err := lockByID(ctx, tx, job.MigrationID)
	if err != nil {
		return nil, err
	}

	// Percobaan ulang job yang sama.
	x, err := executionByJob(ctx, tx, job.ID)
	if err == nil {
		if x.Status != ExecRunning && x.Status != ExecPartial {
			return nil, fmt.Errorf("migration: eksekusi %s sudah %s", x.ID, x.Status)
		}
		if err := CheckSQL(x.SQL); err != nil {
			return nil, err
		}
		return x, tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	if err := checkAction(ctx, tx, m, job.Action); err != nil {
		return nil, err
	}
	open, err := openExecution(ctx, tx, m.ID)
	if err != nil {
		return nil, err
	}

	in := newExecution{migration: m, action: job.Action, jobID: job.ID, userID: job.CreatedBy}
	switch job.Action {
	case ActionApply, ActionForward:
		if open != nil {
			return nil, notAllowed("masih ada eksekusi lain yang belum tuntas untuk migration ini")
		}
		in.sql = m.UpSQL

	case ActionResume:
		if open == nil || open.down() {
			return nil, notAllowed("nggak ada eksekusi batch partial yang bisa dilanjutkan")
		}
		if err := takeOver(ctx, tx, open, job.ID); err != nil {
			return nil, err
		}
		return open, tx.Commit(ctx)

	case ActionRollback:
		if open != nil && open.down() {
			// Lanjutkan pembatalan batch yang sempat berhenti di tengah.
			if err := takeOver(ctx, tx, open, job.ID); err != nil {
				return nil, err
			}
			return open, tx.Commit(ctx)
		}
		in.reverts = m.UpExecutionID
		if open != nil {
			in.reverts = &open.ID // batalkan batch yang sudah committed dari eksekusi partial
		}
		if in.reverts == nil {
			return nil, notAllowed("belum ada eksekusi SQL up yang bisa dibatalkan")
		}
		in.sql = m.DownSQL

	default:
		return nil, notAllowed("aksi " + job.Action + " nggak dikenal")
	}

	if err := CheckSQL(in.sql); err != nil {
		return nil, err
	}
	if job.Action == ActionApply {
		if _, err := tx.Exec(ctx, `
			UPDATE database_migrations SET status='running', executed_by=$2, started_at=$3, updated_at=$3, error=NULL
			WHERE id=$1`, m.ID, job.CreatedBy, s.now()); err != nil {
			return nil, fmt.Errorf("migration: klaim eksekusi: %w", err)
		}
	}
	x, err = insertExecution(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit(ctx)
}

// runOnce mode normal: SQL dan bukti commit dalam satu transaction. Error saat
// COMMIT dibuat Uncertain, karena server bisa saja sudah sempat commit.
func (e *Executor) runOnce(ctx context.Context, pool *pgxpool.Pool, job queue.Job, x *Execution) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if err := lockTarget(ctx, tx); err != nil {
		return err
	}
	if err := queue.RecordCommit(ctx, tx, job.ID); err != nil {
		return err
	}
	if err := e.svc.markSQLStarted(ctx, x); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, x.SQL)
	if err != nil {
		obs.Warn(ctx, "migration.rollback", "SQL gagal, transaction di-rollback", "error", err.Error())
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return queue.Uncertain(fmt.Errorf("hasil COMMIT belum pasti: %w", err))
	}
	obs.Info(ctx, "migration.commit", "transaction migration di-commit", "rows", tag.RowsAffected())
	e.saveProgress(ctx, x, 0, tag.RowsAffected(), "")
	return nil
}

// lockTarget kunci di database target, lapisan kedua selain lock per database di queue.
func lockTarget(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "platform-migration")
	return err
}

// saveProgress cuma cermin di metadata buat ditampilkan. Kalau gagal, checkpoint
// di database target tetap yang dipakai, jadi cukup dicatat di log.
func (e *Executor) saveProgress(ctx context.Context, x *Execution, batches, rows int64, cursor string) {
	if err := e.svc.saveProgress(context.WithoutCancel(ctx), x.ID, batches, rows, cursor); err != nil {
		obs.Warn(ctx, "migration.progress_failed", "gagal menyimpan progres eksekusi", "error", err.Error())
	}
}

func (e *Executor) Verify(ctx context.Context, job queue.Job) (bool, error) {
	pool, err := e.svc.targets.Pool(ctx, job.DatabaseID)
	if err != nil {
		return false, fmt.Errorf("koneksi ke database target gagal: %w", err)
	}
	return queue.CommitRecorded(ctx, pool, job.ID)
}

// Finalize menyimpan hasil job ke eksekusi, status migration, dan migration
// history. Aman dipanggil ulang. Job yang gagal sebelum eksekusinya dibuat
// (misalnya revalidasi) nggak mengubah apa pun, jadi draft tetap draft.
func (e *Executor) Finalize(ctx context.Context, job queue.Job, status queue.Status, errText string) error {
	if status != queue.StatusSuccess && status != queue.StatusFailed && status != queue.StatusPartial {
		return nil
	}
	x, err := executionByJob(ctx, e.svc.db, job.ID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if x.Status == ExecSuccess || x.Status == ExecFailed {
		return nil
	}

	if x.Mode == ModeBatch {
		// Hitungan batch diambil dari checkpoint di database target. Worker bisa
		// menandai job gagal tanpa memanggil Execute (batas percobaan habis), dan
		// kalau sudah ada batch yang committed, eksekusinya partial, bukan gagal.
		pool, err := e.svc.targets.Pool(ctx, job.DatabaseID)
		if err != nil {
			return fmt.Errorf("koneksi ke database target gagal: %w", err)
		}
		p, err := loadProgress(ctx, pool, x.ID)
		if err != nil {
			return err
		}
		if err := e.svc.saveProgress(ctx, x.ID, p.batches, p.rows, p.cursor); err != nil {
			return err
		}
		if status == queue.StatusFailed && p.batches > 0 {
			status = queue.StatusPartial
		}
	}
	return e.svc.finishExecution(ctx, x, status, errText)
}

func (s *Service) finishExecution(ctx context.Context, x *Execution, status queue.Status, errText string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migration: begin finalisasi: %w", err)
	}
	defer tx.Rollback(ctx)

	var current ExecStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM migration_executions WHERE id=$1 FOR UPDATE`, x.ID).Scan(&current); err != nil {
		return fmt.Errorf("migration: kunci eksekusi: %w", err)
	}
	if current == ExecSuccess || current == ExecFailed {
		return nil
	}
	m, err := lockByID(ctx, tx, x.MigrationID)
	if err != nil {
		return err
	}
	at := s.now()

	switch status {
	case queue.StatusSuccess:
		if _, err := tx.Exec(ctx, `
			UPDATE migration_executions SET status='success', error='', finished_at=$2, updated_at=$2
			WHERE id=$1`, x.ID, at); err != nil {
			return fmt.Errorf("migration: simpan eksekusi: %w", err)
		}
		if err := insertHistory(ctx, tx, x.ID, at); err != nil {
			return err
		}
		if x.down() {
			_, err = tx.Exec(ctx, `
				UPDATE database_migrations SET status='rolled_back', error=NULL, updated_at=$2
				WHERE id=$1`, m.ID, at)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE database_migrations SET status='success', up_execution_id=$2, error=NULL,
					finished_at=$3, updated_at=$3
				WHERE id=$1`, m.ID, x.ID, at)
		}

	case queue.StatusPartial:
		if _, err := tx.Exec(ctx, `
			UPDATE migration_executions SET status='partial', error=$2, updated_at=$3
			WHERE id=$1`, x.ID, errText, at); err != nil {
			return fmt.Errorf("migration: simpan eksekusi: %w", err)
		}
		if x.down() {
			_, err = tx.Exec(ctx, `
				UPDATE database_migrations SET status='partially_reverted', error=NULLIF($2, ''), updated_at=$3
				WHERE id=$1`, m.ID, errText, at)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE database_migrations SET status='partial', up_execution_id=$2, error=NULLIF($3, ''), updated_at=$4
				WHERE id=$1`, m.ID, x.ID, errText, at)
		}

	case queue.StatusFailed:
		if _, err := tx.Exec(ctx, `
			UPDATE migration_executions SET status='failed', error=$2, finished_at=$3, updated_at=$3
			WHERE id=$1`, x.ID, errText, at); err != nil {
			return fmt.Errorf("migration: simpan eksekusi: %w", err)
		}
		// Forward atau rollback yang gagal nggak mengubah posisi migration.
		// Apply yang gagal sebelum SQL-nya sempat dikirim (target nggak bisa
		// dihubungi, tabel batch hilang) balik ke draft: SQL-nya belum pernah
		// dieksekusi, jadi belum immutable dan cukup diajukan ulang.
		switch {
		case x.Action != ActionApply || m.Status != StatusRunning:
		case !x.SQLStarted:
			_, err = tx.Exec(ctx, `
				UPDATE database_migrations SET status='draft', error=NULLIF($2, ''), executed_by=NULL,
					started_at=NULL, finished_at=NULL, updated_at=$3
				WHERE id=$1`, m.ID, errText, at)
		default:
			_, err = tx.Exec(ctx, `
				UPDATE database_migrations SET status='failed', error=NULLIF($2, ''), finished_at=$3, updated_at=$3
				WHERE id=$1`, m.ID, errText, at)
		}
	}
	if err != nil {
		return fmt.Errorf("migration: simpan status migration: %w", err)
	}
	return tx.Commit(ctx)
}

// insertHistory menyalin eksekusi yang sudah committed ke migration_history.
// ON CONFLICT supaya Finalize yang dipanggil ulang nggak bikin baris kedua.
func insertHistory(ctx context.Context, tx pgx.Tx, executionID uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO migration_history (execution_id, migration_id, database_id, version, action,
			reverts_execution_id, sql_text, mode, batches, rows_affected, executed_by, started_at, committed_at)
		SELECT x.id, x.migration_id, x.database_id, m.version, x.action, x.reverts_execution_id,
			x.sql_text, x.mode, x.batches_committed, x.rows_committed, x.executed_by, x.started_at, $2
		FROM migration_executions x
		JOIN database_migrations m ON m.id = x.migration_id
		WHERE x.id = $1
		ON CONFLICT (execution_id) DO NOTHING`, executionID, at)
	if err != nil {
		return fmt.Errorf("migration: catat history: %w", err)
	}
	return nil
}
