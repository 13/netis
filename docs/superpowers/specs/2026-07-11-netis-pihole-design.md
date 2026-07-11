# Netis — Pi-hole Integration Module: Design

Date: 2026-07-11
Status: Approved design, pre-implementation

## Purpose

Add a Pi-hole integration to netis that pulls three datasets from a Pi-hole
v6 instance and merges them into the netis device inventory, read-only:

- **DHCP active leases** — currently-leased devices (IP, MAC, hostname, expiry).
- **DHCP static reservations** — configured MAC→IP reservations.
- **Local DNS A records** — hostname→IP mappings from Pi-hole's local DNS.

This enriches netis with the names, addresses, and static/dynamic status
that Pi-hole already knows, without the user re-entering them.

## Architecture

Mirror the existing `internal/proxmox` and `internal/wireguard` integrations
exactly — no new architecture:

- New package `internal/pihole` with a `Client` (HTTP, Pi-hole v6 REST API)
  and a `Sync` (`RunOnce(ctx)` + `Start(ctx, interval)`).
- Config from settings keys, read at startup; the poller starts only when
  `pihole_url` is set.
- The poller emits a `scan_error` event once per outage (reset on success),
  the same failing-flag pattern as the other syncs.
- Reconciliation talks only to the store and the events service.

Known limitation (inherited from the other integrations): an IP is only
attached to a netis subnet whose CIDR contains it (`netip` Contains). IPs
with no matching subnet are skipped — the user must have created the LAN
subnet in netis first.

## Component 1: Pi-hole v6 API client (`internal/pihole/client.go`)

`NewClient(baseURL, password string, insecure bool) *Client`. `insecure`
skips TLS verify (self-signed certs are common); TLS is secure by default.

**Auth.** Pi-hole v6 uses session auth. The client does not hold a token; it
logs in on demand:

- `POST {base}/api/auth` with body `{"password": "<app-password>"}` returns
  `{"session": {"sid": "<sid>", "valid": true}}`.
- The client caches the SID and sends it on every request via the
  `X-FTL-SID` header (also accepted as the `sid` query param).
- On a `401`, the client re-authenticates once and retries the request; a
  second `401` is returned as an error.

**Reads** (all GET, JSON):

- `GET /api/dhcp/leases` → `{"leases": [{"ip","hwaddr","name","expires"}]}`.
  Parsed into `Lease{IP, MAC, Hostname string; Expiry int64}`.
- `GET /api/config/dhcp/hosts` → `{"config": {"dhcp": {"hosts": ["AA:BB:CC:DD:EE:FF,10.0.0.5,name", ...]}}}`.
  Each entry is comma-separated `MAC,IP[,hostname]`. Parsed into
  `Reservation{MAC, IP, Hostname string}`.
- `GET /api/config/dns/hosts` → `{"config": {"dns": {"hosts": ["10.0.0.5 name", ...]}}}`.
  Each entry is `IP hostname` (space-separated, one or more names). Parsed
  into `DNSRecord{IP, Name string}` (one record per IP/name pair).

MACs are normalized lowercase colon-separated; IPs canonicalized via
`netip.Addr.String()`. Malformed entries are skipped, not fatal.

Client methods: `Leases(ctx) ([]Lease, error)`, `Reservations(ctx) ([]Reservation, error)`,
`DNSRecords(ctx) ([]DNSRecord, error)`.

## Component 2: Reconciliation (`internal/pihole/sync.go`)

`NewSync(st *store.Store, c *Client, ev *events.Service) *Sync`.
`RunOnce(ctx)` reads all three datasets, loads the netis subnets once, then
reconciles. `Start(ctx, interval)` loops `RunOnce`, emitting one
`scan_error` per outage.

Subnet matching: for any IP, find the netis subnet whose parsed CIDR
`Contains` it. No match → skip that item.

**DHCP reservations and leases (matched by MAC).** Process reservations
first, then leases, so a reservation's `static` wins over a lease's `dhcp`
for the same MAC.

