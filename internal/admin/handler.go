package admin

import (
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

// Renderer diimplementasi oleh package web. Didefinisikan di sini supaya admin
// nggak perlu import web.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, name string, data any)
}

type Handler struct {
	db       *pgxpool.Pool
	store    *Store
	sessions *auth.Store
	view     Renderer
}

func NewHandler(db *pgxpool.Pool, store *Store, sessions *auth.Store, view Renderer) *Handler {
	return &Handler{db: db, store: store, sessions: sessions, view: view}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Users
	mux.HandleFunc("GET /admin/users", h.ListUsers)
	mux.HandleFunc("POST /admin/users/{id}/toggle-admin", h.ToggleAdmin)

	// Groups
	mux.HandleFunc("GET /admin/groups", h.ListGroups)
	mux.HandleFunc("POST /admin/groups", h.CreateGroup)
	mux.HandleFunc("GET /admin/groups/{id}", h.ShowGroup)
	mux.HandleFunc("POST /admin/groups/{id}", h.UpdateGroup)
	mux.HandleFunc("POST /admin/groups/{id}/delete", h.DeleteGroup)

	// Members
	mux.HandleFunc("POST /admin/groups/{id}/members", h.AddMember)
	mux.HandleFunc("POST /admin/groups/{id}/members/{userID}/remove", h.RemoveMember)

	// Grants
	mux.HandleFunc("POST /admin/groups/{id}/grants", h.SetGrant)
	mux.HandleFunc("POST /admin/groups/{id}/grants/{dbID}/revoke", h.RevokeGrant)
}

// --- User handlers ---

type usersPage struct {
	Users []User
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	h.view.Render(w, r, "admin/users", usersPage{Users: users})
}

func (h *Handler) ToggleAdmin(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}

	// Jangan biarkan admin mencabut status admin dirinya sendiri
	currentUser := auth.CurrentUser(r.Context())
	if currentUser != nil && currentUser.ID == userID {
		http.Error(w, "tidak bisa mengubah status admin sendiri", http.StatusForbidden)
		return
	}

	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		serverError(w, err)
		return
	}
	if user == nil {
		http.NotFound(w, r)
		return
	}

	newStatus := !user.IsPlatformAdmin
	if err := h.store.SetPlatformAdmin(r.Context(), userID, newStatus); err != nil {
		serverError(w, err)
		return
	}

	// Kalau user dijadikan admin, force rotate session (ADR-003: rotasi saat privilege naik)
	if newStatus {
		if err := h.sessions.ForceRotate(r.Context(), userID); err != nil {
			serverError(w, err)
			return
		}
	}
	// Kalau admin dicabut, revoke semua session (PROGRESS.md: cabut akses → RevokeUserSessions)
	if !newStatus {
		if err := h.sessions.RevokeUserSessions(r.Context(), userID); err != nil {
			serverError(w, err)
			return
		}
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// --- Group handlers ---

type groupsPage struct {
	Groups []Group
}

type groupDetailPage struct {
	Group      *Group
	Members    []Member
	NonMembers []User
	Grants     []Grant
}

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.store.ListGroups(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	h.view.Render(w, r, "admin/groups", groupsPage{Groups: groups})
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		// Redirect balik dengan query parameter error sederhana
		http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
		return
	}
	id, err := h.store.CreateGroup(r.Context(), name, description)
	if err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) ShowGroup(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	group, err := h.store.GetGroup(r.Context(), groupID)
	if err != nil {
		serverError(w, err)
		return
	}
	if group == nil {
		http.NotFound(w, r)
		return
	}

	members, err := h.store.ListMembers(r.Context(), groupID)
	if err != nil {
		serverError(w, err)
		return
	}
	nonMembers, err := h.store.NonMembers(r.Context(), groupID)
	if err != nil {
		serverError(w, err)
		return
	}
	grants, err := h.store.ListGrants(r.Context(), groupID)
	if err != nil {
		serverError(w, err)
		return
	}

	h.view.Render(w, r, "admin/group_detail", groupDetailPage{
		Group:      group,
		Members:    members,
		NonMembers: nonMembers,
		Grants:     grants,
	})
}

func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	description := r.FormValue("description")
	if err := h.store.UpdateGroup(r.Context(), groupID, name, description); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteGroup(r.Context(), groupID); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
}

// --- Member handlers ---

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Error(w, "user_id tidak valid", http.StatusBadRequest)
		return
	}
	if err := h.store.AddMember(r.Context(), groupID, userID); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		http.Error(w, "userID tidak valid", http.StatusBadRequest)
		return
	}

	if err := h.store.RemoveMember(r.Context(), groupID, userID); err != nil {
		serverError(w, err)
		return
	}

	// Revoke session user yang dikeluarkan, karena aksesnya bisa berubah
	if err := h.sessions.RevokeUserSessions(r.Context(), userID); err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

// --- Grant handlers ---

func (h *Handler) SetGrant(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	databaseID, err := uuid.Parse(r.FormValue("database_id"))
	if err != nil {
		http.Error(w, "database_id tidak valid", http.StatusBadRequest)
		return
	}
	level := r.FormValue("level")
	if err := h.store.SetGrant(r.Context(), groupID, databaseID, level); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id tidak valid", http.StatusBadRequest)
		return
	}
	dbID, err := uuid.Parse(r.PathValue("dbID"))
	if err != nil {
		http.Error(w, "dbID tidak valid", http.StatusBadRequest)
		return
	}
	if err := h.store.RevokeGrant(r.Context(), groupID, dbID); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("admin: %v", err)
	http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
}
