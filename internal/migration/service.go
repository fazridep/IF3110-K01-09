package migration

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) CanEdit(ctx context.Context, userID, databaseID uuid.UUID) (bool, error) {
	return s.perms.CanEdit(ctx, userID, databaseID)
}

func (s *Service) List(ctx context.Context, userID, databaseID uuid.UUID) ([]Migration, error) {
	if err := s.require(ctx, s.perms.CanView, userID, databaseID); err != nil {
		return nil, err
	}
	return s.listByDatabase(ctx, databaseID)
}

func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (*Migration, error) {
	m, err := s.findByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, s.perms.CanView, userID, m.DatabaseID); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) Create(ctx context.Context, userID, databaseID uuid.UUID, description, sql string) (*Migration, error) {
	if err := s.require(ctx, s.perms.CanEdit, userID, databaseID); err != nil {
		return nil, err
	}
	description = strings.TrimSpace(description)
	if err := validate(description, sql); err != nil {
		return nil, err
	}
	return s.insertDraft(ctx, databaseID, userID, description, sql)
}

func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, description, sql string) (*Migration, error) {
	m, err := s.findByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, s.perms.CanEdit, userID, m.DatabaseID); err != nil {
		return nil, err
	}
	description = strings.TrimSpace(description)
	if err := validate(description, sql); err != nil {
		return nil, err
	}
	if err := s.updateDraft(ctx, id, description, sql); err != nil {
		return nil, err
	}
	return s.findByID(ctx, id)
}

// Execute menjalankan draft di database target dalam satu transaction.
// Error dari SQL migration nggak dikembalikan sebagai error, tapi disimpan di
// Migration.Error dengan status failed.
func (s *Service) Execute(ctx context.Context, userID, id uuid.UUID) (*Migration, error) {
	m, err := s.findByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, s.perms.CanEdit, userID, m.DatabaseID); err != nil {
		return nil, err
	}
	if m.Status != StatusDraft {
		return nil, ErrNotDraft
	}
	if err := CheckSQL(m.SQL); err != nil {
		return nil, err
	}
	if err := s.claim(ctx, id, userID, s.now()); err != nil {
		return nil, err
	}

	status, errText := StatusSuccess, ""
	if err := s.run(ctx, m); err != nil {
		status, errText = StatusFailed, err.Error()
	}
	// Hasil tetap dicatat walaupun request-nya sudah dibatalkan client.
	if err := s.finish(context.WithoutCancel(ctx), id, status, errText, s.now()); err != nil {
		return nil, err
	}
	return s.findByID(ctx, id)
}

func (s *Service) run(ctx context.Context, m *Migration) error {
	pool, err := s.targets.Pool(ctx, m.DatabaseID)
	if err != nil {
		return fmt.Errorf("koneksi ke database target gagal: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Kunci di database target, jadi eksekusi dari proses lain ke database
	// yang sama nunggu sampai transaction ini selesai.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "platform-migration"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) require(ctx context.Context, check func(context.Context, uuid.UUID, uuid.UUID) (bool, error), userID, databaseID uuid.UUID) error {
	ok, err := check(ctx, userID, databaseID)
	if err != nil {
		return fmt.Errorf("migration: cek akses: %w", err)
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func validate(description, sql string) error {
	if description == "" {
		return ErrEmptyDescription
	}
	return CheckSQL(sql)
}
