package web
import (
	"html/template"
	"log"
	"net/http"
	"github.com/if3110/tubes-db-platform/internal/auth"
)

func NewRouter(authStore *auth.Store, authHandler *auth.Handler) http.Handler {
	mux := http.NewServeMux()
	authHandler.RegisterRoutes(mux)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		user := auth.CurrentUser(r.Context())
		RenderHTML(w, dashboardTmpl, map[string]any{
			"Username":        user.Username,
			"IsPlatformAdmin": user.IsPlatformAdmin,
			"CSRFToken":       auth.CSRFToken(r),
		})
	})
	mux.Handle("/", authStore.RequireLogin(protected))
	return auth.CSRFMiddleware(mux)
}

func RenderHTML(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("web: render %s: %v", tmpl.Name(), err)
	}
}

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Dashboard — Database Management Platform</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
    body { background-color: #ffffff; color: #333333; min-height: 100vh; }
    header { background-color: #ffffff; border-bottom: 1px solid #e5e7eb; padding: 16px 24px; display: flex; align-items: center; justify-content: space-between; }
    .logo { font-size: 18px; font-weight: 700; color: #111827; }
    .user-nav { display: flex; align-items: center; gap: 16px; font-size: 14px; }
    .btn-logout { background: transparent; border: 1px solid #d1d5db; color: #374151; padding: 6px 14px; border-radius: 6px; font-size: 13px; cursor: pointer; transition: background 0.2s; }
    .btn-logout:hover { background: #f3f4f6; }
    main { max-width: 1000px; margin: 0 auto; padding: 40px 24px; }
    h1 { font-size: 24px; font-weight: 700; margin-bottom: 12px; color: #111827; }
    p { color: #6b7280; font-size: 15px; }
  </style>
</head>
<body>
<header>
  <div class="logo">DB Platform</div>
  <div class="user-nav">
    <span>Halo, <strong>{{.Username}}</strong>{{if .IsPlatformAdmin}} (Admin){{end}}</span>
    <form method="POST" action="/logout" style="margin:0">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <button class="btn-logout" type="submit">Logout</button>
    </form>
  </div>
</header>
<main>
  <h1>Dashboard</h1>
  <p>Selamat datang di Database Management Platform.</p>
</main>
</body>
</html>
`))