package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/testutil"
)

func seedUser(t *testing.T, db *pgxpool.Pool, username string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`, username,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedDatabase(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO database_connections (name, host, db_name, username, password_enc)
		VALUES ($1, 'localhost', $1, 'app', '\x00')
		RETURNING id`, name,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestListUsersEmpty(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	users, err := s.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Errorf("mau 0 users, dapat %d", len(users))
	}
}

func TestListUsersReturnsAll(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	seedUser(t, db, "alice")
	seedUser(t, db, "bob")

	users, err := s.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Errorf("mau 2 users, dapat %d", len(users))
	}
}

func TestCreateUser(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	ctx := context.Background()

	id, err := s.CreateUser(ctx, "  gina ", "rahasia")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "gina" || u.IsPlatformAdmin {
		t.Errorf("user salah: %+v", u)
	}

	var hash string
	if err := db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifyPassword(hash, "rahasia"); err != nil {
		t.Errorf("password nggak cocok: %v", err)
	}

	if _, err := s.CreateUser(ctx, "gina", "lain"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("username dobel: mau ErrUsernameTaken, dapat %v", err)
	}
	if _, err := s.CreateUser(ctx, " ", "x"); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("username kosong: mau ErrInvalidUser, dapat %v", err)
	}
	if _, err := s.CreateUser(ctx, "hana", ""); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("password kosong: mau ErrInvalidUser, dapat %v", err)
	}
}

func TestListDatabases(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	seedDatabase(t, db, "toko")
	seedDatabase(t, db, "gudang")

	dbs, err := s.ListDatabases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 2 || dbs[0].Name != "gudang" || dbs[1].Name != "toko" {
		t.Errorf("daftar database salah: %+v", dbs)
	}
}

func TestSetPlatformAdmin(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	id := seedUser(t, db, "carol")

	if err := s.SetPlatformAdmin(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsPlatformAdmin {
		t.Error("harus jadi admin")
	}

	if err := s.SetPlatformAdmin(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	u, err = s.GetUser(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if u.IsPlatformAdmin {
		t.Error("seharusnya bukan admin lagi")
	}
}

func TestGroupCRUD(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	ctx := context.Background()

	// Create
	id, err := s.CreateGroup(ctx, "devs", "tim dev")
	if err != nil {
		t.Fatal(err)
	}

	// Read
	g, err := s.GetGroup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "devs" || g.Description != "tim dev" {
		t.Errorf("data group salah: %+v", g)
	}

	// Update
	if err := s.UpdateGroup(ctx, id, "developers", "tim developer"); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GetGroup(ctx, id)
	if g.Name != "developers" {
		t.Errorf("nama nggak berubah: %q", g.Name)
	}

	// List
	groups, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Errorf("mau 1 group, dapat %d", len(groups))
	}

	// Delete
	if err := s.DeleteGroup(ctx, id); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GetGroup(ctx, id)
	if g != nil {
		t.Error("group masih ada setelah dihapus")
	}
}

func TestGroupNameEmptyRejected(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	_, err := s.CreateGroup(context.Background(), "  ", "")
	if err == nil {
		t.Error("nama kosong harus ditolak")
	}
}

func TestMemberAddRemove(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	ctx := context.Background()

	groupID, _ := s.CreateGroup(ctx, "ops", "")
	userID := seedUser(t, db, "dave")

	// Add
	if err := s.AddMember(ctx, groupID, userID); err != nil {
		t.Fatal(err)
	}
	members, err := s.ListMembers(ctx, groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].Username != "dave" {
		t.Errorf("member salah: %+v", members)
	}

	// Duplicate add → no error (ON CONFLICT DO NOTHING)
	if err := s.AddMember(ctx, groupID, userID); err != nil {
		t.Fatal(err)
	}

	// NonMembers
	seedUser(t, db, "eve")
	nonMembers, err := s.NonMembers(ctx, groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nonMembers) != 1 || nonMembers[0].Username != "eve" {
		t.Errorf("non-members salah: %+v", nonMembers)
	}

	// Remove
	if err := s.RemoveMember(ctx, groupID, userID); err != nil {
		t.Fatal(err)
	}
	members, _ = s.ListMembers(ctx, groupID)
	if len(members) != 0 {
		t.Error("member masih ada setelah dikeluarkan")
	}
}

func TestGrantSetAndRevoke(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	ctx := context.Background()

	groupID, _ := s.CreateGroup(ctx, "testers", "")
	dbID := seedDatabase(t, db, "toko")

	// Set grant
	if err := s.SetGrant(ctx, groupID, dbID, "view_only"); err != nil {
		t.Fatal(err)
	}
	grants, err := s.ListGrants(ctx, groupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Level != "view_only" || grants[0].DatabaseName != "toko" {
		t.Errorf("grant salah: %+v", grants)
	}

	// Upsert → level berubah
	if err := s.SetGrant(ctx, groupID, dbID, "view_edit"); err != nil {
		t.Fatal(err)
	}
	grants, _ = s.ListGrants(ctx, groupID)
	if grants[0].Level != "view_edit" {
		t.Errorf("level nggak berubah: %q", grants[0].Level)
	}

	// Invalid level
	if err := s.SetGrant(ctx, groupID, dbID, "superadmin"); err == nil {
		t.Error("level invalid harus ditolak")
	}

	// Revoke
	if err := s.RevokeGrant(ctx, groupID, dbID); err != nil {
		t.Fatal(err)
	}
	grants, _ = s.ListGrants(ctx, groupID)
	if len(grants) != 0 {
		t.Error("grant masih ada setelah dicabut")
	}
}

func TestDeleteGroupCascadesMembers(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	ctx := context.Background()

	groupID, _ := s.CreateGroup(ctx, "temp", "")
	userID := seedUser(t, db, "frank")
	s.AddMember(ctx, groupID, userID)
	if err := s.SetGrant(ctx, groupID, seedDatabase(t, db, "toko"), "view_only"); err != nil {
		t.Fatal(err)
	}

	// Delete group → members dan grants ikut terhapus (ON DELETE CASCADE)
	if err := s.DeleteGroup(ctx, groupID); err != nil {
		t.Fatal(err)
	}

	members, _ := s.ListMembers(ctx, groupID)
	if len(members) != 0 {
		t.Error("members masih ada setelah group dihapus")
	}
	grants, _ := s.ListGrants(ctx, groupID)
	if len(grants) != 0 {
		t.Error("grants masih ada setelah group dihapus")
	}
}
