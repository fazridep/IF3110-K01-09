package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/httpx"
	"github.com/if3110/tubes-db-platform/internal/obs"
)

type userJSON struct {
	ID              uuid.UUID `json:"id"`
	Username        string    `json:"username"`
	IsPlatformAdmin bool      `json:"isPlatformAdmin"`
	Disabled        bool      `json:"disabled"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func toUserJSON(u User) userJSON {
	return userJSON{
		ID:              u.ID,
		Username:        u.Username,
		IsPlatformAdmin: u.IsPlatformAdmin,
		Disabled:        u.Disabled,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toUsersJSON(users []User) []userJSON {
	out := make([]userJSON, 0, len(users))
	for _, u := range users {
		out = append(out, toUserJSON(u))
	}
	return out
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(toUsersJSON(users)))
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
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

	id, err := h.store.CreateUser(r.Context(), in.Username, in.Password)
	switch {
	case errors.Is(err, ErrUsernameTaken):
		httpx.WriteProblem(w, r, httpx.Problem{
			Type:   "urn:problem:" + httpx.CodeConflict,
			Status: http.StatusConflict,
			Detail: "Username sudah dipakai.",
			Errors: []httpx.FieldError{{Field: "username", Detail: "sudah dipakai"}},
		})
		return
	case errors.Is(err, ErrInvalidUser):
		httpx.Invalid(w, r, "Username dan password wajib diisi.")
		return
	case err != nil:
		httpx.ServerError(w, r, err)
		return
	}

	u, err := h.store.GetUser(r.Context(), id)
	if err != nil || u == nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(r.Context(), "admin.user_created", "user baru dibuat", "target_user", u.Username, "target_user_id", u.ID.String())
	httpx.Created(w, "/api/admin/users/"+u.ID.String(), toUserJSON(*u))
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}

	var in struct {
		IsPlatformAdmin *bool `json:"isPlatformAdmin"`
		Disabled        *bool `json:"disabled"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.IsPlatformAdmin == nil && in.Disabled == nil {
		httpx.Invalid(w, r, "Tidak ada perubahan yang dikirim.")
		return
	}

	ctx := r.Context()
	currentUser := auth.CurrentUser(ctx)
	if currentUser != nil && currentUser.ID == userID {
		if in.IsPlatformAdmin != nil {
			httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Kamu tidak bisa mengubah status admin akunmu sendiri.")
			return
		}
		if in.Disabled != nil {
			httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Kamu tidak bisa menonaktifkan akunmu sendiri.")
			return
		}
	}

	user, err := h.store.GetUser(ctx, userID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if user == nil {
		httpx.NotFound(w, r, "User tidak ditemukan.")
		return
	}

	if in.IsPlatformAdmin != nil && *in.IsPlatformAdmin != user.IsPlatformAdmin {
		if err := h.store.SetPlatformAdmin(ctx, userID, *in.IsPlatformAdmin); err != nil {
			httpx.ServerError(w, r, err)
			return
		}
		if *in.IsPlatformAdmin {
			if err := h.sessions.ForceRotate(ctx, userID); err != nil {
				httpx.ServerError(w, r, err)
				return
			}
		} else {
			if err := h.sessions.RevokeUserSessions(ctx, userID); err != nil {
				httpx.ServerError(w, r, err)
				return
			}
		}
		obs.Info(ctx, "admin.user_admin_updated", "status Platform Admin diubah", "target_user_id", userID.String(), "is_platform_admin", *in.IsPlatformAdmin)
	}

	if in.Disabled != nil && *in.Disabled != user.Disabled {
		if err := h.store.SetDisabled(ctx, userID, *in.Disabled); err != nil {
			httpx.ServerError(w, r, err)
			return
		}
		if *in.Disabled {
			if err := h.sessions.RevokeUserSessions(ctx, userID); err != nil {
				httpx.ServerError(w, r, err)
				return
			}
		}
		obs.Info(ctx, "admin.user_active_updated", "status aktif user diubah", "target_user_id", userID.String(), "disabled", *in.Disabled)
	}

	updated, err := h.store.GetUser(ctx, userID)
	if err != nil || updated == nil {
		httpx.ServerError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toUserJSON(*updated))
}
