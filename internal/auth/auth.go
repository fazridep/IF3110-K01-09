package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("auth: username atau password salah")
	ErrSessionNotFound    = errors.New("auth: session tidak ditemukan")
	ErrSessionExpired     = errors.New("auth: session sudah berakhir")
	ErrSessionRevoked     = errors.New("auth: session telah dicabut")
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
