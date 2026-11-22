package migration

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

const MaxBatchSize = 1_000_000

// BatchRowsTable nama temp table berisi row satu batch. SQL batch wajib memakainya,
// kalau nggak, tiap batch bakal memproses seluruh tabel.
const BatchRowsTable = "batch_rows"

var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// normalize merapikan input sebelum divalidasi dan disimpan.
func normalize(d Definition) Definition {
	d.Description = strings.TrimSpace(d.Description)
	d.IrreversibleReason = strings.TrimSpace(d.IrreversibleReason)
	if strings.TrimSpace(d.DownSQL) == "" || d.Irreversible {
		d.DownSQL = ""
	}
	if d.Mode == "" {
		d.Mode = ModeNormal
	}
	if d.Mode == ModeNormal {
		d.Batch = Batch{}
	}
	d.Batch.Table = strings.TrimSpace(d.Batch.Table)
	d.Batch.Key = strings.TrimSpace(d.Batch.Key)
	return d
}

// InputError kesalahan isi form. Field memakai nama field JSON.
type InputError struct {
	Field string
	Err   error
}

func (e *InputError) Error() string { return e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

// Message pesan buat user, tanpa awalan nama package.
func (e *InputError) Message() string {
	return strings.ReplaceAll(e.Err.Error(), "migration: ", "")
}

func invalid(field string, err error) error { return &InputError{Field: field, Err: err} }

// validate mengecek definisi migration tanpa menghubungi database target.
func validate(d Definition) error {
	if d.Description == "" {
		return invalid("description", ErrEmptyDescription)
	}
	if err := CheckSQL(d.UpSQL); err != nil {
		return invalid("upSql", fmt.Errorf("SQL up: %w", err))
	}
	if d.Irreversible {
		if d.IrreversibleReason == "" {
			return invalid("irreversibleReason", ErrReasonRequired)
		}
	} else {
		if d.DownSQL == "" {
			return invalid("downSql", ErrDownRequired)
		}
		if err := CheckSQL(d.DownSQL); err != nil {
			return invalid("downSql", fmt.Errorf("SQL down: %w", err))
		}
	}

	switch d.Mode {
	case ModeNormal:
		return nil
	case ModeBatch:
	default:
		return invalid("mode", fmt.Errorf("%w: mode harus normal atau batch", ErrInvalidBatch))
	}
	if _, err := tableIdent(d.Batch.Table); err != nil {
		return invalid("batch.table", err)
	}
	if !identPattern.MatchString(d.Batch.Key) {
		return invalid("batch.key", fmt.Errorf("%w: kolom kursor harus nama kolom biasa, contoh id", ErrInvalidBatch))
	}
	if d.Batch.Size < 1 || d.Batch.Size > MaxBatchSize {
		return invalid("batch.size", fmt.Errorf("%w: ukuran batch harus 1 sampai %d", ErrInvalidBatch, MaxBatchSize))
	}
	if !usesBatchRows(d.UpSQL) {
		return invalid("upSql", fmt.Errorf("%w: SQL up mode batch harus memakai tabel %s", ErrInvalidBatch, BatchRowsTable))
	}
	if d.DownSQL != "" && !usesBatchRows(d.DownSQL) {
		return invalid("downSql", fmt.Errorf("%w: SQL down mode batch harus memakai tabel %s", ErrInvalidBatch, BatchRowsTable))
	}
	return nil
}

// tableIdent menerima "tabel" atau "schema.tabel", cuma identifier biasa.
func tableIdent(name string) (pgx.Identifier, error) {
	parts := strings.Split(name, ".")
	if name == "" || len(parts) > 2 {
		return nil, fmt.Errorf("%w: tabel harus berbentuk tabel atau schema.tabel", ErrInvalidBatch)
	}
	for _, p := range parts {
		if !identPattern.MatchString(p) {
			return nil, fmt.Errorf("%w: nama tabel %q cuma boleh huruf, angka, dan _", ErrInvalidBatch, name)
		}
	}
	return pgx.Identifier(parts), nil
}

func usesBatchRows(sql string) bool {
	for _, words := range splitStatements(blankLiterals(sql)) {
		if hasWord(words, strings.ToUpper(BatchRowsTable)) {
			return true
		}
	}
	return false
}
