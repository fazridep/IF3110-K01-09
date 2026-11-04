// Package pgerr memetakan kode error PostgreSQL yang dipakai lintas fitur.
package pgerr

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

func IsUniqueViolation(err error) bool {
	return hasCode(err, codeUniqueViolation)
}

func IsForeignKeyViolation(err error) bool {
	return hasCode(err, codeForeignKeyViolation)
}

func hasCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
