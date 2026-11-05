package dbmgmt

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/httpx"
)

type Permissions interface {
	EffectiveLevel(ctx context.Context, userID, dbID uuid.UUID) (access.Level, error)
}

type Handler struct {
	store *Store
	pools *Manager
	view  httpx.Renderer
	perms Permissions
}

func NewHandler(store *Store, pools *Manager, view httpx.Renderer, perms Permissions) *Handler {
	return &Handler{store: store, pools: pools, view: view, perms: perms}
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

	guard := access.RequirePlatformAdmin(admin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, r, http.StatusForbidden, "Halaman ini cuma buat Platform Admin.")
	}))
	mux.Handle("/admin/databases", guard)
	mux.Handle("/admin/databases/", guard)
}

// --- util -----------------------------------------------------------------

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		h.fail(w, r, http.StatusNotFound, "Alamatnya nggak valid.")
	}
	return id, ok
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
	if access.IsPlatformAdmin(r.Context()) {
		msg = ce.Error()
	}
	h.fail(w, r, http.StatusBadGateway, msg)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("dbmgmt: %v", err)
	h.fail(w, r, http.StatusInternalServerError, "Terjadi kesalahan di server.")
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	httpx.Error(w, r, h.view, status, httpx.ErrorPage{
		Message:   msg,
		BackURL:   "/databases",
		BackLabel: "Kembali ke daftar database",
	})
}

func intParam(r *http.Request, key string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return n
}
