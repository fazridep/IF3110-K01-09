package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/if3110/tubes-db-platform/internal/queue"
)

func TestApplyBerhasilMeninggalkanBuktiDanHistory(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE lewat_worker (id INT);`, `DROP TABLE lewat_worker;`))
	job := f.job(m, ActionApply)

	if err := f.exec.Execute(ctx, job, nil); err != nil {
		t.Fatal(err)
	}
	if !f.tableExists("lewat_worker") {
		t.Fatal("SQL harus ke-commit")
	}
	if ok, err := f.exec.Verify(ctx, job); err != nil || !ok {
		t.Fatalf("bukti commit harus ada: %v %v", ok, err)
	}
	// Sebelum Finalize, migration masih running dan belum masuk history.
	if got := f.reload(m); got.Status != StatusRunning || got.ExecutedBy != "editor" {
		t.Fatalf("mau running, dapat %+v", got)
	}
	if f.historyRows() != 0 {
		t.Fatal("history baru diisi waktu finalisasi")
	}

	if err := f.exec.Finalize(ctx, job, queue.StatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	// Finalize dipanggil ulang (worker retry) nggak bikin baris history kedua.
	if err := f.exec.Finalize(ctx, job, queue.StatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	got := f.reload(m)
	if got.Status != StatusSuccess || got.FinishedAt == nil || got.UpExecutionID == nil {
		t.Fatalf("mau success, dapat %+v", got)
	}
	if f.historyRows() != 1 {
		t.Fatalf("mau 1 baris history, dapat %d", f.historyRows())
	}
	h := f.history(false)
	if len(h) != 1 || h[0].ID != *got.UpExecutionID || !h[0].Committed || h[0].SQL != m.UpSQL || h[0].JobID != job.ID {
		t.Fatalf("riwayat salah: %+v", h)
	}
}

// Pesan duplikat: job yang sudah commit dijalankan lagi nggak boleh double commit.
func TestDuplikatNggakDoubleCommit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE cuma_sekali (id SERIAL); INSERT INTO cuma_sekali DEFAULT VALUES;`, `DROP TABLE cuma_sekali;`))
	job := f.job(m, ActionApply)

	if err := f.exec.Execute(ctx, job, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.exec.Execute(ctx, job, nil); !errors.Is(err, queue.ErrAlreadyCommitted) {
		t.Fatalf("mau ErrAlreadyCommitted, dapat %v", err)
	}
	var n int
	if err := f.target.QueryRow(ctx, `SELECT count(*) FROM cuma_sekali`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mau 1 baris, dapat %d %v", n, err)
	}
}

func TestSQLGagalDiRollbackTapiTetapMasukHistory(t *testing.T) {
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE nggak_jadi (id INT); SELECT * FROM tabel_nggak_ada;`, `DROP TABLE nggak_jadi;`))

	err := f.run(m, ActionApply)
	if err == nil || queue.IsTransient(err) {
		t.Fatalf("mau gagal permanen, dapat %v", err)
	}
	if f.tableExists("nggak_jadi") {
		t.Fatal("SQL yang gagal harus rollback")
	}
	got := f.reload(m)
	if got.Status != StatusFailed || got.Error == "" {
		t.Fatalf("mau failed dengan pesan, dapat %+v", got)
	}
	// Spesifikasi v4: attempt gagal tetap tercatat lengkap di migration history.
	var (
		status, sqlText, errText string
		version, attempt         int
		executedBy               string
	)
	err = f.meta.QueryRow(context.Background(), `
		SELECT h.status, h.sql_text, h.error, h.version, h.attempt, u.username
		FROM migration_history h JOIN users u ON u.id = h.executed_by`).
		Scan(&status, &sqlText, &errText, &version, &attempt, &executedBy)
	if err != nil {
		t.Fatalf("attempt gagal harus ada di migration_history: %v", err)
	}
	if status != "failed" || sqlText != m.UpSQL || version != m.Version || attempt != 1 ||
		executedBy != "editor" || !strings.Contains(errText, "tabel_nggak_ada") {
		t.Fatalf("isi attempt gagal salah: %s %q %q v%d #%d %s", status, sqlText, errText, version, attempt, executedBy)
	}
	h := f.history(true)
	if len(h) != 1 || h[0].Status != ExecFailed || h[0].Committed || len(h[0].Attempts) != 1 {
		t.Fatalf("eksekusi gagal harus terlihat beserta attempt-nya, dapat %+v", h)
	}
	// Perbaikannya boleh migration baru.
	if _, err := f.svc.Create(context.Background(), f.editor, f.dbID, normal(`CREATE TABLE nggak_jadi (id INT);`, `DROP TABLE nggak_jadi;`)); err != nil {
		t.Fatal(err)
	}
}

func TestRevalidasiDiWorker(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE nggak_boleh (id INT);`, `DROP TABLE nggak_boleh;`))

	// Izin pembuat job bisa dicabut selama job menunggu di queue.
	job := f.job(m, ActionApply)
	job.CreatedBy = f.viewer
	if err := f.runJob(job); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mau ErrForbidden, dapat %v", err)
	}
	if f.tableExists("nggak_boleh") {
		t.Fatal("SQL nggak boleh jalan")
	}
	if got := f.reload(m); got.Status != StatusDraft {
		t.Fatalf("yang gagal revalidasi tetap draft, dapat %s", got.Status)
	}

	// Aksi yang nggak cocok dengan posisi migration juga ditolak.
	if err := f.exec.Execute(ctx, f.job(m, ActionRollback), nil); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("rollback draft harus ditolak, dapat %v", err)
	}
}

