package queue

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/if3110/tubes-db-platform/internal/platform/obs"
)

type WorkerConfig struct {
	ID string
	// Concurrency jumlah job yang boleh jalan bareng di satu proses. Yang
	// jalan bareng pasti beda database, karena lock-nya per database.
	Concurrency  int
	PollInterval time.Duration
	// LeaseTTL lama lock bertahan tanpa heartbeat. Ini juga lama maksimal
	// database tertahan kalau worker mati mendadak.
	LeaseTTL  time.Duration
	RetryBase time.Duration
	RetryMax  time.Duration
}

func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.ID == "" {
		c.ID = "worker-" + fmt.Sprintf("%08x", rand.Uint32())
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 4
	}
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 30 * time.Second
	}
	if c.RetryBase <= 0 {
		c.RetryBase = 2 * time.Second
	}
	if c.RetryMax <= 0 {
		c.RetryMax = time.Minute
	}
	return c
}

// writeTimeout batas nulis hasil job ke metadata setelah context job dibatalkan.
const writeTimeout = 10 * time.Second

type Worker struct {
	store *Store
	exec  Executor
	cfg   WorkerConfig
}

func NewWorker(store *Store, exec Executor, cfg WorkerConfig) *Worker {
	return &Worker{store: store, exec: exec, cfg: cfg.withDefaults()}
}

// Run jalan sampai ctx dibatalkan. Job yang lagi jalan ikut dibatalkan, dan
// ditandai uncertain supaya hasilnya diperiksa dulu sebelum diulang.
func (w *Worker) Run(ctx context.Context) {
	obs.Info(ctx, "worker.started", "worker mulai", "worker_id", w.cfg.ID, "concurrency", w.cfg.Concurrency)
	var wg sync.WaitGroup
	wg.Go(func() { w.recoverLoop(ctx) })
	for range w.cfg.Concurrency {
		wg.Go(func() { w.claimLoop(ctx) })
	}
	wg.Wait()
	obs.Info(context.Background(), "worker.stopped", "worker berhenti", "worker_id", w.cfg.ID)
}

func (w *Worker) claimLoop(ctx context.Context) {
	for ctx.Err() == nil {
		start := time.Now()
		job, err := w.store.Claim(ctx, w.cfg.ID, w.cfg.LeaseTTL)
		if err != nil && ctx.Err() == nil {
			obs.Error(ctx, "job.claim_failed", "gagal mengambil job", err, "worker_id", w.cfg.ID)
		}
		if job == nil {
			sleep(ctx, w.cfg.PollInterval)
			continue
		}
		w.process(ctx, job, start)
	}
}

