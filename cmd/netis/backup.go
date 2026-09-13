package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"netis/internal/config"
	"netis/internal/store"
)

// runBackup writes a consistent copy of a SQLite database to a file.
//
// Copying netis.db with cp is not safe while netis is running: under WAL the
// committed state is split across the database file and its -wal sidecar, so a
// plain copy can miss recent writes or capture a torn page. VACUUM INTO asks
// SQLite itself for a complete, defragmented snapshot, and it can run against a
// live database.
//
// Postgres is not covered here on purpose: pg_dump already does this properly,
// including roles and ownership, and reimplementing a worse version of it
// inside netis would help nobody.
func runBackup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("netis backup", flag.ContinueOnError)
	to := fs.String("to", "", "destination file (must not already exist)")
	from := fs.String("from", "", "source database (default: NETIS_DB, else netis.db)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" {
		fs.Usage()
		return errors.New("-to is required")
	}
	dsn := *from
	if dsn == "" {
		dsn = config.Load().DSN
	}
	if store.IsPostgresDSN(dsn) {
		return fmt.Errorf("backing up Postgres is pg_dump's job: pg_dump %q > backup.sql", dsn)
	}

	dest, err := filepath.Abs(*to)
	if err != nil {
		return err
	}
	// VACUUM INTO refuses to overwrite, but failing here gives a better message
	// than SQLite's, and avoids opening the source at all.
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	st, err := store.Open(dsn)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer st.Close()

	// The destination is a literal in SQLite's syntax, not a bound parameter.
	if _, err := st.DB.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("vacuum into %s: %w", dest, err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", dest, info.Size())
	return nil
}
