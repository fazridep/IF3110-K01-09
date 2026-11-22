package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

const ttl = time.Minute

func TestEnqueueSatuApprovalSatuJob(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	in := f.approval(t, f.database(t), "apply")

	first, created, err := f.store.Enqueue(ctx, in)
	if err != nil || !created {
		t.Fatalf("pertama: created=%v err=%v", created, err)
	}
	second, created, err := f.store.Enqueue(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if created || second != first {
		t.Fatalf("kirim ulang harus dapat job yang sama: %s vs %s created=%v", second, first, created)
	}
}

func TestClaimFIFOPerDatabaseDanParalelAntarDatabase(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbA, dbB := f.database(t), f.database(t)
	a1 := f.enqueue(t, dbA)
	a2 := f.enqueue(t, dbA)
	b1 := f.enqueue(t, dbB)

	first, err := f.store.Claim(ctx, "w1", ttl)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != a1 || first.Attempt != 1 {
		t.Fatalf("mau a1 attempt 1, dapat %+v", first)
	}

	// a2 harus nunggu a1, tapi database B tetap jalan.
	second, err := f.store.Claim(ctx, "w2", ttl)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.ID != b1 {
		t.Fatalf("mau b1, dapat %+v", second)
	}
	third, err := f.store.Claim(ctx, "w3", ttl)
	if err != nil {
		t.Fatal(err)
	}
	if third != nil {
		t.Fatalf("a2 nggak boleh jalan selama a1 belum selesai, dapat %s", third.ID)
	}

	if err := f.store.finish(ctx, first, transition{status: StatusSuccess, terminal: true}); err != nil {
		t.Fatal(err)
	}
	next, err := f.store.Claim(ctx, "w3", ttl)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.ID != a2 {
		t.Fatalf("setelah a1 selesai mau a2, dapat %+v", next)
	}
}

func TestClaimBarengCumaSatuYangDapatDatabaseItu(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbID := f.database(t)
	f.enqueue(t, dbID)
	f.enqueue(t, dbID)

	results := make(chan *Job, 8)
	for range 8 {
		go func() {
			j, err := f.store.Claim(ctx, "w", ttl)
			if err != nil {
				t.Error(err)
			}
			results <- j
		}()
	}
	got := 0
	for range 8 {
		if <-results != nil {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("cuma boleh satu klaim yang berhasil, dapat %d", got)
	}
}

func TestPartialMenahanJobBerikutnyaSampaiDilanjutkan(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbID := f.database(t)
	partial := f.enqueue(t, dbID)
	later := f.enqueue(t, dbID)

	j, err := f.store.Claim(ctx, "w", ttl)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.finish(ctx, j, transition{status: StatusPartial, errText: "batch 3 gagal", terminal: true}); err != nil {
		t.Fatal(err)
	}
	if f.lockCount(t) != 0 {
		t.Fatal("lock harus dilepas")
	}
	if held, err := f.store.Claim(ctx, "w", ttl); err != nil || held != nil {
		t.Fatalf("job berikutnya harus ditahan selama partial: %+v %v", held, err)
	}

	resume := f.approval(t, dbID, ActionBatch)
	resume.MigrationID = j.MigrationID
	resume.ResumesJobID = &partial
	id, _, err := f.store.Enqueue(ctx, resume)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, partial).Status; got != StatusSuperseded {
		t.Fatalf("job lama mau superseded, dapat %s", got)
	}

	// FIFO: job yang lebih dulu antre jalan duluan, baru lanjutan partial-nya.
	next, err := f.store.Claim(ctx, "w", ttl)
	if err != nil || next == nil || next.ID != later {
		t.Fatalf("mau later, dapat %+v %v", next, err)
	}
	if err := f.store.finish(ctx, next, transition{status: StatusSuccess, terminal: true}); err != nil {
		t.Fatal(err)
	}
	next, err = f.store.Claim(ctx, "w", ttl)
	if err != nil || next == nil || next.ID != id {
		t.Fatalf("mau job lanjutan, dapat %+v %v", next, err)
	}
}

func TestResumeJobYangBukanPartialDitolak(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbID := f.database(t)
	running := f.enqueue(t, dbID)

	resume := f.approval(t, dbID, ActionBatch)
	resume.ResumesJobID = &running
	if _, _, err := f.store.Enqueue(ctx, resume); !errors.Is(err, ErrInvalidResume) {
		t.Fatalf("mau ErrInvalidResume, dapat %v", err)
	}
	if got := f.status(t, running).Status; got != StatusQueued {
		t.Fatalf("job lama nggak boleh berubah: %s", got)
	}
}

func TestLeaseHabisDipulihkanJadiUncertain(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbID := f.database(t)
	id := f.enqueue(t, dbID)

	dead, err := f.store.Claim(ctx, "mati", 50*time.Millisecond)
	if err != nil || dead == nil {
		t.Fatalf("klaim: %+v %v", dead, err)
	}
	if ids, err := f.store.RecoverExpired(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("lease masih hidup, nggak boleh dipulihkan: %v %v", ids, err)
	}
	time.Sleep(80 * time.Millisecond)

	ids, err := f.store.RecoverExpired(ctx)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("mau %s dipulihkan, dapat %v %v", id, ids, err)
	}
	if f.lockCount(t) != 0 {
		t.Fatal("lock worker mati harus dilepas")
	}
	if got := f.status(t, id).Status; got != StatusUncertain {
		t.Fatalf("mau uncertain, dapat %s", got)
	}

	// Worker lama yang ternyata masih hidup nggak boleh nulis hasil lagi.
	if err := f.store.finish(ctx, dead, transition{status: StatusSuccess, terminal: true}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("finish worker lama: mau ErrLeaseLost, dapat %v", err)
	}
	if err := f.store.Heartbeat(ctx, dead, ttl); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("heartbeat worker lama: mau ErrLeaseLost, dapat %v", err)
	}

	again, err := f.store.Claim(ctx, "baru", ttl)
	if err != nil || again == nil {
		t.Fatalf("klaim ulang: %+v %v", again, err)
	}
	if again.Status != StatusUncertain || again.VerifyAttempts != 1 || again.Attempt != 1 {
		t.Fatalf("harus masuk verifikasi tanpa nambah attempt: %+v", again)
	}
}

