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
)

// Level merepresentasikan tingkat akses ke sebuah database.
type Level string

const (
	NoAccess Level = ""
	ViewOnly Level = "view_only"
	ViewEdit Level = "view_edit"
)

var (
	ErrForbidden = errors.New("access: tidak punya izin")
	ErrNoAccess  = errors.New("access: tidak punya akses ke database ini")
)

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
// Platform Admin otomatis dapat view_edit tanpa perlu masuk group.
func (s *Store) EffectiveLevel(ctx context.Context, userID uuid.UUID, dbID uuid.UUID) (Level, error) {
	if IsPlatformAdmin(ctx) {
		return ViewEdit, nil
	}
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

// RequirePlatformAdmin adalah middleware yang menolak request dengan 403
// kalau user bukan Platform Admin.
func RequirePlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsPlatformAdmin(r.Context()) {
			http.Error(w, "hanya Platform Admin yang boleh mengakses halaman ini", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireDBAccess mengembalikan middleware yang mengecek apakah user punya
// akses minimal `minLevel` ke database tertentu. dbID diambil dari path value "id".
func (s *Store) RequireDBAccess(minLevel Level) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.CurrentUser(r.Context())
			if u == nil {
				http.Error(w, "login diperlukan", http.StatusUnauthorized)
				return
			}

			rawID := r.PathValue("id")
			dbID, err := uuid.Parse(rawID)
			if err != nil {
				http.Error(w, "id database tidak valid", http.StatusBadRequest)
				return
			}

			level, err := s.EffectiveLevel(r.Context(), u.ID, dbID)
			if err != nil {
				http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
				return
			}

			if !meetsMinimum(level, minLevel) {
				http.Error(w, "tidak punya akses ke database ini", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
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
