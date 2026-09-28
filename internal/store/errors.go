package store

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// IsUniqueViolation reports whether err is a unique or primary-key constraint
// failure on either backend: a duplicate MAC, subnet CIDR or username. Callers
// answer it as a conflict rather than a server failure.
func IsUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23505" // unique_violation
	}
	return false
}

// IsForeignKeyViolation reports whether err is a foreign-key constraint
// failure on either backend, such as a parent device id that names no device.
func IsForeignKeyViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23503" // foreign_key_violation
	}
	return false
}
