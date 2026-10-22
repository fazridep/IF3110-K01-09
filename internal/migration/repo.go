package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// selectMigration mengambil migration beserta username pembuat dan pelaksananya.
const selectMigration = `
	SELECT m.id, m.database_id, m.version, m.description, m.sql_text, m.status, COALESCE(m.error, ''),
		c.username, m.created_at, m.updated_at, COALESCE(e.username, ''), m.started_at, m.finished_at
	FROM database_migrations m
	JOIN users c ON c.id = m.created_by
	LEFT JOIN users e ON e.id = m.executed_by`

func scanMigration(row pgx.Row) (*Migration, error) {
	m := &Migration{}
	err := row.Scan(&m.ID, &m.DatabaseID, &m.Version, &m.Description, &m.SQL, &m.Status, &m.Error,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.ExecutedBy, &m.StartedAt, &m.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: baca data: %w", err)
	}
	return m, nil
}

func (s *Service) findByID(ctx context.Context, id uuid.UUID) (*Migration, error) {
	return scanMigration(s.db.QueryRow(ctx, selectMigration+` WHERE m.id=$1`, id))
}

func (s *Service) listByDatabase(ctx context.Context, databaseID uuid.UUID) ([]Migration, error) {
	rows, err := s.db.Query(ctx, selectMigration+` WHERE m.database_id=$1 ORDER BY m.version DESC`, databaseID)
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
func (s *Service) insertDraft(ctx context.Context, databaseID, userID uuid.UUID, description, sql string) (*Migration, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO database_migrations (database_id, version, description, sql_text, created_by)
		SELECT $1, COALESCE(MAX(version), 0) + 1, $2, $3, $4
		FROM database_migrations WHERE database_id=$1
		RETURNING id`,
		databaseID, description, sql, userID).Scan(&id)
	if isUniqueViolation(err) {
		return nil, ErrPendingExists
	}
	if err != nil {
		return nil, fmt.Errorf("migration: simpan draft: %w", err)
	}
	return s.findByID(ctx, id)
}

func (s *Service) updateDraft(ctx context.Context, id uuid.UUID, description, sql string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE database_migrations SET description=$2, sql_text=$3, updated_at=$4
		WHERE id=$1 AND status='draft'`,
		id, description, sql, s.now())
	if err != nil {
		return fmt.Errorf("migration: update draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotDraft
	}
	return nil
}

// claim mengubah draft jadi running. Kalau dua orang menekan Execute bareng,
// cuma satu yang berhasil.
func (s *Service) claim(ctx context.Context, id, userID uuid.UUID, at time.Time) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE database_migrations SET status='running', executed_by=$2, started_at=$3, updated_at=$3
		WHERE id=$1 AND status='draft'`,
		id, userID, at)
	if err != nil {
		return fmt.Errorf("migration: klaim eksekusi: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotDraft
	}
	return nil
}

func (s *Service) finish(ctx context.Context, id uuid.UUID, status Status, errText string, at time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE database_migrations SET status=$2, error=NULLIF($3, ''), finished_at=$4, updated_at=$4
		WHERE id=$1`,
		id, status, errText, at)
	if err != nil {
		return fmt.Errorf("migration: simpan hasil eksekusi: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
