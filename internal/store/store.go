package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer, avoids SQLITE_BUSY
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db}
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

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	// Foreign keys must be OFF during migrations: a table rebuild (drop+rename)
	// with enforcement on would cascade-delete child rows. PRAGMA foreign_keys
	// cannot be toggled inside a transaction, so toggle it around the whole
	// pass. Safe: migrations run once at startup on the single pooled
	// connection before the server serves any request. Open() turns it back on.
	if _, err := s.DB.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	for _, name := range names {
		var n int
		if err := s.DB.QueryRow(
			`SELECT count(*) FROM schema_migrations WHERE version=?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
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
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		return err
	}
	return tx.Commit()
}
