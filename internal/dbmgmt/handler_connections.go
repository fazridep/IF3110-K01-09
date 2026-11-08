package dbmgmt

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

// connectionJSON sengaja tanpa password, yang tersimpan nggak pernah dikirim balik.
type connectionJSON struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Database  string    `json:"database"`
	Username  string    `json:"username"`
	SSLMode   string    `json:"sslmode"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toConnectionJSON(c Connection) connectionJSON {
	return connectionJSON{c.ID, c.Name, c.Host, c.Port, c.Database, c.Username, c.SSLMode, c.CreatedAt, c.UpdatedAt}
}

type connectionBody struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
}

func (b connectionBody) input() Input {
	return Input(b)
}

type testResultJSON struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (h *Handler) listConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.store.List(r.Context())
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}
	out := make([]connectionJSON, 0, len(conns))
	for _, c := range conns {
		out = append(out, toConnectionJSON(c))
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(out))
}

func (h *Handler) createConnection(w http.ResponseWriter, r *http.Request) {
	var body connectionBody
	if !httpx.Decode(w, r, &body) {
		return
	}
	id, err := h.store.Create(r.Context(), body.input())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	obs.Info(r.Context(), "connection.created", "koneksi dibuat", obs.KeyDatabase, c.Name)
	httpx.Created(w, "/api/admin/connections/"+id.String(), toConnectionJSON(c))
}

func (h *Handler) showConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toConnectionJSON(c))
}

func (h *Handler) updateConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	var body connectionBody
	if !httpx.Decode(w, r, &body) {
		return
	}
	if err := h.store.Update(r.Context(), id, body.input()); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.pools.Invalidate(id)
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	obs.Info(r.Context(), "connection.updated", "koneksi diubah", obs.KeyDatabase, c.Name)
	httpx.JSON(w, http.StatusOK, toConnectionJSON(c))
}

func (h *Handler) deleteConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		h.dataError(w, r, err)
		return
	}
	h.pools.Invalidate(id)
	obs.Info(r.Context(), "connection.deleted", "koneksi dihapus", "connection_id", id.String())
	httpx.NoContent(w)
}

func (h *Handler) testSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := h.pathID(w, r)
	if !ok {
		return
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return
	}
	pool, err := h.pools.Pool(r.Context(), id)
	if err == nil {
		err = pool.Ping(r.Context())
	}
	if errors.Is(err, ErrBadCiphertext) {
		h.dataError(w, r, err)
		return
	}
	if err != nil {
		h.writeTestResult(w, r, asConnectError(c, err))
		return
	}
	h.writeTestResult(w, r, nil)
}

// testInput ngetes data yang masih ada di form, belum disimpan. Kalau id terisi
// dan password kosong, password yang tersimpan yang dipakai (form edit).
func (h *Handler) testInput(w http.ResponseWriter, r *http.Request) {
	var body struct {
		connectionBody
		ID string `json:"id"`
	}
	if !httpx.Decode(w, r, &body) {
		return
	}
	err := h.testBody(r.Context(), body.input(), body.ID)
	var invalid *ValidationError
	switch {
	case errors.As(err, &invalid):
		h.writeError(w, r, err)
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrBadCiphertext):
		h.dataError(w, r, err)
	default:
		h.writeTestResult(w, r, err)
	}
}

func (h *Handler) testBody(ctx context.Context, in Input, rawID string) error {
	if rawID != "" && in.Password == "" {
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

func (h *Handler) writeTestResult(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		obs.Warn(r.Context(), "connection.test_failed", "tes koneksi gagal", "error", err.Error())
		httpx.JSON(w, http.StatusOK, testResultJSON{Message: connectMessage(err)})
		return
	}
	httpx.JSON(w, http.StatusOK, testResultJSON{OK: true, Message: "Koneksi berhasil."})
}

// connectMessage cuma ngeluarin pesan yang aman. Error selain ConnectError bisa
// berisi detail internal, jadi diganti kalimat umum.
func connectMessage(err error) string {
	var ce *ConnectError
	if errors.As(err, &ce) {
		return ce.Error()
	}
	return "Koneksi gagal. Detailnya ada di log server."
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *ValidationError
	switch {
	case errors.As(err, &invalid):
		httpx.Invalid(w, r, invalid.Message, httpx.FieldError{Field: invalid.Field, Detail: invalid.Message})
	case errors.Is(err, ErrDuplicateName):
		httpx.WriteProblem(w, r, httpx.Problem{
			Type:   "urn:problem:" + httpx.CodeConflict,
			Status: http.StatusConflict,
			Detail: "Nama koneksi sudah dipakai.",
			Errors: []httpx.FieldError{{Field: "name", Detail: "sudah dipakai"}},
		})
	default:
		h.dataError(w, r, err)
	}
}
