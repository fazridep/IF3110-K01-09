package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/httpx"
	"github.com/if3110/tubes-db-platform/internal/testutil"
)

var testDist = fstest.MapFS{
	"index.html":             {Data: []byte(`<!doctype html><div id="root"></div>`)},
	"assets/index-abc1.js":   {Data: []byte(`console.log(1)`)},
	"favicon.svg":            {Data: []byte(`<svg/>`)},
	"assets/fonts/geist.ttf": {Data: []byte(`font`)},
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, target, nil))
	return rr
}

func TestSPAServesAssetsWithLongCache(t *testing.T) {
	spa := NewSPA(testDist)

	rr := get(spa, http.MethodGet, "/assets/index-abc1.js")
	if rr.Code != http.StatusOK || rr.Body.String() != "console.log(1)" {
		t.Fatalf("asset harus dikirim apa adanya: %d %q", rr.Code, rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset ber-hash harus di-cache lama, dapat %q", cc)
	}
	if cc := get(spa, http.MethodGet, "/favicon.svg").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("file tanpa hash jangan di-cache lama, dapat %q", cc)
	}
}

func TestSPAFallbackToIndex(t *testing.T) {
	spa := NewSPA(testDist)
	for _, p := range []string{"/", "/login", "/databases/3/migrations", "/index.html", "/../../etc/passwd"} {
		rr := get(spa, http.MethodGet, p)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `id="root"`) {
			t.Errorf("%s harus dapat index.html, dapat %d %q", p, rr.Code, rr.Body.String())
			continue
		}
		h := rr.Header()
		if h.Get("Cache-Control") != "no-cache" || !strings.Contains(h.Get("Content-Security-Policy"), "script-src 'self'") || h.Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s: header keamanan/cache index kurang: %v", p, h)
		}
	}
}

func TestSPAMissingAssetIs404(t *testing.T) {
	spa := NewSPA(testDist)
	if rr := get(spa, http.MethodGet, "/assets/hilang-123.js"); rr.Code != http.StatusNotFound {
		t.Fatalf("asset yang nggak ada harus 404, bukan index.html: %d", rr.Code)
	}
	if rr := get(spa, http.MethodPost, "/login"); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST ke SPA harus 405, dapat %d", rr.Code)
	}
}

func TestSPANotBuilt(t *testing.T) {
	rr := get(NewSPA(fstest.MapFS{}), http.MethodGet, "/")
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "make frontend-build") {
		t.Fatalf("tanpa build harus 503 dengan petunjuk, dapat %d %q", rr.Code, rr.Body.String())
	}
}

type client struct {
	t       *testing.T
	h       http.Handler
	cookies map[string]*http.Cookie
}

func (c *client) do(method, target, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if ck, ok := c.cookies["csrf"]; ok {
		req.Header.Set("X-CSRF-Token", ck.Value)
	}
	rr := httptest.NewRecorder()
	c.h.ServeHTTP(rr, req)
	for _, ck := range rr.Result().Cookies() {
		if ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck
		}
	}
	return rr
}

