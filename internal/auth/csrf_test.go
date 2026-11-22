package auth_test

import (
	"github.com/if3110/tubes-db-platform/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFMiddleware_GET_NoValidation(t *testing.T) {
	handler := auth.CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/some-page", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("GET seharusnya lolos tanpa validasi CSRF, dapat: %d", rr.Code)
	}
}

func TestCSRFMiddleware_POST_WithoutToken(t *testing.T) {
	handler := auth.CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/action", strings.NewReader("data=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST tanpa CSRF token seharusnya 403, dapat: %d", rr.Code)
	}
}

func TestCSRFMiddleware_POST_WithValidToken(t *testing.T) {
	handler := auth.CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.CSRFToken(r)
		w.Header().Set("X-Test-Token", token)
		w.WriteHeader(http.StatusOK)
	}))
	getReq := httptest.NewRequest(http.MethodGet, "/form", nil)
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)
	var csrfCookie *http.Cookie
	for _, c := range getRR.Result().Cookies() {
		if c.Name == "csrf" {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Fatal("cookie csrf tidak dibuat saat GET")
	}
	csrfToken := csrfCookie.Value
	body := "csrf_token=" + csrfToken + "&data=ok"
	postReq := httptest.NewRequest(http.MethodPost, "/action", strings.NewReader(body))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(csrfCookie)
	postRR := httptest.NewRecorder()
	handler.ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusOK {
		t.Errorf("POST dengan CSRF token valid seharusnya 200, dapat: %d", postRR.Code)
	}
}

func TestCSRFMiddleware_POST_WithWrongToken(t *testing.T) {
	handler := auth.CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/action", strings.NewReader("csrf_token=token-yang-salah"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf", Value: "token-yang-benar"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST dengan CSRF token salah seharusnya 403, dapat: %d", rr.Code)
	}
}
func TestCSRFCookieSecureInProduction(t *testing.T) {
	handler := auth.CSRF(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("cookie csrf harus Secure di production, dapat: %+v", cookies)
	}
}

func TestCSRFMiddleware_APIHeader(t *testing.T) {
	handler := auth.CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	send := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/session", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "csrf", Value: "token-benar"})
		if header != "" {
			req.Header.Set("X-CSRF-Token", header)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	if rr := send("token-benar"); rr.Code != http.StatusNoContent {
		t.Errorf("header X-CSRF-Token yang benar harus lolos, dapat %d", rr.Code)
	}
	rr := send("token-salah")
	if rr.Code != http.StatusForbidden || rr.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("route /api harus dibalas 403 problem+json, dapat %d %q", rr.Code, rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), `"type":"urn:problem:csrf"`) {
		t.Errorf("type problem harus urn:problem:csrf: %s", rr.Body.String())
	}
}

func TestCSRFCookieReadableBySPA(t *testing.T) {
	handler := auth.CSRF(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	c := rr.Result().Cookies()[0]
	if c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie csrf harus bisa dibaca JS dan SameSite=Strict: %+v", c)
	}
}
