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

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/pgerr"
)

// --- Model ---

type User struct {
	ID              uuid.UUID
	Username        string
	IsPlatformAdmin bool
	Disabled        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Group struct {
	ID             uuid.UUID
	Name           string
	Description    string
	LeaderID       *uuid.UUID
	LeaderUsername string
	MemberCount    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Member struct {
	UserID   uuid.UUID
	Username string
	IsLeader bool
	AddedAt  time.Time
}

type Grant struct {
	GroupID      uuid.UUID
	GroupName    string
	DatabaseID   uuid.UUID
	DatabaseName string
	Level        string
	GrantedAt    time.Time
}

// Database cuma id dan nama koneksi, buat pilihan di form grant.
type Database struct {
	ID   uuid.UUID
	Name string
}

var (
	ErrUsernameTaken    = errors.New("admin: username sudah dipakai")
	ErrInvalidUser      = errors.New("admin: username dan password wajib diisi")
	ErrInvalidLevel     = errors.New("admin: level harus view_only atau view_edit")
	ErrGroupNameTaken   = errors.New("admin: nama group sudah dipakai")
	ErrInvalidGroup     = errors.New("admin: nama group tidak boleh kosong")
	ErrGroupNotFound    = errors.New("admin: group tidak ditemukan")
	ErrUserNotFound     = errors.New("admin: user tidak ditemukan")
	ErrDatabaseNotFound = errors.New("admin: database tidak ditemukan")
	ErrLeaderNotMember  = errors.New("admin: leader harus merupakan anggota aktif dari group ini")
)

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
		SELECT id, username, is_platform_admin, disabled_at IS NOT NULL, created_at, updated_at
		FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("admin: list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.Disabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `
		SELECT id, username, is_platform_admin, disabled_at IS NOT NULL, created_at, updated_at
		FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("admin: get user: %w", err)
	}
	return u, nil
}

// CreateUser bikin user biasa. Jadi admin lewat toggle di halaman users.
func (s *Store) CreateUser(ctx context.Context, username, password string) (uuid.UUID, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return uuid.Nil, ErrInvalidUser
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO users (username, password_hash) VALUES ($1, $2)
		ON CONFLICT (username) DO NOTHING
		RETURNING id`, username, auth.HashPassword(password),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrUsernameTaken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("admin: buat user: %w", err)
	}
	return id, nil
}

// SetDisabled nonaktifkan atau aktifkan lagi user. Cabut session-nya urusan handler.
func (s *Store) SetDisabled(ctx context.Context, userID uuid.UUID, disabled bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("admin: set disabled: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE users SET disabled_at = CASE WHEN $1 THEN now() END, updated_at=now()
		WHERE id=$2`, disabled, userID); err != nil {
		return fmt.Errorf("admin: set disabled: %w", err)
	}
	if disabled {
		if _, err := tx.Exec(ctx, `UPDATE groups SET leader_id = NULL, updated_at = now() WHERE leader_id = $1`, userID); err != nil {
			return fmt.Errorf("admin: reset leader user nonaktif: %w", err)
		}
	}
	return tx.Commit(ctx)
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
		SELECT g.id, g.name, g.description, g.leader_id, COALESCE(u.username, ''), COUNT(gm.user_id), g.created_at, g.updated_at
		FROM groups g
		LEFT JOIN users u ON u.id = g.leader_id
		LEFT JOIN group_members gm ON gm.group_id = g.id
		GROUP BY g.id, u.username
		ORDER BY g.name`)
	if err != nil {
		return nil, fmt.Errorf("admin: list groups: %w", err)
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.LeaderID, &g.LeaderUsername, &g.MemberCount, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("admin: scan group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) GetGroup(ctx context.Context, id uuid.UUID) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(ctx, `
		SELECT g.id, g.name, g.description, g.leader_id, COALESCE(u.username, ''), COUNT(gm.user_id), g.created_at, g.updated_at
		FROM groups g
		LEFT JOIN users u ON u.id = g.leader_id
		LEFT JOIN group_members gm ON gm.group_id = g.id
		WHERE g.id=$1
		GROUP BY g.id, u.username`, id,
	).Scan(&g.ID, &g.Name, &g.Description, &g.LeaderID, &g.LeaderUsername, &g.MemberCount, &g.CreatedAt, &g.UpdatedAt)
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
		return uuid.Nil, ErrInvalidGroup
	}
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO groups (name, description) VALUES ($1, $2) RETURNING id`,
		name, strings.TrimSpace(description),
	).Scan(&id)
	if pgerr.IsUniqueViolation(err) {
		return uuid.Nil, ErrGroupNameTaken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("admin: buat group: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateGroup(ctx context.Context, id uuid.UUID, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrInvalidGroup
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE groups SET name=$1, description=$2, updated_at=now() WHERE id=$3`,
		name, strings.TrimSpace(description), id)
	if pgerr.IsUniqueViolation(err) {
		return ErrGroupNameTaken
	}
	if err != nil {
		return fmt.Errorf("admin: update group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGroupNotFound
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

// SetLeader menunjuk atau mencabut leader group. Leader wajib merupakan anggota
// aktif dari group tersebut; jika leaderID nil, leader dikosongkan.
func (s *Store) SetLeader(ctx context.Context, groupID uuid.UUID, leaderID *uuid.UUID) error {
	if leaderID == nil {
		tag, err := s.db.Exec(ctx, `
			UPDATE groups SET leader_id = NULL, updated_at = now() WHERE id = $1`, groupID)
		if err != nil {
			return fmt.Errorf("admin: cabut leader: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrGroupNotFound
		}
		return nil
	}

	g, err := s.GetGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if g == nil {
		return ErrGroupNotFound
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE groups g
		SET leader_id = $2, updated_at = now()
		FROM group_members gm
		JOIN users u ON u.id = gm.user_id
		WHERE g.id = $1
		  AND gm.group_id = g.id
		  AND gm.user_id = $2
		  AND u.disabled_at IS NULL`,
		groupID, *leaderID)
	if err != nil {
		return fmt.Errorf("admin: set leader: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaderNotMember
	}
	return nil
}

// --- Member queries ---

func (s *Store) ListMembers(ctx context.Context, groupID uuid.UUID) ([]Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.username, (g.leader_id IS NOT NULL AND g.leader_id = u.id), gm.added_at
		FROM group_members gm
		JOIN groups g ON g.id = gm.group_id
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
		if err := rows.Scan(&m.UserID, &m.Username, &m.IsLeader, &m.AddedAt); err != nil {
			return nil, fmt.Errorf("admin: scan member: %w", err)
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// NonMembers mengembalikan user yang belum menjadi anggota group tertentu.
func (s *Store) NonMembers(ctx context.Context, groupID uuid.UUID) ([]User, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, username, is_platform_admin, disabled_at IS NOT NULL, created_at, updated_at
		FROM users
		WHERE id NOT IN (SELECT user_id FROM group_members WHERE group_id = $1)
		  AND disabled_at IS NULL
		ORDER BY username`, groupID)
	if err != nil {
		return nil, fmt.Errorf("admin: non-members: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsPlatformAdmin, &u.Disabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
	if pgerr.IsForeignKeyViolation(err) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("admin: tambah member: %w", err)
	}
	return nil
}

func (s *Store) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("admin: begin remove member: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`, groupID, userID); err != nil {
		return fmt.Errorf("admin: hapus member: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE groups SET leader_id = NULL, updated_at = now() WHERE id = $1 AND leader_id = $2`, groupID, userID); err != nil {
		return fmt.Errorf("admin: reset leader saat hapus member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("admin: commit remove member: %w", err)
	}
	return nil
}

// --- Grant queries ---

func (s *Store) ListGrants(ctx context.Context, groupID uuid.UUID) ([]Grant, error) {
	rows, err := s.db.Query(ctx, `
		SELECT gda.group_id, gda.database_id, dc.name, gda.level, gda.granted_at
		FROM group_database_access gda
		JOIN database_connections dc ON dc.id = gda.database_id
		WHERE gda.group_id = $1
		ORDER BY dc.name`, groupID)
	if err != nil {
		return nil, fmt.Errorf("admin: list grants: %w", err)
	}
	defer rows.Close()

	var grants []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.GroupID, &g.DatabaseID, &g.DatabaseName, &g.Level, &g.GrantedAt); err != nil {
			return nil, fmt.Errorf("admin: scan grant: %w", err)
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (s *Store) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name FROM database_connections ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("admin: list databases: %w", err)
	}
	dbs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Database])
	if err != nil {
		return nil, fmt.Errorf("admin: scan database: %w", err)
	}
	return dbs, nil
}

func (s *Store) SetGrant(ctx context.Context, groupID, databaseID uuid.UUID, level access.Level) error {
	if !level.Valid() {
		return ErrInvalidLevel
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO group_database_access (group_id, database_id, level)
		VALUES ($1, $2, $3)
		ON CONFLICT (group_id, database_id) DO UPDATE SET level=$3, granted_at=now()`,
		groupID, databaseID, level)
	if pgerr.IsForeignKeyViolation(err) {
		return ErrDatabaseNotFound
	}
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
