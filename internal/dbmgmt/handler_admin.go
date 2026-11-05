package dbmgmt

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/google/uuid"
)

type adminListPage struct {
	Connections []Connection
	Notice      string
}

type adminDetailPage struct {
	Connection  Connection
	Notice      string
	TestStatus  string
	TestMessage string
}

type adminFormPage struct {
	IsEdit      bool
	ID          string
	Input       Input
	Error       string
	TestStatus  string
	TestMessage string
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	conns, err := h.store.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.view.Render(w, r, "dbmgmt/admin_list", adminListPage{
		Connections: conns,
		Notice:      noticeFrom(r),
	})
}

func (h *Handler) adminNew(w http.ResponseWriter, r *http.Request) {
	h.view.Render(w, r, "dbmgmt/admin_form", adminFormPage{
		Input: Input{Port: 5432, SSLMode: "prefer"},
	})
}

func (h *Handler) adminCreate(w http.ResponseWriter, r *http.Request) {
	in := inputFromForm(r)
	id, err := h.store.Create(r.Context(), in)
	if err != nil {
		h.renderFormError(w, r, false, "", in, err)
		return
	}
	http.Redirect(w, r, "/admin/databases/"+id.String()+"?notice=dibuat", http.StatusSeeOther)
}

func (h *Handler) adminDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	h.view.Render(w, r, "dbmgmt/admin_detail", adminDetailPage{Connection: c, Notice: noticeFrom(r)})
}

func (h *Handler) adminEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	h.view.Render(w, r, "dbmgmt/admin_form", adminFormPage{
		IsEdit: true,
		ID:     c.ID.String(),
		Input: Input{
			Name: c.Name, Host: c.Host, Port: c.Port,
			Database: c.Database, Username: c.Username, SSLMode: c.SSLMode,
		},
	})
}

func (h *Handler) adminUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	in := inputFromForm(r)
	if err := h.store.Update(r.Context(), id, in); err != nil {
		h.renderFormError(w, r, true, id.String(), in, err)
		return
	}
	h.pools.Invalidate(id)
	http.Redirect(w, r, "/admin/databases/"+id.String()+"?notice=disimpan", http.StatusSeeOther)
}

func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		h.dataError(w, r, err)
		return
	}
	h.pools.Invalidate(id)
	http.Redirect(w, r, "/admin/databases?notice=dihapus", http.StatusSeeOther)
}

func (h *Handler) adminTestSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	page := adminDetailPage{Connection: c, TestStatus: "ok", TestMessage: "Koneksi berhasil."}
	pool, err := h.pools.Pool(r.Context(), id)
	if err == nil {
		err = pool.Ping(r.Context())
	}
	if err != nil {
		log.Printf("dbmgmt: tes koneksi %s: %v", id, err)
		page.TestStatus = "gagal"
		page.TestMessage = asConnectError(c, err).Error()
	}
	h.view.Render(w, r, "dbmgmt/admin_detail", page)
}

func (h *Handler) adminTestInput(w http.ResponseWriter, r *http.Request) {
	in := inputFromForm(r)
	isEdit := r.FormValue("is_edit") == "1"
	id := r.FormValue("id")

	page := adminFormPage{IsEdit: isEdit, ID: id, Input: in, TestStatus: "ok", TestMessage: "Koneksi berhasil."}
	err := h.testInput(r.Context(), in, isEdit, id)
	if err != nil {
		page.TestStatus = "gagal"
		page.TestMessage = err.Error()
	}
	page.Input.Password = ""
	h.view.Render(w, r, "dbmgmt/admin_form", page)
}

func (h *Handler) testInput(ctx context.Context, in Input, isEdit bool, rawID string) error {
	if isEdit && in.Password == "" {
		id, err := uuid.Parse(rawID)
		if err != nil {
			return ErrNotFound
		}
		_, password, err := h.store.credentials(ctx, id)
		if err != nil {
			return err
		}
		in.Password = password
	}
	return h.pools.Test(ctx, in)
}

func (h *Handler) renderFormError(w http.ResponseWriter, r *http.Request, isEdit bool, id string, in Input, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, ErrDuplicateName):
		status = http.StatusConflict
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	}
	page := adminFormPage{IsEdit: isEdit, ID: id, Input: in, Error: err.Error()}
	h.view.RenderStatus(w, r, status, "dbmgmt/admin_form", page)
}

func inputFromForm(r *http.Request) Input {
	port, _ := strconv.Atoi(r.FormValue("port"))
	return Input{
		Name:     r.FormValue("name"),
		Host:     r.FormValue("host"),
		Port:     port,
		Database: r.FormValue("db_name"),
		Username: r.FormValue("username"),
		Password: r.FormValue("password"),
		SSLMode:  r.FormValue("sslmode"),
	}
}

var notices = map[string]string{
	"dibuat":   "Koneksi dibuat.",
	"disimpan": "Koneksi disimpan.",
	"dihapus":  "Koneksi dihapus.",
}

func noticeFrom(r *http.Request) string {
	return notices[r.URL.Query().Get("notice")]
}
