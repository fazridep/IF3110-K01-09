package migration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/platform/obs"
	"github.com/if3110/tubes-db-platform/internal/platform/testutil"
	"github.com/if3110/tubes-db-platform/internal/queue"
)

func TestMain(m *testing.M) {
	slog.SetDefault(obs.NewLogger(io.Discard, slog.LevelError))
	os.Exit(m.Run())
}

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

type fakeTargets struct {
	pool *pgxpool.Pool
	err  error
}

func (f fakeTargets) Pool(context.Context, uuid.UUID) (*pgxpool.Pool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pool, nil
}

// targetDown bikin database target nggak bisa dihubungi sampai dipanggil up().
func (f *fixture) targetDown() (up func()) {
	f.svc.targets = fakeTargets{err: errors.New("dial tcp 127.0.0.1:5436: connect: connection refused")}
	return func() { f.svc.targets = fakeTargets{pool: f.target} }
}

type fixture struct {
	t        *testing.T
	svc      *Service
	exec     *Executor
	meta     *pgxpool.Pool
	target   *pgxpool.Pool
	dbID     uuid.UUID
	editor   uuid.UUID
	viewer   uuid.UUID
	stranger uuid.UUID
}

// newFixture menyiapkan database metadata dan satu database target kosong
// (schema terpisah).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	meta := testutil.DB(t)
	target := testutil.EmptyDB(t)
	f := &fixture{
		t:        t,
		meta:     meta,
		target:   target,
		dbID:     createDatabase(t, meta),
		editor:   createUser(t, meta, "editor"),
		viewer:   createUser(t, meta, "viewer"),
		stranger: createUser(t, meta, "stranger"),
	}
	perms := fakePermissions{f.editor: viewEdit, f.viewer: viewOnly}
	f.svc = NewService(meta, perms, fakeTargets{pool: target})
	f.exec = NewExecutor(f.svc)
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

// createDatabase cuma ngisi kolom wajib. Target di test pakai fakeTargets, jadi
// password_enc nggak pernah dibuka.
func createDatabase(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(context.Background(), `
		INSERT INTO database_connections (name, host, db_name, username, password_enc)
		VALUES ('toko', 'localhost', 'toko', 'app', '\x00')
		RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func normal(up, down string) Definition {
	return Definition{Description: "uji", UpSQL: up, DownSQL: down}
}

func (f *fixture) create(d Definition) *Migration {
	f.t.Helper()
	m, err := f.svc.Create(context.Background(), f.editor, f.dbID, d)
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *fixture) job(m *Migration, action string) queue.Job {
	return queue.Job{
		ID: uuid.New(), DatabaseID: f.dbID, MigrationID: m.ID, Action: action,
		CreatedBy: f.editor, Attempt: 1, MaxAttempts: 3,
	}
}

// run meniru worker: Execute, lalu Finalize dengan status yang sesuai hasilnya.
func (f *fixture) run(m *Migration, action string) error {
	f.t.Helper()
	return f.runJob(f.job(m, action))
}

func (f *fixture) runJob(job queue.Job) error {
	f.t.Helper()
	ctx := context.Background()
	err := f.exec.Execute(ctx, job, nil)
	status, errText := queue.StatusSuccess, ""
	switch {
	case err == nil, errors.Is(err, queue.ErrAlreadyCommitted):
	case queue.IsPartial(err):
		status, errText = queue.StatusPartial, err.Error()
	default:
		status, errText = queue.StatusFailed, err.Error()
	}
	if ferr := f.exec.Finalize(ctx, job, status, errText); ferr != nil {
		f.t.Fatal(ferr)
	}
	return err
}

func (f *fixture) mustRun(m *Migration, action string) {
	f.t.Helper()
	if err := f.run(m, action); err != nil {
		f.t.Fatalf("%s v%d: %v", action, m.Version, err)
	}
}

func (f *fixture) reload(m *Migration) *Migration {
	f.t.Helper()
	got, err := findByID(context.Background(), f.meta, m.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fixture) tableExists(name string) bool {
	f.t.Helper()
	var ok bool
	if err := f.target.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&ok); err != nil {
		f.t.Fatal(err)
	}
	return ok
}

func (f *fixture) history(hide bool) []Execution {
	f.t.Helper()
	list, err := f.svc.History(context.Background(), f.editor, f.dbID, HistoryFilter{HideRollback: hide})
	if err != nil {
		f.t.Fatal(err)
	}
	return list
}

func (f *fixture) historyRows() int {
	f.t.Helper()
	var n int
	if err := f.meta.QueryRow(context.Background(), `SELECT count(*) FROM migration_history`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) targetExec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.target.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatal(err)
	}
}
