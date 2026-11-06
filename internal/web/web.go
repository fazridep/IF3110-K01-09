package web

import (
	"net/http"
	"strings"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/httpx"
	"github.com/if3110/tubes-db-platform/internal/obs"
)

type Options struct {
	Sessions *auth.Store
	Auth     *auth.Handler

	RegisterAPI   func(*http.ServeMux)
	RegisterPages func(*http.ServeMux)
	SPA           http.Handler
	Secure        bool
}

// NewRouter ngecek urut route publik, /api/*, halaman lama, lalu SPA.
func NewRouter(o Options) http.Handler {
	public := http.NewServeMux()
	o.Auth.RegisterRoutes(public)
	public.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	api := http.NewServeMux()
	o.Auth.RegisterAPIRoutes(api)
	if o.RegisterAPI != nil {
		o.RegisterAPI(api)
	}
	apiHandler := o.Sessions.RequireAPILogin(api)

	pages := http.NewServeMux()
	if o.RegisterPages != nil {
		o.RegisterPages(pages)
	}
	pagesHandler := o.Sessions.RequireLogin(pages)

	spa := o.SPA
	if spa == nil {
		spa = http.NotFoundHandler()
	}

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, pattern := public.Handler(r); pattern != "" {
			obs.SetRoute(r.Context(), pattern)
			h.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			_, pattern := api.Handler(r)
			if pattern == "" {
				apiNotFound(w, r, api)
				return
			}
			obs.SetRoute(r.Context(), pattern)
			apiHandler.ServeHTTP(w, r)
			return
		}
		if _, pattern := pages.Handler(r); pattern != "" {
			obs.SetRoute(r.Context(), pattern)
			pagesHandler.ServeHTTP(w, r)
			return
		}
		obs.SetRoute(r.Context(), "SPA")
		spa.ServeHTTP(w, r)
	})

	return obs.Middleware(auth.CSRF(o.Secure)(root))
}

func apiNotFound(w http.ResponseWriter, r *http.Request, api *http.ServeMux) {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if m == r.Method {
			continue
		}
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, p := api.Handler(probe); p != "" {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		httpx.Fail(w, r, http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed, "Method "+r.Method+" tidak didukung untuk endpoint ini.")
		return
	}
	httpx.NotFound(w, r, "Endpoint tidak ada.")
}
