package migration

import (
	"errors"
	"testing"
)

func TestCheckSQLAllowed(t *testing.T) {
	cases := map[string]string{
		"create table":       "CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT);",
		"beberapa statement": "ALTER TABLE a ADD COLUMN b INT; CREATE INDEX idx_a_b ON a (b);",
		"index biasa":        "create index idx_x on t (x)",
		"kata di string":     "INSERT INTO logs (msg) VALUES ('jangan VACUUM; COMMIT dulu');",
		"kata di komentar":   "-- CREATE INDEX CONCURRENTLY nanti\nCREATE INDEX idx ON t (x);",
		"komentar blok":      "/* BEGIN; */ UPDATE t SET x = 1;",
		"identifier":         `CREATE TABLE "commit" (id INT);`,
		"function":           "CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; END; $$ LANGUAGE plpgsql;",
		"tag dollar":         "DO $body$ BEGIN COMMIT; END $body$;",
		"refresh view":       "REFRESH MATERIALIZED VIEW CONCURRENTLY mv;",
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			if err := CheckSQL(sql); err != nil {
				t.Fatalf("seharusnya lolos: %v", err)
			}
		})
	}
}

func TestCheckSQLRejected(t *testing.T) {
	cases := map[string]string{
		"index concurrently":  "CREATE INDEX CONCURRENTLY idx ON t (x);",
		"drop concurrently":   "DROP INDEX CONCURRENTLY idx;",
		"reindex":             "REINDEX INDEX CONCURRENTLY idx;",
		"huruf kecil":         "create unique index concurrently idx on t (x)",
		"vacuum":              "VACUUM ANALYZE t;",
		"create database":     "CREATE DATABASE baru;",
		"alter system":        "ALTER SYSTEM SET work_mem = '64MB';",
		"begin commit":        "BEGIN; CREATE TABLE t (id INT); COMMIT;",
		"rollback di tengah":  "CREATE TABLE t (id INT); ROLLBACK;",
		"statement kedua":     "CREATE TABLE t (id INT); CREATE INDEX CONCURRENTLY idx ON t (id);",
		"prepare transaction": "PREPARE TRANSACTION 'x';",
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			if err := CheckSQL(sql); !errors.Is(err, ErrNotTransactional) {
				t.Fatalf("mau ErrNotTransactional, dapat %v", err)
			}
		})
	}
}

func TestCheckSQLEmpty(t *testing.T) {
	for _, sql := range []string{"", "   ", ";;", "-- cuma komentar"} {
		if err := CheckSQL(sql); !errors.Is(err, ErrEmptySQL) {
			t.Errorf("%q: mau ErrEmptySQL, dapat %v", sql, err)
		}
	}
}
