package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"net/http"
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
					http.Error(w, "CSRF token tidak ada", http.StatusForbidden)
					return
				}
				submitted := r.FormValue(csrfFormField)
				if submitted == "" {
					submitted = r.Header.Get(csrfHeaderName)
				}
				if subtle.ConstantTimeCompare([]byte(c.Value), []byte(submitted)) != 1 {
					http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r.WithContext(withCSRFToken(r.Context(), c.Value)))
			}
		})
	}
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
