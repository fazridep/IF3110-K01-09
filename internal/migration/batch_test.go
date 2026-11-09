package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/if3110/tubes-db-platform/internal/queue"
)

type progressLog []int64

func (p *progressLog) Report(_ context.Context, batches, _ int64) error {
	*p = append(*p, batches)
	return nil
}

// seedOrders: 25 row, amount = id. Row dengan amount >= 1000 dilarang, dipakai
// buat bikin batch tertentu gagal tanpa mengubah SQL migration.
func (f *fixture) seedOrders() {
	f.t.Helper()
	f.targetExec(`
		CREATE TABLE orders (id BIGINT PRIMARY KEY, amount NUMERIC(12, 2) NOT NULL CHECK (amount < 1000));
		CREATE TABLE revert_log (seq SERIAL, first_id BIGINT);
		INSERT INTO orders SELECT i, i FROM generate_series(1, 25) AS i;`)
}

func batchDef(backup bool) Definition {
	return Definition{
		Description: "naikkan amount",
		Mode:        ModeBatch,
		UpSQL:       `UPDATE orders o SET amount = o.amount * 1.07 FROM batch_rows b WHERE o.id = b.id;`,
		DownSQL: `INSERT INTO revert_log (first_id) SELECT min(id) FROM batch_rows;
			UPDATE orders o SET amount = b.amount FROM batch_rows b WHERE o.id = b.id;`,
		Batch: Batch{Table: "orders", Key: "id", Size: 10, Backup: backup},
	}
}

func (f *fixture) amounts() map[int64]string {
	f.t.Helper()
	rows, err := f.target.Query(context.Background(), `SELECT id, amount::text FROM orders`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var amount string
		if err := rows.Scan(&id, &amount); err != nil {
			f.t.Fatal(err)
		}
		out[id] = amount
	}
	return out
}

func (f *fixture) amount(id int64) string {
	f.t.Helper()
	return f.amounts()[id]
}

func (f *fixture) execution(m *Migration) *Execution {
	f.t.Helper()
	x, err := openExecution(context.Background(), f.meta, m.ID)
	if err != nil || x == nil {
		f.t.Fatalf("eksekusi terbuka nggak ada: %v", err)
	}
	return x
}

func TestBatchApplyLaluRollbackDenganDataPemulihan(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedOrders()
	before := f.amounts()
	m := f.create(batchDef(true))

	var progress progressLog
	job := f.job(m, ActionApply)
	if err := f.exec.Execute(ctx, job, &progress); err != nil {
		t.Fatal(err)
	}
	if err := f.exec.Finalize(ctx, job, queue.StatusSuccess, ""); err != nil {
		t.Fatal(err)
	}
	if len(progress) != 3 || progress[2] != 3 {
		t.Fatalf("25 row dengan batch 10 jadi 3 batch, progres %v", progress)
	}
	if got := f.amount(7); got != "7.49" {
		t.Fatalf("amount id 7 mau 7.49, dapat %s", got)
	}
	h := f.history(false)
	if len(h) != 1 || h[0].Batches != 3 || h[0].Rows != 25 || !h[0].Committed {
		t.Fatalf("history batch salah: %+v", h)
	}

	// Rollback per batch dari batch terakhir, data kembali persis dari data pemulihan.
	f.mustRun(m, ActionRollback)
	if f.reload(m).Status != StatusRolledBack {
		t.Fatal("mau rolled_back")
	}
	after := f.amounts()
	for id, v := range before {
		if after[id] != v {
			t.Fatalf("id %d mau %s, dapat %s", id, v, after[id])
		}
	}
	var order []int64
	rows, _ := f.target.Query(ctx, `SELECT first_id FROM revert_log ORDER BY seq`)
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		order = append(order, id)
	}
	rows.Close()
	if len(order) != 3 || order[0] != 21 || order[1] != 11 || order[2] != 1 {
		t.Fatalf("urutan pembatalan harus terbalik (21, 11, 1), dapat %v", order)
	}
	h = f.history(false)
	if len(h) != 2 || h[1].Action != ActionRollback || *h[1].RevertsExecutionID != h[0].ID || h[1].Batches != 3 {
		t.Fatalf("history rollback batch salah: %+v", h)
	}
}

