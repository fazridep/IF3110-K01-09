package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

const (
	csrfFormField  = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfCookieName = "csrf"
)

func CSRFToken(r *http.Request) string {
	return csrfFromCtx(r.Context())
}

// CSRF memakai pola double-submit cookie: token di cookie harus sama dengan
// token yang dikirim lewat form atau header X-CSRF-Token.
func CSRF(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				token := csrfGetOrCreate(w, r, secure)
				next.ServeHTTP(w, r.WithContext(withCSRFToken(r.Context(), token)))
			default:
				c, err := r.Cookie(csrfCookieName)
				if err != nil || c.Value == "" {
					csrfFailed(w, r, "CSRF token tidak ada. Muat ulang halaman lalu coba lagi.")
					return
				}
				submitted := r.FormValue(csrfFormField)
				if submitted == "" {
					submitted = r.Header.Get(csrfHeaderName)
				}
				if subtle.ConstantTimeCompare([]byte(c.Value), []byte(submitted)) != 1 {
					csrfFailed(w, r, "CSRF token tidak valid. Muat ulang halaman lalu coba lagi.")
					return
				}
				next.ServeHTTP(w, r.WithContext(withCSRFToken(r.Context(), c.Value)))
			}
		})
	}
}

func csrfFailed(w http.ResponseWriter, r *http.Request, msg string) {
	obs.Warn(r.Context(), "auth.csrf_rejected", "request ditolak CSRF", "method", r.Method, "path", r.URL.Path)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		httpx.Fail(w, r, http.StatusForbidden, "csrf", msg)
		return
	}
	http.Error(w, msg, http.StatusForbidden)
}

func csrfGetOrCreate(w http.ResponseWriter, r *http.Request, secure bool) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	token := rand.Text()
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	})
	return token
}
