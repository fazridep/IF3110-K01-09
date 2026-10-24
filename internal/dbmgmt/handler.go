package dbmgmt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

// Level akses yang dikenali. Nilainya sengaja sama dengan access.Level supaya
// nanti gampang disambung di main.
const (
	LevelNone = ""
	LevelView = "view_only"
	LevelEdit = "view_edit"
)

// Renderer diimplementasi package web.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, name string, data any)
}

// AdminChecker diisi dari package access. Dipisah biar dbmgmt nggak perlu tahu
// cara ngeceknya (dan biar test bisa pakai fake).
type AdminChecker func(ctx context.Context, userID uuid.UUID) (bool, error)

// LevelResolver diisi dari package access.EffectiveLevel. Hasilnya LevelNone
// kalau user nggak punya akses ke database itu.
type LevelResolver func(ctx context.Context, userID, dbID uuid.UUID) (string, error)

type Handler struct {
	store   *Store
	pools   *Manager
	view    Renderer
	isAdmin AdminChecker
	level   LevelResolver
}

func NewHandler(store *Store, pools *Manager, view Renderer, isAdmin AdminChecker, level LevelResolver) *Handler {
	return &Handler{store: store, pools: pools, view: view, isAdmin: isAdmin, level: level}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /databases", h.showDatabases)
	mux.HandleFunc("GET /databases/{id}", h.showDatabase)
	mux.HandleFunc("GET /databases/{id}/tables/{schema}/{table}", h.showTable)

	admin := http.NewServeMux()
	admin.HandleFunc("GET /admin/databases", h.adminList)
	admin.HandleFunc("GET /admin/databases/new", h.adminNew)
	admin.HandleFunc("POST /admin/databases", h.adminCreate)
	admin.HandleFunc("POST /admin/databases/test", h.adminTestInput)
	admin.HandleFunc("GET /admin/databases/{id}", h.adminDetail)
	admin.HandleFunc("GET /admin/databases/{id}/edit", h.adminEdit)
	admin.HandleFunc("POST /admin/databases/{id}", h.adminUpdate)
	admin.HandleFunc("POST /admin/databases/{id}/delete", h.adminDelete)
	admin.HandleFunc("POST /admin/databases/{id}/test", h.adminTestSaved)

	guard := h.requireAdmin(admin)
	mux.Handle("/admin/databases", guard)
	mux.Handle("/admin/databases/", guard)
}

// --- tampilan admin -------------------------------------------------------

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

// adminTestSaved ngetes koneksi yang sudah tersimpan, pakai pool yang nanti
// dipakai viewer juga.
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

// adminTestInput ngetes data yang masih ada di form, belum disimpan. Password
// kosong di form edit berarti pakai yang tersimpan.
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
	page := adminFormPage{IsEdit: isEdit, ID: id, Input: in, Error: err.Error()}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	switch {
	case errors.Is(err, ErrDuplicateName):
		w.WriteHeader(http.StatusConflict)
	case errors.Is(err, ErrNotFound):
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
	h.view.Render(w, r, "dbmgmt/admin_form", page)
}

// --- tampilan user --------------------------------------------------------

type listItem struct {
	Connection Connection
	Level      string
}

type databaseListPage struct {
	Items []listItem
}

type schemaTables struct {
	Schema Schema
	Tables []Table
}

type databasePage struct {
	Connection Connection
	Level      string
	Schemas    []schemaTables
}

type tablePage struct {
	Connection Connection
	Level      string
	Detail     TableDetail
	Rows       RowPage
	PrevPage   int
	NextPage   int
	BaseURL    string
}

