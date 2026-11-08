package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
)

func newTestHandler(t *testing.T) (*Handler, *fakeClock) {
	t.Helper()
	s, clock := newTestStore(t)
	return NewHandler(s.db, s), clock
}

func postLogin(h *Handler, username, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.Login(rr, req)
	return rr
}

func sessionCookie(rr *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	return nil
}

func problemOf(t *testing.T, rr *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	if ct := rr.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
		t.Fatalf("mau problem+json, dapat %q: %s", ct, rr.Body.String())
	}
	var p httpx.Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoginSuccess(t *testing.T) {
	h, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")

	rr := postLogin(h, "budi", "rahasia")
	if rr.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d: %s", rr.Code, rr.Body.String())
	}
	c := sessionCookie(rr)
	if c == nil || c.Value == "" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Expires.IsZero() {
		t.Fatalf("cookie sid nggak sesuai: %+v", c)
	}
	var user UserResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &user); err != nil || user.Username != "budi" || user.IsPlatformAdmin {
		t.Fatalf("body harus berisi user: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), c.Value) || strings.Contains(rr.Body.String(), "password") {
		t.Error("session id dan password nggak boleh muncul di body response")
	}
}

func TestLoginFailureMessageIsSame(t *testing.T) {
	h, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")

	wrongPassword := postLogin(h, "budi", "salah")
	unknownUser := postLogin(h, "nggak-ada", "rahasia")

	for _, rr := range []*httptest.ResponseRecorder{wrongPassword, unknownUser} {
		if rr.Code != http.StatusUnauthorized || sessionCookie(rr) != nil {
			t.Fatalf("login gagal harus 401 tanpa cookie, dapat %d", rr.Code)
		}
	}
	a, b := problemOf(t, wrongPassword), problemOf(t, unknownUser)
	if a.Detail == "" || a.Detail != b.Detail || a.Type != b.Type {
		t.Fatalf("pesan error harus sama: %+v vs %+v", a, b)
	}
}

func TestLoginValidation(t *testing.T) {
	h, _ := newTestHandler(t)

	rr := postLogin(h, "  ", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("input kosong harus 400, dapat %d", rr.Code)
	}
	p := problemOf(t, rr)
	if len(p.Errors) != 2 || p.Errors[0].Field != "username" || p.Errors[1].Field != "password" {
		t.Fatalf("errors harus menunjuk username dan password: %+v", p.Errors)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader("username=budi&password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.Login(rr, req)
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("body form harus ditolak 415, dapat %d", rr.Code)
	}
}

func TestLoginRevokesOldSession(t *testing.T) {
	h, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	old := sessionCookie(postLogin(h, "budi", "rahasia"))

	postLogin(h, "budi", "rahasia", old)
	if _, err := h.store.Validate(context.Background(), old.Value); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("session lama harus dicabut saat login ulang, dapat %v", err)
	}
}

