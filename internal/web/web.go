package web

import (
	"net/http"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

// NewRouter menyusun route yang selalu ada. register dipakai main buat
// menyambung fitur lain (dbmgmt, admin, migration) tanpa web perlu import
// package mereka; route yang didaftarkan di situ otomatis kena RequireLogin.
func NewRouter(sessions *auth.Store, authHandler *auth.Handler, register func(*http.ServeMux), view *Renderer, secure bool) http.Handler {
	mux := http.NewServeMux()
	authHandler.RegisterRoutes(mux)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		view.Render(w, r, "home/index", nil)
	})
	if register != nil {
		register(protected)
	}
	mux.Handle("/", sessions.RequireLogin(protected))

	return auth.CSRF(secure)(mux)
}
