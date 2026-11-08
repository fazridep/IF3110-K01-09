package queue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/if3110/tubes-db-platform/internal/platform/testutil"
)

func TestWorkerSukses(t *testing.T) {
	f := newFixture(t)
	exec := &fakeExecutor{}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	j := waitStatus(t, f, id, StatusSuccess)
	if j.FinishedAt == nil || j.Attempt != 1 {
		t.Fatalf("data selesai salah: %+v", j)
	}
	if exec.finalStatus(id) != StatusSuccess {
		t.Fatal("Finalize harus dipanggil dengan success")
	}
	if f.lockCount(t) != 0 {
		t.Fatal("lock harus dilepas")
	}
}

func TestWorkerGagalPermanenNggakDiulang(t *testing.T) {
	f := newFixture(t)
	exec := &fakeExecutor{execute: func(context.Context, Job, Reporter) error {
		return &pgconn.PgError{Code: "42601", Message: "syntax error"}
	}}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	j := waitStatus(t, f, id, StatusFailed)
	if exec.timesExecuted(id) != 1 || j.Error == "" {
		t.Fatalf("mau sekali jalan dengan pesan error: %d %+v", exec.timesExecuted(id), j)
	}
	if exec.finalStatus(id) != StatusFailed {
		t.Fatal("Finalize harus dipanggil dengan failed")
	}
}

func TestWorkerKegagalanSementaraDiulangLaluSukses(t *testing.T) {
	f := newFixture(t)
	var calls atomic.Int32
	exec := &fakeExecutor{execute: func(context.Context, Job, Reporter) error {
		if calls.Add(1) == 1 {
			return Transient(errors.New("koneksi putus"))
		}
		return nil
	}}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	j := waitStatus(t, f, id, StatusSuccess)
	if j.Attempt != 2 {
		t.Fatalf("mau attempt 2, dapat %d", j.Attempt)
	}
}

func TestWorkerRetryBerhentiDiBatas(t *testing.T) {
	f := newFixture(t)
	exec := &fakeExecutor{execute: func(context.Context, Job, Reporter) error {
		return Transient(errors.New("koneksi putus terus"))
	}}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	j := waitStatus(t, f, id, StatusFailed)
	if j.Attempt != 3 || exec.timesExecuted(id) != 3 {
		t.Fatalf("mau berhenti di 3 percobaan: attempt=%d jalan=%d", j.Attempt, exec.timesExecuted(id))
	}
}

func TestWorkerPartialDitahanDanProgresTersimpan(t *testing.T) {
	f := newFixture(t)
	dbID := f.database(t)
	exec := &fakeExecutor{execute: func(ctx context.Context, _ Job, report Reporter) error {
		if err := report.Report(ctx, 2, 2000); err != nil {
			return err
		}
		return Partial(errors.New("batch 3 gagal"))
	}}
	id := f.enqueue(t, dbID)
	later := f.enqueue(t, dbID)
	startWorker(t, f, exec, fastConfig)

	j := waitStatus(t, f, id, StatusPartial)
	if j.BatchesDone != 2 || j.RowsDone != 2000 {
		t.Fatalf("progres salah: %+v", j)
	}
	time.Sleep(100 * time.Millisecond)
	if exec.timesExecuted(later) != 0 {
		t.Fatal("job berikutnya nggak boleh jalan selama partial")
	}
}

func TestWorkerCommitBelumPastiTernyataSudahCommit(t *testing.T) {
	f := newFixture(t)
	exec := &fakeExecutor{
		execute: func(context.Context, Job, Reporter) error {
			return Uncertain(errors.New("koneksi putus saat COMMIT"))
		},
		verify: func(context.Context, Job) (bool, error) { return true, nil },
	}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	waitStatus(t, f, id, StatusSuccess)
	if exec.timesExecuted(id) != 1 {
		t.Fatalf("SQL nggak boleh dijalankan ulang kalau sudah ke-commit: %d", exec.timesExecuted(id))
	}
	if exec.finalStatus(id) != StatusSuccess {
		t.Fatal("Finalize harus dipanggil dengan success")
	}
}

func TestWorkerCommitBelumPastiTernyataBelumDiulang(t *testing.T) {
	f := newFixture(t)
	var calls atomic.Int32
	exec := &fakeExecutor{
		execute: func(context.Context, Job, Reporter) error {
			if calls.Add(1) == 1 {
				return Uncertain(errors.New("koneksi putus saat COMMIT"))
			}
			return nil
		},
		verify: func(context.Context, Job) (bool, error) { return false, nil },
	}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	j := waitStatus(t, f, id, StatusSuccess)
	if exec.timesExecuted(id) != 2 || j.Attempt != 2 {
		t.Fatalf("mau jalan 2 kali: jalan=%d attempt=%d", exec.timesExecuted(id), j.Attempt)
	}
}

