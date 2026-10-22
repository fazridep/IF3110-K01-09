// Package admin menyediakan handler halaman administrasi platform:
// CRUD user, CRUD group, anggota group, dan grant akses database per group.
// Semua aksi diproteksi oleh access.RequirePlatformAdmin.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- Model ---

type User struct {
	ID              uuid.UUID
	Username        string
	IsPlatformAdmin bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Group struct {
	ID          uuid.UUID
	Name        string
	Description string
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Member struct {
	UserID   uuid.UUID
	Username string
	AddedAt  time.Time
}

type Grant struct {
	GroupID    uuid.UUID
	GroupName  string
	DatabaseID uuid.UUID
	Level      string
	GrantedAt  time.Time
}

// --- Store ---

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// --- User queries ---

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, username, is_platform_admin, created_at, updated_at
		FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("admin: list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `
		SELECT id, username, is_platform_admin, created_at, updated_at
		FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get user: %w", err)
	}
	return u, nil
}

func (s *Store) SetPlatformAdmin(ctx context.Context, userID uuid.UUID, isAdmin bool) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET is_platform_admin=$1, updated_at=now() WHERE id=$2`, isAdmin, userID)
	if err != nil {
		return fmt.Errorf("admin: set admin: %w", err)
	}
	return nil
}

// --- Group queries ---

func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id, g.name, g.description, COUNT(gm.user_id), g.created_at, g.updated_at
		FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id
		GROUP BY g.id
		ORDER BY g.name`)
	if err != nil {
		return nil, fmt.Errorf("admin: list groups: %w", err)
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.MemberCount, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) GetGroup(ctx context.Context, id uuid.UUID) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(ctx, `
		SELECT g.id, g.name, g.description, COUNT(gm.user_id), g.created_at, g.updated_at
		FROM groups g
		LEFT JOIN group_members gm ON gm.group_id = g.id
		WHERE g.id=$1
		GROUP BY g.id`, id,
	).Scan(&g.ID, &g.Name, &g.Description, &g.MemberCount, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get group: %w", err)
	}
	return g, nil
}

func (s *Store) CreateGroup(ctx context.Context, name, description string) (uuid.UUID, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return uuid.Nil, errors.New("admin: nama group tidak boleh kosong")
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO groups (name, description) VALUES ($1, $2) RETURNING id`,
		name, strings.TrimSpace(description),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("admin: buat group: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateGroup(ctx context.Context, id uuid.UUID, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("admin: nama group tidak boleh kosong")
	}
	_, err := s.db.Exec(ctx, `
		UPDATE groups SET name=$1, description=$2, updated_at=now() WHERE id=$3`,
		name, strings.TrimSpace(description), id)
	if err != nil {
		return fmt.Errorf("admin: update group: %w", err)
	}
	return nil
}

func (s *Store) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM groups WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("admin: hapus group: %w", err)
	}
	return nil
}

// --- Member queries ---

func (s *Store) ListMembers(ctx context.Context, groupID uuid.UUID) ([]Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.username, gm.added_at
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE gm.group_id = $1
		ORDER BY u.username`, groupID)
	if err != nil {
		return nil, fmt.Errorf("admin: list members: %w", err)
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Username, &m.AddedAt); err != nil {
			return nil, fmt.Errorf("admin: scan member: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// NonMembers mengembalikan user yang belum menjadi anggota group tertentu.
func (s *Store) NonMembers(ctx context.Context, groupID uuid.UUID) ([]User, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, username, is_platform_admin, created_at, updated_at
		FROM users
		WHERE id NOT IN (SELECT user_id FROM group_members WHERE group_id = $1)
		ORDER BY username`, groupID)
	if err != nil {
		return nil, fmt.Errorf("admin: non-members: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan non-member: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) AddMember(ctx context.Context, groupID, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, groupID, userID)
	if err != nil {
		return fmt.Errorf("admin: tambah member: %w", err)
	}
	return nil
}

func (s *Store) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`, groupID, userID)
	if err != nil {
		return fmt.Errorf("admin: hapus member: %w", err)
	}
	return nil
}

// --- Grant queries ---

func (s *Store) ListGrants(ctx context.Context, groupID uuid.UUID) ([]Grant, error) {
	rows, err := s.db.Query(ctx, `
		SELECT group_id, database_id, level, granted_at
		FROM group_database_access
		WHERE group_id = $1
		ORDER BY granted_at`, groupID)
	if err != nil {
		return nil, fmt.Errorf("admin: list grants: %w", err)
	}
	defer rows.Close()

	var grants []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.GroupID, &g.DatabaseID, &g.Level, &g.GrantedAt); err != nil {
			return nil, fmt.Errorf("admin: scan grant: %w", err)
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (s *Store) SetGrant(ctx context.Context, groupID, databaseID uuid.UUID, level string) error {
	if level != "view_only" && level != "view_edit" {
		return errors.New("admin: level harus view_only atau view_edit")
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO group_database_access (group_id, database_id, level)
		VALUES ($1, $2, $3)
		ON CONFLICT (group_id, database_id) DO UPDATE SET level=$3, granted_at=now()`,
		groupID, databaseID, level)
	if err != nil {
		return fmt.Errorf("admin: set grant: %w", err)
	}
	return nil
}

func (s *Store) RevokeGrant(ctx context.Context, groupID, databaseID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM group_database_access WHERE group_id=$1 AND database_id=$2`, groupID, databaseID)
	if err != nil {
		return fmt.Errorf("admin: revoke grant: %w", err)
	}
	return nil
}
