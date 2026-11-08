package dbmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/auth"
)

type levelFunc func(ctx context.Context, userID, dbID uuid.UUID) (access.Level, error)

func (f levelFunc) EffectiveLevel(ctx context.Context, userID, dbID uuid.UUID) (access.Level, error) {
	return f(ctx, userID, dbID)
}

func allow(level access.Level) levelFunc {
	return func(context.Context, uuid.UUID, uuid.UUID) (access.Level, error) { return level, nil }
}

// newTestMux bikin route dengan level akses palsu. nil berarti user nggak punya
// akses ke database mana pun. Status admin dibaca dari user di context.
func newTestMux(t *testing.T, level levelFunc) (*http.ServeMux, *Handler, *Store) {
	t.Helper()
	store := newTestStore(t)
	if level == nil {
		level = allow(access.NoAccess)
	}
	h := NewHandler(store, NewManager(store), level)
	t.Cleanup(h.pools.Close)
	mux := http.NewServeMux()
	h.RegisterAPIRoutes(mux)
	return mux, h, store
}

func admin() *auth.User { return userAs(true) }

func userAs(isAdmin bool) *auth.User {
	return &auth.User{ID: uuid.New(), Username: "fazri", IsPlatformAdmin: isAdmin}
}

func serve(mux *http.ServeMux, method, target string, body any, u *auth.User) *httptest.ResponseRecorder {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, target, nil)
	} else {
		raw, _ := json.Marshal(body)
		r = httptest.NewRequest(method, target, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
	}
	if u != nil {
		r = r.WithContext(auth.WithUser(r.Context(), u))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response bukan JSON: %v\n%s", err, rec.Body.String())
	}
	return v
}

func problemOf(t *testing.T, rec *httptest.ResponseRecorder) (string, string, []map[string]string) {
	t.Helper()
	p := decode[struct {
		Type   string              `json:"type"`
		Detail string              `json:"detail"`
		Errors []map[string]string `json:"errors"`
	}](t, rec)
	return p.Type, p.Detail, p.Errors
}

func bodyOf(in Input) connectionBody { return connectionBody(in) }

func TestAdminButuhPlatformAdmin(t *testing.T) {
	mux, _, _ := newTestMux(t, nil)
	for _, tc := range []struct {
		name string
		user *auth.User
	}{{"user biasa", userAs(false)}, {"tanpa user", nil}} {
		rec := serve(mux, "GET", "/api/admin/connections", nil, tc.user)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: mau 403, dapat %d", tc.name, rec.Code)
		}
	}
}

func TestAdminBuatLaluLihatDetail(t *testing.T) {
	mux, _, store := newTestMux(t, nil)

	rec := serve(mux, "POST", "/api/admin/connections", bodyOf(sampleInput("db-demo")), admin())
	if rec.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/api/admin/connections/") {
		t.Fatalf("Location aneh: %q", loc)
	}
	if strings.Contains(rec.Body.String(), "rahasia-sekali") || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("password nggak boleh ikut di response: %s", rec.Body.String())
	}

	conns, err := store.List(context.Background())
	if err != nil || len(conns) != 1 || conns[0].Name != "db-demo" {
		t.Fatalf("koneksi nggak tersimpan: %+v %v", conns, err)
	}

	rec = serve(mux, "GET", loc, nil, admin())
	if rec.Code != http.StatusOK {
		t.Fatalf("detail mau 200, dapat %d", rec.Code)
	}
	if c := decode[connectionJSON](t, rec); c.Name != "db-demo" || c.Host != "db.internal" {
		t.Fatalf("detail salah: %+v", c)
	}
}

func TestAdminValidasiGagalTunjukKolom(t *testing.T) {
	mux, _, _ := newTestMux(t, nil)

	rec := serve(mux, "POST", "/api/admin/connections", connectionBody{Name: "tanpa-host", Database: "x", Username: "u", Password: "p"}, admin())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mau 400, dapat %d", rec.Code)
	}
	if _, _, errs := problemOf(t, rec); len(errs) != 1 || errs[0]["field"] != "host" {
		t.Fatalf("mau error di kolom host, dapat %+v", errs)
	}
}

