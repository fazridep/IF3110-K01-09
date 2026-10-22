package migration

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

const (
	StatusDraft   Status = "draft"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

var (
	ErrNotFound         = errors.New("migration: tidak ditemukan")
	ErrForbidden        = errors.New("migration: akses ditolak")
	ErrNotDraft         = errors.New("migration: sudah dijalankan, nggak bisa diubah atau dijalankan lagi")
	ErrPendingExists    = errors.New("migration: masih ada migration yang belum dijalankan di database ini")
	ErrEmptySQL         = errors.New("migration: SQL kosong")
	ErrEmptyDescription = errors.New("migration: deskripsi wajib diisi")
	ErrNotTransactional = errors.New("migration: statement nggak bisa dijalankan di dalam transaction")
)

type Migration struct {
	ID          uuid.UUID
	DatabaseID  uuid.UUID
	Version     int
	Description string
	SQL         string
	Status      Status
	Error       string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExecutedBy  string
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

// Permissions disediakan package access. CanEdit berarti punya level view_edit.
type Permissions interface {
	CanView(ctx context.Context, userID, databaseID uuid.UUID) (bool, error)
	CanEdit(ctx context.Context, userID, databaseID uuid.UUID) (bool, error)
}

// Targets disediakan package dbmgmt: pool ke database target.
type Targets interface {
	Pool(ctx context.Context, databaseID uuid.UUID) (*pgxpool.Pool, error)
}

type Service struct {
	db      *pgxpool.Pool
	perms   Permissions
	targets Targets
	now     func() time.Time
}

func NewService(db *pgxpool.Pool, perms Permissions, targets Targets) *Service {
	return &Service{db: db, perms: perms, targets: targets, now: time.Now}
}
