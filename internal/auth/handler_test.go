package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeView struct {
	name string
	data any
}

func (v *fakeView) Render(w http.ResponseWriter, r *http.Request, name string, data any) {
	v.name, v.data = name, data
	w.WriteHeader(http.StatusOK)
}

func newTestHandler(t *testing.T) (*Handler, *fakeView, *fakeClock) {
	t.Helper()
	s, clock := newTestStore(t)
	view := &fakeView{}
	return NewHandler(s.db, s, view), view, clock
}

func postLogin(h *Handler, username, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.DoLogin(rr, req)
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

func TestLoginSuccess(t *testing.T) {
	h, _, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")

	rr := postLogin(h, "budi", "rahasia")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("mau 303, dapat %d", rr.Code)
	}
	c := sessionCookie(rr)
	if c == nil || c.Value == "" || !c.HttpOnly || c.Expires.IsZero() {
		t.Fatalf("cookie sid nggak sesuai: %+v", c)
	}
	if strings.Contains(rr.Body.String(), c.Value) {
		t.Error("session id nggak boleh muncul di body response")
	}
}

func TestLoginFailureMessageIsSame(t *testing.T) {
	h, view, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")

	postLogin(h, "budi", "salah")
	wrongPassword := view.data.(loginPage).Error
	postLogin(h, "nggak-ada", "rahasia")
	unknownUser := view.data.(loginPage).Error

	if wrongPassword == "" || wrongPassword != unknownUser {
		t.Fatalf("pesan error harus sama: %q vs %q", wrongPassword, unknownUser)
	}
}

func TestLoginRevokesOldSession(t *testing.T) {
	h, _, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	old := sessionCookie(postLogin(h, "budi", "rahasia"))

	postLogin(h, "budi", "rahasia", old)
	if _, err := h.store.Validate(context.Background(), old.Value); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("session lama harus dicabut saat login ulang, dapat %v", err)
	}
}

func TestLogout(t *testing.T) {
	h, _, _ := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(sid)
	rr := httptest.NewRecorder()
	h.DoLogout(rr, req)

	if c := sessionCookie(rr); c == nil || c.MaxAge >= 0 {
		t.Error("cookie sid harus dihapus saat logout")
	}
	if _, err := h.store.Validate(context.Background(), sid.Value); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("session harus dicabut di db, dapat %v", err)
	}
}

// Buka /login saat sudah login dan sudah waktunya rotasi: token baru harus
// ikut dikirim, kalau nggak user ke-logout setelah grace period habis.
func TestShowLoginSendsRotatedCookie(t *testing.T) {
	h, _, clock := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))
	clock.advance(testSessionConfig.RotationInterval)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(sid)
	rr := httptest.NewRecorder()
	h.ShowLogin(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("user yang sudah login harus diarahkan ke /, dapat %d", rr.Code)
	}
	rotated := sessionCookie(rr)
	if rotated == nil || rotated.Value == sid.Value {
		t.Fatal("token hasil rotasi harus dikirim lewat cookie")
	}
	clock.advance(testSessionConfig.GracePeriod + time.Second)
	if _, err := h.store.Validate(context.Background(), rotated.Value); err != nil {
		t.Fatalf("token baru harus tetap berlaku: %v", err)
	}
}

func TestRequireLogin(t *testing.T) {
	h, _, clock := newTestHandler(t)
	createUser(t, h.db, "budi", "rahasia")
	sid := sessionCookie(postLogin(h, "budi", "rahasia"))

	var gotUser *User
	protected := h.store.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = CurrentUser(r.Context())
	}))
	serve := func(cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rr := httptest.NewRecorder()
		protected.ServeHTTP(rr, req)
		return rr
	}

	if rr := serve(); rr.Code != http.StatusSeeOther {
		t.Errorf("tanpa cookie harus redirect ke login, dapat %d", rr.Code)
	}

	serve(sid)
	if gotUser == nil || gotUser.Username != "budi" {
		t.Fatalf("user harus ada di context, dapat %+v", gotUser)
	}

	clock.advance(testSessionConfig.IdleTimeout + time.Second)
	rr := serve(sid)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("session idle harus redirect ke login, dapat %d", rr.Code)
	}
	if c := sessionCookie(rr); c == nil || c.MaxAge >= 0 {
		t.Error("cookie session yang expired harus dihapus")
	}
}

func TestSeedAdmin(t *testing.T) {
	h, _, _ := newTestHandler(t)
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

func TestLoginUserNonaktif(t *testing.T) {
	h, view, _ := newTestHandler(t)
	id := createUser(t, h.db, "budi", "rahasia")
	if _, err := h.db.Exec(context.Background(), `UPDATE users SET disabled_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}

	rr := postLogin(h, "budi", "rahasia")
	if sessionCookie(rr) != nil {
		t.Fatal("user nonaktif nggak boleh dapat session")
	}
	if msg := view.data.(loginPage).Error; !strings.Contains(msg, "dinonaktifkan") {
		t.Fatalf("pesan harus bilang akun nonaktif, dapat %q", msg)
	}

	// Password salah tetap dapat pesan biasa, jadi status akun nggak bocor.
	postLogin(h, "budi", "salah")
	if msg := view.data.(loginPage).Error; strings.Contains(msg, "dinonaktifkan") {
		t.Fatalf("password salah nggak boleh nyebut status akun: %q", msg)
	}
}