// Skenario spesifikasi: A, B, rollback B, forward B, C. Rollback harus dari
// migration aktif terakhir, forward urut, dan hide rollback memasangkan lewat
// execution ID.
func TestRollbackForwardDanHideRollback(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.create(normal(`CREATE TABLE a (id INT);`, `DROP TABLE a;`))
	f.mustRun(a, ActionApply)
	b := f.create(normal(`CREATE TABLE b (id INT);`, `DROP TABLE b;`))
	f.mustRun(b, ActionApply)

	if err := f.run(a, ActionRollback); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("rollback A sebelum B harus ditolak, dapat %v", err)
	}
	f.mustRun(b, ActionRollback)
	if f.tableExists("b") || f.reload(b).Status != StatusRolledBack {
		t.Fatal("rollback B harus menjalankan SQL down")
	}
	if err := f.run(b, ActionRollback); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("rollback dua kali harus ditolak, dapat %v", err)
	}
	f.mustRun(b, ActionForward)
	if !f.tableExists("b") || f.reload(b).Status != StatusSuccess {
		t.Fatal("forward B harus menjalankan SQL up lagi")
	}
	c := f.create(normal(`CREATE TABLE c (id INT);`, `DROP TABLE c;`))
	f.mustRun(c, ActionApply)

	full := f.history(false)
	if len(full) != 5 {
		t.Fatalf("mau 5 eksekusi, dapat %d", len(full))
	}
	e1, e2, e3, e4, e5 := full[0], full[1], full[2], full[3], full[4]
	if e3.Action != ActionRollback || e3.RevertsExecutionID == nil || *e3.RevertsExecutionID != e2.ID || e3.SQL != b.DownSQL {
		t.Fatalf("rollback harus merujuk eksekusi E2 dan memakai SQL down: %+v", e3)
	}
	if e4.Action != ActionForward || e4.SQL != b.UpSQL {
		t.Fatalf("E4 harus forward B: %+v", e4)
	}
	hidden := f.history(true)
	if len(hidden) != 3 || hidden[0].ID != e1.ID || hidden[1].ID != e4.ID || hidden[2].ID != e5.ID {
		t.Fatalf("hide rollback mau A[E1] B[E4] C[E5], dapat %+v", hidden)
	}
	if f.historyRows() != 5 {
		t.Fatalf("filter nggak boleh menghapus history, dapat %d", f.historyRows())
	}
	if got := f.reload(b); got.UpExecutionID == nil || *got.UpExecutionID != e4.ID {
		t.Fatal("eksekusi up yang berlaku untuk B sekarang E4")
	}

	// Migration history append-only.
	if _, err := f.meta.Exec(ctx, `UPDATE migration_history SET sql_text = 'x'`); err == nil {
		t.Fatal("history nggak boleh diubah")
	}
	if _, err := f.meta.Exec(ctx, `DELETE FROM migration_history`); err == nil {
		t.Fatal("history nggak boleh dihapus")
	}
}

