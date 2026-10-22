package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/testutil"
)

func createUser(t *testing.T, db *pgxpool.Pool, username string, isAdmin bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash, is_platform_admin) VALUES ($1, 'x', $2) RETURNING id`,
		username, isAdmin,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func createGroup(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(),
		`INSERT INTO groups (name) VALUES ($1) RETURNING id`, name,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func addMember(t *testing.T, db *pgxpool.Pool, groupID, userID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, userID); err != nil {
		t.Fatal(err)
	}
}

func setGrant(t *testing.T, db *pgxpool.Pool, groupID, dbID uuid.UUID, level string) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`INSERT INTO group_database_access (group_id, database_id, level) VALUES ($1, $2, $3)`,
		groupID, dbID, level); err != nil {
		t.Fatal(err)
	}
}

func TestMeetsMinimum(t *testing.T) {
	tests := []struct {
		have, need Level
		want       bool
	}{
		{ViewEdit, ViewEdit, true},
		{ViewEdit, ViewOnly, true},
		{ViewOnly, ViewOnly, true},
		{ViewOnly, ViewEdit, false},
		{NoAccess, ViewOnly, false},
		{NoAccess, ViewEdit, false},
	}
	for _, tc := range tests {
		got := meetsMinimum(tc.have, tc.need)
		if got != tc.want {
			t.Errorf("meetsMinimum(%q, %q) = %v, want %v", tc.have, tc.need, got, tc.want)
		}
	}
}

func TestEffectiveLevelNoAccess(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	userID := createUser(t, db, "noaccess", false)
	dbID := uuid.New()

	level, err := s.EffectiveLevel(context.Background(), userID, dbID)
	if err != nil {
		t.Fatal(err)
	}
	if level != NoAccess {
		t.Errorf("mau NoAccess, dapat %q", level)
	}
}

func TestEffectiveLevelFromGroup(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	userID := createUser(t, db, "member", false)
	dbID := uuid.New()
	groupID := createGroup(t, db, "devs")

	addMember(t, db, groupID, userID)
	setGrant(t, db, groupID, dbID, "view_only")

	level, err := s.EffectiveLevel(context.Background(), userID, dbID)
	if err != nil {
		t.Fatal(err)
	}
	if level != ViewOnly {
		t.Errorf("mau view_only, dapat %q", level)
	}
}

func TestEffectiveLevelMaxAcrossGroups(t *testing.T) {
	db := testutil.DB(t)
	s := NewStore(db)
	userID := createUser(t, db, "multi", false)
	dbID := uuid.New()

	g1 := createGroup(t, db, "readers")
	g2 := createGroup(t, db, "editors")
	addMember(t, db, g1, userID)
	addMember(t, db, g2, userID)
	setGrant(t, db, g1, dbID, "view_only")
	setGrant(t, db, g2, dbID, "view_edit")

	level, err := s.EffectiveLevel(context.Background(), userID, dbID)
	if err != nil {
		t.Fatal(err)
	}
	if level != ViewEdit {
		t.Errorf("mau view_edit (tertinggi), dapat %q", level)
	}
}

func TestRequirePlatformAdminBlocks(t *testing.T) {
	reached := false
	handler := RequirePlatformAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	// Tanpa user di context → harus ditolak
	req := httptest.NewRequest("GET", "/admin/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("tanpa user: mau 403, dapat %d", rec.Code)
	}
	if reached {
		t.Error("handler tujuan nggak boleh dipanggil")
	}
}
