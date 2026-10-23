package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/testutil"
)

var testSessionConfig = SessionConfig{
	IdleTimeout:      30 * time.Minute,
	AbsoluteTimeout:  8 * time.Hour,
	RotationInterval: 15 * time.Minute,
	GracePeriod:      30 * time.Second,
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestStore(t *testing.T) (*Store, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Now()}
	s := NewStore(testutil.DB(t), testSessionConfig)
	s.now = clock.now
	return s, clock
}

func createUser(t *testing.T, db *pgxpool.Pool, username, password string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, HashPassword(password),
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newSession(t *testing.T, s *Store) (uuid.UUID, string) {
	t.Helper()
	userID := createUser(t, s.db, "budi", "rahasia")
	token, _, err := s.Create(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return userID, token
}

func mustValidate(t *testing.T, s *Store, token string) *Session {
	t.Helper()
	sess, err := s.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return sess
}

func TestValidateFreshSession(t *testing.T) {
	s, _ := newTestStore(t)
	userID, token := newSession(t, s)

	sess := mustValidate(t, s, token)
	if sess.User.ID != userID {
		t.Errorf("user salah: %v", sess.User.ID)
	}
	if sess.NewToken != "" {
		t.Error("session baru dari login nggak perlu langsung dirotasi")
	}
}

func TestValidateUnknownToken(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Validate(context.Background(), "token-ngasal"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("mau ErrSessionNotFound, dapat %v", err)
	}
}

func TestIdleTimeout(t *testing.T) {
	s, clock := newTestStore(t)
	_, token := newSession(t, s)

	clock.advance(testSessionConfig.IdleTimeout + time.Second)
	if _, err := s.Validate(context.Background(), token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("mau ErrSessionExpired, dapat %v", err)
	}
}

func TestAbsoluteTimeoutNotExtendedByRotation(t *testing.T) {
	s, clock := newTestStore(t)
	_, token := newSession(t, s)
	firstExpiry := mustValidate(t, s, token).ExpiresAt

	// Aktif terus tiap 20 menit: nggak pernah idle, dan tiap kali dirotasi.
	for elapsed := time.Duration(0); elapsed < testSessionConfig.AbsoluteTimeout-20*time.Minute; elapsed += 20 * time.Minute {
		clock.advance(20 * time.Minute)
		sess := mustValidate(t, s, token)
		if !sess.ExpiresAt.Equal(firstExpiry) {
			t.Fatalf("rotasi memperpanjang expires_at: %v -> %v", firstExpiry, sess.ExpiresAt)
		}
		if sess.NewToken != "" {
			token = sess.NewToken
		}
	}

	clock.advance(20 * time.Minute)
	if _, err := s.Validate(context.Background(), token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("lewat absolute timeout harus ErrSessionExpired, dapat %v", err)
	}
}

func TestRotationAndGracePeriod(t *testing.T) {
	s, clock := newTestStore(t)
	_, oldToken := newSession(t, s)

	clock.advance(testSessionConfig.RotationInterval)
	newToken := mustValidate(t, s, oldToken).NewToken
	if newToken == "" || newToken == oldToken {
		t.Fatal("token harus dirotasi setelah interval rotasi")
	}

	clock.advance(testSessionConfig.GracePeriod - time.Second)
	if sess := mustValidate(t, s, oldToken); sess.NewToken != "" {
		t.Error("token lama dalam grace period nggak boleh memicu rotasi lagi")
	}

	clock.advance(2 * time.Second)
	if _, err := s.Validate(context.Background(), oldToken); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("token lama setelah grace period harus ditolak, dapat %v", err)
	}
	mustValidate(t, s, newToken)
}

// Dua request bareng yang sama-sama mau merotasi token yang sama, yang kedua
// nggak boleh menimpa token hasil rotasi pertama.
func TestConcurrentRotationKeepsFirstToken(t *testing.T) {
	s, clock := newTestStore(t)
	_, oldToken := newSession(t, s)
	clock.advance(testSessionConfig.RotationInterval)

	ctx := context.Background()
	first, err := s.rotate(ctx, hashToken(oldToken), clock.now())
	if err != nil || first == "" {
		t.Fatalf("rotasi pertama gagal: %q %v", first, err)
	}
	second, err := s.rotate(ctx, hashToken(oldToken), clock.now())
	if err != nil {
		t.Fatal(err)
	}
	if second != "" {
		t.Fatal("rotasi kedua dengan token lama seharusnya nggak jalan")
	}
	mustValidate(t, s, first)
}

func TestRevoke(t *testing.T) {
	s, _ := newTestStore(t)
	_, token := newSession(t, s)

	if err := s.Revoke(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Validate(context.Background(), token); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("mau ErrSessionRevoked, dapat %v", err)
	}
}

func TestRevokeUserSessions(t *testing.T) {
	s, _ := newTestStore(t)
	userID, token1 := newSession(t, s)
	token2, _, err := s.Create(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeUserSessions(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{token1, token2} {
		if _, err := s.Validate(context.Background(), tok); !errors.Is(err, ErrSessionRevoked) {
			t.Fatalf("mau ErrSessionRevoked, dapat %v", err)
		}
	}
}

func TestForceRotate(t *testing.T) {
	s, _ := newTestStore(t)
	userID, token := newSession(t, s)

	if err := s.ForceRotate(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	if mustValidate(t, s, token).NewToken == "" {
		t.Fatal("setelah ForceRotate, request berikutnya harus dapat token baru")
	}
}

func TestValidateUserNonaktif(t *testing.T) {
	s, _ := newTestStore(t)
	userID, token := newSession(t, s)
	if _, err := s.db.Exec(context.Background(), `UPDATE users SET disabled_at=now() WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Validate(context.Background(), token); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("mau ErrSessionRevoked, dapat %v", err)
	}
}
