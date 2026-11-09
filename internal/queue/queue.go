// Package queue menjalankan eksekusi migration secara asynchronous (ADR-013).
// Cara menjalankan SQL-nya urusan Executor, yang diisi dari cmd/worker.
package queue

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	// StatusPartial: sebagian batch sudah committed. Job berikutnya di database
	// yang sama ditahan sampai ini dilanjutkan atau diperbaiki.
	StatusPartial Status = "partial"
	// StatusUncertain: belum tahu apakah SQL ke-commit (koneksi putus saat
	// COMMIT, atau worker mati). Worker berikutnya memeriksa bukti dulu.
	StatusUncertain Status = "uncertain"
	// StatusSuperseded: job partial yang sudah dilanjutkan job lain.
	StatusSuperseded Status = "superseded"
)

var (
	ErrNotFound         = errors.New("queue: job tidak ditemukan")
	ErrForbidden        = errors.New("queue: akses ditolak")
	ErrNotApproved      = errors.New("queue: approval belum disetujui")
	ErrInvalidState     = errors.New("queue: status job nggak mengizinkan aksi ini")
	ErrInvalidResume    = errors.New("queue: job yang dilanjutkan nggak cocok dengan approval")
	ErrLeaseLost        = errors.New("queue: lease job sudah diambil worker lain")
	ErrAlreadyCommitted = errors.New("queue: job ini sudah pernah ke-commit di database target")
)

type Job struct {
	ID             uuid.UUID
	DatabaseID     uuid.UUID
	MigrationID    uuid.UUID
	ApprovalID     uuid.UUID
	Action         string
	ResumesJobID   *uuid.UUID
	Status         Status
	Attempt        int
	VerifyAttempts int
	MaxAttempts    int
	RunAfter       time.Time
	WorkerID       string
	Error          string
	BatchesDone    int64
	RowsDone       int64
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time

	// Terisi waktu dibaca lewat API.
	DatabaseName     string
	MigrationVersion int
	Description      string
	CreatedByName    string

	seq          int64
	leaseToken   uuid.UUID
	traceContext map[string]string
}

type Reporter interface {
	Report(ctx context.Context, batches, rows int64) error
}

type Executor interface {
	// Execute menjalankan SQL. Hasil nil berarti sudah ter-commit. Error polos
	// dianggap gagal permanen, kecuali IsTransient mengenalinya. Pakai Partial
	// dan Uncertain buat dua keadaan lainnya. Transaction yang berisi SQL
	// migration wajib memanggil RecordCommit, supaya Verify bisa bekerja dan
	// pesan duplikat nggak jadi double commit.
	Execute(ctx context.Context, job Job, report Reporter) error
	Verify(ctx context.Context, job Job) (committed bool, err error)
	// Finalize dipanggil sekali sebelum job ditandai selesai, buat update data
	// milik Executor. Harus aman dipanggil ulang.
	Finalize(ctx context.Context, job Job, status Status, errText string) error
}

type partialError struct{ err error }

func (e *partialError) Error() string { return e.err.Error() }
func (e *partialError) Unwrap() error { return e.err }

func Partial(err error) error { return &partialError{err} }

type uncertainError struct{ err error }

func (e *uncertainError) Error() string { return e.err.Error() }
func (e *uncertainError) Unwrap() error { return e.err }

func Uncertain(err error) error { return &uncertainError{err} }

type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

func Transient(err error) error { return &transientError{err} }

func IsPartial(err error) bool {
	var pe *partialError
	return errors.As(err, &pe)
}

func IsUncertain(err error) bool {
	var ue *uncertainError
	return errors.As(err, &ue)
}

var transientSQLStates = map[string]bool{
	"40001": true, "40P01": true,
	"53300": true, "57P01": true, "57P02": true, "57P03": true,
	"08000": true, "08003": true, "08006": true, "08001": true, "08004": true,
}

// IsTransient mengenali kegagalan sementara. Error SQL biasa (salah sintaks,
// constraint) nggak termasuk, karena diulang pun hasilnya sama.
func IsTransient(err error) bool {
	var te *transientError
	if errors.As(err, &te) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return transientSQLStates[pgErr.Code]
	}
	var connErr *pgconn.ConnectError
	var netErr net.Error
	return errors.As(err, &connErr) || errors.As(err, &netErr) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
