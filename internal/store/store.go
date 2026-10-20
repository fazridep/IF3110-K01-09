package store

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: buat pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}
	return pool, nil
}

// Migrate menjalankan file *.up.sql di fsys yang belum tercatat di
// schema_migrations, urut nama file. Hasilnya daftar versi yang baru dijalankan.
func Migrate(ctx context.Context, db *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	_, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, fmt.Errorf("store: buat schema_migrations: %w", err)
	}

	names, err := fs.Glob(fsys, "*.up.sql")
	if err != nil {
		return nil, err
	}

	var applied []string
	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")
		ok, err := migrateOne(ctx, db, fsys, name, version)
		if err != nil {
			return applied, err
		}
		if ok {
			applied = append(applied, version)
		}
	}
	return applied, nil
}

func migrateOne(ctx context.Context, db *pgxpool.Pool, fsys fs.FS, name, version string) (bool, error) {
	sql, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false, err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Versi dicatat duluan supaya dua proses migrate yang jalan bareng nggak
	// menjalankan file yang sama dua kali.
	tag, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, version)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return false, fmt.Errorf("store: migration %s: %w", name, err)
	}
	return true, tx.Commit(ctx)
}
