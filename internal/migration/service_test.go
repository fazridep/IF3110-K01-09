package migration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/testutil"
)

type level int

const (
	noAccess level = iota
	viewOnly
	viewEdit
)

type fakePermissions map[uuid.UUID]level

func (p fakePermissions) CanView(_ context.Context, userID, _ uuid.UUID) (bool, error) {
	return p[userID] >= viewOnly, nil
}

func (p fakePermissions) CanEdit(_ context.Context, userID, _ uuid.UUID) (bool, error) {
	return p[userID] == viewEdit, nil
}

type fakeTargets struct{ pool *pgxpool.Pool }

func (f fakeTargets) Pool(context.Context, uuid.UUID) (*pgxpool.Pool, error) {
	return f.pool, nil
}

type fixture struct {
	svc      *Service
	target   *pgxpool.Pool
	dbID     uuid.UUID
	editor   uuid.UUID
	viewer   uuid.UUID
	stranger uuid.UUID
}

// newFixture menyiapkan metadata db dan satu db target terpisah (schema lain).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	meta := testutil.DB(t)
	target := testutil.DB(t)
	f := &fixture{
		target:   target,
		dbID:     createDatabase(t, meta),
		editor:   createUser(t, meta, "editor"),
		viewer:   createUser(t, meta, "viewer"),
		stranger: createUser(t, meta, "stranger"),
	}
	perms := fakePermissions{f.editor: viewEdit, f.viewer: viewOnly}
	f.svc = NewService(meta, perms, fakeTargets{target})
	return f
}

func createUser(t *testing.T, db *pgxpool.Pool, username string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, auth.HashPassword("rahasia"),
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// createDatabase perlu disesuaikan kalau kolom wajib database_connections berubah.
func createDatabase(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(), `INSERT INTO database_connections (name) VALUES ('toko') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var ok bool
	if err := f.target.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestCreateAssignsLinearVersions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for want := 1; want <= 3; want++ {
		m, err := f.svc.Create(ctx, f.editor, f.dbID, "tambah tabel", fmt.Sprintf("CREATE TABLE t%d (id INT);", want))
		if err != nil {
			t.Fatal(err)
		}
		if m.Version != want || m.Status != StatusDraft || m.CreatedBy != "editor" {
			t.Fatalf("migration ke-%d salah: %+v", want, m)
		}
		if _, err := f.svc.Execute(ctx, f.editor, m.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOnlyOnePendingMigration(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Create(ctx, f.editor, f.dbID, "pertama", "CREATE TABLE a (id INT);"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Create(ctx, f.editor, f.dbID, "kedua", "CREATE TABLE b (id INT);"); !errors.Is(err, ErrPendingExists) {
		t.Fatalf("mau ErrPendingExists, dapat %v", err)
	}
}

func TestExecuteSuccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m, err := f.svc.Create(ctx, f.editor, f.dbID, "tabel produk", "CREATE TABLE produk (id INT); INSERT INTO produk VALUES (1);")
	if err != nil {
		t.Fatal(err)
	}

	m, err = f.svc.Execute(ctx, f.editor, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != StatusSuccess || m.ExecutedBy != "editor" || m.StartedAt == nil || m.FinishedAt == nil {
		t.Fatalf("hasil eksekusi salah: %+v", m)
	}
	if !f.tableExists(t, "produk") {
		t.Fatal("tabel produk harus ada di database target")
	}
}

func TestExecuteFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m, err := f.svc.Create(ctx, f.editor, f.dbID, "rusak", "CREATE TABLE setengah (id INT); SELECT * FROM tabel_nggak_ada;")
	if err != nil {
		t.Fatal(err)
	}

	m, err = f.svc.Execute(ctx, f.editor, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != StatusFailed || m.Error == "" {
		t.Fatalf("migration gagal harus berstatus failed dengan pesan error: %+v", m)
	}
	if f.tableExists(t, "setengah") {
		t.Fatal("statement pertama harus ikut di-rollback")
	}
	if _, err := f.svc.Create(ctx, f.editor, f.dbID, "perbaikan", "CREATE TABLE setengah (id INT);"); err != nil {
		t.Fatalf("setelah gagal harus bisa bikin migration baru: %v", err)
	}
}

func TestExecuteTwice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m, err := f.svc.Create(ctx, f.editor, f.dbID, "sekali", "CREATE TABLE sekali (id INT);")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Execute(ctx, f.editor, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Execute(ctx, f.editor, m.ID); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("eksekusi kedua harus ErrNotDraft, dapat %v", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, m.ID, "ubah", "SELECT 1;"); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("migration yang sudah jalan nggak boleh diubah, dapat %v", err)
	}
}

func TestUpdateDraft(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m, err := f.svc.Create(ctx, f.editor, f.dbID, "awal", "CREATE TABLE awal (id INT);")
	if err != nil {
		t.Fatal(err)
	}

	m, err = f.svc.Update(ctx, f.editor, m.ID, "revisi", "CREATE TABLE revisi (id INT);")
	if err != nil {
		t.Fatal(err)
	}
	if m.Description != "revisi" || m.SQL != "CREATE TABLE revisi (id INT);" || m.Version != 1 {
		t.Fatalf("draft nggak ter-update: %+v", m)
	}
}

func TestRejectsNonTransactionalSQL(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Create(context.Background(), f.editor, f.dbID, "index", "CREATE INDEX CONCURRENTLY idx ON t (x);")
	if !errors.Is(err, ErrNotTransactional) {
		t.Fatalf("mau ErrNotTransactional, dapat %v", err)
	}
}

func TestViewOnlyCanReadButNotChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m, err := f.svc.Create(ctx, f.editor, f.dbID, "draft", "CREATE TABLE d (id INT);")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.List(ctx, f.viewer, f.dbID); err != nil {
		t.Fatalf("view_only harus bisa lihat riwayat: %v", err)
	}
	if _, err := f.svc.Get(ctx, f.viewer, m.ID); err != nil {
		t.Fatalf("view_only harus bisa lihat detail: %v", err)
	}
	if _, err := f.svc.Create(ctx, f.viewer, f.dbID, "x", "SELECT 1;"); !errors.Is(err, ErrForbidden) {
		t.Errorf("view_only nggak boleh bikin migration, dapat %v", err)
	}
	if _, err := f.svc.Update(ctx, f.viewer, m.ID, "x", "SELECT 1;"); !errors.Is(err, ErrForbidden) {
		t.Errorf("view_only nggak boleh ubah migration, dapat %v", err)
	}
	if _, err := f.svc.Execute(ctx, f.viewer, m.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("view_only nggak boleh jalanin migration, dapat %v", err)
	}
	if f.tableExists(t, "d") {
		t.Error("migration nggak boleh jalan")
	}
}

func TestNoAccessCannotRead(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m, err := f.svc.Create(ctx, f.editor, f.dbID, "draft", "CREATE TABLE d (id INT);")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.List(ctx, f.stranger, f.dbID); !errors.Is(err, ErrForbidden) {
		t.Errorf("tanpa akses nggak boleh lihat riwayat, dapat %v", err)
	}
	if _, err := f.svc.Get(ctx, f.stranger, m.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("tanpa akses nggak boleh lihat detail, dapat %v", err)
	}
}
