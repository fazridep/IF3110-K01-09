package auth
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidCredentials = errors.New("auth: username atau password salah")
	ErrSessionNotFound    = errors.New("auth: session tidak ditemukan")
	ErrSessionExpired     = errors.New("auth: session sudah berakhir")
	ErrSessionRevoked     = errors.New("auth: session telah dicabut")
	ErrCSRFTokenInvalid   = errors.New("auth: CSRF token tidak valid")
)

type User struct {
	ID              uuid.UUID
	Username        string
	IsPlatformAdmin bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
type contextKey int
const (
	ctxUser contextKey = iota
	ctxCSRF
)

func CurrentUser(ctx context.Context) *User {
	u, _ := ctx.Value(ctxUser).(*User)
	return u
}

func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxUser, u)
}

func withCSRFToken(ctx context.Context, t string) context.Context {
	return context.WithValue(ctx, ctxCSRF, t)
}

func csrfFromCtx(ctx context.Context) string {
	t, _ := ctx.Value(ctxCSRF).(string)
	return t
}

// password
const (
	argonMemory  = 64 * 1024
	argonTime    = 1
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

func HashPassword(plaintext string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plaintext), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		hex.EncodeToString(salt), hex.EncodeToString(hash),
	), nil
}

func VerifyPassword(encoded, plaintext string) error {
	mem, tc, threads, salt, stored, err := parseArgon2Hash(encoded)
	if err != nil {
		return fmt.Errorf("auth: hash tidak valid: %w", err)
	}
	candidate := argon2.IDKey([]byte(plaintext), salt, tc, mem, threads, uint32(len(stored)))
	if subtle.ConstantTimeCompare(candidate, stored) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

func parseArgon2Hash(encoded string) (mem, tc uint32, threads uint8, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, errors.New("format tidak dikenal")
	}
	var ver int
	if _, e := fmt.Sscanf(parts[2], "v=%d", &ver); e != nil || ver != argon2.Version {
		return 0, 0, 0, nil, nil, errors.New("versi tidak sesuai")
	}
	if _, e := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &tc, &threads); e != nil {
		return 0, 0, 0, nil, nil, errors.New("parameter tidak valid")
	}
	salt, err = hex.DecodeString(parts[4])
	if err != nil {
		return
	}
	hash, err = hex.DecodeString(parts[5])
	return
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}