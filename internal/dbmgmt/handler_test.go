package dbmgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
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

// newTestHandler bikin handler dengan pengecekan admin dan akses palsu, jadi
// handler bisa dites tanpa package access.
func newTestHandler(t *testing.T, isAdmin bool, level LevelResolver) (*Handler, *fakeView, *Store) {
	t.Helper()
	store := newTestStore(t)
	view := &fakeView{}
	admin := func(context.Context, uuid.UUID) (bool, error) { return isAdmin, nil }
	if level == nil {
		level = func(context.Context, uuid.UUID, uuid.UUID) (string, error) { return LevelNone, nil }
	}
	return NewHandler(store, NewManager(store), view, admin, level), view, store
}

func user() *auth.User {
	return &auth.User{ID: uuid.New(), Username: "fazri", IsPlatformAdmin: true}
}

func request(method, target, body string, u *auth.User) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if u != nil {
		r = r.WithContext(auth.WithUser(r.Context(), u))
	}
	return r
}

func serve(mux *http.ServeMux, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestAdminButuhPlatformAdmin(t *testing.T) {
	h, view, _ := newTestHandler(t, false, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rec := serve(mux, request("GET", "/admin/databases", "", user()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mau 403, dapat %d", rec.Code)
	}
	if view.name != "dbmgmt/error" {
		t.Fatalf("mau halaman error, dapat %q", view.name)
	}
}

func TestAdminTanpaLogin(t *testing.T) {
	h, _, _ := newTestHandler(t, true, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rec := serve(mux, request("GET", "/admin/databases", "", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("mau redirect ke /login, dapat %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminBuatLaluLihatDetail(t *testing.T) {
	h, view, store := newTestHandler(t, true, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	form := url.Values{
		"name":     {"db-demo"},
		"host":     {"localhost"},
		"port":     {"5432"},
		"db_name":  {"demo"},
		"username": {"app"},
		"password": {"rahasia"},
		"sslmode":  {"prefer"},
	}
	rec := serve(mux, request("POST", "/admin/databases", form.Encode(), user()))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mau 303, dapat %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/databases/") {
		t.Fatalf("redirect aneh: %q", loc)
	}

	conns, err := store.List(context.Background())
	if err != nil || len(conns) != 1 || conns[0].Name != "db-demo" {
		t.Fatalf("koneksi nggak tersimpan: %+v %v", conns, err)
	}

	rec = serve(mux, request("GET", strings.Split(loc, "?")[0], "", user()))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail mau 200, dapat %d", rec.Code)
	}
	if view.name != "dbmgmt/admin_detail" {
		t.Fatalf("mau admin_detail, dapat %q", view.name)
	}
	page := view.data.(adminDetailPage)
	if page.Connection.Name != "db-demo" {
		t.Fatalf("halaman nampilin koneksi yang salah: %+v", page.Connection)
	}
}

func TestAdminValidasiGagal(t *testing.T) {
	h, view, _ := newTestHandler(t, true, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	form := url.Values{"name": {"tanpa-host"}, "db_name": {"x"}, "username": {"u"}}
	rec := serve(mux, request("POST", "/admin/databases", form.Encode(), user()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mau 400, dapat %d", rec.Code)
	}
	if view.name != "dbmgmt/admin_form" {
		t.Fatalf("mau balik ke form, dapat %q", view.name)
	}
	if page := view.data.(adminFormPage); page.Error == "" {
		t.Fatal("pesan error harusnya diisi")
	}
}

func TestDaftarDatabaseDisaringAkses(t *testing.T) {
	ctx := context.Background()
	var allowed uuid.UUID

	// Level resolver palsu: cuma koneksi pertama yang boleh.
	h, view, store := newTestHandler(t, true, func(_ context.Context, _, dbID uuid.UUID) (string, error) {
		if dbID == allowed {
			return LevelView, nil
		}
		return LevelNone, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	first, err := store.Create(ctx, sampleInput("boleh"))
	if err != nil {
		t.Fatal(err)
	}
	allowed = first
	if _, err := store.Create(ctx, sampleInput("nggak-boleh")); err != nil {
		t.Fatal(err)
	}

	rec := serve(mux, request("GET", "/databases", "", user()))
	if rec.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d", rec.Code)
	}
	if view.name != "dbmgmt/list" {
		t.Fatalf("mau dbmgmt/list, dapat %q", view.name)
	}
	page := view.data.(databaseListPage)
	if len(page.Items) != 1 || page.Items[0].Connection.Name != "boleh" {
		t.Fatalf("harusnya cuma 1 database: %+v", page.Items)
	}
	if page.Items[0].Level != LevelView {
		t.Fatalf("level salah: %q", page.Items[0].Level)
	}
}

func TestBukaDatabaseTanpaAkses(t *testing.T) {
	h, view, store := newTestHandler(t, true, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	id, err := store.Create(context.Background(), sampleInput("rahasia"))
	if err != nil {
		t.Fatal(err)
	}

	rec := serve(mux, request("GET", "/databases/"+id.String(), "", user()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mau 403, dapat %d", rec.Code)
	}
	if view.name != "dbmgmt/error" {
		t.Fatalf("mau halaman error, dapat %q", view.name)
	}
}

func TestIDBukanUUID(t *testing.T) {
	h, _, _ := newTestHandler(t, true, func(context.Context, uuid.UUID, uuid.UUID) (string, error) {
		return LevelEdit, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rec := serve(mux, request("GET", "/databases/bukan-uuid", "", user()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("mau 404, dapat %d", rec.Code)
	}
}

// TestTemplateRender mastiin semua template dbmgmt bisa dirender pakai data
// asli. Typo nama field di template langsung kelihatan di sini, bukan pas demo.
func TestTemplateRender(t *testing.T) {
	rn, err := web.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	conn := Connection{Name: "db-demo", Host: "localhost", Port: 5432, Database: "demo", Username: "app", SSLMode: "prefer"}
	pages := []struct {
		name string
		data any
	}{
		{"dbmgmt/list", databaseListPage{Items: []listItem{{Connection: conn, Level: LevelView}}}},
		{"dbmgmt/list", databaseListPage{}},
		{"dbmgmt/database", databasePage{
			Connection: conn,
			Level:      LevelEdit,
			Schemas: []schemaTables{{
				Schema: Schema{Name: "public", Tables: 1},
				Tables: []Table{{Name: "users", Kind: "table", Columns: 3}},
			}},
		}},
		{"dbmgmt/table", tablePage{
			Connection: conn,
			Level:      LevelEdit,
			Detail: TableDetail{
				Schema:      "public",
				Table:       "users",
				Columns:     []Column{{Name: "id", DataType: "uuid", Default: "gen_random_uuid()"}},
				Constraints: []Constraint{{Name: "users_pkey", Kind: "primary key", Definition: "PRIMARY KEY (id)"}},
				Indexes:     []Index{{Name: "users_pkey", Primary: true, Definition: "CREATE UNIQUE INDEX ..."}},
			},
			Rows: RowPage{
				Columns: []string{"id", "username"},
				Rows:    [][]Cell{{{Value: "1"}, {Null: true}}},
				Total:   1, Page: 1, PerPage: 50, Pages: 1,
			},
			PrevPage: 0, NextPage: 2,
			BaseURL: "/databases/x/tables/public/users",
		}},
		{"dbmgmt/admin_list", adminListPage{Connections: []Connection{conn}}},
		{"dbmgmt/admin_form", adminFormPage{Input: Input{Port: 5432, SSLMode: "prefer"}}},
		{"dbmgmt/admin_form", adminFormPage{
			IsEdit: true, ID: "x", Input: Input{Name: "db", SSLMode: "require"},
			Error: "nama koneksi wajib diisi", TestStatus: "gagal", TestMessage: "password ditolak",
		}},
		{"dbmgmt/admin_detail", adminDetailPage{Connection: conn, Notice: "Koneksi dibuat.", TestStatus: "ok"}},
		{"dbmgmt/error", struct{ Message string }{"Nggak punya akses."}},
	}

	for _, p := range pages {
		rec := httptest.NewRecorder()
		rn.Render(rec, httptest.NewRequest("GET", "/", nil), p.name, p.data)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", p.name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "terjadi kesalahan") {
			t.Errorf("%s: render gagal: %s", p.name, rec.Body.String())
		}
	}
}