func (w *Worker) recoverLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.LeaseTTL / 2)
	defer ticker.Stop()
	for {
		ids, err := w.store.RecoverExpired(ctx)
		if err != nil && ctx.Err() == nil {
			obs.Error(ctx, "lock.recover_failed", "gagal memulihkan job", err, "worker_id", w.cfg.ID)
		}
		for _, id := range ids {
			obs.Warn(ctx, "lock.expired", "lease habis, job akan diperiksa ulang", obs.KeyJobID, id.String(), "worker_id", w.cfg.ID)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (w *Worker) process(parent context.Context, job *Job, claimStart time.Time) {
	claimEnd := time.Now()

	// Trace dari request API yang memasukkan job ini.
	ctx := obs.ExtractTrace(parent, job.traceContext)
	ctx = obs.WithAttrs(ctx,
		obs.KeyJobID, job.ID.String(),
		obs.KeyMigrationID, job.MigrationID.String(),
		"database_id", job.DatabaseID.String(),
		"action", job.Action,
		"worker_id", w.cfg.ID,
	)
	ctx, span := obs.StartKind(ctx, "worker.job", trace.SpanKindConsumer,
		attribute.String("job.id", job.ID.String()),
		attribute.String("job.action", job.Action),
		attribute.Int("job.attempt", job.Attempt),
	)
	obs.RecordSpan(ctx, "lock.acquire", claimStart, claimEnd, attribute.String("database.id", job.DatabaseID.String()))
	obs.Info(ctx, "lock.acquired", "lock database didapat", "lock_wait_ms", claimEnd.Sub(claimStart).Milliseconds())
	obs.Info(ctx, "job.claimed", "job diambil worker", obs.KeyStatus, string(job.Status),
		obs.KeyAttempt, job.Attempt, "queue_wait_ms", time.Since(job.CreatedAt).Milliseconds())

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lost atomic.Bool
	stopHeartbeat := w.startHeartbeat(execCtx, job, func() {
		lost.Store(true)
		cancel()
	})

	err := w.run(execCtx, job, &lost)
	stopHeartbeat()
	if errors.Is(err, ErrLeaseLost) {
		obs.Warn(ctx, "lock.lost", "lease sudah diambil worker lain, hasil job ini diabaikan")
	} else if err != nil {
		obs.Error(ctx, "job.save_failed", "gagal menyimpan hasil job, akan dipulihkan setelah lease habis", err)
	}
	obs.End(span, err)
}

func (w *Worker) startHeartbeat(ctx context.Context, job *Job, onLost func()) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(w.cfg.LeaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := w.store.Heartbeat(ctx, job, w.cfg.LeaseTTL)
				if errors.Is(err, ErrLeaseLost) {
					onLost()
					return
				}
				if err != nil && ctx.Err() == nil {
					obs.Warn(ctx, "lock.heartbeat_failed", "gagal memperpanjang lease", "error", err.Error())
				}
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}

// run hasilnya error cuma kalau hasil job gagal disimpan. Kegagalan SQL dicatat
// sebagai status job.
func (w *Worker) run(ctx context.Context, job *Job, lost *atomic.Bool) error {
	if job.Status == StatusUncertain {
		execute, err := w.verify(ctx, job, lost)
		if err != nil || !execute {
			return err
		}
	}

	start := time.Now()
	obs.Info(ctx, "job.attempt.start", "eksekusi mulai", obs.KeyAttempt, job.Attempt)
	execErr := w.exec.Execute(ctx, *job, reporter{store: w.store, job: job})
	duration := time.Since(start).Milliseconds()
	if lost.Load() {
		return ErrLeaseLost
	}

	outcome := w.classify(ctx, job, execErr)
	attrs := []any{obs.KeyAttempt, job.Attempt, "duration_ms", duration, obs.KeyStatus, string(outcome.status)}
	if execErr != nil {
		attrs = append(attrs, "error", execErr.Error())
		obs.Warn(ctx, "job.attempt.end", "eksekusi gagal", attrs...)
	} else {
		obs.Info(ctx, "job.attempt.end", "eksekusi selesai", attrs...)
	}
	return w.conclude(ctx, job, outcome)
}

// verify memeriksa bukti commit job uncertain. execute=true berarti job belum
// ke-commit dan harus dijalankan ulang.
func (w *Worker) verify(ctx context.Context, job *Job, lost *atomic.Bool) (execute bool, err error) {
	committed, verifyErr := w.exec.Verify(ctx, *job)
	if lost.Load() {
		return false, ErrLeaseLost
	}
	if verifyErr != nil {
		obs.Warn(ctx, "job.verify_failed", "belum bisa memeriksa hasil job", "error", verifyErr.Error(), "verify_attempts", job.VerifyAttempts)
		return false, w.conclude(ctx, job, transition{
			status:  StatusUncertain,
			errText: "Belum bisa memeriksa hasil job: " + verifyErr.Error(),
			delay:   w.backoff(job.VerifyAttempts),
		})
	}
	obs.Info(ctx, "job.verified", "bukti commit diperiksa", "committed", committed, "verify_attempts", job.VerifyAttempts)
	if committed {
		return false, w.conclude(ctx, job, transition{status: StatusSuccess, terminal: true})
	}
	if job.Attempt >= job.MaxAttempts {
		return false, w.conclude(ctx, job, transition{
			status: StatusFailed, terminal: true,
			errText: "Eksekusi nggak selesai setelah batas percobaan. SQL-nya terbukti belum ke-commit.",
		})
	}
	if err := w.store.BeginExecute(ctx, job); err != nil {
		return false, err
	}
	return true, nil
}

func (w *Worker) classify(parent context.Context, job *Job, err error) transition {
	var partial *partialError
	var uncertain *uncertainError
	switch {
	case err == nil, errors.Is(err, ErrAlreadyCommitted):
		return transition{status: StatusSuccess, terminal: true}
	case errors.As(err, &partial):
		return transition{status: StatusPartial, errText: err.Error(), terminal: true}
	case errors.As(err, &uncertain):
		return transition{status: StatusUncertain, errText: err.Error(), delay: w.backoff(job.VerifyAttempts + 1)}
	case parent.Err() != nil:
		// Worker dimatikan di tengah jalan, bisa saja pas lagi COMMIT.
		return transition{status: StatusUncertain, errText: "Worker berhenti di tengah eksekusi, hasilnya sedang diperiksa."}
	case IsTransient(err) && job.Attempt < job.MaxAttempts:
		obs.Warn(parent, "job.retry", "kegagalan sementara, job diulang", obs.KeyAttempt, job.Attempt, "error", err.Error())
		return transition{status: StatusQueued, errText: err.Error(), delay: w.backoff(job.Attempt)}
	default:
		return transition{status: StatusFailed, errText: err.Error(), terminal: true}
	}
}

// conclude menyimpan hasil job dan melepas lock. Status final didahului Finalize.
// Kalau Finalize gagal, job dibuat uncertain supaya diperiksa dan difinalisasi
// lagi, bukan dibiarkan selesai dengan data Executor yang belum ter-update.
func (w *Worker) conclude(parent context.Context, job *Job, t transition) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), writeTimeout)
	defer cancel()

	if t.terminal {
		if err := w.exec.Finalize(ctx, *job, t.status, t.errText); err != nil {
			obs.Error(ctx, "job.finalize_failed", "gagal memfinalisasi job", err)
			t = transition{status: StatusUncertain, errText: "Gagal menyimpan hasil job, sedang diperiksa ulang.", delay: w.backoff(job.VerifyAttempts + 1)}
		}
	}
	if err := w.store.finish(ctx, job, t); err != nil {
		return err
	}
	obs.Info(ctx, "lock.released", "lock database dilepas")
	obs.Info(ctx, "job.finished", "status job berubah", obs.KeyStatus, string(t.status), obs.KeyAttempt, job.Attempt)
	return nil
}

// backoff jeda retry: dua kali lipat tiap percobaan, plus jitter sampai 20%
// supaya job yang gagal bareng nggak diulang bareng juga.
func (w *Worker) backoff(attempt int) time.Duration {
	d := w.cfg.RetryBase
	for i := 1; i < attempt && d < w.cfg.RetryMax; i++ {
		d *= 2
	}
	if d > w.cfg.RetryMax {
		d = w.cfg.RetryMax
	}
	return d + time.Duration(rand.Int64N(int64(d)/5+1))
}

type reporter struct {
	store *Store
	job   *Job
}

func (r reporter) Report(ctx context.Context, batches, rows int64) error {
	if err := r.store.Report(ctx, r.job, batches, rows); err != nil {
		return err
	}
	obs.Info(ctx, "job.progress", "progres batch", obs.KeyBatch, batches, "rows", rows)
	return nil
}
