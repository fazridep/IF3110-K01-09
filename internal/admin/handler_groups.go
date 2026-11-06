package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/httpx"
	"github.com/if3110/tubes-db-platform/internal/obs"
)

type groupJSON struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	LeaderID       *uuid.UUID `json:"leaderId"`
	LeaderUsername string     `json:"leaderUsername"`
	MemberCount    int        `json:"memberCount"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type memberJSON struct {
	UserID   uuid.UUID `json:"userId"`
	Username string    `json:"username"`
	IsLeader bool      `json:"isLeader"`
	AddedAt  time.Time `json:"addedAt"`
}

type grantJSON struct {
	GroupID      uuid.UUID `json:"groupId"`
	DatabaseID   uuid.UUID `json:"databaseId"`
	DatabaseName string    `json:"databaseName"`
	Level        string    `json:"level"`
	GrantedAt    time.Time `json:"grantedAt"`
}

type databaseOptionJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type groupDetailJSON struct {
	Group      groupJSON            `json:"group"`
	Members    []memberJSON         `json:"members"`
	NonMembers []userJSON           `json:"nonMembers"`
	Grants     []grantJSON          `json:"grants"`
	Databases  []databaseOptionJSON `json:"databases"`
}

func toGroupJSON(g Group) groupJSON {
	return groupJSON{
		ID:             g.ID,
		Name:           g.Name,
		Description:    g.Description,
		LeaderID:       g.LeaderID,
		LeaderUsername: g.LeaderUsername,
		MemberCount:    g.MemberCount,
		CreatedAt:      g.CreatedAt,
		UpdatedAt:      g.UpdatedAt,
	}
}

func toGroupsJSON(groups []Group) []groupJSON {
	out := make([]groupJSON, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupJSON(g))
	}
	return out
}

func toMembersJSON(members []Member) []memberJSON {
	out := make([]memberJSON, 0, len(members))
	for _, m := range members {
		out = append(out, memberJSON{
			UserID:   m.UserID,
			Username: m.Username,
			IsLeader: m.IsLeader,
			AddedAt:  m.AddedAt,
		})
	}
	return out
}

func toGrantsJSON(grants []Grant) []grantJSON {
	out := make([]grantJSON, 0, len(grants))
	for _, g := range grants {
		out = append(out, grantJSON{
			GroupID:      g.GroupID,
			DatabaseID:   g.DatabaseID,
			DatabaseName: g.DatabaseName,
			Level:        g.Level,
			GrantedAt:    g.GrantedAt,
		})
	}
	return out
}

func toDatabasesJSON(dbs []Database) []databaseOptionJSON {
	out := make([]databaseOptionJSON, 0, len(dbs))
	for _, d := range dbs {
		out = append(out, databaseOptionJSON{ID: d.ID, Name: d.Name})
	}
	return out
}

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.store.ListGroups(r.Context())
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(toGroupsJSON(groups)))
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		httpx.Invalid(w, r, "Nama group wajib diisi.", httpx.FieldError{Field: "name", Detail: "wajib diisi"})
		return
	}

	id, err := h.store.CreateGroup(r.Context(), in.Name, in.Description)
	switch {
	case errors.Is(err, ErrGroupNameTaken):
		httpx.WriteProblem(w, r, httpx.Problem{
			Type:   "urn:problem:" + httpx.CodeConflict,
			Status: http.StatusConflict,
			Detail: "Nama group sudah dipakai.",
			Errors: []httpx.FieldError{{Field: "name", Detail: "sudah dipakai"}},
		})
		return
	case errors.Is(err, ErrInvalidGroup):
		httpx.Invalid(w, r, "Nama group wajib diisi.", httpx.FieldError{Field: "name", Detail: "wajib diisi"})
		return
	case err != nil:
		httpx.ServerError(w, r, err)
		return
	}

	g, err := h.store.GetGroup(r.Context(), id)
	if err != nil || g == nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(r.Context(), "admin.group_created", "group dibuat", "group_id", g.ID.String(), "group_name", g.Name)
	httpx.Created(w, "/api/admin/groups/"+g.ID.String(), toGroupJSON(*g))
}

func (h *Handler) ShowGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	group, err := h.store.GetGroup(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if group == nil {
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	}

	members, err := h.store.ListMembers(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	nonMembers, err := h.store.NonMembers(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	grants, err := h.store.ListGrants(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	databases, err := h.store.ListDatabases(ctx)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, groupDetailJSON{
		Group:      toGroupJSON(*group),
		Members:    toMembersJSON(members),
		NonMembers: toUsersJSON(nonMembers),
		Grants:     toGrantsJSON(grants),
		Databases:  toDatabasesJSON(databases),
	})
}

func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		httpx.Invalid(w, r, "Nama group wajib diisi.", httpx.FieldError{Field: "name", Detail: "wajib diisi"})
		return
	}

	err := h.store.UpdateGroup(r.Context(), groupID, in.Name, in.Description)
	switch {
	case errors.Is(err, ErrGroupNotFound):
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	case errors.Is(err, ErrGroupNameTaken):
		httpx.WriteProblem(w, r, httpx.Problem{
			Type:   "urn:problem:" + httpx.CodeConflict,
			Status: http.StatusConflict,
			Detail: "Nama group sudah dipakai.",
			Errors: []httpx.FieldError{{Field: "name", Detail: "sudah dipakai"}},
		})
		return
	case errors.Is(err, ErrInvalidGroup):
		httpx.Invalid(w, r, "Nama group wajib diisi.", httpx.FieldError{Field: "name", Detail: "wajib diisi"})
		return
	case err != nil:
		httpx.ServerError(w, r, err)
		return
	}

	g, err := h.store.GetGroup(r.Context(), groupID)
	if err != nil || g == nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(r.Context(), "admin.group_updated", "group diubah", "group_id", groupID.String())
	httpx.JSON(w, http.StatusOK, toGroupJSON(*g))
}

func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	group, err := h.store.GetGroup(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if group == nil {
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	}

	members, err := h.store.ListMembers(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if err := h.store.DeleteGroup(ctx, groupID); err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	for _, m := range members {
		if err := h.sessions.RevokeUserSessions(ctx, m.UserID); err != nil {
			httpx.ServerError(w, r, err)
			return
		}
	}
	obs.Info(ctx, "admin.group_deleted", "group dihapus", "group_id", groupID.String())
	httpx.NoContent(w)
}

func (h *Handler) SetLeader(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		UserID *string `json:"userId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}

	var leaderID *uuid.UUID
	if in.UserID != nil && strings.TrimSpace(*in.UserID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*in.UserID))
		if err != nil {
			httpx.Invalid(w, r, "User leader tidak valid.", httpx.FieldError{Field: "userId", Detail: "UUID tidak valid"})
			return
		}
		leaderID = &parsed
	}

	err := h.store.SetLeader(r.Context(), groupID, leaderID)
	switch {
	case errors.Is(err, ErrGroupNotFound):
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	case errors.Is(err, ErrLeaderNotMember):
		httpx.Invalid(w, r, "Leader harus merupakan anggota aktif dari group ini.", httpx.FieldError{Field: "userId", Detail: "bukan anggota aktif group"})
		return
	case err != nil:
		httpx.ServerError(w, r, err)
		return
	}

	g, err := h.store.GetGroup(r.Context(), groupID)
	if err != nil || g == nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(r.Context(), "admin.group_leader_set", "leader group diubah", "group_id", groupID.String(), "leader_id", g.LeaderID)
	httpx.JSON(w, http.StatusOK, toGroupJSON(*g))
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		UserID string `json:"userId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	userID, err := uuid.Parse(strings.TrimSpace(in.UserID))
	if err != nil {
		httpx.Invalid(w, r, "User yang dipilih tidak valid.", httpx.FieldError{Field: "userId", Detail: "wajib pilih user"})
		return
	}

	ctx := r.Context()
	g, err := h.store.GetGroup(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if g == nil {
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	}
	u, err := h.store.GetUser(ctx, userID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if u == nil || u.Disabled {
		httpx.Invalid(w, r, "User tidak ditemukan atau sedang nonaktif.", httpx.FieldError{Field: "userId", Detail: "user tidak valid"})
		return
	}

	if err := h.store.AddMember(ctx, groupID, userID); err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	members, err := h.store.ListMembers(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(ctx, "admin.member_added", "anggota ditambahkan ke group", "group_id", groupID.String(), "target_user_id", userID.String())
	httpx.JSON(w, http.StatusOK, httpx.NewList(toMembersJSON(members)))
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

	ctx := r.Context()
	g, err := h.store.GetGroup(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if g == nil {
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	}

	if err := h.store.RemoveMember(ctx, groupID, userID); err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if err := h.sessions.RevokeUserSessions(ctx, userID); err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(ctx, "admin.member_removed", "anggota dikeluarkan dari group", "group_id", groupID.String(), "target_user_id", userID.String())
	httpx.NoContent(w)
}

func (h *Handler) SetGrant(w http.ResponseWriter, r *http.Request) {
	groupID, ok := h.pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		DatabaseID string `json:"databaseId"`
		Level      string `json:"level"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}

	var errs []httpx.FieldError
	databaseID, err := uuid.Parse(strings.TrimSpace(in.DatabaseID))
	if err != nil {
		errs = append(errs, httpx.FieldError{Field: "databaseId", Detail: "wajib pilih database"})
	}
	level := access.Level(strings.TrimSpace(in.Level))
	if !level.Valid() {
		errs = append(errs, httpx.FieldError{Field: "level", Detail: "harus view_only atau view_edit"})
	}
	if len(errs) > 0 {
		httpx.Invalid(w, r, "Data grant akses tidak valid.", errs...)
		return
	}

	ctx := r.Context()
	g, err := h.store.GetGroup(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if g == nil {
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	}

	err = h.store.SetGrant(ctx, groupID, databaseID, level)
	switch {
	case errors.Is(err, ErrInvalidLevel):
		httpx.Invalid(w, r, "Level akses harus view_only atau view_edit.", httpx.FieldError{Field: "level", Detail: "level tidak valid"})
		return
	case errors.Is(err, ErrDatabaseNotFound):
		httpx.Invalid(w, r, "Database tidak ditemukan.", httpx.FieldError{Field: "databaseId", Detail: "database tidak ditemukan"})
		return
	case err != nil:
		httpx.ServerError(w, r, err)
		return
	}

	grants, err := h.store.ListGrants(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	obs.Info(ctx, "admin.grant_set", "grant database diubah", "group_id", groupID.String(), "database_id", databaseID.String(), "level", string(level))
	httpx.JSON(w, http.StatusOK, httpx.NewList(toGrantsJSON(grants)))
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

	ctx := r.Context()
	g, err := h.store.GetGroup(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	if g == nil {
		httpx.NotFound(w, r, "Group tidak ditemukan.")
		return
	}

	if err := h.store.RevokeGrant(ctx, groupID, dbID); err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	members, err := h.store.ListMembers(ctx, groupID)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	for _, m := range members {
		if err := h.sessions.RevokeUserSessions(ctx, m.UserID); err != nil {
			httpx.ServerError(w, r, err)
			return
		}
	}
	obs.Info(ctx, "admin.grant_revoked", "grant database dicabut", "group_id", groupID.String(), "database_id", dbID.String())
	httpx.NoContent(w)
}