1. `FindIfaceByMAC(mac)` hits → enrich the existing device (whatever its
   source):
   - `UpsertIPAssignment(ifaceID, subnetID, ip, kind)` where `kind` is
     `static` for a reservation, `dhcp` for a lease.
   - `SetIfaceHostnameIfEmpty(ifaceID, hostname)` when the item has a
     hostname.
2. No MAC match → auto-create: `CreateDevice{Name: hostname or "pihole-<mac>",
   Kind: "other", Source: "pihole"}`, `AddIface(devID, &mac, hostname)`,
   `UpsertIPAssignment(...)`, then emit a `device_new` event.

**Local DNS A records (matched by IP).**

- `FindIfaceByIP(subnetID, ip)` hits → `SetIfaceHostnameIfEmpty(ifaceID, name)`
  and `SetCustomField(deviceID, "pihole_dns", name)` (visible without
  clobbering a user-set hostname).
- No device at that IP → **skip**. DNS records never create MAC-less phantom
  devices.

**Does not touch `iface_status`.** A DHCP lease means "has an address," not
"is online" — liveness stays the scanner's responsibility. The Pi-hole sync
never writes online/last_seen, so the two never conflict.

Idempotent (a second `RunOnce` produces no changes and no duplicate rows).
Never deletes devices, ifaces, or assignments.

## Component 3: Store additions (`internal/store/device.go`)

- `UpsertIPAssignment(ifaceID, subnetID int64, ip, kind string) error` —
  `SELECT id FROM ip_assignment WHERE iface_id=? AND subnet_id=? AND ip=?`;
  if a row exists, `UPDATE ... SET kind=?`; else `INSERT`. Uses
  `errors.Is(sql.ErrNoRows)` to distinguish absent from error. Provides both
  idempotency and the dhcp→static upgrade.
- `SetIfaceHostnameIfEmpty(ifaceID int64, hostname string) error` —
  `UPDATE iface SET hostname=? WHERE id=? AND (hostname IS NULL OR hostname='')`.
  Never overwrites an existing hostname.

## Component 4: Migration + transactional migrate loop

**`internal/store/migrations/0002_pihole_source.sql`.** `device.source` has
`CHECK (source IN ('manual','scan','proxmox','wireguard'))`. SQLite cannot
`ALTER` a CHECK constraint, so this migration rebuilds the table with the
widened constraint. The file contains exactly these statements, in order:

1. `CREATE TABLE device_new (...)` — column definitions byte-identical to the
   0001 `device` table (same columns, types, defaults, `parent_device_id`
   self-reference, `created_at` default) except the source CHECK is
   `CHECK (source IN ('manual','scan','proxmox','wireguard','pihole'))`.
2. `INSERT INTO device_new SELECT * FROM device;`
3. `DROP TABLE device;`
4. `ALTER TABLE device_new RENAME TO device;`

The 0001 schema defines no secondary index on `device` (only the implicit
primary key), so no index recreation is needed. Child tables (`iface`,
`device_tag`, `custom_field`, `device_link`, `event`) reference `device(id)`;
the rebuild is safe only with foreign-key enforcement disabled (see below) —
otherwise `DROP TABLE device` performs an implicit delete that cascades into
every child table and wipes their rows.

**Transactional migrate loop with FK handling (`internal/store/store.go`).**
Because 0002 is the first genuinely multi-statement migration and requires
foreign keys off during the table rebuild, the migrate loop is restructured.
`PRAGMA foreign_keys` cannot be changed inside a transaction, so FK toggling
happens outside the per-migration transactions, around the whole pass:

1. `Open` runs `migrate()` before enabling foreign keys for normal operation.
2. `migrate()`: `PRAGMA foreign_keys=OFF;` (outside any transaction), then for
   each pending migration file: `BEGIN;` exec the file; insert the
   `schema_migrations` version row; `PRAGMA foreign_key_check;` (aborts the
   transaction if the rebuild left a dangling reference); `COMMIT;`. On any
   error: `ROLLBACK` and abort the whole pass with the error.
