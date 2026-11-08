package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

type Handler struct {
	db    *pgxpool.Pool
	store *Store
}

func NewHandler(db *pgxpool.Pool, store *Store) *Handler {
	return &Handler{db: db, store: store}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/session", h.Login)
	mux.HandleFunc("DELETE /api/session", h.Logout)
	mux.HandleFunc("POST /logout", h.logoutForm)
}

func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me", h.Me)
}

type UserResponse struct {
	ID              uuid.UUID `json:"id"`
	Username        string    `json:"username"`
	IsPlatformAdmin bool      `json:"isPlatformAdmin"`
}

func NewUserResponse(u *User) UserResponse {
	return UserResponse{ID: u.ID, Username: u.Username, IsPlatformAdmin: u.IsPlatformAdmin}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

const msgInvalidCredentials = "Username atau password salah."

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	var missing []httpx.FieldError
	if in.Username == "" {
		missing = append(missing, httpx.FieldError{Field: "username", Detail: "wajib diisi"})
	}
	if in.Password == "" {
		missing = append(missing, httpx.FieldError{Field: "password", Detail: "wajib diisi"})
	}
	if len(missing) > 0 {
		httpx.Invalid(w, r, "Username dan password wajib diisi.", missing...)
		return
	}

	ctx := r.Context()
	user, err := authenticate(ctx, h.db, in.Username, in.Password)
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		obs.Warn(ctx, "auth.login", "login gagal", obs.KeyActor, in.Username, obs.KeyStatus, "invalid_credentials")
		httpx.WriteProblem(w, r, httpx.Problem{Type: "urn:problem:invalid-credentials", Title: "Login gagal", Status: http.StatusUnauthorized, Detail: msgInvalidCredentials})
		return
	case errors.Is(err, ErrUserDisabled):
		obs.Warn(ctx, "auth.login", "login ditolak, akun nonaktif", obs.KeyActor, in.Username, obs.KeyStatus, "disabled")
		httpx.WriteProblem(w, r, httpx.Problem{Type: "urn:problem:account-disabled", Title: "Login gagal", Status: http.StatusForbidden, Detail: "Akun ini sudah dinonaktifkan. Hubungi Platform Admin."})
		return
	case err != nil:
		httpx.ServerError(w, r, err)
		return
	}

	if old := tokenFromRequest(r); old != "" {
		if err := h.store.Revoke(ctx, old); err != nil {
			httpx.ServerError(w, r, err)
			return
		}
	}
	token, expiresAt, err := h.store.Create(ctx, user.ID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	h.store.setCookie(w, token, expiresAt)
	obs.SetActor(ctx, user.Username, user.ID.String())
	obs.Info(ctx, "auth.login", "login berhasil", obs.KeyStatus, "ok")
	httpx.Created(w, "/api/me", NewUserResponse(user))
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if !h.revokeCurrent(w, r) {
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) logoutForm(w http.ResponseWriter, r *http.Request) {
	if !h.revokeCurrent(w, r) {
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) revokeCurrent(w http.ResponseWriter, r *http.Request) bool {
	if token := tokenFromRequest(r); token != "" {
		if err := h.store.Revoke(r.Context(), token); err != nil {
			httpx.ServerError(w, r, err)
			return false
		}
		obs.Info(r.Context(), "auth.logout", "logout", obs.KeyStatus, "ok")
	}
	h.store.clearCookie(w)
	return true
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r.Context())
	if u == nil {
		httpx.Fail(w, r, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Silakan login dulu.")
		return
	}
	httpx.JSON(w, http.StatusOK, NewUserResponse(u))
}
