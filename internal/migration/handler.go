package migration

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

// Handler API JSON migration. Eksekusi nggak ada di sini: semua lewat approval
// (POST /api/approvals) lalu queue (POST /api/jobs).
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/databases/{dbId}/migrations", h.list)
	mux.HandleFunc("POST /api/databases/{dbId}/migrations", h.create)
	mux.HandleFunc("GET /api/databases/{dbId}/history", h.history)
	mux.HandleFunc("GET /api/migrations/{id}", h.show)
	mux.HandleFunc("PUT /api/migrations/{id}", h.update)
}

type batchJSON struct {
	Table  string `json:"table"`
	Key    string `json:"key"`
	Size   int    `json:"size"`
	Backup bool   `json:"backup"`
}

type definitionJSON struct {
	Description        string     `json:"description"`
	UpSQL              string     `json:"upSql"`
	DownSQL            string     `json:"downSql"`
	Irreversible       bool       `json:"irreversible"`
	IrreversibleReason string     `json:"irreversibleReason"`
	Mode               Mode       `json:"mode"`
	Batch              *batchJSON `json:"batch"`
}

func (in definitionJSON) definition() Definition {
	d := Definition{
		Description:        in.Description,
		UpSQL:              in.UpSQL,
		DownSQL:            in.DownSQL,
		Irreversible:       in.Irreversible,
		IrreversibleReason: in.IrreversibleReason,
		Mode:               in.Mode,
	}
	if in.Batch != nil {
		d.Batch = Batch{Table: in.Batch.Table, Key: in.Batch.Key, Size: in.Batch.Size, Backup: in.Batch.Backup}
	}
	return d
}

type migrationJSON struct {
	ID         uuid.UUID `json:"id"`
	DatabaseID uuid.UUID `json:"databaseId"`
	Version    int       `json:"version"`
	definitionJSON
	Status        Status     `json:"status"`
	Error         string     `json:"error"`
	CreatedBy     string     `json:"createdBy"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	ExecutedBy    string     `json:"executedBy"`
	StartedAt     *time.Time `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	UpExecutionID *uuid.UUID `json:"upExecutionId"`
}

type migrationDetailJSON struct {
	migrationJSON
	CanEdit bool `json:"canEdit"`
	// AllowedActions aksi yang saat ini boleh diajukan approval-nya.
	AllowedActions []string `json:"allowedActions"`
	// BlockedReason alasan draft ini belum bisa diajukan apply.
	BlockedReason string `json:"blockedReason"`
}

func toMigrationJSON(m *Migration) migrationJSON {
	out := migrationJSON{
		ID:         m.ID,
		DatabaseID: m.DatabaseID,
		Version:    m.Version,
		definitionJSON: definitionJSON{
			Description:        m.Description,
			UpSQL:              m.UpSQL,
			DownSQL:            m.DownSQL,
			Irreversible:       m.Irreversible,
			IrreversibleReason: m.IrreversibleReason,
			Mode:               m.Mode,
		},
		Status:        m.Status,
		Error:         m.Error,
		CreatedBy:     m.CreatedBy,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		ExecutedBy:    m.ExecutedBy,
		StartedAt:     m.StartedAt,
		FinishedAt:    m.FinishedAt,
		UpExecutionID: m.UpExecutionID,
	}
	if m.Mode == ModeBatch {
		out.Batch = &batchJSON{Table: m.Batch.Table, Key: m.Batch.Key, Size: m.Batch.Size, Backup: m.Batch.Backup}
	}
	return out
}

type executionJSON struct {
	ID                 uuid.UUID  `json:"id"`
	MigrationID        uuid.UUID  `json:"migrationId"`
	Version            int        `json:"version"`
	Description        string     `json:"description"`
	Action             string     `json:"action"`
	RevertsExecutionID *uuid.UUID `json:"revertsExecutionId"`
	JobID              uuid.UUID  `json:"jobId"`
	Status             ExecStatus `json:"status"`
	SQL                string     `json:"sql"`
	Mode               Mode       `json:"mode"`
	BatchSize          int        `json:"batchSize"`
	BatchesCommitted   int64      `json:"batchesCommitted"`
	RowsCommitted      int64      `json:"rowsCommitted"`
	Cursor             string     `json:"cursor"`
	Error              string     `json:"error"`
	ExecutedBy         string     `json:"executedBy"`
	StartedAt          time.Time  `json:"startedAt"`
	FinishedAt         *time.Time `json:"finishedAt"`
	Committed          bool       `json:"committed"`
	CommittedAt        *time.Time `json:"committedAt"`
}

