package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderLoginPage(t *testing.T) {
	view, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	view.Render(rr, httptest.NewRequest(http.MethodGet, "/login", nil), "auth/login", struct{ Error string }{"Username atau password salah."})

	body := rr.Body.String()
	for _, want := range []string{`name="csrf_token"`, "Username atau password salah.", "<title>Login"} {
		if !strings.Contains(body, want) {
			t.Errorf("halaman login nggak ada %q", want)
		}
	}
	if strings.Contains(body, "Logout") {
		t.Error("tombol logout nggak boleh muncul kalau belum login")
	}
}

func TestRenderUnknownPage(t *testing.T) {
	view, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	view.Render(rr, httptest.NewRequest(http.MethodGet, "/", nil), "nggak/ada", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("mau 500, dapat %d", rr.Code)
	}
}
