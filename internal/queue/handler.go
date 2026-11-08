package queue

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/jobs", h.list)
	mux.HandleFunc("POST /api/jobs", h.submit)
	mux.HandleFunc("GET /api/jobs/{id}", h.show)
	mux.HandleFunc("POST /api/jobs/{id}/recheck", h.recheck)
}

type jobJSON struct {
	ID               uuid.UUID  `json:"id"`
	DatabaseID       uuid.UUID  `json:"databaseId"`
	DatabaseName     string     `json:"databaseName"`
	MigrationID      uuid.UUID  `json:"migrationId"`
	MigrationVersion int        `json:"migrationVersion"`
	Description      string     `json:"description"`
	ApprovalID       uuid.UUID  `json:"approvalId"`
	Action           string     `json:"action"`
	ResumesJobID     *uuid.UUID `json:"resumesJobId"`
	Status           Status     `json:"status"`
	Attempt          int        `json:"attempt"`
	MaxAttempts      int        `json:"maxAttempts"`
	Error            string     `json:"error"`
	BatchesCommitted int64      `json:"batchesCommitted"`
	RowsCommitted    int64      `json:"rowsCommitted"`
	CreatedBy        string     `json:"createdBy"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	RunAfter         time.Time  `json:"runAfter"`
	StartedAt        *time.Time `json:"startedAt"`
	FinishedAt       *time.Time `json:"finishedAt"`
}

func toJobJSON(j Job) jobJSON {
	return jobJSON{
		ID: j.ID, DatabaseID: j.DatabaseID, DatabaseName: j.DatabaseName,
		MigrationID: j.MigrationID, MigrationVersion: j.MigrationVersion, Description: j.Description,
		ApprovalID: j.ApprovalID, Action: j.Action, ResumesJobID: j.ResumesJobID,
		Status: j.Status, Attempt: j.Attempt, MaxAttempts: j.MaxAttempts, Error: j.Error,
		BatchesCommitted: j.BatchesDone, RowsCommitted: j.RowsDone, CreatedBy: j.CreatedByName,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, RunAfter: j.RunAfter,
		StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
	}
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ApprovalID  string `json:"approvalId"`
		ResumeJobID string `json:"resumeJobId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	approvalID, err := uuid.Parse(strings.TrimSpace(in.ApprovalID))
	if err != nil {
		httpx.Invalid(w, r, "Approval wajib dipilih.", httpx.FieldError{Field: "approvalId", Detail: "UUID tidak valid"})
		return
	}
	var resumes *uuid.UUID
	if raw := strings.TrimSpace(in.ResumeJobID); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Invalid(w, r, "Job yang dilanjutkan tidak valid.", httpx.FieldError{Field: "resumeJobId", Detail: "UUID tidak valid"})
			return
		}
		resumes = &id
	}

	job, err := h.svc.Submit(r.Context(), auth.CurrentUser(r.Context()).ID, approvalID, resumes)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.Accepted(w, httpx.JobAccepted{
		JobID:     job.ID.String(),
		Status:    string(job.Status),
		StatusURL: "/api/jobs/" + job.ID.String(),
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var database *uuid.UUID
	if raw := q.Get("databaseId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Invalid(w, r, "Parameter databaseId tidak valid.", httpx.FieldError{Field: "databaseId", Detail: "UUID tidak valid"})
			return
		}
		database = &id
	}
	var before int64
	if raw := q.Get("cursor"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			httpx.Invalid(w, r, "Parameter cursor tidak valid.", httpx.FieldError{Field: "cursor", Detail: "cursor tidak dikenal"})
			return
		}
		before = n
	}
	limit := defaultListLimit
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, maxListLimit)
	}

	jobs, next, err := h.svc.List(r.Context(), auth.CurrentUser(r.Context()).ID, database, q.Get("status"), before, limit)
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	items := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, toJobJSON(j))
	}
	out := httpx.NewList(items)
	if next > 0 {
		out.NextCursor = strconv.FormatInt(next, 10)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.NotFound(w, r, "Job tidak ditemukan.")
		return
	}
	job, err := h.svc.Get(r.Context(), auth.CurrentUser(r.Context()).ID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toJobJSON(*job))
}

func (h *Handler) recheck(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.NotFound(w, r, "Job tidak ditemukan.")
		return
	}
	job, err := h.svc.Recheck(r.Context(), auth.CurrentUser(r.Context()).ID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toJobJSON(*job))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, r, "Datanya nggak ketemu.")
	case errors.Is(err, ErrForbidden):
		httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Hanya user dengan akses View + Edit yang boleh melakukan ini.")
	case errors.Is(err, ErrNotApproved):
		httpx.Fail(w, r, http.StatusConflict, "approval-required", "Perubahan ini belum disetujui semua leader group.")
	case errors.Is(err, ErrInvalidResume):
		httpx.Fail(w, r, http.StatusConflict, "invalid-resume", "Job yang dilanjutkan harus berstatus partial, dari migration yang sama, dan approval-nya berjenis batch.")
	case errors.Is(err, ErrInvalidState):
		httpx.Fail(w, r, http.StatusConflict, httpx.CodeConflict, "Status job nggak mengizinkan aksi ini.")
	default:
		httpx.ServerError(w, r, err)
	}
}
