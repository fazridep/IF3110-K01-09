package approval

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

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/approvals", h.List)
	mux.HandleFunc("POST /api/approvals", h.Submit)
	mux.HandleFunc("GET /api/approvals/{id}", h.Get)
	mux.HandleFunc("POST /api/approvals/{id}/approve", h.Approve)
	mux.HandleFunc("POST /api/approvals/{id}/reject", h.Reject)
}

type groupDecisionJSON struct {
	GroupID        uuid.UUID  `json:"groupId"`
	GroupName      string     `json:"groupName"`
	AccessLevel    string     `json:"accessLevel"`
	LeaderID       *uuid.UUID `json:"leaderId"`
	LeaderUsername string     `json:"leaderUsername"`
	Decision       Status     `json:"decision"`
	DecidedBy      string     `json:"decidedBy"`
	Note           string     `json:"note"`
	DecidedAt      *time.Time `json:"decidedAt"`
}

type approvalJSON struct {
	ID              uuid.UUID           `json:"id"`
	MigrationID     uuid.UUID           `json:"migrationId"`
	DatabaseID      uuid.UUID           `json:"databaseId"`
	DatabaseName    string              `json:"databaseName"`
	Version         int                 `json:"version"`
	Description     string              `json:"description"`
	SQL             string              `json:"sql"`
	Action          Action              `json:"action"`
	Status          Status              `json:"status"`
	RequestedByID   uuid.UUID           `json:"requestedById"`
	RequestedBy     string              `json:"requestedBy"`
	RejectionReason string              `json:"rejectionReason"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	ResolvedAt      *time.Time          `json:"resolvedAt"`
	Approvals       []groupDecisionJSON `json:"approvals"`
	ApprovedCount   int                 `json:"approvedCount"`
	TotalRequired   int                 `json:"totalRequired"`
	CanDecide       bool                `json:"canDecide"`
	MyLeaderGroups  []string            `json:"myLeaderGroups"`
}

func toApprovalJSON(r Request) approvalJSON {
	decs := make([]groupDecisionJSON, 0, len(r.Approvals))
	for _, gd := range r.Approvals {
		decs = append(decs, groupDecisionJSON{
			GroupID:        gd.GroupID,
			GroupName:      gd.GroupName,
			AccessLevel:    string(gd.AccessLevel),
			LeaderID:       gd.LeaderID,
			LeaderUsername: gd.LeaderUsername,
			Decision:       gd.Decision,
			DecidedBy:      gd.DecidedBy,
			Note:           gd.Note,
			DecidedAt:      gd.DecidedAt,
		})
	}
	myGroups := r.MyLeaderGroups
	if myGroups == nil {
		myGroups = []string{}
	}
	return approvalJSON{
		ID:              r.ID,
		MigrationID:     r.MigrationID,
		DatabaseID:      r.DatabaseID,
		DatabaseName:    r.DatabaseName,
		Version:         r.Version,
		Description:     r.Description,
		SQL:             r.SQL,
		Action:          r.Action,
		Status:          r.Status,
		RequestedByID:   r.RequestedByID,
		RequestedBy:     r.RequestedBy,
		RejectionReason: r.RejectionReason,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		ResolvedAt:      r.ResolvedAt,
		Approvals:       decs,
		ApprovedCount:   r.ApprovedCount,
		TotalRequired:   r.TotalRequired,
		CanDecide:       r.CanDecide,
		MyLeaderGroups:  myGroups,
	}
}

func toApprovalsJSON(items []Request) []approvalJSON {
	out := make([]approvalJSON, 0, len(items))
	for _, r := range items {
		out = append(out, toApprovalJSON(r))
	}
	return out
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	if u == nil {
		httpx.Fail(w, r, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Silakan login dulu.")
		return
	}

	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	var dbFilter *uuid.UUID
	if rawDB := strings.TrimSpace(r.URL.Query().Get("databaseId")); rawDB != "" {
		parsed, err := uuid.Parse(rawDB)
		if err != nil {
			httpx.Invalid(w, r, "Parameter databaseId tidak valid.", httpx.FieldError{Field: "databaseId", Detail: "UUID tidak valid"})
			return
		}
		dbFilter = &parsed
	}

	items, err := h.svc.List(r.Context(), u.ID, statusFilter, dbFilter)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(toApprovalsJSON(items)))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	if u == nil {
		httpx.Fail(w, r, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Silakan login dulu.")
		return
	}
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.Invalid(w, r, "ID approval tidak valid.")
		return
	}

	req, err := h.svc.Get(r.Context(), u.ID, id)
	if errors.Is(err, ErrNotFound) {
		httpx.NotFound(w, r, "Permintaan approval tidak ditemukan.")
		return
	}
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toApprovalJSON(*req))
}

func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	if u == nil {
		httpx.Fail(w, r, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Silakan login dulu.")
		return
	}

	var in struct {
		MigrationID string `json:"migrationId"`
		Action      string `json:"action"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	migID, err := uuid.Parse(strings.TrimSpace(in.MigrationID))
	if err != nil {
		httpx.Invalid(w, r, "Migration wajib dipilih.", httpx.FieldError{Field: "migrationId", Detail: "UUID migration tidak valid"})
		return
	}

	req, err := h.svc.Submit(r.Context(), u.ID, migID, Action(strings.TrimSpace(in.Action)))
	switch {
	case errors.Is(err, ErrInvalidAction):
		httpx.Invalid(w, r, "Jenis aksi tidak valid.", httpx.FieldError{Field: "action", Detail: "harus apply, rollback, forward, atau batch"})
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, r, "Migration tidak ditemukan.")
	case errors.Is(err, ErrForbidden):
		httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Hanya user dengan akses View + Edit yang boleh mengajukan approval.")
	case errors.Is(err, ErrPendingExists):
		httpx.Fail(w, r, http.StatusConflict, httpx.CodeConflict, "Masih ada permintaan approval yang sedang menunggu review untuk migration ini.")
	case errors.Is(err, ErrNoApproverGroups):
		httpx.Fail(w, r, http.StatusConflict, httpx.CodeConflict, "Database ini belum memiliki group dengan grant akses.")
	case err != nil:
		httpx.ServerError(w, r, err)
	default:
		obs.Info(r.Context(), "approval.submitted", "permintaan approval diajukan",
			obs.KeyMigrationID, req.MigrationID.String(),
			obs.KeyDatabase, req.DatabaseName,
			"approval_id", req.ID.String(),
		)
		httpx.Created(w, "/api/approvals/"+req.ID.String(), toApprovalJSON(*req))
	}
}