func (h *Handler) showDatabases(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	conns, err := h.store.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	items := make([]listItem, 0, len(conns))
	for _, c := range conns {
		level, err := h.level(r.Context(), u.ID, c.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if level == LevelNone {
			continue
		}
		items = append(items, listItem{Connection: c, Level: level})
	}
	h.view.Render(w, r, "dbmgmt/list", databaseListPage{Items: items})
}

func (h *Handler) showDatabase(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, level, ok := h.connectionWithAccess(w, r, id)
	if !ok {
		return
	}

	pool, err := h.pools.Pool(r.Context(), id)
	if err != nil {
		h.connectError(w, r, c, err)
		return
	}
	schemas, err := Schemas(r.Context(), pool)
	if err != nil {
		h.connectError(w, r, c, err)
		return
	}

	page := databasePage{Connection: c, Level: level}
	for _, s := range schemas {
		tables, err := Tables(r.Context(), pool, s.Name)
		if err != nil {
			h.connectError(w, r, c, err)
			return
		}
		page.Schemas = append(page.Schemas, schemaTables{Schema: s, Tables: tables})
	}
	h.view.Render(w, r, "dbmgmt/database", page)
}

func (h *Handler) showTable(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, level, ok := h.connectionWithAccess(w, r, id)
	if !ok {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")

	pool, err := h.pools.Pool(r.Context(), id)
	if err != nil {
		h.connectError(w, r, c, err)
		return
	}
	detail, err := InspectTable(r.Context(), pool, schema, table)
	if err != nil {
		h.connectError(w, r, c, err)
		return
	}
	rows, err := FetchRows(r.Context(), pool, schema, table, intParam(r, "page", 1), intParam(r, "per", DefaultPerPage))
	if err != nil {
		h.connectError(w, r, c, err)
		return
	}

	page := tablePage{
		Connection: c,
		Level:      level,
		Detail:     detail,
		Rows:       rows,
		BaseURL:    fmt.Sprintf("/databases/%s/tables/%s/%s", id, url.PathEscape(schema), url.PathEscape(table)),
	}
	page.PrevPage = rows.Page - 1
	page.NextPage = rows.Page + 1
	h.view.Render(w, r, "dbmgmt/table", page)
}

// connectionWithAccess ngambil koneksi sekalian ngecek user punya akses.
// Hasil ok=false berarti response sudah dikirim (redirect, 403, atau 404).
func (h *Handler) connectionWithAccess(w http.ResponseWriter, r *http.Request, id uuid.UUID) (Connection, string, bool) {
	u := auth.CurrentUser(r.Context())
	level, err := h.level(r.Context(), u.ID, id)
	if err != nil {
		h.serverError(w, r, err)
		return Connection{}, "", false
	}
	if level == LevelNone {
		h.fail(w, r, http.StatusForbidden, "Kamu nggak punya akses ke database ini.")
		return Connection{}, "", false
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return Connection{}, "", false
	}
	return c, level, true
}

func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.CurrentUser(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ok, err := h.isAdmin(r.Context(), u.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if !ok {
			h.fail(w, r, http.StatusForbidden, "Halaman ini cuma buat Platform Admin.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- util -----------------------------------------------------------------

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, http.StatusNotFound, "Alamatnya nggak valid.")
		return uuid.Nil, false
	}
	return id, true
}

// dataError memetakan error data ke halaman yang sesuai.
func (h *Handler) dataError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrTableNotFound):
		h.fail(w, r, http.StatusNotFound, "Datanya nggak ketemu.")
	case errors.Is(err, ErrInUse):
		h.fail(w, r, http.StatusConflict, "Koneksi ini masih punya riwayat migration, jadi nggak bisa dihapus.")
	case errors.Is(err, ErrBadCiphertext):
		h.fail(w, r, http.StatusInternalServerError, "Kredensial koneksi nggak bisa dibuka. Cek CREDENTIAL_ENCRYPTION_KEY.")
	default:
		h.serverError(w, r, err)
	}
}

// connectError dipakai kalau database target nggak bisa dihubungi. Detailnya
// cuma ditunjukin ke admin.
func (h *Handler) connectError(w http.ResponseWriter, r *http.Request, c Connection, err error) {
	var ce *ConnectError
	if !errors.As(asConnectError(c, err), &ce) {
		h.dataError(w, r, err)
		return
	}
	log.Printf("dbmgmt: %v", ce)
	msg := "Database ini lagi nggak bisa dihubungi. Coba lagi nanti, atau kabari Platform Admin."
	if u := auth.CurrentUser(r.Context()); u != nil {
		if ok, err := h.isAdmin(r.Context(), u.ID); err == nil && ok {
			msg = ce.Error()
		}
	}
	h.fail(w, r, http.StatusBadGateway, msg)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("dbmgmt: %v", err)
	h.fail(w, r, http.StatusInternalServerError, "Terjadi kesalahan di server.")
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	h.view.Render(w, r, "dbmgmt/error", struct{ Message string }{msg})
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

func intParam(r *http.Request, key string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return n
}

// Teks notice nggak diambil langsung dari URL biar nggak bisa dipalsukan.
var notices = map[string]string{
	"dibuat":   "Koneksi dibuat.",
	"disimpan": "Koneksi disimpan.",
	"dihapus":  "Koneksi dihapus.",
}

func noticeFrom(r *http.Request) string {
	return notices[r.URL.Query().Get("notice")]
}
