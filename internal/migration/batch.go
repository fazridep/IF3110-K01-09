package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"

	"github.com/if3110/tubes-db-platform/internal/platform/obs"
	"github.com/if3110/tubes-db-platform/internal/platform/pgerr"
	"github.com/if3110/tubes-db-platform/internal/queue"
)

// Checkpoint batch disimpan di database target, satu transaction dengan
// perubahan batch-nya (ADR-015). Kalau batch commit, checkpoint-nya ada. Kalau
// rollback, checkpoint-nya juga hilang. Schema _dbplatform disembunyikan dari viewer.
const (
	batchScopeTable      = "_dbplatform.batch_scope"
	batchCheckpointTable = "_dbplatform.batch_checkpoints"
	batchBackupTable     = "_dbplatform.batch_backup"
)

var (
	errCommitUnknown  = errors.New("hasil COMMIT batch belum pasti")
	errBatchDuplicate = errors.New("batch ini sudah di-commit proses lain")
)

func ensureBatchTables(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, batchBackupTable).Scan(&exists); err != nil {
		return fmt.Errorf("cek tabel checkpoint: %w", err)
	}
	if exists {
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// Advisory lock yang sama dengan tabel bukti di queue, karena CREATE ... IF
	// NOT EXISTS yang jalan bareng tetap bisa bentrok di catalog.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('_dbplatform.setup'))`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS _dbplatform;
		CREATE TABLE IF NOT EXISTS `+batchScopeTable+` (
			execution_id UUID        PRIMARY KEY,
			upper_key    TEXT,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS `+batchCheckpointTable+` (
			execution_id  UUID        NOT NULL,
			batch_no      INTEGER     NOT NULL,
			start_key     TEXT,
			end_key       TEXT        NOT NULL,
			rows_affected BIGINT      NOT NULL,
			committed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (execution_id, batch_no)
		);
		CREATE TABLE IF NOT EXISTS `+batchBackupTable+` (
			execution_id UUID    NOT NULL,
			batch_no     INTEGER NOT NULL,
			row_data     JSONB   NOT NULL
		);
		CREATE INDEX IF NOT EXISTS batch_backup_batch ON `+batchBackupTable+` (execution_id, batch_no);`); err != nil {
		return fmt.Errorf("siapkan tabel checkpoint: %w", err)
	}
	return tx.Commit(ctx)
}

// batchTarget tabel dan kolom kursor yang sudah dicek ada di database target.
type batchTarget struct {
	table   string // identifier yang sudah di-sanitize
	key     string
	keyType string // dari format_type, aman disisipkan ke CAST
}

func resolveBatchTarget(ctx context.Context, q querier, b Batch) (*batchTarget, error) {
	ident, err := tableIdent(b.Table)
	if err != nil {
		return nil, err
	}
	table := ident.Sanitize()
	var exists bool
	if err := q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTargetUnavailable, err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: tabel %s nggak ada di database target", ErrInvalidBatch, b.Table)
	}

	var keyType string
	var unique bool
	err = q.QueryRow(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod),
			EXISTS (SELECT 1 FROM pg_index i
				WHERE i.indrelid = a.attrelid AND i.indisunique AND i.indnkeyatts = 1
				  AND i.indkey[0] = a.attnum AND i.indpred IS NULL)
		FROM pg_attribute a
		WHERE a.attrelid = to_regclass($1) AND a.attname = $2 AND a.attnum > 0 AND NOT a.attisdropped`,
		table, b.Key).Scan(&keyType, &unique)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: kolom %s nggak ada di tabel %s", ErrInvalidBatch, b.Key, b.Table)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTargetUnavailable, err)
	}
	if !unique {
		return nil, fmt.Errorf("%w: kolom kursor %s harus unik (primary key atau unique index)", ErrInvalidBatch, b.Key)
	}
	return &batchTarget{table: table, key: pgx.Identifier{b.Key}.Sanitize(), keyType: keyType}, nil
}

type checkpoint struct {
	no    int
	start *string
	end   string
	rows  int64
}

type batchProgress struct {
	batches int64
	rows    int64
	cursor  string
}