func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	if u == nil {
		httpx.Fail(w, r, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Silakan login dulu.")
		return
	}
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.Invalid(w, r, "ID approval tidak valid.")
		return
	}

	var in struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 {
		if !httpx.Decode(w, r, &in) {
			return
		}
	}

	req, err := h.svc.Approve(r.Context(), u.ID, id, in.Note)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, r, "Permintaan approval tidak ditemukan.")
	case errors.Is(err, ErrNotLeader), errors.Is(err, ErrForbidden):
		httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Hanya Leader dari group yang memiliki akses ke database ini yang boleh menyetujui.")
	case errors.Is(err, ErrAlreadyResolved), errors.Is(err, ErrAlreadyDecided):
		httpx.Fail(w, r, http.StatusConflict, httpx.CodeConflict, err.Error())
	case err != nil:
		httpx.ServerError(w, r, err)
	default:
		obs.Info(r.Context(), "approval.approved", "leader memberikan persetujuan",
			obs.KeyMigrationID, req.MigrationID.String(),
			obs.KeyDatabase, req.DatabaseName,
			obs.KeyStatus, string(req.Status),
			"approval_id", req.ID.String(),
			"approved_count", req.ApprovedCount,
			"total_required", req.TotalRequired,
		)
		httpx.JSON(w, http.StatusOK, toApprovalJSON(*req))
	}
}

func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	if u == nil {
		httpx.Fail(w, r, http.StatusUnauthorized, httpx.CodeUnauthenticated, "Silakan login dulu.")
		return
	}
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.Invalid(w, r, "ID approval tidak valid.")
		return
	}

	var in struct {
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = strings.TrimSpace(in.Note)
	}
	if reason == "" {
		httpx.Invalid(w, r, "Alasan penolakan wajib diisi.", httpx.FieldError{Field: "reason", Detail: "wajib diisi"})
		return
	}

	req, err := h.svc.Reject(r.Context(), u.ID, id, reason)
	switch {
	case errors.Is(err, ErrEmptyReason):
		httpx.Invalid(w, r, "Alasan penolakan wajib diisi.", httpx.FieldError{Field: "reason", Detail: "wajib diisi"})
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, r, "Permintaan approval tidak ditemukan.")
	case errors.Is(err, ErrNotLeader), errors.Is(err, ErrForbidden):
		httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Hanya Leader dari group yang memiliki akses ke database ini yang boleh menolak.")
	case errors.Is(err, ErrAlreadyResolved):
		httpx.Fail(w, r, http.StatusConflict, httpx.CodeConflict, err.Error())
	case err != nil:
		httpx.ServerError(w, r, err)
	default:
		obs.Info(r.Context(), "approval.rejected", "leader menolak permintaan approval",
			obs.KeyMigrationID, req.MigrationID.String(),
			obs.KeyDatabase, req.DatabaseName,
			obs.KeyStatus, string(req.Status),
			"approval_id", req.ID.String(),
		)
		httpx.JSON(w, http.StatusOK, toApprovalJSON(*req))
	}
}

