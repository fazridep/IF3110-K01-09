package migration

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status posisi migration di database target (ADR-015).
type Status string

const (
	StatusDraft   Status = "draft"
	StatusRunning Status = "running"
	// StatusSuccess: SQL up sedang berlaku.
	StatusSuccess    Status = "success"
	StatusFailed     Status = "failed"
	StatusRolledBack Status = "rolled_back"
	// StatusPartial: sebagian batch SQL up sudah committed.
	StatusPartial Status = "partial"
	// StatusPartiallyReverted: sebagian batch SQL down sudah committed.
	StatusPartiallyReverted Status = "partially_reverted"
)

// applied berarti perubahan migration ini (sebagian atau semua) ada di database.
func (s Status) applied() bool {
	return s == StatusSuccess || s == StatusPartial || s == StatusPartiallyReverted || s == StatusRunning
}

type Mode string

const (
	ModeNormal Mode = "normal"
	ModeBatch  Mode = "batch"
)

// Aksi sama dengan aksi approval dan job. ActionResume melanjutkan migration
// batch yang partial, namanya "batch" mengikuti queue.ActionBatch.
const (
	ActionApply    = "apply"
	ActionForward  = "forward"
	ActionRollback = "rollback"
	ActionResume   = "batch"
)

var Actions = []string{ActionApply, ActionForward, ActionRollback, ActionResume}

var (
	ErrNotFound          = errors.New("migration: tidak ditemukan")
	ErrForbidden         = errors.New("migration: akses ditolak")
	ErrNotDraft          = errors.New("migration: sudah dijalankan, SQL-nya nggak bisa diubah lagi")
	ErrBusy              = errors.New("migration: sedang diproses worker, tunggu sampai selesai")
	ErrPendingExists     = errors.New("migration: masih ada migration yang belum dijalankan di database ini")
	ErrEmptySQL          = errors.New("migration: SQL kosong")
	ErrEmptyDescription  = errors.New("migration: deskripsi wajib diisi")
	ErrNotTransactional  = errors.New("migration: statement nggak bisa dijalankan di dalam transaction")
	ErrDownRequired      = errors.New("migration: SQL down wajib diisi, atau tandai irreversible beserta alasannya")
	ErrReasonRequired    = errors.New("migration: alasan irreversible wajib diisi")
	ErrInvalidBatch      = errors.New("migration: konfigurasi batch tidak valid")
	ErrTargetUnavailable = errors.New("migration: database target nggak bisa dihubungi")
	ErrActionNotAllowed  = errors.New("migration: aksi nggak bisa dijalankan")
)

// NotAllowedError alasan suatu aksi ditolak, bisa langsung ditampilkan ke user.
type NotAllowedError struct{ Reason string }

func (e *NotAllowedError) Error() string        { return "migration: " + e.Reason }
func (e *NotAllowedError) Is(target error) bool { return target == ErrActionNotAllowed }

func notAllowed(reason string) error { return &NotAllowedError{Reason: reason} }

// Batch konfigurasi mode batch. Tiap batch mengambil Size row dari Table urut
// Key (kursor), menaruhnya di temp table batch_rows, lalu menjalankan SQL.
type Batch struct {
	Table  string
	Key    string
	Size   int
	Backup bool
}

// Definition bagian migration yang diisi user. Setelah pernah dieksekusi,
// isinya nggak bisa diubah.
type Definition struct {
	Description        string
	UpSQL              string
	DownSQL            string
	Irreversible       bool
	IrreversibleReason string
	Mode               Mode
	Batch              Batch
}

func (d Definition) reversible() bool {
	return !d.Irreversible && d.DownSQL != ""
}

type Migration struct {
	ID         uuid.UUID
	DatabaseID uuid.UUID
	Version    int
	Definition
	Status        Status
	Error         string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExecutedBy    string
	StartedAt     *time.Time
	FinishedAt    *time.Time
	UpExecutionID *uuid.UUID
}

type ExecStatus string

const (
	ExecRunning ExecStatus = "running"
	ExecSuccess ExecStatus = "success"
	ExecFailed  ExecStatus = "failed"
	ExecPartial ExecStatus = "partial"
)

// Execution satu kali menjalankan SQL up (apply, forward) atau down (rollback).
// Committed berarti eksekusinya sudah masuk migration_history.
type Execution struct {
	ID                 uuid.UUID
	MigrationID        uuid.UUID
	DatabaseID         uuid.UUID
	Version            int
	Description        string
	Action             string
	RevertsExecutionID *uuid.UUID
	JobID              uuid.UUID
	Status             ExecStatus
	SQL                string
	Mode               Mode
	BatchSize          int
	Batches            int64
	Rows               int64
	Cursor             string
	Error              string
	SQLStarted         bool
	ExecutedBy         string
	StartedAt          time.Time
	FinishedAt         *time.Time
	Committed          bool
	CommittedAt        *time.Time
}

func (e *Execution) down() bool { return e.Action == ActionRollback }

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
