package auth
import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db    *pgxpool.Pool
	store *Store
}

func NewHandler(db *pgxpool.Pool, store *Store) *Handler {
	return &Handler{db: db, store: store}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", h.ShowLogin)
	mux.HandleFunc("POST /login", h.DoLogin)
	mux.HandleFunc("POST /logout", h.DoLogout)
}

func (h *Handler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	if tok := TokenFromRequest(r); tok != "" {
		if user, _, err := h.store.Validate(r.Context(), tok); err == nil && user != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	renderLogin(w, loginData{CSRFToken: CSRFToken(r)})
}

func (h *Handler) DoLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "request tidak valid", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		renderLogin(w, loginData{CSRFToken: CSRFToken(r), Error: "Username dan password wajib diisi."})
		return
	}
	user, err := h.authenticate(r.Context(), username, password)
	if err != nil {
		renderLogin(w, loginData{CSRFToken: CSRFToken(r), Error: "Username atau password salah."})
		return
	}
	token, err := h.store.Create(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "gagal membuat session", http.StatusInternalServerError)
		return
	}
	h.store.SetCookie(w, token, time.Now().Add(h.store.cfg.AbsoluteTimeout))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) DoLogout(w http.ResponseWriter, r *http.Request) {
	if token := TokenFromRequest(r); token != "" {
		_ = h.store.Revoke(r.Context(), token)
	}
	ClearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) authenticate(ctx context.Context, username, password string) (*User, error) {
	u := &User{}
	var hash string
	err := h.db.QueryRow(ctx, `SELECT id, username, password_hash, is_platform_admin, created_at, updated_at FROM users WHERE username=$1`, username).Scan(&u.ID, &u.Username, &hash, &u.IsPlatformAdmin, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		_ = VerifyPassword("$argon2id$v=19$m=65536,t=1,p=4$00000000000000000000000000000000$0000000000000000000000000000000000000000000000000000000000000000", "dummy")
		return nil, ErrInvalidCredentials
	}
	if err = VerifyPassword(hash, password); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

func SeedAdmin(ctx context.Context, db *pgxpool.Pool, username, password string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("auth: username dan password tidak boleh kosong")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}
	var id uuid.UUID
	err = db.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, is_platform_admin)
		VALUES ($1, $2, TRUE)
		ON CONFLICT (username) DO NOTHING
		RETURNING id
	`, username, hash).Scan(&id)
	if err != nil {
		return fmt.Errorf("auth: username '%s' sudah ada", username)
	}
	fmt.Printf("✓ Admin dibuat: username=%s id=%s\n", username, id)
	return nil
}

// template Login
type loginData struct {
	CSRFToken string
	Error     string
}

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Login Form</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
    body {
      background-color: #f8f9fa;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      color: #333;
    }
    .card {
      background: #ffffff;
      width: 100%;
      max-width: 380px;
      padding: 32px;
      border: 1px solid #e9ecef;
      border-radius: 8px;
      box-shadow: 0 4px 16px rgba(0, 0, 0, 0.04);
    }
    h1 {
      text-align: center;
      font-size: 26px;
      font-weight: 700;
      margin-bottom: 24px;
      color: #111;
    }
    .form-group {
      margin-bottom: 16px;
    }
    label {
      display: block;
      font-size: 14px;
      font-weight: 500;
      margin-bottom: 6px;
      color: #333;
    }
    input[type="text"],
    input[type="password"] {
      width: 100%;
      padding: 10px 12px;
      font-size: 14px;
      border: 1px solid #dcdcdc;
      border-radius: 6px;
      outline: none;
      transition: border-color 0.2s;
    }
    input[type="text"]:focus,
    input[type="password"]:focus {
      border-color: #007bff;
    }
    input::placeholder {
      color: #aaa;
    }
    .form-group:last-of-type {
      margin-bottom: 24px;
    }
    button[type="submit"] {
      width: 100%;
      background-color: #007bff;
      color: #ffffff;
      border: none;
      border-radius: 6px;
      padding: 11px;
      font-size: 15px;
      font-weight: 500;
      cursor: pointer;
      transition: background-color 0.2s;
    }
    button[type="submit"]:hover {
      background-color: #0069d9;
    }
    .error {
      background-color: #fde8e8;
      color: #c81e1e;
      border: 1px solid #f8b4b4;
      padding: 10px 12px;
      border-radius: 6px;
      font-size: 13px;
      margin-bottom: 16px;
    }
  </style>
</head>
<body>
<main class="card">
  <h1>Login Form</h1>
  {{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}
  <form method="POST" action="/login">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <div class="form-group">
      <label for="username">Username:</label>
      <input type="text" id="username" name="username" placeholder="Enter your username" autocomplete="username" required autofocus>
    </div>
    <div class="form-group">
      <label for="password">Password:</label>
      <input type="password" id="password" name="password" placeholder="Enter your password" autocomplete="current-password" required>
    </div>
    <button type="submit">Login</button>
  </form>
</main>
</body>
</html>
`))

func renderLogin(w http.ResponseWriter, data loginData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := loginTmpl.Execute(w, data); err != nil {
		log.Printf("auth: render login: %v", err)
	}
}