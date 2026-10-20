package testutil

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/store"
	"github.com/if3110/tubes-db-platform/migrations"
)

// DB memberi pool ke TEST_DATABASE_URL di schema baru yang sudah dimigrasi.
// Tiap test dapat schema sendiri, jadi aman dijalankan paralel. Kalau env-nya
// kosong, test di-skip.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL kosong, integration test dilewati")
	}
	ctx := context.Background()

	admin, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("test_%x", rand.Uint64())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	if _, err := store.Migrate(ctx, pool, migrations.Metadata); err != nil {
		t.Fatal(err)
	}
	return pool
}
