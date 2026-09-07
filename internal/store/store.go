package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

type Store struct {
	DB *sql.DB
	// dialect decides placeholder rewriting, how new-row ids are read back,
	// and which migration directory applies. Set once by Open.
	dialect Dialect
}

// Dialect reports which backend this store is talking to.
func (s *Store) Dialect() Dialect { return s.dialect }

// IsPostgresDSN reports whether dsn addresses a Postgres server rather than a
// SQLite file. Anything that is not a Postgres URL — including ":memory:" and
// bare paths — is a SQLite database.
func IsPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// Open connects to the database named by dsn, runs any pending migrations for
// that backend, and returns a ready store. A Postgres URL selects the Postgres
// backend; any other value is a SQLite file path (or ":memory:").
//
// The backend is fixed for the life of the process: there is no runtime switch,
// and data does not move between backends on its own. Use `netis migrate-db` to
// copy an existing SQLite database into Postgres.
func Open(dsn string) (*Store, error) {
	if IsPostgresDSN(dsn) {
		return openPostgres(dsn)
	}
	return openSQLite(dsn)
}

func openSQLite(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer, avoids SQLITE_BUSY
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	// NORMAL is the recommended synchronous level under WAL (durable at
	// checkpoint, much cheaper per write); busy_timeout guards against an
	// external process (e.g. sqlite3 CLI) briefly locking the file.
	if _, err := db.Exec(`PRAGMA synchronous = NORMAL; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, dialect: SQLite}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	// Enforce foreign keys for all normal operation (migrations ran with them off).
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func openPostgres(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// A real server handles concurrent writers, so the SQLite single-connection
	// limit must not apply here — it would serialise every request.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	s := &Store{DB: db, dialect: Postgres}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// migrationDir is the embedded directory holding this backend's migrations.
// The two series share version numbers from 0005 on, so a later change is
// added under the same name to both.
func (s *Store) migrationDir() string { return "migrations/" + string(s.dialect) }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	dir := s.migrationDir()
	entries, err := migrationsFS.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	// SQLite only: foreign keys must be OFF during migrations, because a table
	// rebuild (drop+rename) with enforcement on would cascade-delete child
	// rows. PRAGMA foreign_keys cannot be toggled inside a transaction, so
	// toggle it around the whole pass. Safe: migrations run once at startup on
	// the single pooled connection before the server serves any request.
	// openSQLite turns it back on. Postgres needs none of this — its migrations
	// never rebuild a table.
	if s.dialect == SQLite {
		if _, err := s.DB.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
	}
	for _, name := range names {
		var n int
		if err := s.queryRow(context.Background(),
			`SELECT count(*) FROM schema_migrations WHERE version=?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		if err := s.applyMigration(name, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one migration file in a transaction: exec the script,
// verify no dangling foreign-key references, record the version, commit. Any
// error rolls back the whole file so a partial migration can't wedge the DB.
func (s *Store) applyMigration(name, script string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(script); err != nil {
		return fmt.Errorf("migration %s: %w", name, err)
	}
	// The FK check pairs with the PRAGMA above and is SQLite-only. On Postgres
	// constraints were enforced by the statements themselves.
	if s.dialect == SQLite {
		rows, err := tx.Query(`PRAGMA foreign_key_check`)
		if err != nil {
			return fmt.Errorf("migration %s fk check: %w", name, err)
		}
		violations := 0
		for rows.Next() {
			violations++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("migration %s fk check: %w", name, err)
		}
		rows.Close()
		if violations > 0 {
			return fmt.Errorf("migration %s: %d foreign key violation(s)", name, violations)
		}
	}
	if _, err := tx.Exec(s.dialect.rebind(
		`INSERT INTO schema_migrations (version) VALUES (?)`), name); err != nil {
		return err
	}
	return tx.Commit()
}