func newTestRouter(t *testing.T) (*client, *auth.Store) {
	t.Helper()
	db := testutil.DB(t)
	if _, err := db.Exec(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ('budi', $1)`, auth.HashPassword("rahasia")); err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewStore(db, auth.SessionConfig{
		IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 8 * time.Hour,
		RotationInterval: 15 * time.Minute, GracePeriod: 30 * time.Second,
	})
	h := NewRouter(Options{
		Sessions: sessions,
		Auth:     auth.NewHandler(db, sessions),
		RegisterAPI: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/items", func(w http.ResponseWriter, r *http.Request) {
				httpx.JSON(w, http.StatusOK, httpx.NewList([]string{auth.CurrentUser(r.Context()).Username}))
			})
		},
		RegisterPages: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /legacy", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("halaman lama " + auth.CurrentUser(r.Context()).Username))
			})
		},
		SPA: NewSPA(testDist),
	})
	return &client{t: t, h: h, cookies: map[string]*http.Cookie{}}, sessions
}

func problemType(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	if ct := rr.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
		t.Fatalf("mau problem+json, dapat %q: %s", ct, rr.Body.String())
	}
	var p httpx.Problem
	_ = json.Unmarshal(rr.Body.Bytes(), &p)
	if p.RequestID == "" {
		t.Errorf("problem harus bawa requestId buat korelasi log: %s", rr.Body.String())
	}
	return p.Type
}

func TestRouterLoginFlow(t *testing.T) {
	c, _ := newTestRouter(t)

	rr := c.do(http.MethodGet, "/login", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `id="root"`) || c.cookies["csrf"] == nil {
		t.Fatalf("SPA harus disajikan dan cookie csrf dibuat: %d", rr.Code)
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("setiap response harus bawa X-Request-ID")
	}

	if rr := c.do(http.MethodGet, "/api/me", ""); rr.Code != http.StatusUnauthorized || problemType(t, rr) != "urn:problem:unauthenticated" {
		t.Fatalf("GET /api/me tanpa login harus 401, dapat %d", rr.Code)
	}
	if rr := c.do(http.MethodGet, "/legacy", ""); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?next=%2Flegacy" {
		t.Fatalf("halaman lama tanpa login harus redirect, dapat %d %q", rr.Code, rr.Header().Get("Location"))
	}

	rr = c.do(http.MethodPost, "/api/session", `{"username":"budi","password":"rahasia"}`)
	if rr.Code != http.StatusCreated || c.cookies["sid"] == nil {
		t.Fatalf("login harus 201 dan pasang cookie sid, dapat %d %s", rr.Code, rr.Body.String())
	}

	if rr := c.do(http.MethodGet, "/api/me", ""); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"username":"budi"`) {
		t.Fatalf("GET /api/me harus 200, dapat %d %s", rr.Code, rr.Body.String())
	}
	if rr := c.do(http.MethodGet, "/api/items", ""); rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"items":["budi"]}` {
		t.Fatalf("route dari RegisterAPI harus dapat user: %d %s", rr.Code, rr.Body.String())
	}
	if rr := c.do(http.MethodGet, "/legacy", ""); rr.Body.String() != "halaman lama budi" {
		t.Fatalf("halaman lama harus tetap jalan: %q", rr.Body.String())
	}
	if rr := c.do(http.MethodGet, "/databases/1", ""); !strings.Contains(rr.Body.String(), `id="root"`) {
		t.Fatal("path yang bukan API/halaman lama harus dapat SPA")
	}

	if rr := c.do(http.MethodDelete, "/api/session", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("logout harus 204, dapat %d", rr.Code)
	}
	if rr := c.do(http.MethodGet, "/api/me", ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("setelah logout harus 401, dapat %d", rr.Code)
	}
}

func TestRouterRejectsMissingCSRF(t *testing.T) {
	c, _ := newTestRouter(t)
	c.do(http.MethodGet, "/", "")
	delete(c.cookies, "csrf")

	rr := c.do(http.MethodPost, "/api/session", `{"username":"budi","password":"rahasia"}`)
	if rr.Code != http.StatusForbidden || problemType(t, rr) != "urn:problem:csrf" {
		t.Fatalf("login tanpa CSRF harus 403, dapat %d", rr.Code)
	}
	if c.cookies["sid"] != nil {
		t.Fatal("login yang ditolak CSRF nggak boleh dapat session")
	}
}

func TestRouterAPINotFoundAndMethod(t *testing.T) {
	c, _ := newTestRouter(t)
	c.do(http.MethodGet, "/", "")

	if rr := c.do(http.MethodGet, "/api/nggak-ada", ""); rr.Code != http.StatusNotFound || problemType(t, rr) != "urn:problem:not-found" {
		t.Fatalf("endpoint asing harus 404 problem, dapat %d", rr.Code)
	}
	rr := c.do(http.MethodDelete, "/api/items", "")
	if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != "GET" || problemType(t, rr) != "urn:problem:method-not-allowed" {
		t.Fatalf("method salah harus 405 dengan Allow, dapat %d %q", rr.Code, rr.Header().Get("Allow"))
	}
	if rr := c.do(http.MethodGet, "/api/health", ""); rr.Code != http.StatusOK {
		t.Fatalf("health harus publik, dapat %d", rr.Code)
	}
}