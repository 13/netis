package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Dialect names the SQL backend a Store is talking to. The store's queries are
// written once in SQLite syntax; the differences that cannot be papered over in
// the SQL text itself (placeholders, last-insert-id) live in this file.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// rebind converts the `?` placeholders the store's queries are written with
// into Postgres's `$1..$n` form. SQLite takes the query unchanged.
//
// A `?` inside a single-quoted SQL string literal is left alone. A doubled
// quote, SQL's escape for a quote inside such a literal, needs no special case:
// it closes and immediately reopens the literal, so a single in-literal flag
// tracks it correctly.
func (d Dialect) rebind(q string) string {
	if d != Postgres || !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	inLiteral := false
	n := 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'':
			inLiteral = !inLiteral
			b.WriteByte(c)
		case c == '?' && !inLiteral:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// exec, query and queryRow are the store's entry points to the database: they
// rebind the query for the active dialect and otherwise behave exactly like
// their database/sql counterparts. Store methods must use these rather than
// s.DB directly, or their `?` placeholders will not survive on Postgres.
func (s *Store) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.DB.ExecContext(ctx, s.dialect.rebind(q), args...)
}

func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.DB.QueryContext(ctx, s.dialect.rebind(q), args...)
}

func (s *Store) queryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return s.DB.QueryRowContext(ctx, s.dialect.rebind(q), args...)
}

// insertReturningID runs an INSERT and reports the id of the new row. SQLite
// reads it back from the result; Postgres has no last-insert-id, so the
// statement is extended with RETURNING id and read as a query.
//
// The INSERT must therefore actually produce a row: a conditional insert whose
// row count can be zero (INSERT ... WHERE NOT EXISTS, ON CONFLICT DO NOTHING)
// has to use exec and RowsAffected instead, or Postgres will return
// sql.ErrNoRows where SQLite returns an id of 0.
func (s *Store) insertReturningID(ctx context.Context, q string, args ...any) (int64, error) {
	if s.dialect == Postgres {
		var id int64
		err := s.DB.QueryRowContext(ctx, s.dialect.rebind(q+` RETURNING id`), args...).Scan(&id)
		return id, err
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// eachRow runs a query and calls fn once per row, handling the close and the
// deferred row error that a bare Query loop is easy to get wrong. It exists for
// the bulk lookups that replaced per-row queries in ListDevices.
func (s *Store) eachRow(ctx context.Context, q string, fn func(*sql.Rows) error, args ...any) error {
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// conn is the subset of database/sql shared by *sql.DB and *sql.Tx, so the
// store's helpers can run either directly or inside a transaction.
type conn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) execOn(ctx context.Context, c conn, q string, args ...any) (sql.Result, error) {
	return c.ExecContext(ctx, s.dialect.rebind(q), args...)
}

// insertReturningIDOn is insertReturningID against an explicit connection; see
// that function for the constraint on which INSERTs may use it.
func (s *Store) insertReturningIDOn(ctx context.Context, c conn, q string, args ...any) (int64, error) {
	if s.dialect == Postgres {
		var id int64
		err := c.QueryRowContext(ctx, s.dialect.rebind(q+` RETURNING id`), args...).Scan(&id)
		return id, err
	}
	res, err := c.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// withTx runs fn inside a transaction, committing when it returns nil and
// rolling back on any error or panic.
//
// fn must do all of its work through the conn it is given. On SQLite the pool
// is capped at a single connection, so anything inside fn that reaches for
// s.DB — including another Store method — waits for a connection the
// transaction is already holding, and deadlocks.
func (s *Store) withTx(ctx context.Context, fn func(c conn) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
