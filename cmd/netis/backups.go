package main

import (
	"context"
	"log/slog"
	"sync"

	"netis/internal/backup"
	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/web"
)

// startBackups starts the scheduled backup loop when NETIS_BACKUP_DIR is set,
// tracked on wg so shutdown waits for a backup in progress. It returns the
// scheduler for the About tab and /metrics, or nil when backups are off.
//
// Postgres is skipped with a warning: its backups are pg_dump's job, as for
// `netis backup`.
func startBackups(ctx context.Context, wg *sync.WaitGroup, cfg config.Config, st *store.Store, evs *events.Service) web.BackupStatus {
	if cfg.BackupDir == "" {
		return nil
	}
	if st.Dialect() != store.SQLite {
		slog.Warn("NETIS_BACKUP_DIR is set but scheduled backups need SQLite; back up Postgres with pg_dump",
			"dir", cfg.BackupDir)
		return nil
	}
	s := backup.New(st, evs, cfg.BackupDir, cfg.BackupInterval, cfg.BackupKeep)
	s.Start(ctx, wg)
	slog.Info("scheduled backups enabled", "dir", cfg.BackupDir,
		"interval", cfg.BackupInterval, "keep", cfg.BackupKeep)
	return s
}
