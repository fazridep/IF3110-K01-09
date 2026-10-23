package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/testutil"
	"github.com/if3110/tubes-db-platform/internal/web"
)

type fakeView struct {
	name string
	data any
}

func (f *fakeView) Render(w http.ResponseWriter, r *http.Request, name string, data any) {
	f.name = name
	f.data = data
}

func newTestMux(t *testing.T) (*http.ServeMux, *fakeView, *Store) {
	t.Helper()
	db := testutil.DB(t)
	store := NewStore(db)
	view := &fakeView{}
	h := NewHandler(db, store, auth.NewStore(db, auth.SessionConfig{}), view)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, view, store
}

func post(mux *http.ServeMux, target string, form url.Values, u *auth.User) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(auth.WithUser(r.Context(), u))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestBukanAdminDitolak(t *testing.T) {
	mux, _, store := newTestMux(t)
	biasa := &auth.User{ID: uuid.New(), Username: "biasa"}

	for _, path := range []string{"/admin/users", "/admin/groups"} {
		r := httptest.NewRequest("GET", path, nil)
		r = r.WithContext(auth.WithUser(r.Context(), biasa))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s: mau 403, dapat %d", path, rec.Code)
		}
	}

	rec := post(mux, "/admin/users", url.Values{"username": {"susup"}, "password": {"x"}}, biasa)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /admin/users: mau 403, dapat %d", rec.Code)
	}
	users, _ := store.ListUsers(context.Background())
	if len(users) != 0 {
		t.Fatalf("user nggak boleh kebuat: %+v", users)
	}
}

func TestAdminTambahUser(t *testing.T) {
	mux, view, store := newTestMux(t)
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsPlatformAdmin: true}

	rec := post(mux, "/admin/users", url.Values{"username": {"gina"}, "password": {"rahasia"}}, admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mau 303, dapat %d", rec.Code)
	}
	users, _ := store.ListUsers(context.Background())
	if len(users) != 1 || users[0].Username != "gina" {
		t.Fatalf("user nggak tersimpan: %+v", users)
	}

	rec = post(mux, "/admin/users", url.Values{"username": {"gina"}, "password": {"lain"}}, admin)
	if rec.Code != http.StatusConflict {
		t.Fatalf("username dobel: mau 409, dapat %d", rec.Code)
	}
	if page := view.data.(usersPage); page.Error == "" || page.Username != "gina" {
		t.Fatalf("form harus balik dengan pesan error: %+v", page)
	}

	rec = post(mux, "/admin/users", url.Values{"username": {"hana"}}, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("password kosong: mau 400, dapat %d", rec.Code)
	}
}

func TestTemplateRender(t *testing.T) {
	rn, err := web.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	group := &Group{ID: uuid.New(), Name: "devs"}
	pages := []struct {
		name string
		data any
	}{
		{"admin/users", usersPage{Users: []User{{ID: uuid.New(), Username: "gina"}}}},
		{"admin/users", usersPage{Error: "Username sudah dipakai.", Username: "gina"}},
		{"admin/groups", groupsPage{Groups: []Group{*group}}},
		{"admin/group_detail", groupDetailPage{
			Group:     group,
			Members:   []Member{{UserID: uuid.New(), Username: "gina"}},
			Grants:    []Grant{{DatabaseID: uuid.New(), DatabaseName: "toko", Level: "view_only"}},
			Databases: []Database{{ID: uuid.New(), Name: "toko"}},
		}},
		{"admin/group_detail", groupDetailPage{Group: group}},
	}
	for _, p := range pages {
		rec := httptest.NewRecorder()
		rn.Render(rec, httptest.NewRequest("GET", "/", nil), p.name, p.data)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "terjadi kesalahan") {
			t.Errorf("%s: render gagal (%d): %s", p.name, rec.Code, rec.Body.String())
		}
	}
}

func TestNonaktifkanUser(t *testing.T) {
	mux, _, store := newTestMux(t)
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

	path := "/admin/users/" + id.String() + "/toggle-active"
	if rec := post(mux, path, url.Values{}, admin); rec.Code != http.StatusSeeOther {
		t.Fatalf("nonaktifkan: mau 303, dapat %d", rec.Code)
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
	if others, _ := store.NonMembers(ctx, groupID); len(others) != 0 {
		t.Fatalf("user nonaktif nggak boleh muncul di pilihan anggota: %+v", others)
	}

	post(mux, path, url.Values{}, admin)
	if u, _ := store.GetUser(ctx, id); u.Disabled {
		t.Fatal("user harusnya aktif lagi")
	}

	self := &auth.User{ID: id, Username: "gina", IsPlatformAdmin: true}
	if rec := post(mux, path, url.Values{}, self); rec.Code != http.StatusForbidden {
		t.Fatalf("nonaktifkan diri sendiri: mau 403, dapat %d", rec.Code)
	}
}