func loadCheckpoints(ctx context.Context, q querier, executionID uuid.UUID) ([]checkpoint, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, batchCheckpointTable).Scan(&exists); err != nil {
		return nil, fmt.Errorf("cek tabel checkpoint: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT batch_no, start_key, end_key, rows_affected FROM `+batchCheckpointTable+`
		WHERE execution_id = $1 ORDER BY batch_no`, executionID)
	if err != nil {
		return nil, fmt.Errorf("baca checkpoint: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (checkpoint, error) {
		var c checkpoint
		err := r.Scan(&c.no, &c.start, &c.end, &c.rows)
		return c, err
	})
}

func progressOf(cps []checkpoint) batchProgress {
	var p batchProgress
	for _, c := range cps {
		p.batches++
		p.rows += c.rows
		p.cursor = c.end
	}
	return p
}

func loadProgress(ctx context.Context, q querier, executionID uuid.UUID) (batchProgress, error) {
	cps, err := loadCheckpoints(ctx, q, executionID)
	return progressOf(cps), err
}

// runBatches menjalankan eksekusi mode batch. Batch yang sudah committed
// (checkpoint ada) dilewati, jadi retry dan resume melanjutkan dari checkpoint.
// Setelah semua batch selesai, bukti commit job ditulis di transaction terakhir.
func (e *Executor) runBatches(ctx context.Context, pool *pgxpool.Pool, job queue.Job, m *Migration, x *Execution, report queue.Reporter) error {
	if err := ensureBatchTables(ctx, pool); err != nil {
		return err
	}
	t, err := resolveBatchTarget(ctx, pool, m.Batch)
	if err != nil {
		return err
	}

	var p batchProgress
	if x.down() {
		p, err = e.revertBatches(ctx, pool, t, m, x, report)
	} else {
		p, err = e.applyBatches(ctx, pool, t, m, x, report)
	}
	if err != nil {
		return batchError(ctx, job, p, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return batchError(ctx, job, p, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err := queue.RecordCommit(ctx, tx, job.ID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return queue.Uncertain(fmt.Errorf("hasil COMMIT belum pasti: %w", err))
	}
	obs.Info(ctx, "migration.batch.done", "semua batch selesai", "batches", p.batches, "rows", p.rows)
	return nil
}

// batchError menentukan arti kegagalan batch buat worker. Batch yang sudah
// committed tetap committed, jadi kalau ada, hasilnya partial.
func batchError(ctx context.Context, job queue.Job, p batchProgress, err error) error {
	switch {
	case ctx.Err() != nil, errors.Is(err, queue.ErrLeaseLost):
		return err
	case errors.Is(err, errCommitUnknown):
		return queue.Uncertain(err)
	case queue.IsTransient(err) && job.Attempt < job.MaxAttempts:
		return queue.Transient(err)
	case p.batches > 0:
		return queue.Partial(fmt.Errorf("berhenti setelah %d batch committed: %w", p.batches, err))
	}
	return err
}

func (e *Executor) applyBatches(ctx context.Context, pool *pgxpool.Pool, t *batchTarget, m *Migration, x *Execution, report queue.Reporter) (batchProgress, error) {
	upper, err := batchScope(ctx, pool, t, x.ID)
	if err != nil {
		return batchProgress{}, err
	}
	cps, err := loadCheckpoints(ctx, pool, x.ID)
	if err != nil {
		return batchProgress{}, err
	}
	p := progressOf(cps)
	if upper == nil {
		return p, nil // tabel kosong waktu eksekusi dimulai
	}

	for {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		var cursor *string
		if p.batches > 0 {
			cursor = &p.cursor
		}
		c, err := e.upBatch(ctx, pool, t, m, x, int(p.batches)+1, cursor, *upper)
		if errors.Is(err, errBatchDuplicate) {
			if cps, err = loadCheckpoints(ctx, pool, x.ID); err != nil {
				return p, err
			}
			p = progressOf(cps)
			continue
		}
		if err != nil {
			return p, err
		}
		if c == nil {
			return p, nil
		}
		p.batches++
		p.rows += c.rows
		p.cursor = c.end
		if err := e.progress(ctx, x, report, p); err != nil {
			return p, err
		}
	}
}

// batchScope menentukan cakupan eksekusi sejak awal: kunci terbesar saat
// eksekusi dimulai. Row yang masuk belakangan nggak ikut diproses.
func batchScope(ctx context.Context, pool *pgxpool.Pool, t *batchTarget, executionID uuid.UUID) (*string, error) {
	if _, err := pool.Exec(ctx, `
		INSERT INTO `+batchScopeTable+` (execution_id, upper_key)
		SELECT $1::uuid, max(`+t.key+`)::text FROM `+t.table+`
		ON CONFLICT (execution_id) DO NOTHING`, executionID); err != nil {
		return nil, fmt.Errorf("tentukan cakupan batch: %w", err)
	}
	var upper *string
	if err := pool.QueryRow(ctx, `SELECT upper_key FROM `+batchScopeTable+` WHERE execution_id = $1`, executionID).Scan(&upper); err != nil {
		return nil, fmt.Errorf("baca cakupan batch: %w", err)
	}
	return upper, nil
}

// upBatch memproses satu batch: ambil row berikutnya setelah kursor ke temp
// table batch_rows, simpan data pemulihan kalau diminta, jalankan SQL up, lalu
// tulis checkpoint. Semuanya satu transaction. Hasil nil berarti nggak ada row lagi.
func (e *Executor) upBatch(ctx context.Context, pool *pgxpool.Pool, t *batchTarget, m *Migration, x *Execution, no int, cursor *string, upper string) (c *checkpoint, err error) {
	ctx = obs.WithAttrs(ctx, obs.KeyBatch, no)
	ctx, span := obs.Start(ctx, "migration.batch", attribute.Int("migration.batch", no))
	defer func() { obs.End(span, err) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if err := lockTarget(ctx, tx); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE `+BatchRowsTable+` (LIKE `+t.table+`) ON COMMIT DROP`); err != nil {
		return nil, err
	}
	// Dua bentuk query supaya rencana eksekusinya tetap index range scan.
	fill := `INSERT INTO ` + BatchRowsTable + ` SELECT * FROM ` + t.table +
		` WHERE ` + t.key + ` <= CAST($1::text AS ` + t.keyType + `)`
	args := []any{upper, m.Batch.Size}
	if cursor != nil {
		fill += ` AND ` + t.key + ` > CAST($3::text AS ` + t.keyType + `)`
		args = append(args, *cursor)
	}
	fill += ` ORDER BY ` + t.key + ` LIMIT $2`
	if _, err := tx.Exec(ctx, fill, args...); err != nil {
		return nil, err
	}

	var n int64
	var end *string
	if err := tx.QueryRow(ctx, `SELECT count(*), max(`+t.key+`)::text FROM `+BatchRowsTable).Scan(&n, &end); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `ANALYZE `+BatchRowsTable); err != nil {
		return nil, err
	}
	if m.Batch.Backup {
		if _, err := tx.Exec(ctx, `
			INSERT INTO `+batchBackupTable+` (execution_id, batch_no, row_data)
			SELECT $1::uuid, $2::int, to_jsonb(b) FROM `+BatchRowsTable+` b`, x.ID, no); err != nil {
			return nil, fmt.Errorf("simpan data pemulihan: %w", err)
		}
	}

	if err := e.svc.markSQLStarted(ctx, x); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, x.SQL)
	if err != nil {
		obs.Warn(ctx, "migration.batch.rollback", "batch gagal, transaction batch di-rollback", "error", err.Error())
		return nil, err
	}
	c = &checkpoint{no: no, start: cursor, end: *end, rows: tag.RowsAffected()}
	if err := writeCheckpoint(ctx, tx, x.ID, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", errCommitUnknown, err)
	}
	obs.Info(ctx, "migration.batch.commit", "batch di-commit", "rows", c.rows, "cursor", c.end, "batch_rows", n)
	return c, nil
}

