package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cookieName = "sid"

type SessionConfig struct {
	IdleTimeout      time.Duration
	AbsoluteTimeout  time.Duration
	RotationInterval time.Duration
	GracePeriod      time.Duration
	Secure           bool
}

type Store struct {
	db  *pgxpool.Pool
	cfg SessionConfig
	now func() time.Time
}

func NewStore(db *pgxpool.Pool, cfg SessionConfig) *Store {
	return &Store{db: db, cfg: cfg, now: time.Now}
}

// Session hasil validasi token. NewToken terisi kalau token baru saja
// dirotasi dan harus dikirim ulang ke client.
type Session struct {
	User      *User
	ExpiresAt time.Time
	NewToken  string
}

type sessionRow struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	TokenHash      []byte
	RotatedAt      *time.Time
	LastAccessedAt time.Time
	ExpiresAt      time.Time
	RevokedAt      *time.Time
}

func (s *Store) Create(ctx context.Context, userID uuid.UUID) (token string, expiresAt time.Time, err error) {
	token = rand.Text()
	now := s.now().UTC()
	expiresAt = now.Add(s.cfg.AbsoluteTimeout)
	_, err = s.db.Exec(ctx, `
		INSERT INTO sessions (user_id, token_hash, rotated_at, created_at, last_accessed_at, expires_at)
		VALUES ($1, $2, $3, $3, $3, $4)`,
		userID, hashToken(token), now, expiresAt)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: simpan session: %w", err)
	}
	return token, expiresAt, nil
}

func (s *Store) Validate(ctx context.Context, token string) (*Session, error) {
	now := s.now().UTC()
	presented := hashToken(token)
	row, err := s.findSession(ctx, presented, now)
	if err != nil {
		return nil, err
	}
	if row.RevokedAt != nil {
		return nil, ErrSessionRevoked
	}
	// Tepat di expires_at session sudah dianggap habis. Pakai After doang bikin
	// hasilnya beda di macOS, yang jamnya cuma presisi mikrodetik.
	if !now.Before(row.ExpiresAt) || now.Sub(row.LastAccessedAt) >= s.cfg.IdleTimeout {
		return nil, ErrSessionExpired
	}
	user, err := s.fetchUser(ctx, row.UserID)
	if err != nil {
		return nil, err
	}
	sess := &Session{User: user, ExpiresAt: row.ExpiresAt}

	// Token lama yang masih dalam grace period nggak boleh memicu rotasi lagi,
	// supaya token baru yang sudah dikirim ke client tetap berlaku.
	isCurrent := bytes.Equal(row.TokenHash, presented)
	if isCurrent && (row.RotatedAt == nil || now.Sub(*row.RotatedAt) >= s.cfg.RotationInterval) {
		sess.NewToken, err = s.rotate(ctx, presented, now)
		if err != nil {
			return nil, err
		}
		if sess.NewToken != "" {
			return sess, nil
		}
	}

	if _, err := s.db.Exec(ctx, `UPDATE sessions SET last_accessed_at=$1 WHERE id=$2`, now, row.ID); err != nil {
		return nil, fmt.Errorf("auth: update session: %w", err)
	}
	return sess, nil
}

func (s *Store) Revoke(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE sessions SET revoked_at=$1
		WHERE (token_hash=$2 OR previous_token_hash=$2) AND revoked_at IS NULL`,
		s.now().UTC(), hashToken(token))
	return err
}

func (s *Store) RevokeUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE sessions SET revoked_at=$1 WHERE user_id=$2 AND revoked_at IS NULL`, s.now().UTC(), userID)
	return err
}

// ForceRotate memaksa semua session aktif user dirotasi di request berikutnya.
// Dipanggil saat privilege user naik, misalnya dijadikan Platform Admin.
func (s *Store) ForceRotate(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE sessions SET rotated_at=NULL WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *Store) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := tokenFromRequest(r)
		if token == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		sess, err := s.Validate(r.Context(), token)
		if isSessionError(err) {
			s.clearCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Printf("auth: validasi session: %v", err)
			http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
			return
		}
		s.refreshCookie(w, sess)
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), sess.User)))
	})
}

func isSessionError(err error) bool {
	return errors.Is(err, ErrSessionNotFound) || errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrSessionRevoked)
}

func (s *Store) refreshCookie(w http.ResponseWriter, sess *Session) {
	if sess.NewToken != "" {
		s.setCookie(w, sess.NewToken, sess.ExpiresAt)
	}
}

func (s *Store) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Secure,
	})
}

func (s *Store) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Secure,
	})
}

func tokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// rotate mengganti token hanya kalau oldHash masih token aktif. Kalau request
// lain sudah duluan merotasi, hasilnya "" dan token dari request itu yang dipakai.
func (s *Store) rotate(ctx context.Context, oldHash []byte, now time.Time) (string, error) {
	token := rand.Text()
	tag, err := s.db.Exec(ctx, `
		UPDATE sessions SET token_hash=$1, previous_token_hash=$2, rotated_at=$3, last_accessed_at=$3
		WHERE token_hash=$2 AND revoked_at IS NULL`,
		hashToken(token), oldHash, now)
	if err != nil {
		return "", fmt.Errorf("auth: rotasi session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", nil
	}
	return token, nil
}

func (s *Store) findSession(ctx context.Context, tokenHash []byte, now time.Time) (*sessionRow, error) {
	row := &sessionRow{}
	err := s.db.QueryRow(ctx, `
		SELECT id, user_id, token_hash, rotated_at, last_accessed_at, expires_at, revoked_at
		FROM sessions
		WHERE token_hash=$1 OR (previous_token_hash=$1 AND rotated_at > $2)
		LIMIT 1`,
		tokenHash, now.Add(-s.cfg.GracePeriod),
	).Scan(&row.ID, &row.UserID, &row.TokenHash, &row.RotatedAt, &row.LastAccessedAt, &row.ExpiresAt, &row.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: cari session: %w", err)
	}
	return row, nil
}

func (s *Store) fetchUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	u := &User{}
	var disabled bool
	err := s.db.QueryRow(ctx, `SELECT id, username, is_platform_admin, disabled_at IS NOT NULL, created_at, updated_at FROM users WHERE id=$1`, userID).
		Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &disabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("auth: ambil user: %w", err)
	}
	// Session user nonaktif dianggap dicabut, jaga-jaga kalau ada yang lolos
	// dari RevokeUserSessions.
	if disabled {
		return nil, ErrSessionRevoked
	}
	return u, nil
}
