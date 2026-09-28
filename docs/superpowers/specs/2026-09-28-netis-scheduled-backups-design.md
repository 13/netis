# Netis — Scheduled Backups (F4): Design

Date: 2026-09-28
Status: Approved (standing authorization), pre-implementation

## Purpose

`netis backup` takes a safe SQLite snapshot, but only when something outside
netis runs it (cron, a systemd timer). Most home installs never set that up.
F4 lets the running server take the snapshots itself, on a fixed interval, and
keep the newest few.

## Configuration (env, `internal/config`)

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_BACKUP_DIR` | unset | Directory for scheduled backups. Unset = feature off. |
| `NETIS_BACKUP_INTERVAL` | `24h` | Go duration between backups. Unparseable or under 1m falls back to the default. |
| `NETIS_BACKUP_KEEP` | `7` | Backups kept; older ones are deleted. Unparseable or < 1 falls back to the default. |

## Behaviour

- New package `internal/backup` owns the scheduler, naming and pruning.
- Snapshot goes through the server's open store connection:
  `Store.VacuumInto(ctx, path)` runs `VACUUM INTO ?`, the same statement
  `netis backup` uses. Running it on the live connection is safe under WAL and
  avoids a second writer-free handle racing the server. A second read-only
  handle is what the CLI needs because it runs out of process; the server does
  not.
- File name `netis-YYYYMMDD-HHMMSS.db` (UTC). Written as
  `.netis-YYYYMMDD-HHMMSS.db.tmp` in the same directory, fsynced, then renamed,
  so a crash never leaves a half-written file under a backup name. The temp
  file is removed on failure.
- After a successful backup, prune: list names matching
  `^netis-\d{8}-\d{6}\.db$`, sort (lexical = chronological), delete all but the
  newest KEEP. Anything else in the directory is left alone.
- Schedule: at startup the newest existing backup's timestamp seeds "last
  success"; the first run is due at `last + interval` (immediately if there is
  none or it is overdue). So restarts don't take a backup each time, and a
  long-stopped instance catches up at once. Afterwards every interval.
- The directory is created (0750) if missing.
- Postgres: log one warning that scheduled backups need `pg_dump` and are
  skipped; the scheduler is not started.
- The loop joins the shutdown WaitGroup (`&bg`) in `main.go`, like retention.

## Visibility

- Scheduler keeps an in-memory `Status{Enabled, LastAttempt, LastSuccess, OK, File}`.
- Failure: logged with the full error; one `scan_error` event per outage
  ("scheduled backup failing"), like integration syncs. Event text and UI carry
  no paths or raw error (every signed-in user can read them).
- `/metrics` (only when enabled): `netis_last_backup_timestamp_seconds` (last
  success, 0 if none) and `netis_last_backup_success` (1/0, last attempt).
- Settings → About, Runtime card: a "Last backup" row — "disabled",
  "not available on Postgres (use pg_dump)", "none yet", or
  "<time> (<file>)" / "failed at <time>, see server log".
- Web gets the status through `web.Options.Backups` (an interface with
  `Status() backup.Status`); nil means disabled.

## Tests

- Backup file created, valid SQLite, contains data; no temp file left.
- Pruning keeps the N newest and ignores unrelated files.
- Disabled when dir is empty (no scheduler, About says disabled, no metrics).
- Failure is recorded (status not OK, event emitted once) when the directory
  is unwritable.
- Config parsing defaults and fallbacks.
