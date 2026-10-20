package store_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/if3110/tubes-db-platform/internal/store"
	"github.com/if3110/tubes-db-platform/internal/testutil"
)

func TestMigrateRunsOnlyNewFiles(t *testing.T) {
	db := testutil.DB(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"9001_a.up.sql":   {Data: []byte("CREATE TABLE a (id INT);")},
		"9001_a.down.sql": {Data: []byte("DROP TABLE a;")},
	}

	applied, err := store.Migrate(ctx, db, fsys)
	if err != nil || len(applied) != 1 || applied[0] != "9001_a" {
		t.Fatalf("migrate pertama: %v %v", applied, err)
	}

	fsys["9002_b.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE b (id INT);")}
	applied, err = store.Migrate(ctx, db, fsys)
	if err != nil || len(applied) != 1 || applied[0] != "9002_b" {
		t.Fatalf("migrate kedua harus cuma jalanin file baru: %v %v", applied, err)
	}
}

func TestMigrateFailureRollsBack(t *testing.T) {
	db := testutil.DB(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"9001_rusak.up.sql": {Data: []byte("CREATE TABLE c (id INT); SELECT * FROM tabel_nggak_ada;")},
	}

	if _, err := store.Migrate(ctx, db, fsys); err == nil {
		t.Fatal("migration rusak harus error")
	}
	var exists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('c') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("tabel c harus ikut di-rollback")
	}
}