func TestRollbackGagalNggakMengubahPosisi(t *testing.T) {
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE tetap (id INT);`, `DROP TABLE tabel_salah;`))
	f.mustRun(m, ActionApply)

	if err := f.run(m, ActionRollback); err == nil {
		t.Fatal("SQL down yang salah harus gagal")
	}
	if got := f.reload(m); got.Status != StatusSuccess || !f.tableExists("tetap") {
		t.Fatalf("posisi aktif tetap, dapat %s", got.Status)
	}
	if f.historyRows() != 2 {
		t.Fatalf("apply sukses dan rollback gagal sama-sama tercatat, dapat %d", f.historyRows())
	}
	h := f.history(true)
	if len(h) != 2 || h[1].Action != ActionRollback || h[1].Status != ExecFailed {
		t.Fatalf("rollback gagal tetap terlihat walau hide rollback, dapat %+v", h)
	}
}

func TestIrreversibleNggakBisaRollback(t *testing.T) {
	f := newFixture(t)
	m := f.create(Definition{Description: "hapus kolom", UpSQL: `CREATE TABLE x (id INT);`,
		Irreversible: true, IrreversibleReason: "data kolom lama nggak bisa dikembalikan"})
	f.mustRun(m, ActionApply)

	err := f.svc.CheckAction(context.Background(), m.ID, ActionRollback)
	var na *NotAllowedError
	if !errors.As(err, &na) {
		t.Fatalf("mau NotAllowedError, dapat %v", err)
	}
	if err := f.run(m, ActionRollback); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("worker juga menolak, dapat %v", err)
	}
}

func TestApplyDitahanSelamaMasihPartial(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.create(normal(`CREATE TABLE a (id INT);`, `DROP TABLE a;`))
	f.mustRun(a, ActionApply)
	if _, err := f.meta.Exec(ctx, `UPDATE database_migrations SET status='partial' WHERE id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	b := f.create(normal(`CREATE TABLE b (id INT);`, `DROP TABLE b;`))
	if err := f.svc.CheckAction(ctx, b.ID, ActionApply); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("migration baru harus ditahan selama ada yang partial, dapat %v", err)
	}
	allowed, blocked, err := f.svc.AllowedActions(ctx, b)
	if err != nil || len(allowed) != 0 || !strings.Contains(blocked, "v1 belum tuntas") {
		t.Fatalf("draft yang ditahan harus kasih alasan, dapat %v %q %v", allowed, blocked, err)
	}
}

// Apply yang gagal karena target nggak bisa dihubungi sampai percobaan habis:
// SQL-nya belum pernah dikirim, jadi migration balik ke draft dan bisa diajukan
// lagi. Eksekusi gagalnya tetap tercatat.
func TestApplyTargetMatiBalikKeDraft(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE belum_jalan (id INT);`, `DROP TABLE belum_jalan;`))

	up := f.targetDown()
	job := f.job(m, ActionApply)
	job.Attempt = job.MaxAttempts // percobaan terakhir, worker nggak mengulang lagi
	if err := f.runJob(job); !queue.IsTransient(err) {
		t.Fatalf("mau error sementara, dapat %v", err)
	}
	up()

	got := f.reload(m)
	if got.Status != StatusDraft || got.Error != "Database target nggak bisa dihubungi." || got.ExecutedBy != "" || got.StartedAt != nil {
		t.Fatalf("mau balik ke draft dengan pesan error tanpa detail koneksi, dapat %+v", got)
	}
	h := f.history(false)
	if len(h) != 1 || h[0].Status != ExecFailed || h[0].SQLStarted || h[0].Committed {
		t.Fatalf("eksekusi gagal tetap tercatat tanpa SQL jalan, dapat %+v", h)
	}
	if err := f.svc.CheckAction(ctx, m.ID, ActionApply); err != nil {
		t.Fatalf("draft harus bisa diajukan apply lagi: %v", err)
	}
	if _, err := f.svc.Update(ctx, f.editor, m.ID, normal(`CREATE TABLE sudah_jalan (id INT);`, `DROP TABLE sudah_jalan;`)); err != nil {
		t.Fatalf("SQL yang belum pernah dieksekusi masih boleh diubah: %v", err)
	}
	f.mustRun(m, ActionApply)
	if got := f.reload(m); got.Status != StatusSuccess || got.Error != "" || !f.tableExists("sudah_jalan") {
		t.Fatalf("apply ulang harus sukses, dapat %+v", got)
	}
}

// Rollback yang gagal karena target mati tetap nggak mengubah posisi.
func TestRollbackTargetMatiTetapAktif(t *testing.T) {
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE tetap_aktif (id INT);`, `DROP TABLE tetap_aktif;`))
	f.mustRun(m, ActionApply)

	up := f.targetDown()
	job := f.job(m, ActionRollback)
	job.Attempt = job.MaxAttempts
	if err := f.runJob(job); err == nil {
		t.Fatal("rollback harus gagal")
	}
	up()
	if got := f.reload(m); got.Status != StatusSuccess {
		t.Fatalf("mau tetap success, dapat %s", got.Status)
	}
}

