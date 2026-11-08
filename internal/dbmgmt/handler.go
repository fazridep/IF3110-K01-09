package dbmgmt

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

type Permissions interface {
	EffectiveLevel(ctx context.Context, userID, dbID uuid.UUID) (access.Level, error)
}

type Handler struct {
	store *Store
	pools *Manager
	perms Permissions
}

func NewHandler(store *Store, pools *Manager, perms Permissions) *Handler {
	return &Handler{store: store, pools: pools, perms: perms}
}

func (h *Handler) RegisterAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/databases", h.listDatabases)
	mux.HandleFunc("GET /api/databases/{id}", h.showDatabase)
	mux.HandleFunc("GET /api/databases/{id}/tables/{schema}/{table}", h.showTable)

	admin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, access.RequireAPIPlatformAdmin(fn))
	}
	admin("GET /api/admin/connections", h.listConnections)
	admin("POST /api/admin/connections", h.createConnection)
	admin("POST /api/admin/connections/test", h.testInput)
	admin("GET /api/admin/connections/{id}", h.showConnection)
	admin("PUT /api/admin/connections/{id}", h.updateConnection)
	admin("DELETE /api/admin/connections/{id}", h.deleteConnection)
	admin("POST /api/admin/connections/{id}/test", h.testSaved)
}

func (h *Handler) pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.NotFound(w, r, "Datanya nggak ketemu.")
	}
	return id, ok
}

func (h *Handler) dataError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrTableNotFound):
		httpx.NotFound(w, r, "Datanya nggak ketemu.")
	case errors.Is(err, ErrInUse):
		httpx.Fail(w, r, http.StatusConflict, "connection-in-use", "Koneksi ini masih punya riwayat migration, jadi nggak bisa dihapus.")
	case errors.Is(err, ErrBadCiphertext):
		httpx.Fail(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Kredensial koneksi nggak bisa dibuka. Cek CREDENTIAL_ENCRYPTION_KEY.")
	default:
		httpx.ServerError(w, r, err)
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
	obs.Warn(r.Context(), "dbmgmt.connect_failed", "database target nggak bisa dihubungi",
		obs.KeyDatabase, c.Name, "error", ce.Error())
	msg := "Database ini lagi nggak bisa dihubungi. Coba lagi nanti, atau kabari Platform Admin."
	if access.IsPlatformAdmin(r.Context()) {
		msg = ce.Error()
	}
	httpx.Fail(w, r, http.StatusBadGateway, httpx.CodeUnavailable, msg)
}

func intParam(r *http.Request, key string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return n
}