3. After the loop: `PRAGMA foreign_keys=ON;` so normal operation enforces FKs.

This is safe because migrations run once at startup on the single pooled
connection (`SetMaxOpenConns(1)`) before the server serves any request. The
result: FKs are enforced at runtime as before, the table rebuild no longer
cascade-deletes child rows, each migration is atomic, and a partial failure
can no longer wedge the DB. This is a targeted fix to code being modified for
this feature, not an unrelated refactor.

## Component 5: Config, wiring, settings UI

**Settings keys:** `pihole_url`, `pihole_password`, `pihole_insecure`.
- `pihole_password` is a secret: handled exactly like `proxmox_secret` — never
  echoed back into the form, and a blank submission keeps the stored value.
- `pihole_insecure` is normalized from the checkbox `"on"` to `"1"` on save
  (matching the Proxmox `insecure` handling), and the template pre-checks on
  `== "1"`.
- All three added to `settingsKeys`; a Pi-hole subsection is added to the
  integrations section of `settings.templ`.

**Wiring (`cmd/netis/main.go`).** After the WireGuard block, conditional on a
non-empty `pihole_url`:

```
if phURL, _ := st.GetSetting("pihole_url"); phURL != "" {
    phPass, _ := st.GetSetting("pihole_password")
    phInsecure, _ := st.GetSetting("pihole_insecure")
    ph := pihole.NewSync(st, pihole.NewClient(phURL, phPass, phInsecure == "1"), evs)
    go ph.Start(ctx, time.Minute)
}
```

## Error handling

- API/auth failures surface from `RunOnce`; `Start` emits one `scan_error`
  event per outage and resets on the next success — identical to the Proxmox
  and WireGuard syncs.
- Per-item parse failures (malformed lease/reservation/DNS entry) are skipped
  and do not abort the run.
- An IP outside every netis subnet is skipped silently (expected, common).

## Testing

- **Client** (`client_test.go`): `httptest` fixture server serving
  `/api/auth` (returns a SID) and the three read endpoints from recorded
  fixtures; assert correct parsing of leases/reservations/DNS records, and
  that a `401` triggers exactly one re-auth then retry.
- **Sync** (`sync_test.go`, fake client): reservation → `static`, lease →
  `dhcp`, reservation-beats-lease for the same MAC, unmatched MAC → device
  created with `source='pihole'` + `device_new` event, DNS A record → iface
  hostname set + `pihole_dns` custom field, DNS record with no matching
  device → skipped, **idempotent** (two `RunOnce` calls → identical state, no
  duplicate rows), and an assertion that `iface_status` rows are never
  created/modified by the sync.
- **Store** (`device_test.go`): `UpsertIPAssignment` inserts once, does not
  duplicate on re-run, and upgrades `dhcp`→`static`;
  `SetIfaceHostnameIfEmpty` sets when empty and does not overwrite when set.
- **Migration** (`store_test.go`): after `Open`, a `device` row with
  `source='pihole'` inserts successfully; existing rows survive the rebuild;
  and a deliberately failing migration leaves no partial state (transactional
  rollback).

## Project layout (files added / modified)

- Create: `internal/pihole/client.go`, `internal/pihole/sync.go`,
  `internal/pihole/client_test.go`, `internal/pihole/sync_test.go`,
  `internal/store/migrations/0002_pihole_source.sql`.
- Modify: `internal/store/device.go` (+2 methods, +tests),
  `internal/store/store.go` (transactional migrate loop, +test),
  `internal/web/settings.go` (settings keys + secret/insecure handling),
  `internal/web/views/settings.templ` (Pi-hole subsection),
  `cmd/netis/main.go` (wiring), `README.md` (Pi-hole settings keys +
  limitations note).

## Out of scope

- Writing back to Pi-hole (read-only pull only).
- Local CNAME records (A records only).
- Using DHCP lease presence as an online/liveness signal (scanner's job).
- Pi-hole v5 / file-over-SSH access (v6 REST API only).
