// Package approval mengelola persetujuan (approval) perubahan database oleh
// Leader Group.
//
// Aturan bisnis:
// Setiap perubahan pada suatu database wajib disetujui oleh Leader dari SEMUA
// group yang memiliki akses ke database tersebut (baik view_only maupun view_edit).
// Status request hanya berpindah dari pending_review ke approved apabila seluruh
// group terkait sudah memberikan persetujuan. Jika salah satu leader menolak,
// request langsung berstatus rejected.
package approval

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/access"
)

type Status string

const (
	StatusPendingReview Status = "pending_review"
	StatusApproved      Status = "approved"
	StatusRejected      Status = "rejected"
	StatusCancelled     Status = "cancelled"
)

type Action string

const (
	ActionApply    Action = "apply"
	ActionRollback Action = "rollback"
	ActionForward  Action = "forward"
	ActionBatch    Action = "batch"
)

func (a Action) Valid() bool {
	switch a {
	case ActionApply, ActionRollback, ActionForward, ActionBatch:
		return true
	default:
		return false
	}
}

var (
	ErrNotFound         = errors.New("approval: permintaan approval tidak ditemukan")
	ErrForbidden        = errors.New("approval: akses ditolak")
	ErrNotLeader        = errors.New("approval: hanya Leader dari group yang memiliki akses ke database ini yang boleh memberikan keputusan")
	ErrAlreadyResolved  = errors.New("approval: permintaan approval sudah selesai diproses")
	ErrAlreadyDecided   = errors.New("approval: group yang kamu pimpin sudah memberikan keputusan untuk permintaan ini")
	ErrPendingExists    = errors.New("approval: masih ada permintaan approval yang menunggu review untuk migration ini")
	ErrNoApproverGroups = errors.New("approval: belum ada group yang memiliki akses ke database ini")
	ErrEmptyReason      = errors.New("approval: alasan penolakan wajib diisi")
	ErrInvalidAction    = errors.New("approval: jenis aksi tidak valid")
)

type GroupDecision struct {
	GroupID        uuid.UUID
	GroupName      string
	AccessLevel    access.Level
	LeaderID       *uuid.UUID
	LeaderUsername string
	Decision       Status
	DecidedBy      string
	Note           string
	DecidedAt      *time.Time
}

type Request struct {
	ID              uuid.UUID
	MigrationID     uuid.UUID
	DatabaseID      uuid.UUID
	DatabaseName    string
	Version         int
	Description     string
	SQL             string
	Action          Action
	Status          Status
	RequestedByID   uuid.UUID
	RequestedBy     string
	RejectionReason string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResolvedAt      *time.Time
	Approvals       []GroupDecision
	ApprovedCount   int
	TotalRequired   int
	CanDecide       bool
	MyLeaderGroups  []string
}

// Permissions disediakan oleh package access.
type Permissions interface {
	CanView(ctx context.Context, userID, dbID uuid.UUID) (bool, error)
	CanEdit(ctx context.Context, userID, dbID uuid.UUID) (bool, error)
	DatabaseApproverGroups(ctx context.Context, dbID uuid.UUID) ([]access.ApproverGroup, error)
	LeaderGroupsForDatabase(ctx context.Context, userID, dbID uuid.UUID) ([]uuid.UUID, error)
}

// OnApprovedFunc opsional: dipanggil saat semua leader group sudah menyetujui request.
type OnApprovedFunc func(ctx context.Context, req *Request) error

type Service struct {
	db         *pgxpool.Pool
	perms      Permissions
	onApproved OnApprovedFunc
}

func NewService(db *pgxpool.Pool, perms Permissions) *Service {
	return &Service{db: db, perms: perms}
}

func (s *Service) SetOnApproved(fn OnApprovedFunc) {
	s.onApproved = fn
}

