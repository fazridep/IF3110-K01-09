package dbmgmt

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/httpx"
)

// API /api/databases: daftar database yang boleh dibuka user, schema, tabel, dan
// isi tabel. Host dan username koneksi sengaja nggak ikut, itu urusan admin.

type databaseJSON struct {
	ID       uuid.UUID    `json:"id"`
	Name     string       `json:"name"`
	Database string       `json:"database"`
	Level    access.Level `json:"level"`
}

type tableJSON struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Columns int    `json:"columns"`
}

type schemaJSON struct {
	Name   string      `json:"name"`
	Tables []tableJSON `json:"tables"`
}

type databaseDetailJSON struct {
	databaseJSON
	Schemas []schemaJSON `json:"schemas"`
}

type columnJSON struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default"`
}

type constraintJSON struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Definition string `json:"definition"`
}

type indexJSON struct {
	Name       string `json:"name"`
	Unique     bool   `json:"unique"`
	Primary    bool   `json:"primary"`
	Definition string `json:"definition"`
}

type rowsJSON struct {
	Columns []string    `json:"columns"`
	Rows    [][]*string `json:"rows"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"perPage"`
	Pages   int         `json:"pages"`
}

type tableDetailJSON struct {
	Database    databaseJSON     `json:"database"`
	Schema      string           `json:"schema"`
	Table       string           `json:"table"`
	Columns     []columnJSON     `json:"columns"`
	Constraints []constraintJSON `json:"constraints"`
	Indexes     []indexJSON      `json:"indexes"`
	Rows        rowsJSON         `json:"rows"`
}

func toDatabaseJSON(c Connection, level access.Level) databaseJSON {
	return databaseJSON{ID: c.ID, Name: c.Name, Database: c.Database, Level: level}
}

func toRowsJSON(p RowPage) rowsJSON {
	out := rowsJSON{
		Columns: p.Columns, Rows: make([][]*string, 0, len(p.Rows)),
		Total: p.Total, Page: p.Page, PerPage: p.PerPage, Pages: p.Pages,
	}
	if out.Columns == nil {
		out.Columns = []string{}
	}
	for _, row := range p.Rows {
		cells := make([]*string, len(row))
		for i := range row {
			if !row[i].Null {
				cells[i] = &row[i].Value
			}
		}
		out.Rows = append(out.Rows, cells)
	}
	return out
}

func (h *Handler) listDatabases(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r.Context())
	conns, err := h.store.List(r.Context())
	if err != nil {
		httpx.ServerError(w, r, err)
		return
	}

	items := make([]databaseJSON, 0, len(conns))
	for _, c := range conns {
		level, err := h.perms.EffectiveLevel(r.Context(), u.ID, c.ID)
		if err != nil {
			httpx.ServerError(w, r, err)
			return
		}
		if level != access.NoAccess {
			items = append(items, toDatabaseJSON(c, level))
		}
	}
	httpx.JSON(w, http.StatusOK, httpx.NewList(items))
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

	out := databaseDetailJSON{databaseJSON: toDatabaseJSON(c, level), Schemas: make([]schemaJSON, 0, len(schemas))}
	for _, s := range schemas {
		tables, err := Tables(r.Context(), pool, s.Name)
		if err != nil {
			h.connectError(w, r, c, err)
			return
		}
		js := schemaJSON{Name: s.Name, Tables: make([]tableJSON, 0, len(tables))}
		for _, t := range tables {
			js.Tables = append(js.Tables, tableJSON(t))
		}
		out.Schemas = append(out.Schemas, js)
	}
	httpx.JSON(w, http.StatusOK, out)
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
	rows, err := FetchRows(r.Context(), pool, schema, table, intParam(r, "page", 1), intParam(r, "perPage", DefaultPerPage))
	if err != nil {
		h.connectError(w, r, c, err)
		return
	}

	out := tableDetailJSON{
		Database:    toDatabaseJSON(c, level),
		Schema:      detail.Schema,
		Table:       detail.Table,
		Columns:     make([]columnJSON, 0, len(detail.Columns)),
		Constraints: make([]constraintJSON, 0, len(detail.Constraints)),
		Indexes:     make([]indexJSON, 0, len(detail.Indexes)),
		Rows:        toRowsJSON(rows),
	}
	for _, col := range detail.Columns {
		out.Columns = append(out.Columns, columnJSON(col))
	}
	for _, con := range detail.Constraints {
		out.Constraints = append(out.Constraints, constraintJSON(con))
	}
	for _, idx := range detail.Indexes {
		out.Indexes = append(out.Indexes, indexJSON(idx))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// connectionWithAccess ngambil koneksi sekalian ngecek user punya akses. Nggak
// punya akses dibalas 404 biar keberadaan database nggak bocor. Hasil ok=false
// berarti response sudah dikirim.
func (h *Handler) connectionWithAccess(w http.ResponseWriter, r *http.Request, id uuid.UUID) (Connection, access.Level, bool) {
	u := auth.CurrentUser(r.Context())
	level, err := h.perms.EffectiveLevel(r.Context(), u.ID, id)
	if err != nil {
		httpx.ServerError(w, r, err)
		return Connection{}, access.NoAccess, false
	}
	if level == access.NoAccess {
		httpx.NotFound(w, r, "Datanya nggak ketemu.")
		return Connection{}, access.NoAccess, false
	}
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.dataError(w, r, err)
		return Connection{}, access.NoAccess, false
	}
	return c, level, true
}