func TestAdminBuatTanpaPasswordDitolak(t *testing.T) {
	mux, _, store := newTestMux(t, nil)

	in := sampleInput("db-demo")
	in.Password = ""
	rec := serve(mux, "POST", "/api/admin/connections", bodyOf(in), admin())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mau 400, dapat %d", rec.Code)
	}
	if _, _, errs := problemOf(t, rec); len(errs) != 1 || errs[0]["field"] != "password" {
		t.Fatalf("mau error di kolom password, dapat %+v", errs)
	}
	if conns, _ := store.List(context.Background()); len(conns) != 0 {
		t.Fatalf("koneksi nggak boleh kesimpan: %+v", conns)
	}
}

func TestAdminNamaKembar(t *testing.T) {
	mux, _, store := newTestMux(t, nil)
	if _, err := store.Create(context.Background(), sampleInput("kembar")); err != nil {
		t.Fatal(err)
	}
	rec := serve(mux, "POST", "/api/admin/connections", bodyOf(sampleInput("kembar")), admin())
	if rec.Code != http.StatusConflict {
		t.Fatalf("mau 409, dapat %d", rec.Code)
	}
	if _, _, errs := problemOf(t, rec); len(errs) != 1 || errs[0]["field"] != "name" {
		t.Fatalf("mau error di kolom name, dapat %+v", errs)
	}
}

func TestAdminUbahTanpaPasswordPakaiYangLama(t *testing.T) {
	ctx := context.Background()
	mux, _, store := newTestMux(t, nil)
	id, err := store.Create(ctx, sampleInput("lama"))
	if err != nil {
		t.Fatal(err)
	}

	in := sampleInput("baru")
	in.Password = ""
	rec := serve(mux, "PUT", "/api/admin/connections/"+id.String(), bodyOf(in), admin())
	if rec.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	c, password, err := store.credentials(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "baru" || password != "rahasia-sekali" {
		t.Fatalf("mau nama baru dan password lama, dapat %q %q", c.Name, password)
	}

	rec = serve(mux, "PUT", "/api/admin/connections/"+uuid.NewString(), bodyOf(in), admin())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("koneksi nggak ada: mau 404, dapat %d", rec.Code)
	}
}

func TestAdminHapus(t *testing.T) {
	ctx := context.Background()
	mux, _, store := newTestMux(t, nil)

	id, err := store.Create(ctx, sampleInput("db-hapus"))
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(mux, "DELETE", "/api/admin/connections/"+id.String(), nil, admin()); rec.Code != http.StatusNoContent {
		t.Fatalf("mau 204, dapat %d", rec.Code)
	}
	if rec := serve(mux, "DELETE", "/api/admin/connections/"+id.String(), nil, admin()); rec.Code != http.StatusNotFound {
		t.Fatalf("hapus ulang: mau 404, dapat %d", rec.Code)
	}
}