// Spesifikasi v4: attempt gagal boleh di-forward lagi. SQL-nya tetap yang sama
// (immutable), jadi forward berhasil kalau penyebabnya sudah dibereskan.
func TestForwardSetelahApplyGagal(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m := f.create(normal(`INSERT INTO pelanggan VALUES (1);`, `DELETE FROM pelanggan WHERE id = 1;`))

	if err := f.run(m, ActionApply); err == nil {
		t.Fatal("apply harus gagal karena tabelnya belum ada")
	}
	if got := f.reload(m); got.Status != StatusFailed {
		t.Fatalf("mau failed, dapat %s", got.Status)
	}
	if _, err := f.svc.Update(ctx, f.editor, m.ID, normal(`SELECT 1;`, `SELECT 1;`)); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("SQL yang pernah dieksekusi nggak boleh diubah, dapat %v", err)
	}
	if err := f.svc.CheckAction(ctx, m.ID, ActionForward); err != nil {
		t.Fatalf("migration gagal harus boleh di-forward: %v", err)
	}

	f.targetExec(`CREATE TABLE pelanggan (id INT)`)
	f.mustRun(m, ActionForward)
	if got := f.reload(m); got.Status != StatusSuccess {
		t.Fatalf("forward sukses harus jadi success, dapat %s", got.Status)
	}
	h := f.history(false)
	if len(h) != 2 || h[0].Status != ExecFailed || h[1].Action != ActionForward || !h[1].Committed {
		t.Fatalf("mau apply gagal lalu forward sukses, dapat %+v", h)
	}
	if f.historyRows() != 2 {
		t.Fatalf("dua attempt harus tercatat, dapat %d", f.historyRows())
	}
}

// Retry di dalam satu eksekusi: tiap attempt jadi satu baris history.
func TestRetryTiapAttemptTercatat(t *testing.T) {
	f := newFixture(t)
	m := f.create(normal(`CREATE TABLE setelah_retry (id INT);`, `DROP TABLE setelah_retry;`))
	job := f.job(m, ActionApply)

	up := f.targetDown()
	if err := f.exec.Execute(context.Background(), job, nil); !queue.IsTransient(err) {
		t.Fatalf("attempt 1 mau gagal sementara, dapat %v", err)
	}
	up()
	job.Attempt = 2
	if err := f.runJob(job); err != nil {
		t.Fatal(err)
	}

	h := f.history(false)
	if len(h) != 1 || len(h[0].Attempts) != 2 {
		t.Fatalf("mau satu eksekusi dengan dua attempt, dapat %+v", h)
	}
	a1, a2 := h[0].Attempts[0], h[0].Attempts[1]
	if a1.Attempt != 1 || a1.Status != AttemptFailed || a1.Error != "Database target nggak bisa dihubungi." {
		t.Fatalf("attempt 1 salah: %+v", a1)
	}
	if a2.Attempt != 2 || a2.Status != AttemptSuccess || a2.ExecutedBy != "editor" {
		t.Fatalf("attempt 2 salah: %+v", a2)
	}

	// Finalize yang dipanggil ulang nggak menambah baris.
	if err := f.exec.Finalize(context.Background(), job, queue.StatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	if f.historyRows() != 2 {
		t.Fatalf("Finalize ulang nggak boleh menambah baris, dapat %d", f.historyRows())
	}
}
