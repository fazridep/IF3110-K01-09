package dbmgmt

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/auth"
)

// Halaman /databases: daftar database yang boleh dibuka user, schema, tabel,
// dan isi tabel. Izin dicek per database lewat h.perms.

type listItem struct {
	Connection Connection
	Level      access.Level
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
	Level      access.Level
	Schemas    []schemaTables
}

type tablePage struct {
	Connection Connection
	Level      access.Level
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
		level, err := h.perms.EffectiveLevel(r.Context(), u.ID, c.ID)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if level == access.NoAccess {
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
// Hasil ok=false berarti response sudah dikirim (403 atau 404).
func (h *Handler) connectionWithAccess(w http.ResponseWriter, r *http.Request, id uuid.UUID) (Connection, access.Level, bool) {
	u := auth.CurrentUser(r.Context())
	level, err := h.perms.EffectiveLevel(r.Context(), u.ID, id)
	if err != nil {
		h.serverError(w, r, err)
		return Connection{}, access.NoAccess, false
	}
	if level == access.NoAccess {
		h.fail(w, r, http.StatusForbidden, "Kamu nggak punya akses ke database ini.")
		return Connection{}, access.NoAccess, false
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return Connection{}, access.NoAccess, false
	}
	return c, level, true
}