func TestWorkerVerifikasiGagalTetapMenahanDatabase(t *testing.T) {
	f := newFixture(t)
	dbID := f.database(t)
	exec := &fakeExecutor{
		execute: func(context.Context, Job, Reporter) error { return Uncertain(errors.New("putus")) },
		verify:  func(context.Context, Job) (bool, error) { return false, errors.New("target mati") },
	}
	id := f.enqueue(t, dbID)
	later := f.enqueue(t, dbID)
	startWorker(t, f, exec, fastConfig)

	deadline := time.Now().Add(5 * time.Second)
	for f.status(t, id).VerifyAttempts < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	j := f.status(t, id)
	if j.Status != StatusUncertain || j.VerifyAttempts != 3 {
		t.Fatalf("harus berhenti uncertain di batas verifikasi: %+v", j)
	}
	if exec.timesExecuted(later) != 0 {
		t.Fatal("job berikutnya nggak boleh jalan selama hasil belum pasti")
	}
}

func TestWorkerDibatalkanJobJadiUncertainLaluWorkerLainMelanjutkan(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	first := &fakeExecutor{execute: func(ctx context.Context, _ Job, _ Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	id := f.enqueue(t, f.database(t))
	stop := startWorker(t, f, first, fastConfig)
	<-started
	stop()

	if got := f.status(t, id).Status; got != StatusUncertain {
		t.Fatalf("mau uncertain setelah worker dimatikan, dapat %s", got)
	}
	if f.lockCount(t) != 0 {
		t.Fatal("lock harus dilepas waktu worker berhenti")
	}

	second := &fakeExecutor{}
	startWorker(t, f, second, fastConfig)
	waitStatus(t, f, id, StatusSuccess)
	if second.timesExecuted(id) != 1 {
		t.Fatal("worker baru harus menjalankan ulang setelah terbukti belum commit")
	}
}

// Worker yang mati mendadak nggak sempat nulis apa-apa. Lease-nya habis sendiri,
// dan worker lain memeriksa bukti commit sebelum memutuskan.
func TestWorkerMatiMendadakDipulihkanWorkerLain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		committed bool
		runs      int
	}{
		{"sudah commit, nggak dijalankan ulang", true, 0},
		{"belum commit, dijalankan ulang", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			id := f.enqueue(t, f.database(t))
			if j, err := f.store.Claim(ctx, "mati", 100*time.Millisecond); err != nil || j == nil {
				t.Fatalf("klaim: %+v %v", j, err)
			}

			exec := &fakeExecutor{verify: func(context.Context, Job) (bool, error) { return tc.committed, nil }}
			startWorker(t, f, exec, fastConfig)
			waitStatus(t, f, id, StatusSuccess)
			if exec.timesExecuted(id) != tc.runs {
				t.Fatalf("mau jalan %d kali, dapat %d", tc.runs, exec.timesExecuted(id))
			}
		})
	}
}

