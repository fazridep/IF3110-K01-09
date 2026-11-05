package admin

import (
	"context"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/httpx"
)

type Sessions interface {
	RevokeUserSessions(ctx context.Context, userID uuid.UUID) error
	ForceRotate(ctx context.Context, userID uuid.UUID) error
}

type Handler struct {
	store    *Store
	sessions Sessions
	view     httpx.Renderer
}

func NewHandler(store *Store, sessions Sessions, view httpx.Renderer) *Handler {
	return &Handler{store: store, sessions: sessions, view: view}
}

// RegisterRoutes mendaftarkan halaman admin. Semuanya lewat
// access.RequirePlatformAdmin.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	admin := http.NewServeMux()

	// Users
	admin.HandleFunc("GET /admin/users", h.ListUsers)
	admin.HandleFunc("POST /admin/users", h.CreateUser)
	admin.HandleFunc("POST /admin/users/{id}/toggle-admin", h.ToggleAdmin)
	admin.HandleFunc("POST /admin/users/{id}/toggle-active", h.ToggleActive)

	// Groups
	admin.HandleFunc("GET /admin/groups", h.ListGroups)
	admin.HandleFunc("POST /admin/groups", h.CreateGroup)
	admin.HandleFunc("GET /admin/groups/{id}", h.ShowGroup)
	admin.HandleFunc("POST /admin/groups/{id}", h.UpdateGroup)
	admin.HandleFunc("POST /admin/groups/{id}/delete", h.DeleteGroup)

	// Members
	admin.HandleFunc("POST /admin/groups/{id}/members", h.AddMember)
	admin.HandleFunc("POST /admin/groups/{id}/members/{userID}/remove", h.RemoveMember)

	// Grants
	admin.HandleFunc("POST /admin/groups/{id}/grants", h.SetGrant)
	admin.HandleFunc("POST /admin/groups/{id}/grants/{dbID}/revoke", h.RevokeGrant)

	guard := access.RequirePlatformAdmin(admin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, r, http.StatusForbidden, "Halaman ini cuma buat Platform Admin.")
	}))
	for _, p := range []string{"/admin/users", "/admin/users/", "/admin/groups", "/admin/groups/"} {
		mux.Handle(p, guard)
	}
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, ok := httpx.PathUUID(r, name)
	if !ok {
		h.fail(w, r, http.StatusBadRequest, "Alamatnya nggak valid.")
	}
	return id, ok
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("admin: %v", err)
	h.fail(w, r, http.StatusInternalServerError, "Terjadi kesalahan di server.")
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	httpx.Error(w, r, h.view, status, httpx.ErrorPage{Message: msg})
}
