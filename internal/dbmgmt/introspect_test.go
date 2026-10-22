package dbmgmt

import (
	"context"
	"errors"
	"testing"

	"github.com/if3110/tubes-db-platform/internal/testutil"
)

func TestIntrospection(t *testing.T) {
	ctx := context.Background()
	db := testutil.DB(t)

	var schema string
	if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}

	_, err := db.Exec(ctx, `
		CREATE TABLE orders (
			id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			note TEXT
		);
		CREATE VIEW order_codes AS SELECT code FROM orders;
		INSERT INTO orders (code, note) VALUES ('A', NULL), ('B', 'dua'), ('C', 'tiga');
	`)
	if err != nil {
		t.Fatal(err)
	}

	schemas, err := Schemas(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSchema(schemas, schema) {
		t.Fatalf("schema %q nggak muncul di %+v", schema, schemas)
	}
	for _, s := range schemas {
		if s.Name == "information_schema" || len(s.Name) > 3 && s.Name[:3] == "pg_" {
			t.Fatalf("schema sistem ikut kebawa: %q", s.Name)
		}
	}

	tables, err := Tables(ctx, db, schema)
	if err != nil {
		t.Fatal(err)
	}
	orders, ok := findTable(tables, "orders")
	if !ok {
		t.Fatalf("tabel orders nggak ketemu di %+v", tables)
	}
	if orders.Kind != "table" || orders.Columns != 3 {
		t.Fatalf("info tabel salah: %+v", orders)
	}
	if view, ok := findTable(tables, "order_codes"); !ok || view.Kind != "view" {
		t.Fatalf("view nggak kebaca: %+v", view)
	}

	detail, err := InspectTable(ctx, db, schema, "orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Columns) != 3 {
		t.Fatalf("mau 3 kolom, dapat %+v", detail.Columns)
	}
	if detail.Columns[0].Name != "id" || detail.Columns[0].Nullable {
		t.Fatalf("kolom id salah: %+v", detail.Columns[0])
	}
	if !detail.Columns[2].Nullable {
		t.Fatalf("kolom note harusnya nullable: %+v", detail.Columns[2])
	}
	if !hasConstraint(detail.Constraints, "primary key") || !hasConstraint(detail.Constraints, "unique") {
		t.Fatalf("constraint nggak lengkap: %+v", detail.Constraints)
	}
	if len(detail.Indexes) < 2 {
		t.Fatalf("index nggak kebaca: %+v", detail.Indexes)
	}
	if detail.Indexes[0].Definition == "" {
		t.Fatalf("definisi index kosong: %+v", detail.Indexes[0])
	}
	if !hasPrimaryIndex(detail.Indexes) {
		t.Fatalf("index primary nggak ada: %+v", detail.Indexes)
	}

	page1, err := FetchRows(ctx, db, schema, "orders", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 3 || page1.Pages != 2 || len(page1.Rows) != 2 {
		t.Fatalf("pagination halaman 1 salah: %+v", page1)
	}
	if len(page1.Columns) != 3 || page1.Columns[0] != "id" {
		t.Fatalf("nama kolom salah: %+v", page1.Columns)
	}

	page2, err := FetchRows(ctx, db, schema, "orders", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Rows) != 1 {
		t.Fatalf("halaman 2 harusnya 1 baris, dapat %d", len(page2.Rows))
	}

	// note baris 'A' NULL, jadi harus ada minimal satu cell null di dua halaman.
	if countNulls(page1)+countNulls(page2) != 1 {
		t.Fatalf("harusnya ada tepat 1 NULL: %+v %+v", page1.Rows, page2.Rows)
	}

	// Halaman di luar jangkauan dijepit ke halaman terakhir.
	over, err := FetchRows(ctx, db, schema, "orders", 99, 2)
	if err != nil {
		t.Fatal(err)
	}
	if over.Page != 2 || len(over.Rows) != 1 {
		t.Fatalf("halaman 99 harusnya jadi halaman 2: %+v", over)
	}

	// perPage dibatasi.
	capped, err := FetchRows(ctx, db, schema, "orders", 1, 999)
	if err != nil {
		t.Fatal(err)
	}
	if capped.PerPage != MaxPerPage {
		t.Fatalf("perPage mau %d, dapat %d", MaxPerPage, capped.PerPage)
	}
}

func TestIntrospectionNggakKetemu(t *testing.T) {
	ctx := context.Background()
	db := testutil.DB(t)
	var schema string
	if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}

	if _, err := InspectTable(ctx, db, schema, "nggak_ada"); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("mau ErrTableNotFound, dapat %v", err)
	}
	if _, err := FetchRows(ctx, db, schema, "nggak_ada", 1, 10); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("mau ErrTableNotFound, dapat %v", err)
	}

	// Nama tabel aneh harus dianggap nggak ada, bukan dijalankan sebagai SQL.
	if _, err := FetchRows(ctx, db, schema, `orders; DROP TABLE users`, 1, 10); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("nama tabel aneh harusnya ditolak, dapat %v", err)
	}
	var exists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('users') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Fatalf("tabel users kena drop: %v", err)
	}
}

func TestFetchRowsFormatTeks(t *testing.T) {
	ctx := context.Background()
	db := testutil.DB(t)
	var schema string
	if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}

	_, err := db.Exec(ctx, `
		CREATE TABLE ragam (
			n  NUMERIC(10,2),
			u  UUID,
			iv INTERVAL,
			j  JSONB,
			d  DATE,
			a  INT[]
		);
		INSERT INTO ragam VALUES
			(12.50, 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', '1 day 2 hours', '{"k":1}', '2026-10-22', '{1,2}');
	`)
	if err != nil {
		t.Fatal(err)
	}

	page, err := FetchRows(ctx, db, schema, "ragam", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"12.50", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", "1 day 02:00:00", `{"k": 1}`, "2026-10-22", "{1,2}"}
	for i, w := range want {
		if got := page.Rows[0][i].Value; got != w {
			t.Errorf("kolom %s: mau %q, dapat %q", page.Columns[i], w, got)
		}
	}
}

// UPDATE mindahin baris id 1 ke belakang, jadi tanpa ORDER BY urutannya berubah.
func TestFetchRowsUrutPrimaryKey(t *testing.T) {
	ctx := context.Background()
	db := testutil.DB(t)
	var schema string
	if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}

	_, err := db.Exec(ctx, `
		CREATE TABLE antrian (id INT PRIMARY KEY, isi TEXT);
		INSERT INTO antrian VALUES (1, 'a'), (2, 'b'), (3, 'c');
		UPDATE antrian SET isi = 'a2' WHERE id = 1;
	`)
	if err != nil {
		t.Fatal(err)
	}

	page, err := FetchRows(ctx, db, schema, "antrian", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range []string{"1", "2", "3"} {
		if got := page.Rows[i][0].Value; got != w {
			t.Fatalf("baris %d: mau id %s, dapat %s", i, w, got)
		}
	}
}

func hasSchema(list []Schema, name string) bool {
	for _, s := range list {
		if s.Name == name {
			return true
		}
	}
	return false
}

func findTable(list []Table, name string) (Table, bool) {
	for _, t := range list {
		if t.Name == name {
			return t, true
		}
	}
	return Table{}, false
}

func hasConstraint(list []Constraint, kind string) bool {
	for _, c := range list {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

func hasPrimaryIndex(list []Index) bool {
	for _, i := range list {
		if i.Primary {
			return true
		}
	}
	return false
}

func countNulls(p RowPage) int {
	n := 0
	for _, row := range p.Rows {
		for _, cell := range row {
			if cell.Null {
				n++
			}
		}
	}
	return n
}