// Batch kedua gagal: batch pertama tetap committed, statusnya partial. Setelah
// datanya dibereskan, lanjutkan memproses dari checkpoint tanpa mengulang batch 1.
func TestBatchPartialLaluDilanjutkan(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedOrders()
	f.targetExec(`UPDATE orders SET amount = 990 WHERE id = 15`) // 990 * 1.07 melanggar CHECK
	m := f.create(batchDef(false))

	err := f.run(m, ActionApply)
	if !queue.IsPartial(err) || !strings.Contains(err.Error(), "1 batch committed") {
		t.Fatalf("mau partial setelah 1 batch, dapat %v", err)
	}
	if got := f.reload(m); got.Status != StatusPartial {
		t.Fatalf("mau partial, dapat %s", got.Status)
	}
	x := f.execution(m)
	if x.Status != ExecPartial || x.Batches != 1 || x.Rows != 10 || x.Cursor != "10" {
		t.Fatalf("checkpoint salah: %+v", x)
	}
	if f.amount(5) != "5.35" || f.amount(11) != "11.00" {
		t.Fatal("batch 1 committed, batch 2 di-rollback")
	}
	if f.historyRows() != 0 {
		t.Fatal("eksekusi partial belum masuk history")
	}

	// Row yang masuk setelah eksekusi mulai di luar cakupan.
	f.targetExec(`INSERT INTO orders VALUES (100, 100)`)
	f.targetExec(`UPDATE orders SET amount = 15 WHERE id = 15`)

	if err := f.svc.CheckAction(ctx, m.ID, ActionResume); err != nil {
		t.Fatalf("partial harus bisa dilanjutkan: %v", err)
	}
	f.mustRun(m, ActionResume)
	if f.reload(m).Status != StatusSuccess {
		t.Fatal("mau success setelah dilanjutkan")
	}
	if f.amount(5) != "5.35" {
		t.Fatalf("batch 1 nggak boleh diulang, dapat %s", f.amount(5))
	}
	if f.amount(15) != "16.05" || f.amount(25) != "26.75" {
		t.Fatal("batch 2 dan 3 harus diproses")
	}
	if f.amount(100) != "100.00" {
		t.Fatal("row di luar cakupan awal nggak ikut diproses")
	}
	h := f.history(false)
	if len(h) != 1 || h[0].ID != x.ID || h[0].Batches != 3 || h[0].Rows != 25 {
		t.Fatalf("eksekusi yang dilanjutkan tetap satu execution ID: %+v", h)
	}
}

// Batalkan batch yang sudah committed dari eksekusi partial. Pembatalan yang
// gagal di tengah jadi partially_reverted dan bisa dilanjutkan.
func TestBatchPembatalanPartial(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedOrders()
	f.targetExec(`UPDATE orders SET amount = 990 WHERE id = 25`) // batch 3 gagal
	m := f.create(batchDef(true))
	if err := f.run(m, ActionApply); !queue.IsPartial(err) {
		t.Fatalf("mau partial, dapat %v", err)
	}
	up := f.execution(m)

	// Pembatalan batch 2 gagal karena revert_log hilang, batch 1 belum disentuh.
	f.targetExec(`ALTER TABLE revert_log RENAME TO revert_log_x`)
	if err := f.run(m, ActionRollback); err == nil {
		t.Fatal("pembatalan harus gagal")
	}
	if got := f.reload(m); got.Status != StatusPartial {
		t.Fatalf("pembatalan yang belum membatalkan apa pun nggak mengubah status, dapat %s", got.Status)
	}

	// Buat batch 1 gagal dibatalkan: pakai trigger yang menolak update id 1.
	f.targetExec(`ALTER TABLE revert_log_x RENAME TO revert_log`)
	f.targetExec(`
		CREATE FUNCTION tolak_satu() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.id = 1 THEN RAISE EXCEPTION 'id 1 dikunci'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER tolak_satu BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION tolak_satu();`)
	err := f.run(m, ActionRollback)
	if !queue.IsPartial(err) {
		t.Fatalf("mau partial setelah batch 2 dibatalkan, dapat %v", err)
	}
	if got := f.reload(m); got.Status != StatusPartiallyReverted {
		t.Fatalf("mau partially_reverted, dapat %s", got.Status)
	}
	if f.amount(15) != "15.00" || f.amount(5) != "5.35" {
		t.Fatal("batch 2 sudah dibatalkan, batch 1 belum")
	}
	down := f.execution(m)
	if !down.down() || *down.RevertsExecutionID != up.ID || down.Batches != 1 {
		t.Fatalf("eksekusi pembatalan salah: %+v", down)
	}

	f.targetExec(`DROP TRIGGER tolak_satu ON orders`)
	f.mustRun(m, ActionRollback)
	if got := f.reload(m); got.Status != StatusRolledBack {
		t.Fatalf("mau rolled_back, dapat %s", got.Status)
	}
	if f.amount(5) != "5.00" {
		t.Fatal("batch 1 harus dibatalkan")
	}
	var reverts int
	if err := f.target.QueryRow(ctx, `SELECT count(*) FROM revert_log`).Scan(&reverts); err != nil || reverts != 2 {
		t.Fatalf("batch 2 nggak boleh dibatalkan dua kali, log %d %v", reverts, err)
	}

	// Pasangan partial + pembatalannya ikut tersembunyi.
	if h := f.history(true); len(h) != 1 || h[0].Status != ExecFailed {
		t.Fatalf("yang tersisa cuma rollback yang gagal, dapat %+v", h)
	}
	if err := f.run(m, ActionForward); !queue.IsPartial(err) {
		t.Fatalf("forward ulang mau partial, dapat %v", err)
	}
	if f.reload(m).Status != StatusPartial {
		t.Fatal("forward ulang tetap kena batch 3 yang gagal")
	}
}