func TestAdminHapusMasihDipakai(t *testing.T) {
	ctx := context.Background()
	mux, _, store := newTestMux(t, nil)

	id, err := store.Create(ctx, sampleInput("db-dipakai"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(ctx, `
		CREATE TABLE perujuk (database_id UUID NOT NULL REFERENCES database_connections(id));
		INSERT INTO perujuk VALUES ('`+id.String()+`');
	`)
	if err != nil {
		t.Fatal(err)
	}

	rec := serve(mux, "DELETE", "/api/admin/connections/"+id.String(), nil, admin())
	if rec.Code != http.StatusConflict {
		t.Fatalf("mau 409, dapat %d", rec.Code)
	}
	if typ, _, _ := problemOf(t, rec); typ != "urn:problem:connection-in-use" {
		t.Fatalf("type salah: %q", typ)
	}
}

func TestAdminTesKoneksi(t *testing.T) {
	ctx := context.Background()
	in := targetInput(t)
	if in.Password == "" {
		t.Skip("database test tanpa password")
	}
	mux, _, store := newTestMux(t, nil)
	id, err := store.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	withoutPassword := bodyOf(in)
	withoutPassword.Password = ""
	cases := []struct {
		name, path string
		body       any
		ok         bool
	}{
		{"tersimpan", "/api/admin/connections/" + id.String() + "/test", nil, true},
		{"form baru", "/api/admin/connections/test", bodyOf(in), true},
		{"form edit pakai password tersimpan", "/api/admin/connections/test", struct {
			connectionBody
			ID string `json:"id"`
		}{withoutPassword, id.String()}, true},
	}
	wrong := in
	wrong.Password = "kata-sandi-yang-salah"
	cases = append(cases, struct {
		name, path string
		body       any
		ok         bool
	}{"password salah", "/api/admin/connections/test", bodyOf(wrong), false})

	for _, tc := range cases {
		rec := serve(mux, "POST", tc.path, tc.body, admin())
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: mau 200, dapat %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		res := decode[testResultJSON](t, rec)
		if res.OK != tc.ok {
			t.Fatalf("%s: ok=%v, mau %v: %q", tc.name, res.OK, tc.ok, res.Message)
		}
		if strings.Contains(rec.Body.String(), wrong.Password) {
			t.Fatalf("%s: password nggak boleh ikut di response: %s", tc.name, rec.Body.String())
		}
	}
}

func TestAdminTesInputNggakValid(t *testing.T) {
	mux, _, _ := newTestMux(t, nil)
	rec := serve(mux, "POST", "/api/admin/connections/test", connectionBody{Name: "x"}, admin())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mau 400, dapat %d", rec.Code)
	}
}

func TestDaftarDatabaseDisaringAkses(t *testing.T) {
	ctx := context.Background()
	var allowed uuid.UUID
	mux, _, store := newTestMux(t, func(_ context.Context, _, dbID uuid.UUID) (access.Level, error) {
		if dbID == allowed {
			return access.ViewOnly, nil
		}
		return access.NoAccess, nil
	})

	first, err := store.Create(ctx, sampleInput("boleh"))
	if err != nil {
		t.Fatal(err)
	}
	allowed = first
	if _, err := store.Create(ctx, sampleInput("nggak-boleh")); err != nil {
		t.Fatal(err)
	}

	rec := serve(mux, "GET", "/api/databases", nil, admin())
	if rec.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d", rec.Code)
	}
	list := decode[struct{ Items []databaseJSON }](t, rec)
	if len(list.Items) != 1 || list.Items[0].Name != "boleh" || list.Items[0].Level != access.ViewOnly {
		t.Fatalf("harusnya cuma 1 database view_only: %+v", list.Items)
	}
	if strings.Contains(rec.Body.String(), "db.internal") {
		t.Fatalf("host nggak boleh kelihatan viewer: %s", rec.Body.String())
	}
}

func TestDaftarDatabaseKosongTetapArray(t *testing.T) {
	mux, _, _ := newTestMux(t, nil)
	rec := serve(mux, "GET", "/api/databases", nil, userAs(false))
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("mau items array kosong: %s", rec.Body.String())
	}
}

// Admin tanpa grant juga ditolak (ADR-011), dan balasannya 404 biar keberadaan
// database nggak bocor.
func TestBukaDatabaseTanpaAkses(t *testing.T) {
	mux, _, store := newTestMux(t, nil)
	id, err := store.Create(context.Background(), sampleInput("rahasia"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/databases/" + id.String(), "/api/databases/" + id.String() + "/tables/public/x"} {
		if rec := serve(mux, "GET", path, nil, admin()); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: mau 404, dapat %d", path, rec.Code)
		}
	}
}

func TestIDBukanUUID(t *testing.T) {
	mux, _, _ := newTestMux(t, allow(access.ViewEdit))
	if rec := serve(mux, "GET", "/api/databases/bukan-uuid", nil, admin()); rec.Code != http.StatusNotFound {
		t.Fatalf("mau 404, dapat %d", rec.Code)
	}
}

