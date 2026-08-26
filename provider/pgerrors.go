package provider

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL error codes (see https://www.postgresql.org/docs/current/errcodes-appendix.html)
// this provider reacts to specifically, beyond just surfacing the error message.
const (
	pgErrDuplicateObject   = "42710" // CREATE ROLE on a role name that already exists
	pgErrDuplicateDatabase = "42P04" // CREATE DATABASE on a name that already exists
)

// isPgErrorCode reports whether err is a *pgconn.PgError with the given SQLSTATE code.
func isPgErrorCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
