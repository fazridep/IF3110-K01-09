package auth

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Renderer diimplementasi oleh package web. Didefinisikan di sini supaya auth
// nggak perlu import web (web sudah import auth).
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, name string, data any)
}

type Handler struct {
	db    *pgxpool.Pool
	store *Store
	view  Renderer
}

func NewHandler(db *pgxpool.Pool, store *Store, view Renderer) *Handler {
	return &Handler{db: db, store: store, view: view}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", h.ShowLogin)
	mux.HandleFunc("POST /login", h.DoLogin)
	mux.HandleFunc("POST /logout", h.DoLogout)
}

type loginPage struct {
	Error string
}

func (h *Handler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	if token := tokenFromRequest(r); token != "" {
		if sess, err := h.store.Validate(r.Context(), token); err == nil {
			h.store.refreshCookie(w, sess)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	h.view.Render(w, r, "auth/login", loginPage{})
}

func (h *Handler) DoLogin(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.view.Render(w, r, "auth/login", loginPage{Error: "Username dan password wajib diisi."})
		return
	}

	user, err := authenticate(r.Context(), h.db, username, password)
	if errors.Is(err, ErrInvalidCredentials) {
		h.view.Render(w, r, "auth/login", loginPage{Error: "Username atau password salah."})
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	if old := tokenFromRequest(r); old != "" {
		if err := h.store.Revoke(r.Context(), old); err != nil {
			serverError(w, err)
			return
		}
	}
	token, expiresAt, err := h.store.Create(r.Context(), user.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	h.store.setCookie(w, token, expiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) DoLogout(w http.ResponseWriter, r *http.Request) {
	if token := tokenFromRequest(r); token != "" {
		if err := h.store.Revoke(r.Context(), token); err != nil {
			serverError(w, err)
			return
		}
	}
	h.store.clearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("auth: %v", err)
	http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
}
