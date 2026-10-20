package auth
import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

const (
	csrfFormField  = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfCookieName = "csrf"
	csrfTokenLen   = 32
)

func CSRFToken(r *http.Request) string {
	return csrfFromCtx(r.Context())
}

func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			token := csrfGetOrCreate(w, r)
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
			if !csrfEqual(c.Value, submitted) {
				http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(withCSRFToken(r.Context(), c.Value)))
		}
	})
}

func csrfGetOrCreate(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	raw := make([]byte, csrfTokenLen)
	_, _ = rand.Read(raw)
	token := hex.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
	})
	return token
}

func csrfEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}