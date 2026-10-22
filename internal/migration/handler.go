package migration

import (
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, name string, data any)
}

type Handler struct {
	svc  *Service
	view Renderer
}

func NewHandler(svc *Service, view Renderer) *Handler {
	return &Handler{svc: svc, view: view}
}

// RegisterRoutes didaftarkan ke mux yang sudah dibungkus auth.RequireLogin.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /databases/{dbID}/migrations", h.list)
	mux.HandleFunc("GET /databases/{dbID}/migrations/new", h.newForm)
	mux.HandleFunc("POST /databases/{dbID}/migrations", h.create)
	mux.HandleFunc("GET /migrations/{id}", h.detail)
	mux.HandleFunc("POST /migrations/{id}", h.update)
	mux.HandleFunc("POST /migrations/{id}/execute", h.execute)
}

type listPage struct {
	DatabaseID uuid.UUID
	Migrations []Migration
	CanEdit    bool
}

type formPage struct {
	DatabaseID  uuid.UUID
	ID          uuid.UUID
	Description string
	SQL         string
	Error       string
}

type detailPage struct {
	Migration *Migration
	CanEdit   bool
	Form      formPage
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	dbID, ok := pathID(w, r, "dbID")
	if !ok {
		return
	}
	userID := auth.CurrentUser(r.Context()).ID
	migrations, err := h.svc.List(r.Context(), userID, dbID)
	if err != nil {
		h.fail(w, err)
		return
	}
	canEdit, err := h.svc.CanEdit(r.Context(), userID, dbID)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.view.Render(w, r, "migration/list", listPage{DatabaseID: dbID, Migrations: migrations, CanEdit: canEdit})
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	dbID, ok := pathID(w, r, "dbID")
	if !ok {
		return
	}
	canEdit, err := h.svc.CanEdit(r.Context(), auth.CurrentUser(r.Context()).ID, dbID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if !canEdit {
		h.fail(w, ErrForbidden)
		return
	}
	h.view.Render(w, r, "migration/form", formPage{DatabaseID: dbID})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	dbID, ok := pathID(w, r, "dbID")
	if !ok {
		return
	}
	form := formPage{DatabaseID: dbID, Description: r.FormValue("description"), SQL: r.FormValue("sql")}
	m, err := h.svc.Create(r.Context(), auth.CurrentUser(r.Context()).ID, dbID, form.Description, form.SQL)
	if isInputError(err) {
		form.Error = err.Error()
		h.view.Render(w, r, "migration/form", form)
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/migrations/"+m.ID.String(), http.StatusSeeOther)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	h.showDetail(w, r, "")
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	_, err := h.svc.Update(r.Context(), auth.CurrentUser(r.Context()).ID, id, r.FormValue("description"), r.FormValue("sql"))
	if isInputError(err) {
		h.showDetail(w, r, err.Error())
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/migrations/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	_, err := h.svc.Execute(r.Context(), auth.CurrentUser(r.Context()).ID, id)
	if isInputError(err) {
		h.showDetail(w, r, err.Error())
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/migrations/"+id.String(), http.StatusSeeOther)
}

// showDetail menampilkan detail migration. Kalau formError terisi, form edit
// diisi ulang dengan input user beserta pesan error-nya.
func (h *Handler) showDetail(w http.ResponseWriter, r *http.Request, formError string) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	userID := auth.CurrentUser(r.Context()).ID
	m, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	canEdit, err := h.svc.CanEdit(r.Context(), userID, m.DatabaseID)
	if err != nil {
		h.fail(w, err)
		return
	}

	form := formPage{DatabaseID: m.DatabaseID, ID: m.ID, Description: m.Description, SQL: m.SQL}
	if formError != "" && r.Method == http.MethodPost && r.FormValue("sql") != "" {
		form.Description, form.SQL = r.FormValue("description"), r.FormValue("sql")
	}
	form.Error = formError
	h.view.Render(w, r, "migration/detail", detailPage{Migration: m, CanEdit: canEdit, Form: form})
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		http.Error(w, "Kamu nggak punya akses untuk aksi ini.", http.StatusForbidden)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "Migration nggak ditemukan.", http.StatusNotFound)
	default:
		log.Printf("migration: %v", err)
		http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
	}
}

func isInputError(err error) bool {
	return errors.Is(err, ErrEmptySQL) || errors.Is(err, ErrEmptyDescription) ||
		errors.Is(err, ErrNotTransactional) || errors.Is(err, ErrPendingExists) || errors.Is(err, ErrNotDraft)
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}
