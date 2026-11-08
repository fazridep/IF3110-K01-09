package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/queue"
)

// Executor menyambungkan worker ke migration. Versi ini baru menjalankan aksi
// apply dalam satu transaction. Rollback, forward, dan batch ditolak sebagai
// gagal permanen sampai modelnya ada (SQL down, riwayat eksekusi).
type Executor struct {
	svc *Service
}

func NewExecutor(svc *Service) *Executor {
	return &Executor{svc: svc}
}

var errActionUnsupported = errors.New("aksi ini belum didukung worker")

func (e *Executor) Execute(ctx context.Context, job queue.Job, _ queue.Reporter) error {
	if job.Action != "apply" {
		return fmt.Errorf("%w: %s", errActionUnsupported, job.Action)
	}
	m, err := e.svc.findByID(ctx, job.MigrationID)
	if err != nil {
		return err
	}

	// Revalidasi: izin dan SQL bisa saja berubah selama job menunggu di queue.
	if err := e.svc.require(ctx, e.svc.perms.CanEdit, job.CreatedBy, m.DatabaseID); err != nil {
		return err
	}
	if err := CheckSQL(m.SQL); err != nil {
		return err
	}
	switch m.Status {
	case StatusDraft:
		if err := e.svc.claim(ctx, m.ID, job.CreatedBy, e.svc.now()); err != nil {
			return err
		}
	case StatusRunning:
		// Percobaan ulang job ini.
	default:
		return ErrNotDraft
	}
	return e.apply(ctx, job.ID, m)
}

// apply menjalankan SQL bersama bukti commit dalam satu transaction. Error saat
// COMMIT dibuat Uncertain, karena server bisa saja sudah sempat commit.
func (e *Executor) apply(ctx context.Context, jobID uuid.UUID, m *Migration) error {
	pool, err := e.svc.targets.Pool(ctx, m.DatabaseID)
	if err != nil {
		return queue.Transient(fmt.Errorf("koneksi ke database target gagal: %w", err))
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	// Kunci di database target, lapisan kedua selain lock per database di queue.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "platform-migration"); err != nil {
		return err
	}
	if err := queue.RecordCommit(ctx, tx, jobID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return queue.Uncertain(fmt.Errorf("hasil COMMIT belum pasti: %w", err))
	}
	return nil
}

func (e *Executor) Verify(ctx context.Context, job queue.Job) (bool, error) {
	pool, err := e.svc.targets.Pool(ctx, job.DatabaseID)
	if err != nil {
		return false, fmt.Errorf("koneksi ke database target gagal: %w", err)
	}
	return queue.CommitRecorded(ctx, pool, job.ID)
}

// Finalize menyimpan hasil ke record migration, cuma kalau SQL-nya memang sudah
// dimulai (running). Job yang gagal revalidasi membiarkan migration tetap draft,
// dan migration yang sudah selesai nggak ketimpa.
func (e *Executor) Finalize(ctx context.Context, job queue.Job, status queue.Status, errText string) error {
	if job.Action != "apply" {
		return nil
	}
	var result Status
	switch status {
	case queue.StatusSuccess:
		result = StatusSuccess
	case queue.StatusFailed:
		result = StatusFailed
	default:
		return nil
	}
	m, err := e.svc.findByID(ctx, job.MigrationID)
	if err != nil {
		return err
	}
	if m.Status != StatusRunning {
		return nil
	}
	return e.svc.finish(ctx, job.MigrationID, result, errText, e.svc.now())
}
