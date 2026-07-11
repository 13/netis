# Netis — Dashboard Enhancements: Design

Date: 2026-07-11
Status: Approved design, pre-implementation

## Purpose

Enrich the netis dashboard so it answers "is everything healthy and what
needs my attention?" at a glance:

- An **integration status widget** showing, per integration
  (`proxmox`/`wireguard`/`pihole`/`scan`), when it last ran, whether it
  succeeded, and a short count/detail.
- A **summary stats row** (total devices, online, offline, unknown/new,
  subnets).
- An **attention list** of newly-discovered unknown devices and IP conflicts.
- **Live refresh** of all of the above over SSE, so the dashboard updates as
  scans and syncs run instead of only on page reload.

## Architecture

The load-bearing change is that sync/scan status is currently invisible to the
web layer: each `Sync` keeps an in-memory `failing` bool and `RunOnce` returns
only an `error`. This design persists status in a new `integration_status`
table, has each background loop record status after every run, and renders the
dashboard body as a single SSE-refreshed fragment.

No new shared objects are plumbed between the background loops and the web
server — the loops write status through the `*store.Store` they already hold,
and the web handler reads it back.

## Component 1: Status persistence

### Table `integration_status` (migration `0003_integration_status.sql`)

The transactional migrate loop (added with the Pi-hole `0002` migration) makes
a third migration safe. New table:

```sql
CREATE TABLE integration_status (
  name TEXT PRIMARY KEY,                 -- proxmox | wireguard | pihole | scan
  last_run TEXT NOT NULL,                -- UTC RFC3339
  ok INTEGER NOT NULL DEFAULT 0,         -- 1 = last run succeeded
  detail TEXT NOT NULL DEFAULT '',       -- error message, or a short summary
  item_count INTEGER NOT NULL DEFAULT 0  -- primary count for the summary line
);
```

This migration is a plain `CREATE TABLE` (no table rebuild), so it needs no
foreign-key handling beyond what the existing migrate loop already provides.

### Store methods (`internal/store/status_integration.go`)

- `type IntegrationStatus struct { Name, LastRun, Detail string; OK bool; ItemCount int }`
- `func (s *Store) SetIntegrationStatus(st IntegrationStatus) error` — upsert
  on `name` (`INSERT ... ON CONFLICT(name) DO UPDATE SET ...`).
- `func (s *Store) ListIntegrationStatus() ([]IntegrationStatus, error)` —
  ordered by `name`.

## Component 2: Recording status from the background loops

Each sync's `RunOnce` changes from returning `error` to returning a small
per-package stats value plus an error, so the loop can record counts:

- `proxmox`: `type Stats struct { Guests, Nodes int }`; `RunOnce(ctx) (Stats, error)`.
- `wireguard`: `type Stats struct { Peers int }`; `RunOnce(ctx) (Stats, error)`.
- `pihole`: `type Stats struct { Leases, Reservations, DNSRecords, Created int }`;
  `RunOnce(ctx) (Stats, error)`.

Each `Start` loop, after every `RunOnce`, calls `SetIntegrationStatus`:

- Success → `ok=1`, `last_run=now`, `item_count` = the package's primary count
  (pihole: `Leases`; proxmox: `Guests`; wireguard: `Peers`), `detail` = a short
  human summary (e.g. `"12 guests, 3 nodes"`, `"48 leases, 5 new"`).
- Failure → `ok=0`, `last_run=now`, `detail` = the error string, `item_count`
  unchanged from the struct's zero value (0).

The existing once-per-outage `scan_error` event behavior is preserved
alongside the status write (the event log still records failures).

