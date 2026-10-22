package dbmgmt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrTableNotFound = errors.New("dbmgmt: tabel tidak ditemukan")

const (
	DefaultPerPage = 50
	MaxPerPage     = 200
)

// Schema adalah satu schema di database target beserta jumlah tabelnya.
type Schema struct {
	Name   string
	Tables int
}

// Table satu tabel atau view. Kind: table, view, materialized view,
// partitioned table, atau foreign table.
type Table struct {
	Name    string
	Kind    string
	Columns int
}

type Column struct {
	Name     string
	DataType string
	Nullable bool
	Default  string
}

type Constraint struct {
	Name       string
	Kind       string
	Definition string
}

type Index struct {
	Name       string
	Unique     bool
	Primary    bool
	Definition string
}

type TableDetail struct {
	Schema      string
	Table       string
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}

// Cell satu nilai di viewer. Nilai NULL dibedain biar di UI bisa ditulis miring.
type Cell struct {
	Value string
	Null  bool
}

type RowPage struct {
	Columns []string
	Rows    [][]Cell
	Total   int64
	Page    int
	PerPage int
	Pages   int
}

// Schemas mendaftar schema user di database target, tanpa schema sistem.
func Schemas(ctx context.Context, db *pgxpool.Pool) ([]Schema, error) {
	rows, err := db.Query(ctx, `
		SELECT n.nspname, count(c.oid)
		FROM pg_namespace n
		LEFT JOIN pg_class c
		       ON c.relnamespace = n.oid AND c.relkind IN ('r','v','m','p','f')
		WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
		GROUP BY n.nspname
		ORDER BY n.nspname`)
	if err != nil {
		return nil, fmt.Errorf("dbmgmt: daftar schema: %w", err)
	}
	defer rows.Close()

	var out []Schema
	for rows.Next() {
		var s Schema
		if err := rows.Scan(&s.Name, &s.Tables); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Tables mendaftar tabel dan view di satu schema.
func Tables(ctx context.Context, db *pgxpool.Pool, schema string) ([]Table, error) {
	rows, err := db.Query(ctx, `
		SELECT c.relname, c.relkind::text, count(a.attnum)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attribute a
		       ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		WHERE n.nspname = $1 AND c.relkind IN ('r','v','m','p','f')
		GROUP BY c.relname, c.relkind
		ORDER BY c.relname`, schema)
	if err != nil {
		return nil, fmt.Errorf("dbmgmt: daftar tabel schema %s: %w", schema, err)
	}
	defer rows.Close()

	var out []Table
	for rows.Next() {
		var t Table
		var kind string
		if err := rows.Scan(&t.Name, &kind, &t.Columns); err != nil {
			return nil, err
		}
		t.Kind = tableKind(kind)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TableDetail ngambil kolom, constraint, dan index dari satu tabel.
func InspectTable(ctx context.Context, db *pgxpool.Pool, schema, table string) (TableDetail, error) {
	ok, err := tableExists(ctx, db, schema, table)
	if err != nil {
		return TableDetail{}, err
	}
	if !ok {
		return TableDetail{}, ErrTableNotFound
	}
	detail := TableDetail{Schema: schema, Table: table}

	cols, err := db.Query(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull, COALESCE(pg_get_expr(d.adbin, d.adrelid), '')
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = to_regclass($1) AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, qualified(schema, table))
	if err != nil {
		return TableDetail{}, fmt.Errorf("dbmgmt: kolom %s.%s: %w", schema, table, err)
	}
	defer cols.Close()
	for cols.Next() {
		var c Column
		if err := cols.Scan(&c.Name, &c.DataType, &c.Nullable, &c.Default); err != nil {
			return TableDetail{}, err
		}
		detail.Columns = append(detail.Columns, c)
	}
	if err := cols.Err(); err != nil {
		return TableDetail{}, err
	}

	cons, err := db.Query(ctx, `
		SELECT conname, contype::text, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = to_regclass($1)
		ORDER BY conname`, qualified(schema, table))
	if err != nil {
		return TableDetail{}, fmt.Errorf("dbmgmt: constraint %s.%s: %w", schema, table, err)
	}
	defer cons.Close()
	for cons.Next() {
		var c Constraint
		var kind string
		if err := cons.Scan(&c.Name, &kind, &c.Definition); err != nil {
			return TableDetail{}, err
		}
		c.Kind = constraintKind(kind)
		detail.Constraints = append(detail.Constraints, c)
	}
	if err := cons.Err(); err != nil {
		return TableDetail{}, err
	}

	idxs, err := db.Query(ctx, `
		SELECT ic.relname, i.indisunique, i.indisprimary, pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		WHERE i.indrelid = to_regclass($1)
		ORDER BY ic.relname`, qualified(schema, table))
	if err != nil {
		return TableDetail{}, fmt.Errorf("dbmgmt: index %s.%s: %w", schema, table, err)
	}
	defer idxs.Close()
	for idxs.Next() {
		var i Index
		if err := idxs.Scan(&i.Name, &i.Unique, &i.Primary, &i.Definition); err != nil {
			return TableDetail{}, err
		}
		detail.Indexes = append(detail.Indexes, i)
	}
	return detail, idxs.Err()
}

// FetchRows ngambil isi tabel dengan LIMIT/OFFSET. Schema dan nama tabel
// di-quote lewat pgx.Identifier, jadi nggak bisa dipakai buat SQL injection.
func FetchRows(ctx context.Context, db *pgxpool.Pool, schema, table string, page, perPage int) (RowPage, error) {
	ok, err := tableExists(ctx, db, schema, table)
	if err != nil {
		return RowPage{}, err
	}
	if !ok {
		return RowPage{}, ErrTableNotFound
	}
	if perPage <= 0 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	if page < 1 {
		page = 1
	}

	ident := qualified(schema, table)
	var total int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM `+ident).Scan(&total); err != nil {
		return RowPage{}, fmt.Errorf("dbmgmt: hitung %s.%s: %w", schema, table, err)
	}

	pages := 0
	if total > 0 {
		pages = int((total + int64(perPage) - 1) / int64(perPage))
		if page > pages {
			page = pages
		}
	}

	rows, err := db.Query(ctx, `SELECT * FROM `+ident+` LIMIT $1 OFFSET $2`, perPage, (page-1)*perPage)
	if err != nil {
		return RowPage{}, fmt.Errorf("dbmgmt: baca %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	out := RowPage{Total: total, Page: page, PerPage: perPage, Pages: pages}
	for _, f := range rows.FieldDescriptions() {
		out.Columns = append(out.Columns, f.Name)
	}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return RowPage{}, err
		}
		cells := make([]Cell, len(values))
		for i, v := range values {
			cells[i] = cellValue(v)
		}
		out.Rows = append(out.Rows, cells)
	}
	return out, rows.Err()
}

func cellValue(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{Null: true}
	case []byte:
		return Cell{Value: string(x)}
	case time.Time:
		return Cell{Value: x.Format("2006-01-02 15:04:05")}
	default:
		return Cell{Value: fmt.Sprint(x)}
	}
}

func tableExists(ctx context.Context, db *pgxpool.Pool, schema, table string) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2
			  AND c.relkind IN ('r','v','m','p','f')
		)`, schema, table).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("dbmgmt: cek tabel %s.%s: %w", schema, table, err)
	}
	return ok, nil
}

func qualified(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

func tableKind(kind string) string {
	switch kind {
	case "r":
		return "table"
	case "v":
		return "view"
	case "m":
		return "materialized view"
	case "p":
		return "partitioned table"
	case "f":
		return "foreign table"
	}
	return kind
}

func constraintKind(kind string) string {
	switch kind {
	case "p":
		return "primary key"
	case "f":
		return "foreign key"
	case "u":
		return "unique"
	case "c":
		return "check"
	case "x":
		return "exclusion"
	case "t":
		return "trigger"
	}
	return strings.TrimSpace(kind)
}
