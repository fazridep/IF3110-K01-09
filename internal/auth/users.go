package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dummyHash dipakai saat username nggak ada, supaya waktu responsnya sama
// dengan saat password salah.
const dummyHash = "$argon2id$v=19$m=65536,t=1,p=4$00000000000000000000000000000000$0000000000000000000000000000000000000000000000000000000000000000"

func authenticate(ctx context.Context, db *pgxpool.Pool, username, password string) (*User, error) {
	u := &User{}
	var hash string
	err := db.QueryRow(ctx, `
		SELECT id, username, password_hash, is_platform_admin, created_at, updated_at
		FROM users WHERE username=$1`, username,
	).Scan(&u.ID, &u.Username, &hash, &u.IsPlatformAdmin, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = VerifyPassword(dummyHash, password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("auth: cari user: %w", err)
	}
	if err := VerifyPassword(hash, password); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

func SeedAdmin(ctx context.Context, db *pgxpool.Pool, username, password string) (uuid.UUID, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return uuid.Nil, errors.New("auth: username dan password tidak boleh kosong")
	}
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, is_platform_admin)
		VALUES ($1, $2, TRUE)
		ON CONFLICT (username) DO NOTHING
		RETURNING id`, username, HashPassword(password),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("auth: username %q sudah ada", username)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("auth: simpan admin: %w", err)
	}
	return id, nil
}