func toExecutionJSON(e Execution) executionJSON {
	return executionJSON{
		ID: e.ID, MigrationID: e.MigrationID, Version: e.Version, Description: e.Description,
		Action: e.Action, RevertsExecutionID: e.RevertsExecutionID, JobID: e.JobID, Status: e.Status,
		SQL: e.SQL, Mode: e.Mode, BatchSize: e.BatchSize, BatchesCommitted: e.Batches,
		RowsCommitted: e.Rows, Cursor: e.Cursor, Error: e.Error, ExecutedBy: e.ExecutedBy,
		StartedAt: e.StartedAt, FinishedAt: e.FinishedAt, Committed: e.Committed, CommittedAt: e.CommittedAt,
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	dbID, ok := httpx.PathUUID(r, "dbId")
	if !ok {
		httpx.NotFound(w, r, "Database tidak ditemukan.")
		return
	}
	list, err := h.svc.List(r.Context(), auth.CurrentUser(r.Context()).ID, dbID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]migrationJSON, 0, len(list))
	for i := range list {
		items = append(items, toMigrationJSON(&list[i]))
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(items))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	dbID, ok := httpx.PathUUID(r, "dbId")
	if !ok {
		httpx.NotFound(w, r, "Database tidak ditemukan.")
		return
	}
	var in definitionJSON
	if !httpx.Decode(w, r, &in) {
		return
	}
	m, err := h.svc.Create(r.Context(), auth.CurrentUser(r.Context()).ID, dbID, in.definition())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	obs.Info(r.Context(), "migration.created", "draft migration dibuat",
		obs.KeyMigrationID, m.ID.String(), "database_id", dbID.String(), "migration_version", m.Version)
	httpx.Created(w, "/api/migrations/"+m.ID.String(), toMigrationJSON(m))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.NotFound(w, r, "Migration tidak ditemukan.")
		return
	}
	userID := auth.CurrentUser(r.Context()).ID
	m, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	canEdit, err := h.svc.CanEdit(r.Context(), userID, m.DatabaseID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	actions, blocked, err := h.svc.AllowedActions(r.Context(), m)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, migrationDetailJSON{migrationJSON: toMigrationJSON(m), CanEdit: canEdit, AllowedActions: actions, BlockedReason: blocked})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.NotFound(w, r, "Migration tidak ditemukan.")
		return
	}
	var in definitionJSON
	if !httpx.Decode(w, r, &in) {
		return
	}
	m, err := h.svc.Update(r.Context(), auth.CurrentUser(r.Context()).ID, id, in.definition())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	obs.Info(r.Context(), "migration.updated", "draft migration diubah", obs.KeyMigrationID, m.ID.String())
	httpx.JSON(w, http.StatusOK, toMigrationJSON(m))
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	dbID, ok := httpx.PathUUID(r, "dbId")
	if !ok {
		httpx.NotFound(w, r, "Database tidak ditemukan.")
		return
	}
	q := r.URL.Query()
	f := HistoryFilter{HideRollback: q.Get("hideRollback") == "true" || q.Get("hideRollback") == "1"}
	if raw := strings.TrimSpace(q.Get("migrationId")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Invalid(w, r, "Parameter migrationId tidak valid.", httpx.FieldError{Field: "migrationId", Detail: "UUID tidak valid"})
			return
		}
		f.MigrationID = &id
	}
	list, err := h.svc.History(r.Context(), auth.CurrentUser(r.Context()).ID, dbID, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]executionJSON, 0, len(list))
	for _, e := range list {
		items = append(items, toExecutionJSON(e))
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(items))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var input *InputError
	switch {
	case errors.As(err, &input):
		httpx.Invalid(w, r, input.Message(), httpx.FieldError{Field: input.Field, Detail: input.Message()})
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(w, r, "Migration atau database tidak ditemukan.")
	case errors.Is(err, ErrForbidden):
		httpx.Fail(w, r, http.StatusForbidden, httpx.CodeForbidden, "Hanya user dengan akses View + Edit yang boleh membuat atau mengubah migration.")
	case errors.Is(err, ErrPendingExists):
		httpx.Fail(w, r, http.StatusConflict, "pending-exists", "Masih ada draft migration lain di database ini. Selesaikan dulu sebelum membuat yang baru.")
	case errors.Is(err, ErrNotDraft):
		httpx.Fail(w, r, http.StatusConflict, "migration-immutable", "Migration ini sudah pernah dijalankan, SQL-nya nggak bisa diubah. Buat migration baru untuk perbaikan.")
	case errors.Is(err, ErrBusy):
		httpx.Fail(w, r, http.StatusConflict, "migration-busy", "Migration ini sedang menunggu atau dijalankan worker.")
	case errors.Is(err, ErrTargetUnavailable):
		httpx.Fail(w, r, http.StatusBadGateway, httpx.CodeUnavailable, "Database target nggak bisa dihubungi untuk mengecek tabel batch.")
	default:
		httpx.ServerError(w, r, err)
	}
}
