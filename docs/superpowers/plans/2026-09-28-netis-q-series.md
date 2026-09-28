# netis Q-series: audit fixes and features

Source: three read-only audits on 2026-09-28 (web/auth, scan/store/integrations,
tests/CI/ops/UX) against `35a281f`. One branch per item (`q<N>-<slug>`), merged
locally with `--no-ff` as `Merge Q<N>: <summary>`, same as the P-series.

Every item: TDD where testable; `make test`, `go test -race ./...`, `make lint`
green; store changes also run against Postgres
(`NETIS_TEST_PG_DSN=postgres://netis:netis@127.0.0.1:55433/netis?sslmode=disable`).
New migrations go in **both** `migrations/sqlite/` and `migrations/postgres/`.
Store methods use `s.exec/s.query/s.queryRow`, never `s.DB`. Integration syncs
never overwrite user-set values.

## Batch 1 — broken features

- **Q1** Proxmox/WireGuard raw `s.store.DB.QueryRowContext` with `?` breaks on
  Postgres (`proxmox/sync.go:87,105`, `wireguard/sync.go:85`). Move to store
  methods; Postgres test.
- **Q2** Integration loop and Run-now bypass `recordStatus`; no status rows, no
  failing events, Run-now says "not configured". Route both through one runner
  that records status, applies a per-run deadline, and holds a per-integration
  lock (no overlapping runs). Shutdown waits for integration + retention
  goroutines before closing the store.
- **Q3** Pi-hole client logs in every minute and never logs out; session slots
  exhaust. Reuse one client / log out after each run.
- **Q4** Scan correctness: known IP with different ARP MAC treated as the old
  device; ping setup errors counted as "down" (all devices go offline); MarkSeen
  read-then-write races under parallel subnet scans (use conditional UPDATE +
  rows affected).
- **Q5** Proxmox sync rewrites name/kind/parent every minute, clobbering edits.
  Set only on create.

## Batch 2 — security

- **Q6** Login limiter: reserve attempt atomically, per-username bucket, IPv6
  keyed by /64; limiter on current-password check; `MaxBytesReader` on bodies;
  server `ReadHeaderTimeout`/read deadline (SSE exempt); >72-byte passwords 400.
- **Q7** SSE stream re-checks session on keepalive and closes when revoked;
  session tokens stored as sha256 (migration 0008 both dialects); SSH dial
  honours context + handshake deadline; validate `wg_iface`.
- **Q8** Viewers: hide admin forms/config values in settings; integration error
  detail generic in UI, full in log; backup redacts DSN; SSE multi-line data
  split; Prometheus label escaping per text format.

## Batch 3 — integrity and hardening

- **Q9** Pi-hole lease move removes stale DHCP IP; Proxmox adopts iface from
  unreviewed scan device; device create validates MAC/IP and reports errors;
  constraint violations → 400/409; stale open ports deleted after port scan;
  partial unique indexes on `wg_pubkey`/`proxmox_vmid`, lookup indexes;
  SQLite pragmas in DSN.
- **Q10** `netis backup` opens source read-only, no migrate, fails if missing;
  README restore steps; systemd hardening + EnvironmentFile; `.dockerignore`;
  image sets `NETIS_PRIVILEGED_ICMP=1`; release gated on tests.
- **Q11** UI: delete confirmations; theme follows OS when unset; mobile nav;
  dialog a11y (role, focus trap/return, focus-visible); viewers not shown admin
  buttons.
- **Q12** Tests for untested admin handlers, WireGuard SSH Run, integration loop
  lifecycle; CI coverage summary.

## Batch 4 — features (sensible defaults, noted per spec)

- **F1** Alerts: webhook + ntfy notifier for new device, offline, IP conflict;
  settings UI; per-type toggles.
- **F2** API tokens (hashed, per-user, admin-created) for `/api/*`; write
  endpoints for devices; CSV/JSON export and CSV import.
- **F3** Discovery: flag locally-administered (randomized) MACs; online
  hysteresis / TCP fallback for ping-ignoring hosts.
- **F4** Scheduled backups with retention (`NETIS_BACKUP_DIR`, interval, keep N).
- **F5** Change user role; audit log of admin actions.
- **F6** Wake-on-LAN directed broadcast per subnet; flag Proxmox guests /
  WireGuard peers that vanished upstream.
- **F7** Extra discovery sources (mDNS names, IPv6 neighbours, more DHCP
  sources, OIDC) — scoped per spec when reached.
