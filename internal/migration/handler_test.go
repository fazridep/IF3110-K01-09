package migration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/web"
)

type server struct {
	t        *testing.T
	handler  http.Handler
	sessions *auth.Store
}

func newServer(t *testing.T, f *fixture) *server {
	t.Helper()
	view, err := web.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewStore(f.svc.db, auth.SessionConfig{
		IdleTimeout:      time.Hour,
		AbsoluteTimeout:  time.Hour,
		RotationInterval: time.Hour,
		GracePeriod:      time.Minute,
	})
	mux := http.NewServeMux()
	NewHandler(f.svc, view).RegisterRoutes(mux)
	return &server{t: t, handler: sessions.RequireLogin(mux), sessions: sessions}
}

func (s *server) do(userID uuid.UUID, method, path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	token, _, err := s.sessions.Create(context.Background(), userID)
	if err != nil {
		s.t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "sid", Value: token})
	rr := httptest.NewRecorder()
	s.handler.ServeHTTP(rr, req)
	return rr
}

func TestHandlerFlow(t *testing.T) {
	f := newFixture(t)
	srv := newServer(t, f)
	listPath := "/databases/" + f.dbID.String() + "/migrations"

	rr := srv.do(f.editor, http.MethodPost, listPath, url.Values{
		"description": {"tabel kategori"},
		"sql":         {"CREATE TABLE kategori (id INT);"},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create: mau 303, dapat %d: %s", rr.Code, rr.Body)
	}
	detailPath := rr.Header().Get("Location")

	if rr := srv.do(f.viewer, http.MethodPost, detailPath+"/execute", nil); rr.Code != http.StatusForbidden {
		t.Errorf("view_only execute: mau 403, dapat %d", rr.Code)
	}
	if rr := srv.do(f.stranger, http.MethodGet, listPath, nil); rr.Code != http.StatusForbidden {
		t.Errorf("tanpa akses lihat riwayat: mau 403, dapat %d", rr.Code)
	}

	rr = srv.do(f.viewer, http.MethodGet, detailPath, nil)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "Jalankan migration") {
		t.Errorf("view_only lihat detail: harus 200 tanpa tombol jalankan, dapat %d", rr.Code)
	}

	if rr := srv.do(f.editor, http.MethodPost, detailPath+"/execute", nil); rr.Code != http.StatusSeeOther {
		t.Fatalf("execute: mau 303, dapat %d: %s", rr.Code, rr.Body)
	}

	rr = srv.do(f.viewer, http.MethodGet, listPath, nil)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "v1") || !strings.Contains(body, "success") {
		t.Errorf("riwayat harus menampilkan v1 success, dapat %d", rr.Code)
	}
}

func TestHandlerShowsValidationError(t *testing.T) {
	f := newFixture(t)
	srv := newServer(t, f)

	rr := srv.do(f.editor, http.MethodPost, "/databases/"+f.dbID.String()+"/migrations", url.Values{
		"description": {"index"},
		"sql":         {"CREATE INDEX CONCURRENTLY idx ON t (x);"},
	})
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "transaction") || !strings.Contains(body, "CONCURRENTLY") {
		t.Fatalf("form harus tampil lagi dengan pesan error dan SQL user, dapat %d", rr.Code)
	}
}

func TestHandlerHalamanDitolakPakaiLayout(t *testing.T) {
	f := newFixture(t)
	srv := newServer(t, f)

	rr := srv.do(f.viewer, http.MethodGet, "/databases/"+f.dbID.String()+"/migrations/new", nil)
	body := rr.Body.String()
	if rr.Code != http.StatusForbidden || !strings.Contains(body, "<html") || !strings.Contains(body, "nggak punya akses") {
		t.Fatalf("mau 403 dengan halaman berlayout, dapat %d: %s", rr.Code, body)
	}
}
