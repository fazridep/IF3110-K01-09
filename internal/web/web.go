package web

import (
	"net/http"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/admin"
	"github.com/if3110/tubes-db-platform/internal/auth"
)

func NewRouter(sessions *auth.Store, authHandler *auth.Handler, adminHandler *admin.Handler, view *Renderer, secure bool) http.Handler {
	mux := http.NewServeMux()
	authHandler.RegisterRoutes(mux)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		view.Render(w, r, "home/index", nil)
	})

	// Admin routes: login wajib + harus Platform Admin
	adminMux := http.NewServeMux()
	adminHandler.RegisterRoutes(adminMux)
	protected.Handle("/admin/", access.RequirePlatformAdmin(adminMux))

	mux.Handle("/", sessions.RequireLogin(protected))

	return auth.CSRF(secure)(mux)
}
