# Backups and database backends

Choosing SQLite or Postgres, moving between them, and taking, scheduling and restoring backups.

[← Back to README](../README.md)

- [Database backends](#database-backends)
- [Manual backups](#manual-backups)
- [Scheduled backups](#scheduled-backups)
- [Restoring a backup](#restoring-a-backup)
- [Postgres backups](#postgres-backups)

## Database backends

netis runs on SQLite (the default) or PostgreSQL. `NETIS_DB` decides which:
a filesystem path is a SQLite database, a `postgres://` URL is a Postgres
server.

```sh
NETIS_DB=/var/lib/netis/netis.db ./netis                       # SQLite
NETIS_DB='postgres://netis:secret@db:5432/netis' ./netis       # Postgres
```

The backend is chosen once at startup, so switching means restarting netis
with a different `NETIS_DB`. Nothing moves between the two on its own: point
netis at an empty Postgres database and it creates its schema and starts
fresh. The Postgres connection pool is sized with `NETIS_DB_MAX_OPEN_CONNS`
and `NETIS_DB_MAX_IDLE_CONNS` (see [Configuration](configuration.md)).

To carry an existing SQLite database over instead, stop netis and run the
one-shot importer, then restart with the Postgres URL:

```sh
netis migrate-db --from /var/lib/netis/netis.db \
                 --to 'postgres://netis:secret@db:5432/netis'
```

It creates the schema on the destination, copies every table preserving ids
and foreign keys, and advances the id sequences past the imported rows. It
refuses to write into a database that already holds netis rows unless
`--force` is given. The source database is only read.

SQLite remains the right default for a single netis instance: it is a file,
needs no server, and the binary stays static. Postgres is worth it when the
database has to live outside the container, be backed up by existing
infrastructure, or be read by something else.

## Manual backups

SQLite: copying `netis.db` with `cp` while netis is running is not safe — under
WAL the committed state is split between the database and its `-wal` sidecar.
Use the built-in snapshot, which works against a live database:

```sh
netis backup --to /backups/netis-$(date +%F).db
```

The source is `--from`, or `NETIS_DB` when that is not given. It must be an
existing database file: the backup opens it read-only, so it never creates a
database, and never migrates one — running a newer netis binary's `backup`
leaves the live schema alone. An existing destination file is refused rather
than overwritten.

In Docker, run the binary inside the container; `NETIS_DB` already points at
`/data/netis.db` there, so the backup lands in the same volume:

```sh
docker exec netis /netis backup --to /data/backup-$(date +%F).db
```

To take one nightly, a cron entry (note the escaped `%`):

```
15 3 * * * docker exec netis /netis backup --to /data/backup-$(date +\%F).db
```

or a systemd timer on a native install:

```ini
# /etc/systemd/system/netis-backup.service
[Service]
Type=oneshot
User=netis
ExecStart=/bin/sh -c '/opt/netis/netis backup --from /var/lib/netis/netis.db --to /var/lib/netis/backup-$(date +%%F).db'

# /etc/systemd/system/netis-backup.timer
[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

Enable it with `systemctl enable --now netis-backup.timer`. Neither prunes old
backups (the [scheduled backups](#scheduled-backups) below do); delete them on
whatever schedule suits you, and copy them off the machine.

## Scheduled backups

netis can take these snapshots itself. Set `NETIS_BACKUP_DIR` and the server
writes `netis-YYYYMMDD-HHMMSS.db` (UTC) there every `NETIS_BACKUP_INTERVAL`
(default `24h`), keeping the newest `NETIS_BACKUP_KEEP` (default `7`). Each
file is the same `VACUUM INTO` snapshot as `netis backup`, taken through the
server's own database connection, written under a temporary name and renamed
into place once complete, so a crash never leaves a half-written backup.
Pruning only deletes files matching that name pattern; anything else in the
directory is left alone.

The first backup is due one interval after the newest one already in the
directory, so restarts do not take extra backups and an instance that was
stopped for longer catches up at once. A failed backup is retried after at most
an hour, logged in full, and announced once as a `scan_error` event. Settings →
System shows the last backup (or the failure), and `/metrics` exports
`netis_last_backup_timestamp_seconds` and `netis_last_backup_success` for
alerting on stale backups (see [Prometheus metrics](api.md#prometheus-metrics)).

In Docker, point it at a directory on a volume — ideally a different one from
the database, so it can be copied off or mounted from the host:

```sh
docker run -d --name netis \
  --network host \
  -v netis-data:/data \
  -v /srv/backups/netis:/backups \
  -e NETIS_DB=/data/netis.db \
  -e NETIS_BACKUP_DIR=/backups \
  ghcr.io/13/netis:latest
```

> [!IMPORTANT]
> With Postgres the setting is ignored with a warning at startup: use `pg_dump`
> (see [Postgres backups](#postgres-backups)).

Restore a scheduled backup exactly like a manual one; see
[Restoring a backup](#restoring-a-backup).

## Restoring a backup

To restore a SQLite backup (manual or scheduled):

1. Stop netis (`systemctl stop netis`, or `docker stop netis`).
2. Copy the backup over the database file, e.g.
   `cp backup-2026-01-01.db /var/lib/netis/netis.db` (keep the owner the
   netis user). The image has no shell, so with Docker do steps 2 and 3
   from a throwaway container on the same volume:
   `docker run --rm -v netis-data:/data alpine sh -c 'cp /data/backup-2026-01-01.db /data/netis.db && rm -f /data/netis.db-wal /data/netis.db-shm'`.
3. Delete the `netis.db-wal` and `netis.db-shm` files next to it if present:
   they belong to the old database, and SQLite would replay the old WAL on top
   of the restored one.
4. Start netis again and check the dashboard and device list. If
   `NETIS_SECRET_KEY` was set when the backup was taken, the same key must be
   set now (see [Encrypting stored credentials](security.md#encrypting-stored-credentials)).

With the Docker example from [Scheduled backups](#scheduled-backups), mount
both volumes in step 2:
`docker run --rm -v netis-data:/data -v /srv/backups/netis:/backups alpine sh -c 'cp /backups/netis-20260928-031500.db /data/netis.db && rm -f /data/netis.db-wal /data/netis.db-shm'`.

A backup from an older release is fine: netis migrates it forward on start.

## Postgres backups

> [!IMPORTANT]
> `netis backup` and `NETIS_BACKUP_DIR` only cover SQLite. For Postgres, use
> `pg_dump`, which already handles this properly.

```sh
pg_dump "$NETIS_DB" > /backups/netis-$(date +%F).sql
```
