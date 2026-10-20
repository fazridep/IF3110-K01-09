package auth
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	cookieName = "sid"
	tokenLen   = 32
)

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
}

func NewStore(db *pgxpool.Pool, cfg SessionConfig) *Store {
	return &Store{db: db, cfg: cfg}
}

type sessionRow struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	TokenHash         []byte
	PreviousTokenHash []byte
	RotatedAt         *time.Time
	CreatedAt         time.Time
	LastAccessedAt    time.Time
	ExpiresAt         time.Time
	RevokedAt         *time.Time
}

func (s *Store) Create(ctx context.Context, userID uuid.UUID) (string, error) {
	raw := make([]byte, tokenLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	token := hex.EncodeToString(raw)
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, created_at, last_accessed_at, expires_at) VALUES ($1, $2, $3, $3, $4)`, userID, hashToken(token), now, now.Add(s.cfg.AbsoluteTimeout))
	if err != nil {
		return "", fmt.Errorf("auth: simpan session: %w", err)
	}
	return token, nil
}

func (s *Store) Validate(ctx context.Context, token string) (user *User, rotatedToken string, err error) {
	now := time.Now().UTC()
	row, err := s.findSession(ctx, hashToken(token), now)
	if err != nil {
		return nil, "", err
	}
	if row.RevokedAt != nil {
		return nil, "", ErrSessionRevoked
	}
	if now.After(row.ExpiresAt) || now.Sub(row.LastAccessedAt) > s.cfg.IdleTimeout {
		return nil, "", ErrSessionExpired
	}
	u, err := s.fetchUser(ctx, row.UserID)
	if err != nil {
		return nil, "", err
	}
	needRotation := row.RotatedAt == nil || now.Sub(*row.RotatedAt) >= s.cfg.RotationInterval
	if needRotation {
		rotatedToken, _ = s.rotate(ctx, row.TokenHash, now)
	} else {
		_, _ = s.db.Exec(ctx, `UPDATE sessions SET last_accessed_at=$1 WHERE token_hash=$2`, now, row.TokenHash)
	}
	return u, rotatedToken, nil
}

func (s *Store) Revoke(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `UPDATE sessions SET revoked_at=$1 WHERE (token_hash=$2 OR previous_token_hash=$2) AND revoked_at IS NULL`, time.Now().UTC(), hashToken(token))
	return err
}

func (s *Store) RevokeUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE sessions SET revoked_at=$1 WHERE user_id=$2 AND revoked_at IS NULL`, time.Now().UTC(), userID)
	return err
}

func (s *Store) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := TokenFromRequest(r)
		if token == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user, rotatedToken, err := s.Validate(r.Context(), token)
		if err != nil {
			ClearCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if rotatedToken != "" {
			s.SetCookie(w, rotatedToken, time.Time{})
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

func (s *Store) SetCookie(w http.ResponseWriter, token string, expires time.Time) {
	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Secure,
	}
	if !expires.IsZero() {
		cookie.Expires = expires
	}
	http.SetCookie(w, cookie)
}

func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Store) rotate(ctx context.Context, oldHash []byte, now time.Time) (string, error) {
	raw := make([]byte, tokenLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	newToken := hex.EncodeToString(raw)
	_, err := s.db.Exec(ctx, `UPDATE sessions SET token_hash=$1, previous_token_hash=$2, rotated_at=$3, last_accessed_at=$3 WHERE token_hash=$2 OR (previous_token_hash=$2 AND rotated_at > $4)`, hashToken(newToken), oldHash, now, now.Add(-s.cfg.GracePeriod))
	if err != nil {
		return "", err
	}
	return newToken, nil
}

func (s *Store) findSession(ctx context.Context, tokenHash []byte, now time.Time) (*sessionRow, error) {
	row := &sessionRow{}
	err := s.db.QueryRow(ctx, `SELECT id, user_id, token_hash, previous_token_hash, rotated_at, created_at, last_accessed_at, expires_at, revoked_at FROM sessions WHERE token_hash=$1 OR (previous_token_hash=$1 AND rotated_at > $2) LIMIT 1`, tokenHash, now.Add(-s.cfg.GracePeriod)).Scan(&row.ID, &row.UserID, &row.TokenHash, &row.PreviousTokenHash, &row.RotatedAt, &row.CreatedAt, &row.LastAccessedAt, &row.ExpiresAt, &row.RevokedAt)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return row, nil
}

func (s *Store) fetchUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `SELECT id, username, is_platform_admin, created_at, updated_at FROM users WHERE id=$1`, userID).Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("auth: user tidak ditemukan: %w", err)
	}
	return u, nil
}