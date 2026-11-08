package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
	"github.com/if3110/tubes-db-platform/internal/platform/testutil"
)

func newTestMux(t *testing.T) (*http.ServeMux, *Store) {
	t.Helper()
	db := testutil.DB(t)
	store := NewStore(db)
	h := NewHandler(store, auth.NewStore(db, auth.SessionConfig{}))
	mux := http.NewServeMux()
	h.RegisterAPIRoutes(mux)
	return mux, store
}

func doJSON(mux *http.ServeMux, method, target, body string, u *auth.User) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if u != nil {
		r = r.WithContext(auth.WithUser(r.Context(), u))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
		t.Fatalf("mau problem+json, dapat %q: %s", ct, rec.Body.String())
	}
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBukanAdminDitolak(t *testing.T) {
	mux, store := newTestMux(t)
	biasa := &auth.User{ID: uuid.New(), Username: "biasa"}

	for _, path := range []string{"/api/admin/users", "/api/admin/groups"} {
		rec := doJSON(mux, http.MethodGet, path, "", biasa)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s: mau 403, dapat %d", path, rec.Code)
		}
		if p := decodeProblem(t, rec); p.Type != "urn:problem:forbidden" {
			t.Errorf("GET %s: mau urn:problem:forbidden, dapat %q", path, p.Type)
		}
	}

	rec := doJSON(mux, http.MethodPost, "/api/admin/users", `{"username":"susup","password":"x"}`, biasa)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/admin/users: mau 403, dapat %d", rec.Code)
	}
	users, _ := store.ListUsers(context.Background())
	if len(users) != 0 {
		t.Fatalf("user nggak boleh kebuat: %+v", users)
	}
}

func TestAdminTambahUser(t *testing.T) {
	mux, store := newTestMux(t)
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsPlatformAdmin: true}

	rec := doJSON(mux, http.MethodPost, "/api/admin/users", `{"username":"gina","password":"rahasia"}`, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d: %s", rec.Code, rec.Body.String())
	}
	users, _ := store.ListUsers(context.Background())
	if len(users) != 1 || users[0].Username != "gina" {
		t.Fatalf("user nggak tersimpan: %+v", users)
	}

	// Username dobel -> 409 Conflict dengan field error
	rec = doJSON(mux, http.MethodPost, "/api/admin/users", `{"username":"gina","password":"lain"}`, admin)
	if rec.Code != http.StatusConflict {
		t.Fatalf("username dobel: mau 409, dapat %d", rec.Code)
	}
	if p := decodeProblem(t, rec); len(p.Errors) == 0 || p.Errors[0].Field != "username" {
		t.Fatalf("harus ada field error username: %+v", p)
	}

	// Password kosong -> 400 Bad Request
	rec = doJSON(mux, http.MethodPost, "/api/admin/users", `{"username":"hana","password":""}`, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("password kosong: mau 400, dapat %d", rec.Code)
	}
}

func TestNonaktifkanDanAdminUser(t *testing.T) {
	mux, store := newTestMux(t)
	ctx := context.Background()
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsPlatformAdmin: true}

	id, err := store.CreateUser(ctx, "gina", "rahasia")
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewStore(store.db, auth.SessionConfig{})
	if _, _, err := sessions.Create(ctx, id); err != nil {
		t.Fatal(err)
	}
	groupID, _ := store.CreateGroup(ctx, "devs", "")
	_ = store.AddMember(ctx, groupID, id)
	_ = store.SetLeader(ctx, groupID, &id)

	path := "/api/admin/users/" + id.String()
	if rec := doJSON(mux, http.MethodPatch, path, `{"disabled":true}`, admin); rec.Code != http.StatusOK {
		t.Fatalf("nonaktifkan: mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	u, _ := store.GetUser(ctx, id)
	if !u.Disabled {
		t.Fatal("user harusnya nonaktif")
	}
	var aktif int
	store.db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, id).Scan(&aktif)
	if aktif != 0 {
		t.Fatalf("session user nonaktif harus dicabut, masih %d", aktif)
	}
	g, _ := store.GetGroup(ctx, groupID)
	if g.LeaderID != nil {
		t.Fatalf("leader group harus di-reset saat user dinonaktifkan: %+v", g)
	}

	// Aktifkan kembali
	if rec := doJSON(mux, http.MethodPatch, path, `{"disabled":false}`, admin); rec.Code != http.StatusOK {
		t.Fatalf("aktifkan: mau 200, dapat %d", rec.Code)
	}
	if u, _ := store.GetUser(ctx, id); u.Disabled {
		t.Fatal("user harusnya aktif lagi")
	}

	// Nonaktifkan atau ubah admin diri sendiri harus ditolak 403
	self := &auth.User{ID: id, Username: "gina", IsPlatformAdmin: true}
	if rec := doJSON(mux, http.MethodPatch, path, `{"disabled":true}`, self); rec.Code != http.StatusForbidden {
		t.Fatalf("nonaktifkan diri sendiri: mau 403, dapat %d", rec.Code)
	}
	if rec := doJSON(mux, http.MethodPatch, path, `{"isPlatformAdmin":false}`, self); rec.Code != http.StatusForbidden {
		t.Fatalf("ubah admin diri sendiri: mau 403, dapat %d", rec.Code)
	}
}

