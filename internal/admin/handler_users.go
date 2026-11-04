package admin

import (
	"errors"
	"net/http"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

type usersPage struct {
	Users    []User
	Error    string
	Username string
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	h.renderUsers(w, r, http.StatusOK, usersPage{})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	_, err := h.store.CreateUser(r.Context(), username, r.FormValue("password"))
	switch {
	case errors.Is(err, ErrUsernameTaken):
		h.renderUsers(w, r, http.StatusConflict, usersPage{Error: "Username sudah dipakai.", Username: username})
	case errors.Is(err, ErrInvalidUser):
		h.renderUsers(w, r, http.StatusBadRequest, usersPage{Error: "Username dan password wajib diisi.", Username: username})
	case err != nil:
		h.serverError(w, r, err)
	default:
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

func (h *Handler) renderUsers(w http.ResponseWriter, r *http.Request, status int, page usersPage) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	page.Users = users
	h.view.RenderStatus(w, r, status, "admin/users", page)
}

func (h *Handler) ToggleAdmin(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	currentUser := auth.CurrentUser(r.Context())
	if currentUser != nil && currentUser.ID == userID {
		h.fail(w, r, http.StatusForbidden, "Kamu nggak bisa mengubah status admin akunmu sendiri.")
		return
	}

	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if user == nil {
		h.fail(w, r, http.StatusNotFound, "Datanya nggak ketemu.")
		return
	}

	newStatus := !user.IsPlatformAdmin
	if err := h.store.SetPlatformAdmin(r.Context(), userID, newStatus); err != nil {
		h.serverError(w, r, err)
		return
	}

	if newStatus {
		if err := h.sessions.ForceRotate(r.Context(), userID); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	if !newStatus {
		if err := h.sessions.RevokeUserSessions(r.Context(), userID); err != nil {
			h.serverError(w, r, err)
			return
		}
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) ToggleActive(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	if u := auth.CurrentUser(r.Context()); u != nil && u.ID == userID {
		h.fail(w, r, http.StatusForbidden, "Kamu nggak bisa menonaktifkan akunmu sendiri.")
		return
	}
	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if user == nil {
		h.fail(w, r, http.StatusNotFound, "Datanya nggak ketemu.")
		return
	}

	disable := !user.Disabled
	if err := h.store.SetDisabled(r.Context(), userID, disable); err != nil {
		h.serverError(w, r, err)
		return
	}
	if disable {
		if err := h.sessions.RevokeUserSessions(r.Context(), userID); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