func TestWorkerHeartbeatMenjagaJobPanjang(t *testing.T) {
	f := newFixture(t)
	exec := &fakeExecutor{execute: func(ctx context.Context, _ Job, _ Reporter) error {
		// Lebih lama dari LeaseTTL.
		select {
		case <-time.After(700 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)
	startWorker(t, f, exec, fastConfig)

	waitStatus(t, f, id, StatusSuccess)
	if exec.timesExecuted(id) != 1 {
		t.Fatalf("job panjang nggak boleh diambil worker lain: %d", exec.timesExecuted(id))
	}
}

func TestWorkerKehilanganLeaseMembatalkanEksekusi(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cancelled := make(chan struct{})
	exec := &fakeExecutor{execute: func(ctx context.Context, _ Job, _ Reporter) error {
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}}
	id := f.enqueue(t, f.database(t))
	cfg := fastConfig
	cfg.Concurrency = 1
	startWorker(t, f, exec, cfg)
	waitStatus(t, f, id, StatusRunning)

	if _, err := f.db.Exec(ctx, `UPDATE database_locks SET lease_token = gen_random_uuid()`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("eksekusi harus dibatalkan waktu lease hilang")
	}
}

func TestWorkerFinalizeGagalDiperiksaUlang(t *testing.T) {
	f := newFixture(t)
	var calls atomic.Int32
	exec := &fakeExecutor{
		finalize: func(Job, Status) error {
			if calls.Add(1) == 1 {
				return errors.New("metadata sibuk")
			}
			return nil
		},
		verify: func(context.Context, Job) (bool, error) { return true, nil },
	}
	id := f.enqueue(t, f.database(t))
	startWorker(t, f, exec, fastConfig)

	waitStatus(t, f, id, StatusSuccess)
	if exec.timesExecuted(id) != 1 || calls.Load() != 2 {
		t.Fatalf("mau SQL sekali, finalize dua kali: %d %d", exec.timesExecuted(id), calls.Load())
	}
}

func TestBeberapaWorkerDatabaseSamaBerurutanDatabaseBedaParalel(t *testing.T) {
	f := newFixture(t)
	var mu atomicMax
	running := map[uuid.UUID]*atomic.Int32{}
	dbA, dbB := f.database(t), f.database(t)
	running[dbA], running[dbB] = &atomic.Int32{}, &atomic.Int32{}
	var overlapSameDB atomic.Bool

	exec := &fakeExecutor{execute: func(_ context.Context, job Job, _ Reporter) error {
		if running[job.DatabaseID].Add(1) > 1 {
			overlapSameDB.Store(true)
		}
		mu.enter()
		time.Sleep(60 * time.Millisecond)
		mu.leave()
		running[job.DatabaseID].Add(-1)
		return nil
	}}

	var ids []uuid.UUID
	for range 3 {
		ids = append(ids, f.enqueue(t, dbA), f.enqueue(t, dbB))
	}
	startWorker(t, f, exec, fastConfig)
	startWorker(t, f, exec, fastConfig)

	for _, id := range ids {
		waitStatus(t, f, id, StatusSuccess)
		if exec.timesExecuted(id) != 1 {
			t.Fatalf("job %s jalan %d kali", id, exec.timesExecuted(id))
		}
	}
	if overlapSameDB.Load() {
		t.Fatal("dua job di database yang sama jalan bareng")
	}
	if mu.max.Load() < 2 {
		t.Fatal("database berbeda harusnya jalan paralel")
	}
}

type atomicMax struct {
	cur, max atomic.Int32
}

func (a *atomicMax) enter() {
	n := a.cur.Add(1)
	for {
		m := a.max.Load()
		if n <= m || a.max.CompareAndSwap(m, n) {
			return
		}
	}
}

func (a *atomicMax) leave() { a.cur.Add(-1) }

func TestBackoff(t *testing.T) {
	w := NewWorker(nil, nil, WorkerConfig{RetryBase: time.Second, RetryMax: 8 * time.Second})
	cases := []struct {
		attempt int
		want    time.Duration
	}{{1, time.Second}, {2, 2 * time.Second}, {3, 4 * time.Second}, {4, 8 * time.Second}, {9, 8 * time.Second}}
	for _, tc := range cases {
		got := w.backoff(tc.attempt)
		if got < tc.want || got > tc.want+tc.want/5 {
			t.Errorf("attempt %d: %v, mau %v plus jitter maksimal 20%%", tc.attempt, got, tc.want)
		}
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"ditandai", Transient(errors.New("x")), true},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, true},
		{"server restart", &pgconn.PgError{Code: "57P01"}, true},
		{"sintaks salah", &pgconn.PgError{Code: "42601"}, false},
		{"constraint", &pgconn.PgError{Code: "23505"}, false},
		{"eof", fmt.Errorf("baca: %w", io.ErrUnexpectedEOF), true},
		{"jaringan", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"biasa", errors.New("x"), false},
	}
	for _, tc := range cases {
		if got := IsTransient(tc.err); got != tc.want {
			t.Errorf("%s: %v, mau %v", tc.name, got, tc.want)
		}
	}
}

func TestBuktiCommit(t *testing.T) {
	ctx := context.Background()
	target := testutil.DB(t)
	id := uuid.New()

	if ok, err := CommitRecorded(ctx, target, id); err != nil || ok {
		t.Fatalf("belum ada bukti: %v %v", ok, err)
	}

	// Transaction yang rollback nggak meninggalkan bukti.
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordCommit(ctx, tx, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := CommitRecorded(ctx, target, id); err != nil || ok {
		t.Fatalf("rollback nggak boleh meninggalkan bukti: %v %v", ok, err)
	}

	tx, err = target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordCommit(ctx, tx, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := CommitRecorded(ctx, target, id); err != nil || !ok {
		t.Fatalf("bukti harus ada setelah commit: %v %v", ok, err)
	}

	// Pesan duplikat: job yang sama nggak bisa commit dua kali.
	tx, err = target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := RecordCommit(ctx, tx, id); !errors.Is(err, ErrAlreadyCommitted) {
		t.Fatalf("mau ErrAlreadyCommitted, dapat %v", err)
	}
}
