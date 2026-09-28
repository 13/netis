package store

import (
	"context"
	"errors"
)

// ErrBackupUnsupported is returned by VacuumInto on Postgres, whose backups
// are pg_dump's job.
var ErrBackupUnsupported = errors.New("backups of a Postgres database need pg_dump")

// VacuumInto writes a complete, consistent snapshot of a SQLite database to
// path, which must not already exist. It runs on the store's own connection,
// which is safe while the server is live: SQLite takes the snapshot inside a
// read transaction, so it sees one committed state including what is still in
// the WAL. The store holds a single SQLite connection, so other queries wait
// for the snapshot to finish, which for a netis-sized database is a moment.
func (s *Store) VacuumInto(ctx context.Context, path string) error {
	if s.dialect != SQLite {
		return ErrBackupUnsupported
	}
	_, err := s.exec(ctx, `VACUUM INTO ?`, path)
	return err
}
