package queue

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

// ActionBatch aksi approval buat melanjutkan migration batch yang partial.
// ActionRollback juga bisa menggantikan job partial, buat membatalkan batch
// yang sudah committed atau melanjutkan pembatalan yang berhenti di tengah.
const (
	ActionBatch    = "batch"
	ActionRollback = "rollback"
)

type Permissions interface {
	CanView(ctx context.Context, userID, dbID uuid.UUID) (bool, error)
	CanEdit(ctx context.Context, userID, dbID uuid.UUID) (bool, error)
	ViewableDatabases(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

type Approval struct {
	ID          uuid.UUID
	MigrationID uuid.UUID
	DatabaseID  uuid.UUID
	Action      string
	Approved    bool
}

// Approvals disambungkan ke package approval di cmd/server. Lookup menjawab
// ErrNotFound kalau approval nggak ada atau user nggak boleh melihatnya.
type Approvals interface {
	Lookup(ctx context.Context, userID, approvalID uuid.UUID) (Approval, error)
}

const DefaultMaxAttempts = 3

type Service struct {
	store       *Store
	perms       Permissions
	approvals   Approvals
	maxAttempts int
}

func NewService(store *Store, perms Permissions, approvals Approvals, maxAttempts int) *Service {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	return &Service{store: store, perms: perms, approvals: approvals, maxAttempts: maxAttempts}
}

// Submit memasukkan eksekusi yang sudah disetujui ke queue, lalu langsung balik
// tanpa menunggu SQL-nya. Aman dipanggil ulang: satu approval cuma jadi satu job.
func (s *Service) Submit(ctx context.Context, userID, approvalID uuid.UUID, resumes *uuid.UUID) (*Job, error) {
	a, err := s.approvals.Lookup(ctx, userID, approvalID)
	if err != nil {
		return nil, err
	}
	canEdit, err := s.perms.CanEdit(ctx, userID, a.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("queue: cek akses: %w", err)
	}
	if !canEdit {
		return nil, ErrForbidden
	}
	if !a.Approved {
		return nil, ErrNotApproved
	}
	// Job partial menahan semua job berikutnya di database yang sama, jadi
	// lanjutkan batch atau pembatalannya harus menggantikan job partial itu.
	// Kalau SPA nggak mengirim resumeJobId, job partial-nya dicari sendiri.
	canResume := a.Action == ActionBatch || a.Action == ActionRollback
	if resumes != nil && !canResume {
		return nil, ErrInvalidResume
	}
	if resumes == nil && canResume {
		if resumes, err = s.store.PartialJob(ctx, a.MigrationID); err != nil {
			return nil, err
		}
	}

	ctx, span := obs.Start(ctx, "queue.enqueue")
	id, created, err := s.store.Enqueue(ctx, NewJob{
		DatabaseID:   a.DatabaseID,
		MigrationID:  a.MigrationID,
		ApprovalID:   a.ID,
		Action:       a.Action,
		ResumesJobID: resumes,
		MaxAttempts:  s.maxAttempts,
		CreatedBy:    userID,
		TraceContext: obs.InjectTrace(ctx),
	})
	obs.End(span, err)
	if err != nil {
		return nil, err
	}
	obs.Info(ctx, "job.enqueued", "job masuk queue", obs.KeyJobID, id.String(),
		obs.KeyMigrationID, a.MigrationID.String(), "action", a.Action, "created", created)
	return s.store.Get(ctx, id)
}

// Get menjawab ErrNotFound juga kalau user nggak boleh melihat database-nya,
// supaya keberadaan job nggak bocor.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (*Job, error) {
	j, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	ok, err := s.perms.CanView(ctx, userID, j.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("queue: cek akses: %w", err)
	}
	if !ok {
		return nil, ErrNotFound
	}
	return j, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, database *uuid.UUID, status string, beforeSeq int64, limit int) ([]Job, int64, error) {
	dbs, err := s.perms.ViewableDatabases(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("queue: daftar database: %w", err)
	}
	if database != nil {
		allowed := false
		for _, id := range dbs {
			allowed = allowed || id == *database
		}
		if !allowed {
			return nil, 0, nil
		}
		dbs = []uuid.UUID{*database}
	}
	if len(dbs) == 0 {
		return nil, 0, nil
	}
	return s.store.List(ctx, ListFilter{Databases: dbs, Status: status, BeforeSeq: beforeSeq, Limit: limit})
}

func (s *Service) Recheck(ctx context.Context, userID, id uuid.UUID) (*Job, error) {
	j, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	canEdit, err := s.perms.CanEdit(ctx, userID, j.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("queue: cek akses: %w", err)
	}
	if !canEdit {
		return nil, ErrForbidden
	}
	if err := s.store.Recheck(ctx, id); err != nil {
		return nil, err
	}
	obs.Info(ctx, "job.recheck", "pemeriksaan ulang diminta", obs.KeyJobID, id.String())
	return s.store.Get(ctx, id)
}
