package queue

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/platform/obs"
	"github.com/if3110/tubes-db-platform/internal/platform/testutil"
)

func TestMain(m *testing.M) {
	slog.SetDefault(obs.NewLogger(io.Discard, slog.LevelError))
	os.Exit(m.Run())
}

type fixture struct {
	db    *pgxpool.Pool
	store *Store
	user  uuid.UUID
	seq   atomic.Int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testutil.DB(t)
	f := &fixture{db: db, store: NewStore(db)}
	err := db.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ('pemilik', 'x') RETURNING id`).Scan(&f.user)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) database(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.db.QueryRow(context.Background(), `
		INSERT INTO database_connections (name, host, port, db_name, username, password_enc, sslmode)
		VALUES ($1, 'h', 5432, 'd', 'u', '\x00', 'disable') RETURNING id`,
		fmt.Sprintf("db-%d", f.seq.Add(1))).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// approval bikin migration dan approval yang sudah disetujui. Migration berstatus
// success supaya bisa lebih dari satu per database, isinya nggak dipakai executor palsu.
func (f *fixture) approval(t *testing.T, dbID uuid.UUID, action string) NewJob {
	t.Helper()
	ctx := context.Background()
	n := f.seq.Add(1)
	var migID, approvalID uuid.UUID
	err := f.db.QueryRow(ctx, `
		INSERT INTO database_migrations (database_id, version, description, sql_text, status, created_by)
		VALUES ($1, $2, 'uji', 'SELECT 1', 'success', $3) RETURNING id`, dbID, n, f.user).Scan(&migID)
	if err != nil {
		t.Fatal(err)
	}
	err = f.db.QueryRow(ctx, `
		INSERT INTO approval_requests (migration_id, database_id, action, status, requested_by)
		VALUES ($1, $2, $3, 'approved', $4) RETURNING id`, migID, dbID, action, f.user).Scan(&approvalID)
	if err != nil {
		t.Fatal(err)
	}
	return NewJob{
		DatabaseID: dbID, MigrationID: migID, ApprovalID: approvalID, Action: action,
		MaxAttempts: 3, CreatedBy: f.user,
	}
}

func (f *fixture) enqueue(t *testing.T, dbID uuid.UUID) uuid.UUID {
	t.Helper()
	id, created, err := f.store.Enqueue(context.Background(), f.approval(t, dbID, "apply"))
	if err != nil || !created {
		t.Fatalf("enqueue: created=%v err=%v", created, err)
	}
	return id
}

func (f *fixture) status(t *testing.T, id uuid.UUID) *Job {
	t.Helper()
	j, err := f.store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func (f *fixture) lockCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM database_locks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type fakeExecutor struct {
	mu        sync.Mutex
	executed  map[uuid.UUID]int
	finalized map[uuid.UUID]Status
	execute   func(ctx context.Context, job Job, report Reporter) error
	verify    func(ctx context.Context, job Job) (bool, error)
	finalize  func(job Job, status Status) error
}

func (e *fakeExecutor) Execute(ctx context.Context, job Job, report Reporter) error {
	e.mu.Lock()
	if e.executed == nil {
		e.executed = map[uuid.UUID]int{}
	}
	e.executed[job.ID]++
	e.mu.Unlock()
	if e.execute != nil {
		return e.execute(ctx, job, report)
	}
	return nil
}

func (e *fakeExecutor) Verify(ctx context.Context, job Job) (bool, error) {
	if e.verify != nil {
		return e.verify(ctx, job)
	}
	return false, nil
}

func (e *fakeExecutor) Finalize(_ context.Context, job Job, status Status, _ string) error {
	if e.finalize != nil {
		if err := e.finalize(job, status); err != nil {
			return err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.finalized == nil {
		e.finalized = map[uuid.UUID]Status{}
	}
	e.finalized[job.ID] = status
	return nil
}

func (e *fakeExecutor) timesExecuted(id uuid.UUID) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executed[id]
}

func (e *fakeExecutor) finalStatus(id uuid.UUID) Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.finalized[id]
}

var fastConfig = WorkerConfig{
	ID:           "uji",
	Concurrency:  2,
	PollInterval: 10 * time.Millisecond,
	LeaseTTL:     300 * time.Millisecond,
	RetryBase:    10 * time.Millisecond,
	RetryMax:     50 * time.Millisecond,
}

func startWorker(t *testing.T, f *fixture, exec Executor, cfg WorkerConfig) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		NewWorker(f.store, exec, cfg).Run(ctx)
		close(done)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitStatus(t *testing.T, f *fixture, id uuid.UUID, want Status) *Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j := f.status(t, id)
		if j.Status == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s nggak sampai %s, sekarang %+v", id, want, f.status(t, id))
	return nil
}
