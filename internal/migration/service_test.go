package migration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func TestVersiLinearDanSatuDraft(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	for want := 1; want <= 3; want++ {
		m := f.create(normal(fmt.Sprintf("CREATE TABLE t%d (id INT);", want), fmt.Sprintf("DROP TABLE t%d;", want)))
		if m.Version != want || m.Status != StatusDraft || m.CreatedBy != "editor" || m.Mode != ModeNormal {
			t.Fatalf("migration ke-%d salah: %+v", want, m)
		}
		if _, err := f.svc.Create(ctx, f.editor, f.dbID, normal("SELECT 1;", "SELECT 1;")); !errors.Is(err, ErrPendingExists) {
			t.Fatalf("draft kedua harus ditolak, dapat %v", err)
		}
		f.mustRun(m, ActionApply)
	}
}

func TestUpdateDraftDanImmutable(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal("CREATE TABLE awal (id INT);", "DROP TABLE awal;"))

	// Approval yang belum dipakai dibatalkan waktu SQL draft diubah.
	var approvalID uuid.UUID
	if err := f.meta.QueryRow(ctx, `
		INSERT INTO approval_requests (migration_id, database_id, action, status, requested_by)
		VALUES ($1, $2, 'apply', 'approved', $3) RETURNING id`, m.ID, f.dbID, f.editor).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}
	m, err := f.svc.Update(ctx, f.editor, m.ID, normal("CREATE TABLE revisi (id INT);", "DROP TABLE revisi;"))
	if err != nil {
		t.Fatal(err)
	}
	if m.UpSQL != "CREATE TABLE revisi (id INT);" || m.DownSQL != "DROP TABLE revisi;" {
		t.Fatalf("draft nggak ter-update: %+v", m)
	}
	var status string
	if err := f.meta.QueryRow(ctx, `SELECT status FROM approval_requests WHERE id=$1`, approvalID).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("approval lama harus cancelled, dapat %q %v", status, err)
	}

	// Draft yang job-nya sedang antre nggak boleh diubah.
	if err := f.meta.QueryRow(ctx, `
		INSERT INTO approval_requests (migration_id, database_id, action, status, requested_by)
		VALUES ($1, $2, 'apply', 'approved', $3) RETURNING id`, m.ID, f.dbID, f.editor).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.meta.Exec(ctx, `
		INSERT INTO migration_jobs (database_id, migration_id, approval_id, action, max_attempts, created_by)
		VALUES ($1, $2, $3, 'apply', 3, $4)`, f.dbID, m.ID, approvalID, f.editor); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Update(ctx, f.editor, m.ID, normal("SELECT 1;", "SELECT 1;")); !errors.Is(err, ErrBusy) {
		t.Fatalf("mau ErrBusy, dapat %v", err)
	}
	if _, err := f.meta.Exec(ctx, `UPDATE migration_jobs SET status='failed' WHERE migration_id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}

	// Setelah dijalankan, SQL up dan down immutable.
	f.mustRun(m, ActionApply)
	if _, err := f.svc.Update(ctx, f.editor, m.ID, normal("SELECT 1;", "SELECT 1;")); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("mau ErrNotDraft, dapat %v", err)
	}
}

func TestCreateValidasi(t *testing.T) {
	f := newFixture(t)
	var in *InputError
	_, err := f.svc.Create(context.Background(), f.editor, f.dbID, normal("CREATE INDEX CONCURRENTLY idx ON t (x);", "DROP INDEX idx;"))
	if !errors.As(err, &in) || in.Field != "upSql" || !errors.Is(err, ErrNotTransactional) {
		t.Fatalf("mau ErrNotTransactional di upSql, dapat %v", err)
	}
	_, err = f.svc.Create(context.Background(), f.editor, f.dbID, normal("CREATE TABLE a (id INT);", ""))
	if !errors.As(err, &in) || in.Field != "downSql" {
		t.Fatalf("SQL down wajib, dapat %v", err)
	}
}

func TestAksesMigration(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal("CREATE TABLE d (id INT);", "DROP TABLE d;"))

	// view_only boleh lihat, nggak boleh bikin atau ubah.
	if _, err := f.svc.List(ctx, f.viewer, f.dbID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, f.viewer, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.History(ctx, f.viewer, f.dbID, HistoryFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Create(ctx, f.viewer, f.dbID, normal("SELECT 1;", "SELECT 1;")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer create mau ErrForbidden, dapat %v", err)
	}
	if _, err := f.svc.Update(ctx, f.viewer, m.ID, normal("SELECT 1;", "SELECT 1;")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer update mau ErrForbidden, dapat %v", err)
	}

	// Tanpa akses sama sekali: dianggap nggak ada.
	if _, err := f.svc.List(ctx, f.stranger, f.dbID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger list mau ErrNotFound, dapat %v", err)
	}
	if _, err := f.svc.Get(ctx, f.stranger, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger get mau ErrNotFound, dapat %v", err)
	}
	if _, err := f.svc.Create(ctx, f.stranger, f.dbID, normal("SELECT 1;", "SELECT 1;")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger create mau ErrNotFound, dapat %v", err)
	}
}

func TestAllowedActions(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal("CREATE TABLE a (id INT);", "DROP TABLE a;"))
	check := func(want ...string) {
		t.Helper()
		got, err := f.svc.AllowedActions(ctx, f.reload(m))
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("mau %v, dapat %v", want, got)
		}
	}
	check(ActionApply)
	f.mustRun(m, ActionApply)
	check(ActionRollback)
	f.mustRun(m, ActionRollback)
	check(ActionForward)
}