func TestViewerSchemaDanTabel(t *testing.T) {
	ctx := context.Background()
	in := targetInput(t)
	mux, _, store := newTestMux(t, allow(access.ViewEdit))
	id, err := store.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	schema := "viewer_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	_, err = store.db.Exec(ctx, `
		CREATE SCHEMA `+schema+`;
		CREATE TABLE `+schema+`.barang (id INT PRIMARY KEY, nama TEXT);
		INSERT INTO `+schema+`.barang VALUES (1, 'sapu'), (2, NULL), (3, 'ember');
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.db.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })

	rec := serve(mux, "GET", "/api/databases/"+id.String(), nil, userAs(false))
	if rec.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"`+schema+`"`) || !strings.Contains(rec.Body.String(), `"name":"barang"`) {
		t.Fatalf("schema/tabel nggak muncul: %s", rec.Body.String())
	}

	rec = serve(mux, "GET", "/api/databases/"+id.String()+"/tables/"+schema+"/barang?page=1&perPage=2", nil, userAs(false))
	if rec.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	got := decode[struct {
		Columns []columnJSON `json:"columns"`
		Indexes []indexJSON  `json:"indexes"`
		Rows    rowsJSON     `json:"rows"`
	}](t, rec)
	if len(got.Columns) != 2 || len(got.Indexes) != 1 {
		t.Fatalf("kolom/index salah: %+v", got)
	}
	if got.Rows.Total != 3 || got.Rows.Pages != 2 || len(got.Rows.Rows) != 2 {
		t.Fatalf("paging salah: %+v", got.Rows)
	}
	if got.Rows.Rows[1][1] != nil {
		t.Fatalf("NULL harus jadi null di JSON: %v", *got.Rows.Rows[1][1])
	}

	rec = serve(mux, "GET", "/api/databases/"+id.String()+"/tables/"+schema+"/nggak_ada", nil, userAs(false))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tabel nggak ada: mau 404, dapat %d", rec.Code)
	}
}

func TestGagalKonekDetailCumaBuatAdmin(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		isAdmin   bool
		lihatHost bool
	}{
		{"user biasa", false, false},
		{"admin", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux, _, store := newTestMux(t, allow(access.ViewOnly))

			in := sampleInput("mati")
			in.Host = "127.0.0.1"
			in.Port = 1
			in.SSLMode = "disable"
			id, err := store.Create(ctx, in)
			if err != nil {
				t.Fatal(err)
			}

			rec := serve(mux, "GET", "/api/databases/"+id.String(), nil, userAs(tc.isAdmin))
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("mau 502, dapat %d", rec.Code)
			}
			_, detail, _ := problemOf(t, rec)
			if got := strings.Contains(detail, "127.0.0.1"); got != tc.lihatHost {
				t.Fatalf("host kelihatan = %v, mau %v: %q", got, tc.lihatHost, detail)
			}
		})
	}
}

// Query pertama setelah database mati dapat EOF dari koneksi lama, setelah pgx
// konek ulang dapat connection refused. Dua-duanya harus 502, bukan 500.
func TestDatabaseMatiSetelahPoolKebentuk(t *testing.T) {
	ctx := context.Background()
	in, kill := proxyTarget(t, targetInput(t))
	mux, _, store := newTestMux(t, allow(access.ViewOnly))

	in.Name = "toko"
	id, err := store.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	page := "/api/databases/" + id.String()
	if rec := serve(mux, "GET", page, nil, userAs(false)); rec.Code != http.StatusOK {
		t.Fatalf("sebelum mati mau 200, dapat %d", rec.Code)
	}

	kill()
	for _, tc := range []struct {
		name string
		wait time.Duration
		path string
	}{
		{"langsung", 0, page},
		{"setelah konek ulang", 1200 * time.Millisecond, page},
		{"halaman tabel", 0, page + "/tables/public/apa_saja"},
	} {
		time.Sleep(tc.wait)
		rec := serve(mux, "GET", tc.path, nil, userAs(false))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("%s: mau 502, dapat %d: %s", tc.name, rec.Code, rec.Body.String())
		}
		if _, detail, _ := problemOf(t, rec); strings.Contains(detail, "127.0.0.1") {
			t.Fatalf("%s: user biasa nggak boleh lihat host: %q", tc.name, detail)
		}
	}
}