The **scan scheduler** runs subnets individually on its ticker (via
`run(ctx, sn)`), so there is no clean "pass". It records the single `scan`
status row after each subnet sweep (latest-wins): `ok` reflects that sweep's
`RunSubnet` result, `last_run=now`, `detail` = the swept subnet's CIDR (e.g.
`"scanned 192.168.1.0/24"`), `item_count=0` (the engine does not return a
count, and changing `RunSubnet`'s signature is out of scope). A failed sweep
records `ok=0` + the error.

After writing status, the sync/scheduler publishes the `dashboard` SSE topic
via the `events.Service` it already holds (`events.Service` exposes
`Broker()`). The store method stays pure (no broker dependency).

## Component 3: Dashboard widgets (`internal/web/dashboard.go`, `views/dashboard.templ`)

The dashboard page shell stays; its dynamic body becomes one fragment. Layout
top-to-bottom:

1. **Summary stats row** — tiles for total devices, online, offline,
   unknown/new, subnets. Computed from a single `ListDevices()` call: total =
   len; online = count with `Online`; offline = total − online; unknown =
   count with `Source=="scan"` and name matching `unknown-*`; subnets =
   `len(ListSubnets())`.
2. **Two-column band:**
   - **Integration status** — `ListIntegrationStatus()`; per row: name, a
     green (`ok`) / red (`!ok`) badge, relative "last run" from `last_run`,
     and `detail`. No rows → the widget renders nothing (unconfigured
     integrations never wrote a status row).
   - **Attention list** — unknown devices (source=`scan`, name `unknown-*`,
     each linking to `/devices/{id}`) and IP conflicts (any subnet IP whose
     `SubnetOccupancy` `Count>1`, linking to `/subnets/{id}`). Empty → "nothing
     needs attention".
3. **Subnet cards** — the existing per-subnet online/used/free cards.
4. **Recent events** — the existing 15-event table.

Relative time is a small template helper `relTime(rfc3339 string) string`
("just now", "3m ago", "2h ago", "5d ago"; falls back to the raw string on
parse failure).

### Fragment route

- `GET /` (`handleDashboard`) renders the full page: shell + the fragment.
- `GET /dashboard/widgets` (`handleDashboardWidgets`) renders only the
  fragment body. Both require auth (no admin gate — read-only view).
- The page wraps the fragment in
  `<div id="dash" hx-get="/dashboard/widgets" hx-trigger="sse:dashboard, sse:events">…</div>`
  so it re-fetches when either topic fires. `events` is already published by
  `events.Service.Emit`; `dashboard` is the new topic from Component 2.

Both handlers share one assembly function that returns a
`views.DashboardData` (stats, integration statuses, attention items, subnet
rows, events), so the page and the fragment never diverge.

## Error handling

- Status writes in the loops are best-effort with respect to the run itself: a
  `SetIntegrationStatus` failure is logged but does not abort or fail the sync
  (the sync already succeeded/failed on its own terms). It is not swallowed
  silently — it is logged.
- The dashboard handlers surface store-read errors as HTTP 500, matching the
  existing handler style.
- `relTime` never errors: on an unparseable timestamp it returns the input
  unchanged.

## Testing

- **Store** (`status_integration_test.go`): `SetIntegrationStatus` inserts then
  updates the same `name` (no duplicate row, fields updated);
  `ListIntegrationStatus` returns rows ordered by name. Migration `0003`
  applies cleanly against a populated DB (reuse the existing
  populated-migration test pattern).
- **Syncs** (each package's `sync_test.go`): `RunOnce` returns the correct
  `Stats` counts for a fixture; a `Start`-level or direct test that a
  successful run writes an `integration_status` row with `ok=1` and the
  expected `item_count`, and a failing run writes `ok=0` + the error detail.
- **Web** (`dashboard_test.go`): `GET /dashboard/widgets` renders the stats
  numbers, an integration status row (seeded via `SetIntegrationStatus`), an
  unknown device in the attention list, and a conflict IP; `GET /` still 200s
  and contains the fragment container. `relTime` unit tests for the bucket
  boundaries and the parse-failure fallback.

## Project layout (files added / modified)

- Create: `internal/store/migrations/0003_integration_status.sql`,
  `internal/store/status_integration.go` (+`status_integration_test.go`).
- Modify: `internal/proxmox/sync.go`, `internal/wireguard/sync.go`,
  `internal/pihole/sync.go` (RunOnce→Stats, Start records status + publishes
  `dashboard`) and their tests; `internal/scan/scheduler.go` (record `scan`
  status + publish); `internal/web/dashboard.go` + `internal/web/views/dashboard.templ`
  (widgets, `relTime`, shared assembly, fragment) + `dashboard_test.go`;
  `internal/web/server.go` (add the `/dashboard/widgets` route).
- `cmd/netis/main.go` is unchanged — the syncs already receive the store and
  the events service.

## Out of scope

- Configurable/rearrangeable widgets — fixed layout.
- Historical status/uptime graphs for integrations (only the latest run is
  stored).
- Per-widget SSE granularity — the whole dashboard body is one refreshed
  fragment.
- Notifications/alerting beyond the existing in-app event log.