func TestHeartbeatMemperpanjangLease(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.enqueue(t, f.database(t))

	j, err := f.store.Claim(ctx, "w", 100*time.Millisecond)
	if err != nil || j == nil {
		t.Fatalf("klaim: %+v %v", j, err)
	}
	if err := f.store.Heartbeat(ctx, j, ttl); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if ids, err := f.store.RecoverExpired(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("lease sudah diperpanjang: %v %v", ids, err)
	}
}

func TestUncertainKehabisanPercobaanDitahanSampaiRecheck(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbID := f.database(t)
	id := f.enqueue(t, dbID)
	later := f.enqueue(t, dbID)

	if _, err := f.db.Exec(ctx, `UPDATE migration_jobs SET status='uncertain', verify_attempts=max_attempts WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if j, err := f.store.Claim(ctx, "w", ttl); err != nil || j != nil {
		t.Fatalf("nggak boleh ada yang jalan, termasuk %s: %+v %v", later, j, err)
	}

	if err := f.store.Recheck(ctx, id); err != nil {
		t.Fatal(err)
	}
	j, err := f.store.Claim(ctx, "w", ttl)
	if err != nil || j == nil || j.ID != id {
		t.Fatalf("setelah recheck mau job itu lagi, dapat %+v %v", j, err)
	}
	if err := f.store.Recheck(ctx, later); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("recheck job non-uncertain: mau ErrInvalidState, dapat %v", err)
	}
}

func TestRetryMenungguJedaRunAfter(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t, f.database(t))

	j, err := f.store.Claim(ctx, "w", ttl)
	if err != nil || j == nil {
		t.Fatal(err)
	}
	if err := f.store.finish(ctx, j, transition{status: StatusQueued, errText: "putus", delay: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if next, err := f.store.Claim(ctx, "w", ttl); err != nil || next != nil {
		t.Fatalf("masih dalam jeda: %+v %v", next, err)
	}
	if _, err := f.db.Exec(ctx, `UPDATE migration_jobs SET run_after=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	next, err := f.store.Claim(ctx, "w", ttl)
	if err != nil || next == nil || next.Attempt != 2 {
		t.Fatalf("mau attempt 2, dapat %+v %v", next, err)
	}
}

func TestListDisaringDatabaseDanHalaman(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dbA, dbB := f.database(t), f.database(t)
	a1, a2, a3 := f.enqueue(t, dbA), f.enqueue(t, dbA), f.enqueue(t, dbA)
	f.enqueue(t, dbB)

	page, next, err := f.store.List(ctx, ListFilter{Databases: []uuid.UUID{dbA}, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ID != a3 || page[1].ID != a2 || next == 0 {
		t.Fatalf("halaman 1 salah: %+v next=%d", page, next)
	}
	page, next, err = f.store.List(ctx, ListFilter{Databases: []uuid.UUID{dbA}, Limit: 2, BeforeSeq: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != a1 || next != 0 {
		t.Fatalf("halaman 2 salah: %+v next=%d", page, next)
	}
	if page[0].DatabaseName == "" || page[0].CreatedByName != "pemilik" {
		t.Fatalf("data tambahan kosong: %+v", page[0])
	}
}