func TestGroupsLeaderMembersGrantsAPI(t *testing.T) {
	mux, store := newTestMux(t)
	ctx := context.Background()
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsPlatformAdmin: true}

	userID, _ := store.CreateUser(ctx, "budi", "rahasia")
	outsiderID, _ := store.CreateUser(ctx, "citra", "rahasia")
	dbID := seedDatabase(t, store.db, "toko")

	// Buat group
	rec := doJSON(mux, http.MethodPost, "/api/admin/groups", `{"name":"devs","description":"Tim Developer"}`, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("buat group: mau 201, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var created groupJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Set leader ke outsider (bukan anggota) -> 400
	rec = doJSON(mux, http.MethodPut, "/api/admin/groups/"+created.ID.String()+"/leader", `{"userId":"`+outsiderID.String()+`"}`, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("leader bukan anggota: mau 400, dapat %d: %s", rec.Code, rec.Body.String())
	}

	// Tambah anggota budi
	rec = doJSON(mux, http.MethodPost, "/api/admin/groups/"+created.ID.String()+"/members", `{"userId":"`+userID.String()+`"}`, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("tambah member: mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}

	// Set budi jadi leader -> 200
	rec = doJSON(mux, http.MethodPut, "/api/admin/groups/"+created.ID.String()+"/leader", `{"userId":"`+userID.String()+`"}`, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("set leader: mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var updatedGroup groupJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &updatedGroup)
	if updatedGroup.LeaderID == nil || *updatedGroup.LeaderID != userID || updatedGroup.LeaderUsername != "budi" {
		t.Fatalf("leader group tidak ter-set: %+v", updatedGroup)
	}

	// Set grant database
	rec = doJSON(mux, http.MethodPut, "/api/admin/groups/"+created.ID.String()+"/grants",
		`{"databaseId":"`+dbID.String()+`","level":"view_edit"}`, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("set grant: mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}

	// Set grant level tidak valid -> 400
	rec = doJSON(mux, http.MethodPut, "/api/admin/groups/"+created.ID.String()+"/grants",
		`{"databaseId":"`+dbID.String()+`","level":"superadmin"}`, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("grant invalid: mau 400, dapat %d", rec.Code)
	}

	// Show detail group
	rec = doJSON(mux, http.MethodGet, "/api/admin/groups/"+created.ID.String(), "", admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("show group: mau 200, dapat %d", rec.Code)
	}
	var detail groupDetailJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Members) != 1 || !detail.Members[0].IsLeader || len(detail.Grants) != 1 {
		t.Fatalf("detail group tidak sesuai: %+v", detail)
	}

	// Keluarkan budi -> 204 & leader otomatis kosong
	rec = doJSON(mux, http.MethodDelete, "/api/admin/groups/"+created.ID.String()+"/members/"+userID.String(), "", admin)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: mau 204, dapat %d", rec.Code)
	}
	g, _ := store.GetGroup(ctx, created.ID)
	if g.LeaderID != nil {
		t.Fatalf("leader harus di-reset saat member dikeluarkan, dapat %v", g.LeaderID)
	}
}