// Job batch yang sudah selesai dijalankan ulang (pesan duplikat) nggak
// memproses ulang batch mana pun.
func TestBatchDuplikatNggakDoubleCommit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedOrders()
	m := f.create(batchDef(false))
	job := f.job(m, ActionApply)
	if err := f.exec.Execute(ctx, job, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.exec.Execute(ctx, job, nil); !errors.Is(err, queue.ErrAlreadyCommitted) {
		t.Fatalf("mau ErrAlreadyCommitted, dapat %v", err)
	}
	if f.amount(1) != "1.07" {
		t.Fatalf("batch nggak boleh dijalankan dua kali, dapat %s", f.amount(1))
	}
	if ok, err := f.exec.Verify(ctx, job); err != nil || !ok {
		t.Fatalf("bukti commit job batch harus ada: %v %v", ok, err)
	}
}

// Worker menandai gagal tanpa Execute (batas percobaan habis) padahal sudah ada
// batch committed: Finalize menjadikannya partial.
func TestFinalizeGagalDenganBatchCommittedJadiPartial(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedOrders()
	f.targetExec(`UPDATE orders SET amount = 990 WHERE id = 15`)
	m := f.create(batchDef(false))
	job := f.job(m, ActionApply)
	if err := f.exec.Execute(ctx, job, nil); !queue.IsPartial(err) {
		t.Fatalf("mau partial, dapat %v", err)
	}
	if err := f.exec.Finalize(ctx, job, queue.StatusFailed, "batas percobaan habis"); err != nil {
		t.Fatal(err)
	}
	if got := f.reload(m); got.Status != StatusPartial {
		t.Fatalf("mau partial, dapat %s", got.Status)
	}
}

func TestBatchDicekKeTargetWaktuDibuat(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.targetExec(`CREATE TABLE tanpa_pk (id BIGINT, x INT)`)

	d := batchDef(false)
	d.Batch.Table = "tabel_nggak_ada"
	var in *InputError
	if _, err := f.svc.Create(ctx, f.editor, f.dbID, d); !errors.As(err, &in) || in.Field != "batch.table" {
		t.Fatalf("tabel yang nggak ada harus ditolak, dapat %v", err)
	}
	d.Batch.Table = "tanpa_pk"
	if _, err := f.svc.Create(ctx, f.editor, f.dbID, d); !errors.As(err, &in) || !strings.Contains(in.Message(), "unik") {
		t.Fatalf("kolom kursor yang nggak unik harus ditolak, dapat %v", err)
	}
}

// Tabel batch hilang sebelum batch pertama: SQL belum pernah jalan, balik ke
// draft. SQL batch yang error di batch pertama tetap gagal.
func TestBatchGagalSebelumSQLJalanBalikKeDraft(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seedOrders()
	m := f.create(batchDef(false))
	f.targetExec(`ALTER TABLE orders RENAME TO orders_lama`)

	if err := f.run(m, ActionApply); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("mau ErrInvalidBatch, dapat %v", err)
	}
	if got := f.reload(m); got.Status != StatusDraft {
		t.Fatalf("mau balik ke draft, dapat %s", got.Status)
	}

	f.targetExec(`ALTER TABLE orders_lama RENAME TO orders`)
	f.targetExec(`UPDATE orders SET amount = 990 WHERE id = 3`) // batch 1 langsung gagal
	if err := f.run(m, ActionApply); err == nil || queue.IsPartial(err) {
		t.Fatalf("mau gagal tanpa batch committed, dapat %v", err)
	}
	if got := f.reload(m); got.Status != StatusFailed {
		t.Fatalf("SQL yang sudah jalan lalu error tetap gagal, dapat %s", got.Status)
	}
	if err := f.svc.CheckAction(ctx, m.ID, ActionApply); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("migration gagal nggak bisa diajukan lagi, dapat %v", err)
	}
}
