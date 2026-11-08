// Package access satu-satunya tempat aturan izin.
//
// Level akses dihitung dari semua group yang diikuti user (ADR-002):
// level tertinggi yang menang, view_edit > view_only > tidak ada akses.
package access

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
)

// Level merepresentasikan tingkat akses ke sebuah database.
type Level string

const (
	NoAccess Level = ""
	ViewOnly Level = "view_only"
	ViewEdit Level = "view_edit"
)

func (l Level) Valid() bool {
	return l == ViewOnly || l == ViewEdit
}

// Store menyimpan koneksi ke database metadata untuk query izin.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// IsPlatformAdmin mengecek apakah user adalah Platform Admin.
// Nilainya diambil dari auth.User yang sudah di-set middleware auth.RequireLogin.
func IsPlatformAdmin(ctx context.Context) bool {
	u := auth.CurrentUser(ctx)
	return u != nil && u.IsPlatformAdmin
}

// EffectiveLevel menghitung level akses efektif user terhadap sebuah database.
// Platform Admin juga harus lewat group (ADR-011).
func (s *Store) EffectiveLevel(ctx context.Context, userID uuid.UUID, dbID uuid.UUID) (Level, error) {
	// MAX langsung pada TEXT akan salah karena 'view_only' > 'view_edit' secara
	// alfabet. Kita konversi ke angka dulu baru MAX, lalu kembalikan ke TEXT.
	var level *string
	err := s.db.QueryRow(ctx, `
		SELECT CASE MAX(CASE gda.level WHEN 'view_edit' THEN 2 WHEN 'view_only' THEN 1 END)
			WHEN 2 THEN 'view_edit'
			WHEN 1 THEN 'view_only'
		END
		FROM group_database_access gda
		JOIN group_members gm ON gm.group_id = gda.group_id
		WHERE gm.user_id = $1 AND gda.database_id = $2`,
		userID, dbID,
	).Scan(&level)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return NoAccess, fmt.Errorf("access: hitung level: %w", err)
	}
	if level == nil {
		return NoAccess, nil
	}
	return Level(*level), nil
}

// CanView dan CanEdit dipakai package migration (interface Permissions).
func (s *Store) CanView(ctx context.Context, userID, dbID uuid.UUID) (bool, error) {
	level, err := s.EffectiveLevel(ctx, userID, dbID)
	return meetsMinimum(level, ViewOnly), err
}

func (s *Store) CanEdit(ctx context.Context, userID, dbID uuid.UUID) (bool, error) {
	level, err := s.EffectiveLevel(ctx, userID, dbID)
	return meetsMinimum(level, ViewEdit), err
}

// ViewableDatabases mendaftar database yang boleh dilihat user, yaitu yang
// di-grant ke salah satu group-nya.
func (s *Store) ViewableDatabases(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT gda.database_id
		FROM group_database_access gda
		JOIN group_members gm ON gm.group_id = gda.group_id
		WHERE gm.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("access: daftar database: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("access: daftar database: %w", err)
	}
	return ids, nil
}

// RequirePlatformAdmin adalah middleware yang menolak request dengan 403
// kalau user bukan Platform Admin. denied dipakai buat nampilin halaman
// penolakannya; kalau nil, cukup teks biasa.
func RequirePlatformAdmin(next, denied http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsPlatformAdmin(r.Context()) {
			if denied != nil {
				denied.ServeHTTP(w, r)
				return
			}
			http.Error(w, "hanya Platform Admin yang boleh mengakses halaman ini", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAPIPlatformAdmin menolak request JSON API dengan 403 problem+json
// kalau user bukan Platform Admin.
func RequireAPIPlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsPlatformAdmin(r.Context()) {
			httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Hanya Platform Admin yang boleh mengakses endpoint ini.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ApproverGroup merepresentasikan group yang memiliki akses (view_only / view_edit)
// ke suatu database beserta leader-nya.
type ApproverGroup struct {
	GroupID        uuid.UUID
	GroupName      string
	LeaderID       *uuid.UUID
	LeaderUsername string
	Level          Level
}

// IsGroupLeader mengecek apakah user adalah leader dari group tertentu.
func (s *Store) IsGroupLeader(ctx context.Context, userID, groupID uuid.UUID) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM groups g
			JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = g.leader_id
			WHERE g.id = $1 AND g.leader_id = $2
		)`, groupID, userID,
	).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("access: cek group leader: %w", err)
	}
	return ok, nil
}

// DatabaseApproverGroups mengembalikan semua group yang memiliki grant akses
// (baik view_only maupun view_edit) ke database tertentu beserta info leader-nya.
func (s *Store) DatabaseApproverGroups(ctx context.Context, dbID uuid.UUID) ([]ApproverGroup, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id, g.name, g.leader_id, COALESCE(u.username, ''), gda.level
		FROM group_database_access gda
		JOIN groups g ON g.id = gda.group_id
		LEFT JOIN users u ON u.id = g.leader_id AND u.disabled_at IS NULL
		WHERE gda.database_id = $1
		ORDER BY g.name`, dbID)
	if err != nil {
		return nil, fmt.Errorf("access: list approver groups: %w", err)
	}
	defer rows.Close()

	var out []ApproverGroup
	for rows.Next() {
		var ag ApproverGroup
		var lvl string
		if err := rows.Scan(&ag.GroupID, &ag.GroupName, &ag.LeaderID, &ag.LeaderUsername, &lvl); err != nil {
			return nil, fmt.Errorf("access: scan approver group: %w", err)
		}
		if ag.LeaderUsername == "" {
			ag.LeaderID = nil
		}
		ag.Level = Level(lvl)
		out = append(out, ag)
	}
	return out, rows.Err()
}

// LeaderGroupsForDatabase mengembalikan daftar group_id yang memiliki akses ke dbID
// di mana userID adalah leader aktif dari group tersebut.
func (s *Store) LeaderGroupsForDatabase(ctx context.Context, userID, dbID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id
		FROM group_database_access gda
		JOIN groups g ON g.id = gda.group_id
		JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = g.leader_id
		JOIN users u ON u.id = g.leader_id AND u.disabled_at IS NULL
		WHERE gda.database_id = $1 AND g.leader_id = $2
		ORDER BY g.name`, dbID, userID)
	if err != nil {
		return nil, fmt.Errorf("access: leader groups for database: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("access: scan leader group id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// meetsMinimum mengecek apakah level user memenuhi level minimum yang diminta.
func meetsMinimum(have, need Level) bool {
	return rank(have) >= rank(need)
}

func rank(l Level) int {
	switch l {
	case ViewEdit:
		return 2
	case ViewOnly:
		return 1
	default:
		return 0
	}
}
