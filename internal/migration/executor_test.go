package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/queue"
)

func newExecutorJob(f *fixture, m *Migration, action string) queue.Job {
	return queue.Job{ID: uuid.New(), DatabaseID: m.DatabaseID, MigrationID: m.ID, Action: action, CreatedBy: f.editor}
}

func draft(t *testing.T, f *fixture, sql string) *Migration {
	t.Helper()
	m, err := f.svc.Create(context.Background(), f.editor, f.dbID, "uji worker", sql)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func tableExists(t *testing.T, f *fixture, name string) bool {
	t.Helper()
	var exists bool
	err := f.target.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestExecutorApplyBerhasilDanMeninggalkanBukti(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	e := NewExecutor(f.svc)
	m := draft(t, f, `CREATE TABLE lewat_worker (id INT);`)
	job := newExecutorJob(f, m, "apply")

	if err := e.Execute(ctx, job, nil); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, f, "lewat_worker") {
		t.Fatal("SQL harus ke-commit")
	}
	if ok, err := e.Verify(ctx, job); err != nil || !ok {
		t.Fatalf("bukti commit harus ada: %v %v", ok, err)
	}

	// Sebelum Finalize, migration masih running, jadi riwayatnya belum final.
	got, _ := f.svc.findByID(ctx, m.ID)
	if got.Status != StatusRunning || got.ExecutedBy != "editor" {
		t.Fatalf("mau running, dapat %+v", got)
	}
	if err := e.Finalize(ctx, job, queue.StatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = f.svc.findByID(ctx, m.ID)
	if got.Status != StatusSuccess || got.FinishedAt == nil {
		t.Fatalf("mau success, dapat %+v", got)
	}
}

// Pesan duplikat: retry job yang sudah commit nggak boleh menjalankan SQL lagi.
func TestExecutorDuplikatNggakDoubleCommit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	e := NewExecutor(f.svc)
	m := draft(t, f, `CREATE TABLE cuma_sekali (id SERIAL); INSERT INTO cuma_sekali DEFAULT VALUES;`)
	job := newExecutorJob(f, m, "apply")

	if err := e.Execute(ctx, job, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Execute(ctx, job, nil); !errors.Is(err, queue.ErrAlreadyCommitted) {
		t.Fatalf("mau ErrAlreadyCommitted, dapat %v", err)
	}
	var n int
	if err := f.target.QueryRow(ctx, `SELECT count(*) FROM cuma_sekali`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mau 1 baris, dapat %d %v", n, err)
	}
}

func TestExecutorSQLGagalDiRollback(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	e := NewExecutor(f.svc)
	m := draft(t, f, `CREATE TABLE nggak_jadi (id INT); SELECT * FROM tabel_nggak_ada;`)
	job := newExecutorJob(f, m, "apply")

	err := e.Execute(ctx, job, nil)
	if err == nil || queue.IsTransient(err) {
		t.Fatalf("mau gagal permanen, dapat %v", err)
	}
	if tableExists(t, f, "nggak_jadi") {
		t.Fatal("SQL yang gagal harus rollback")
	}
	if ok, err := e.Verify(ctx, job); err != nil || ok {
		t.Fatalf("bukti nggak boleh ada: %v %v", ok, err)
	}
	if err := e.Finalize(ctx, job, queue.StatusFailed, "tabel nggak ada"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.findByID(ctx, m.ID)
	if got.Status != StatusFailed || got.Error != "tabel nggak ada" {
		t.Fatalf("mau failed dengan pesan, dapat %+v", got)
	}
}

func TestExecutorRevalidasi(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	e := NewExecutor(f.svc)
	m := draft(t, f, `CREATE TABLE nggak_boleh (id INT);`)

	// Izin pembuat job bisa dicabut selama job menunggu di queue.
	job := newExecutorJob(f, m, "apply")
	job.CreatedBy = f.viewer
	if err := e.Execute(ctx, job, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mau ErrForbidden, dapat %v", err)
	}
	if tableExists(t, f, "nggak_boleh") {
		t.Fatal("SQL nggak boleh jalan")
	}
	if err := e.Finalize(ctx, job, queue.StatusFailed, "ditolak"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.findByID(ctx, m.ID)
	if got.Status != StatusDraft {
		t.Fatalf("yang gagal revalidasi tetap draft, dapat %s", got.Status)
	}
}

func TestExecutorAksiLainDitolakDulu(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	e := NewExecutor(f.svc)
	m := draft(t, f, `SELECT 1;`)
	job := newExecutorJob(f, m, "rollback")

	if err := e.Execute(ctx, job, nil); !errors.Is(err, errActionUnsupported) {
		t.Fatalf("mau errActionUnsupported, dapat %v", err)
	}
	if err := e.Finalize(ctx, job, queue.StatusFailed, "x"); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeNggakMenimpaMigrationYangSudahSelesai(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	e := NewExecutor(f.svc)
	m := draft(t, f, `SELECT 1;`)
	job := newExecutorJob(f, m, "apply")
	if err := e.Execute(ctx, job, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Finalize(ctx, job, queue.StatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Finalize(ctx, job, queue.StatusFailed, "telat"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.findByID(ctx, m.ID)
	if got.Status != StatusSuccess {
		t.Fatalf("mau tetap success, dapat %s", got.Status)
	}
}
