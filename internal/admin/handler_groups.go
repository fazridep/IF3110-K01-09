package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
)

type groupsPage struct {
	Groups []Group
}

type groupDetailPage struct {
	Group      *Group
	Members    []Member
	NonMembers []User
	Grants     []Grant
	Databases  []Database
}

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.store.ListGroups(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.view.Render(w, r, "admin/groups", groupsPage{Groups: groups})
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
		return
	}
	id, err := h.store.CreateGroup(r.Context(), name, description)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) ShowGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	group, err := h.store.GetGroup(r.Context(), groupID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if group == nil {
		h.fail(w, r, http.StatusNotFound, "Datanya nggak ketemu.")
		return
	}

	members, err := h.store.ListMembers(r.Context(), groupID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	nonMembers, err := h.store.NonMembers(r.Context(), groupID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	grants, err := h.store.ListGrants(r.Context(), groupID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	databases, err := h.store.ListDatabases(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.view.Render(w, r, "admin/group_detail", groupDetailPage{
		Group:      group,
		Members:    members,
		NonMembers: nonMembers,
		Grants:     grants,
		Databases:  databases,
	})
}

func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	name := r.FormValue("name")
	description := r.FormValue("description")
	if err := h.store.UpdateGroup(r.Context(), groupID, name, description); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.store.DeleteGroup(r.Context(), groupID); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/groups", http.StatusSeeOther)
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, "User yang dipilih nggak valid.")
		return
	}
	if err := h.store.AddMember(r.Context(), groupID, userID); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	userID, ok := h.pathID(w, r, "userID")
	if !ok {
		return
	}

	if err := h.store.RemoveMember(r.Context(), groupID, userID); err != nil {
		h.serverError(w, r, err)
		return
	}

	if err := h.sessions.RevokeUserSessions(r.Context(), userID); err != nil {
		h.serverError(w, r, err)
		return
	}

	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) SetGrant(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	databaseID, err := uuid.Parse(r.FormValue("database_id"))
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, "Database yang dipilih nggak valid.")
		return
	}
	err = h.store.SetGrant(r.Context(), groupID, databaseID, access.Level(r.FormValue("level")))
	if errors.Is(err, ErrInvalidLevel) {
		h.fail(w, r, http.StatusBadRequest, "Level akses harus View Only atau View + Edit.")
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}

func (h *Handler) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	dbID, ok := h.pathID(w, r, "dbID")
	if !ok {
		return
	}
	if err := h.store.RevokeGrant(r.Context(), groupID, dbID); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/groups/"+groupID.String(), http.StatusSeeOther)
}
