package admin

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
)

type Sessions interface {
	RevokeUserSessions(ctx context.Context, userID uuid.UUID) error
	ForceRotate(ctx context.Context, userID uuid.UUID) error
}

type Handler struct {
	store    *Store
	sessions Sessions
}

func NewHandler(store *Store, sessions Sessions) *Handler {
	return &Handler{store: store, sessions: sessions}
}

// RegisterAPIRoutes mendaftarkan endpoint JSON administrasi platform.
// Semua endpoint diproteksi oleh access.RequireAPIPlatformAdmin.
func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	handle := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, access.RequireAPIPlatformAdmin(fn))
	}

	// Users
	handle("GET /api/admin/users", h.ListUsers)
	handle("POST /api/admin/users", h.CreateUser)
	handle("PATCH /api/admin/users/{id}", h.UpdateUser)

	// Groups
	handle("GET /api/admin/groups", h.ListGroups)
	handle("POST /api/admin/groups", h.CreateGroup)
	handle("GET /api/admin/groups/{id}", h.ShowGroup)
	handle("PATCH /api/admin/groups/{id}", h.UpdateGroup)
	handle("PUT /api/admin/groups/{id}", h.UpdateGroup)
	handle("DELETE /api/admin/groups/{id}", h.DeleteGroup)

	// Leader
	handle("PUT /api/admin/groups/{id}/leader", h.SetLeader)

	// Members
	handle("POST /api/admin/groups/{id}/members", h.AddMember)
	handle("DELETE /api/admin/groups/{id}/members/{userID}", h.RemoveMember)

	// Grants
	handle("PUT /api/admin/groups/{id}/grants", h.SetGrant)
	handle("POST /api/admin/groups/{id}/grants", h.SetGrant)
	handle("DELETE /api/admin/groups/{id}/grants/{dbID}", h.RevokeGrant)
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, ok := httpx.PathUUID(r, name)
	if !ok {
		httpx.Invalid(w, r, "ID pada URL tidak valid.")
	}
	return id, ok
}