func TestLoginUserNonaktif(t *testing.T) {
	h, _ := newTestHandler(t)
	id := createUser(t, h.db, "budi", "rahasia")
	if _, err := h.db.Exec(context.Background(), `UPDATE users SET disabled_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}

	rr := postLogin(h, "budi", "rahasia")
	if rr.Code != http.StatusForbidden || sessionCookie(rr) != nil {
		t.Fatalf("user nonaktif harus 403 tanpa session, dapat %d", rr.Code)
	}
	if p := problemOf(t, rr); !strings.Contains(p.Detail, "dinonaktifkan") {
		t.Fatalf("pesan harus bilang akun nonaktif, dapat %q", p.Detail)
	}

	rr = postLogin(h, "budi", "salah")
	if p := problemOf(t, rr); rr.Code != http.StatusUnauthorized || strings.Contains(p.Detail, "dinonaktifkan") {
		t.Fatalf("password salah nggak boleh nyebut status akun: %d %q", rr.Code, p.Detail)
	}
}

func TestLogout(t *testing.T) {
	h, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))

	req := httptest.NewRequest(http.MethodDelete, "/api/session", nil)
	req.AddCookie(sid)
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout harus 204, dapat %d", rr.Code)
	}
	if c := sessionCookie(rr); c == nil || c.MaxAge >= 0 {
		t.Error("cookie sid harus dihapus saat logout")
	}
	if _, err := h.store.Validate(context.Background(), sid.Value); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("session harus dicabut di db, dapat %v", err)
	}

	rr = httptest.NewRecorder()
	h.Logout(rr, httptest.NewRequest(http.MethodDelete, "/api/session", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout ulang harus tetap 204, dapat %d", rr.Code)
	}
}

func TestLogoutForm(t *testing.T) {
	h, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(sid)
	rr := httptest.NewRecorder()
	h.logoutForm(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Fatalf("logout dari form lama harus redirect ke /login, dapat %d %q", rr.Code, rr.Header().Get("Location"))
	}
}

func serveAPI(h *Handler, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.RegisterAPIRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.store.RequireAPILogin(mux).ServeHTTP(rr, req)
	return rr
}

func TestMe(t *testing.T) {
	h, clock := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))

	rr := serveAPI(h, sid)
	var user UserResponse
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &user) != nil || user.Username != "budi" {
		t.Fatalf("GET /api/me harus balikin user, dapat %d %s", rr.Code, rr.Body.String())
	}

	if rr := serveAPI(h); rr.Code != http.StatusUnauthorized || problemOf(t, rr).Type != "urn:problem:unauthenticated" {
		t.Fatalf("tanpa cookie harus 401 problem, dapat %d", rr.Code)
	}

	clock.advance(testSessionConfig.IdleTimeout + time.Second)
	rr = serveAPI(h, sid)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("session idle harus 401, dapat %d", rr.Code)
	}
	if c := sessionCookie(rr); c == nil || c.MaxAge >= 0 {
		t.Error("cookie session yang expired harus dihapus")
	}
}

// Token hasil rotasi harus ikut dikirim di response API.
func TestAPISendsRotatedCookie(t *testing.T) {
	h, clock := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))
	clock.advance(testSessionConfig.RotationInterval)

	rr := serveAPI(h, sid)
	rotated := sessionCookie(rr)
	if rr.Code != http.StatusOK || rotated == nil || rotated.Value == sid.Value {
		t.Fatalf("token hasil rotasi harus dikirim lewat cookie, dapat %d %+v", rr.Code, rotated)
	}
	clock.advance(testSessionConfig.GracePeriod + time.Second)
	if _, err := h.store.Validate(context.Background(), rotated.Value); err != nil {
		t.Fatalf("token baru harus tetap berlaku: %v", err)
	}
}

func TestRequireLogin(t *testing.T) {
	h, clock := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))

	var gotUser *User
	protected := h.store.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = CurrentUser(r.Context())
	}))
	serve := func(target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rr := httptest.NewRecorder()
		protected.ServeHTTP(rr, req)
		return rr
	}

	if rr := serve("/"); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Errorf("tanpa cookie harus redirect ke /login, dapat %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if loc := serve("/databases?page=2").Header().Get("Location"); loc != "/login?next=%2Fdatabases%3Fpage%3D2" {
		t.Errorf("redirect harus bawa next, dapat %q", loc)
	}
	evil := httptest.NewRequest(http.MethodGet, "/", nil)
	evil.URL = &url.URL{Path: "//evil.example/x"}
	evilRR := httptest.NewRecorder()
	protected.ServeHTTP(evilRR, evil)
	if loc := evilRR.Header().Get("Location"); loc != "/login" {
		t.Errorf("next ke host lain harus dibuang, dapat %q", loc)
	}

	serve("/", sid)
	if gotUser == nil || gotUser.Username != "budi" {
		t.Fatalf("user harus ada di context, dapat %+v", gotUser)
	}

	clock.advance(testSessionConfig.IdleTimeout + time.Second)
	rr := serve("/", sid)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("session idle harus redirect ke login, dapat %d", rr.Code)
	}
	if c := sessionCookie(rr); c == nil || c.MaxAge >= 0 {
		t.Error("cookie session yang expired harus dihapus")
	}
}

func TestSeedAdmin(t *testing.T) {
	h, _ := newTestHandler(t)
	ctx := context.Background()

	if _, err := SeedAdmin(ctx, h.db, "admin", "rahasia"); err != nil {
		t.Fatal(err)
	}
	user, err := authenticate(ctx, h.db, "admin", "rahasia")
	if err != nil || !user.IsPlatformAdmin {
		t.Fatalf("admin harus bisa login dan berstatus Platform Admin: %+v %v", user, err)
	}
	if _, err := SeedAdmin(ctx, h.db, "admin", "lain"); err == nil {
		t.Fatal("username yang sudah ada harus ditolak")
	}
}