func writeCheckpoint(ctx context.Context, tx pgx.Tx, executionID uuid.UUID, c *checkpoint) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO `+batchCheckpointTable+` (execution_id, batch_no, start_key, end_key, rows_affected)
		VALUES ($1, $2, $3, $4, $5)`, executionID, c.no, c.start, c.end, c.rows)
	if pgerr.IsUniqueViolation(err) {
		return errBatchDuplicate
	}
	if err != nil {
		return fmt.Errorf("simpan checkpoint: %w", err)
	}
	return nil
}

// revertBatches membatalkan batch yang sudah committed dari eksekusi up yang
// dirujuk, urut dari batch terakhir. Checkpoint eksekusi rollback memakai nomor
// batch yang dibatalkan, jadi kalau berhenti di tengah, batch yang sudah
// dibatalkan nggak diulang.
func (e *Executor) revertBatches(ctx context.Context, pool *pgxpool.Pool, t *batchTarget, m *Migration, x *Execution, report queue.Reporter) (batchProgress, error) {
	if x.RevertsExecutionID == nil {
		return batchProgress{}, errors.New("eksekusi rollback nggak menunjuk eksekusi up")
	}
	ups, err := loadCheckpoints(ctx, pool, *x.RevertsExecutionID)
	if err != nil {
		return batchProgress{}, err
	}
	done, err := loadCheckpoints(ctx, pool, x.ID)
	if err != nil {
		return batchProgress{}, err
	}
	reverted := map[int]bool{}
	var p batchProgress
	for _, c := range done {
		reverted[c.no] = true
		p.batches++
		p.rows += c.rows
		p.cursor = c.end
	}

	for i := len(ups) - 1; i >= 0; i-- {
		if reverted[ups[i].no] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return p, err
		}
		rows, err := e.downBatch(ctx, pool, t, m, x, ups[i])
		if errors.Is(err, errBatchDuplicate) {
			continue
		}
		if err != nil {
			return p, err
		}
		p.batches++
		p.rows += rows
		p.cursor = ups[i].end
		if err := e.progress(ctx, x, report, p); err != nil {
			return p, err
		}
	}
	return p, nil
}

// downBatch mengisi batch_rows dengan data pemulihan dari eksekusi up (kalau
// disimpan) atau row yang sekarang ada di rentang batch itu, lalu menjalankan
// SQL down yang sudah disetujui.
func (e *Executor) downBatch(ctx context.Context, pool *pgxpool.Pool, t *batchTarget, m *Migration, x *Execution, up checkpoint) (rows int64, err error) {
	ctx = obs.WithAttrs(ctx, obs.KeyBatch, up.no)
	ctx, span := obs.Start(ctx, "migration.batch", attribute.Int("migration.batch", up.no), attribute.Bool("migration.revert", true))
	defer func() { obs.End(span, err) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if err := lockTarget(ctx, tx); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE `+BatchRowsTable+` (LIKE `+t.table+`) ON COMMIT DROP`); err != nil {
		return 0, err
	}
	if m.Batch.Backup {
		_, err = tx.Exec(ctx, `
			INSERT INTO `+BatchRowsTable+`
			SELECT (jsonb_populate_record(NULL::`+t.table+`, row_data)).*
			FROM `+batchBackupTable+` WHERE execution_id = $1 AND batch_no = $2`,
			*x.RevertsExecutionID, up.no)
	} else {
		fill := `INSERT INTO ` + BatchRowsTable + ` SELECT * FROM ` + t.table +
			` WHERE ` + t.key + ` <= CAST($1::text AS ` + t.keyType + `)`
		args := []any{up.end}
		if up.start != nil {
			fill += ` AND ` + t.key + ` > CAST($2::text AS ` + t.keyType + `)`
			args = append(args, *up.start)
		}
		_, err = tx.Exec(ctx, fill, args...)
	}
	if err != nil {
		return 0, fmt.Errorf("siapkan row batch %d: %w", up.no, err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE `+BatchRowsTable); err != nil {
		return 0, err
	}

	if err := e.svc.markSQLStarted(ctx, x); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, x.SQL)
	if err != nil {
		obs.Warn(ctx, "migration.batch.rollback", "pembatalan batch gagal, transaction batch di-rollback", "error", err.Error())
		return 0, err
	}
	c := &checkpoint{no: up.no, start: up.start, end: up.end, rows: tag.RowsAffected()}
	if err := writeCheckpoint(ctx, tx, x.ID, c); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("%w: %v", errCommitUnknown, err)
	}
	obs.Info(ctx, "migration.batch.commit", "pembatalan batch di-commit", "rows", c.rows, "cursor", c.end)
	return c.rows, nil
}

func (e *Executor) progress(ctx context.Context, x *Execution, report queue.Reporter, p batchProgress) error {
	e.saveProgress(ctx, x, p.batches, p.rows, p.cursor)
	if report == nil {
		return nil
	}
	return report.Report(ctx, p.batches, p.rows)
}
