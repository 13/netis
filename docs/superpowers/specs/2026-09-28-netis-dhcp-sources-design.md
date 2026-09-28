# Netis — More DHCP Lease Sources (AdGuard Home, OPNsense): Design

Date: 2026-09-28
Status: Approved (standing authorization), implemented on `f7b-dhcp-sources`

## Purpose

Pi-hole is the only DHCP server netis can read leases from. Many home
networks hand out addresses from AdGuard Home or from the router itself, and
OPNsense is the most common self-hosted router. This adds both as integrations
that behave exactly like Pi-hole: a lease creates or enriches a device by MAC,
a reservation marks its address static, and nothing the user set is ever
overwritten.

## Shared lease handling: `internal/leases`

The lease-apply half of `pihole.Sync.RunOnce` moves, unchanged, into
`leases.Apply(ctx, store, events, source, subnets, static, dynamic)`:

- Entries outside every configured subnet are skipped.
- Static entries (reservations) are applied first with kind `static`; the
  `(subnet, ip)` pairs they claim are remembered, and a dynamic lease for one
  of them only fills an empty hostname — so a reservation is never downgraded.
- A dynamic lease is upserted as `dhcp` and then `ClaimDHCPLease` retires the
  iface's previous DHCP address in the subnet and any other iface's DHCP claim
  on this one (the Q9 fix).
- An unknown MAC creates a device with `Source: source`, named after the
  hostname or `<source>-<mac>`, and emits `device_new`
  (`"<source> device <name> at <ip>"`).
- A known MAC only gets the IP attached and its hostname filled when empty.
  `UpsertIPAssignment` never turns a static assignment into `dhcp`.

Pi-hole keeps its own DNS-record step and calls `leases.Apply` for the rest, so
its behaviour and tests are unchanged. `leases.SubnetFor` replaces the private
`subnetForIP`.

IP ownership (0012's `ip_assignment.source`). A row `Apply` inserts is labelled
with the integration (`pihole`, `adguard`, `opnsense`) through the new
`UpsertIPAssignmentFrom`; an existing row keeps its label, so a user's row
(`''`) stays the user's, exactly like `SyncIntegrationIPs`. Retiring stale
leases (`ClaimDHCPLease`) still keys on `kind='dhcp'`, not on the label: the
scanner's rows are unlabelled too and must be retired when a lease moves (the
Q9 fix), and a static row — the user's or a reservation's — is never removed.
One known edge: a reservation still upgrades an existing `dhcp` row to
`static`, even one the user set to `dhcp` by hand; the DHCP server does
reserve that address, and this is the pre-existing Pi-hole behaviour.

## Device source values

`device.source` has a CHECK constraint (`manual, scan, proxmox, wireguard,
pihole`). Two options:

1. A generic `dhcp` source for every future lease source.
2. One source per integration: `adguard`, `opnsense`.

Chosen: **(2)**. Every existing integration has its own source, the device page
and the JSON API/exports show it verbatim, and "which box told netis about this
device" is exactly what a user wants to know when a stray device shows up. A
generic value would also leave Pi-hole as the one odd exception. Both options
need the same migration (neither value is allowed today), so the cost is equal.

Migration **0015_dhcp_sources**:

- Postgres: drop and re-add `device_source_check`.
- SQLite: the CHECK cannot be altered, so `device` is rebuilt the way 0005 did
  it (foreign keys are off during migrations; `PRAGMA foreign_key_check` runs
  after), with every current column listed explicitly — including
  `alert_offline` from 0010 — and the two 0009 partial unique indexes
  (`idx_device_wg_pubkey`, `idx_device_proxmox_vmid`) recreated, since dropping
  the table drops them.

Because the rebuild names its columns, a column added to `device` by a
migration numbered below 0015 on another branch would be silently dropped when
the branches meet. A store test now parses every SQLite migration for
`ALTER TABLE device ADD COLUMN` and asserts each such column still exists after
all migrations, so that merge fails loudly instead. Another test asserts the
0009 indexes still reject duplicates after 0015.

`AdoptDiscoveredIface` (a Proxmox guest taking over an interface found first by
someone else) accepts `adguard` and `opnsense` devices alongside `scan` and
`pihole`, with the same "unreviewed only" rule.

## AdGuard Home (`internal/adguard`)

- `GET <url>/control/dhcp/status` with HTTP basic auth (the AdGuard admin user
  and password). The base URL may carry a path prefix.
- Response: `enabled`, `leases[]` and `static_leases[]`, each
  `{mac, ip, hostname, expires}` (`expires` is an RFC 3339 string on dynamic
  leases). Static leases are applied as static, leases as dhcp.
- DHCP server disabled (`enabled: false`): the run succeeds with 0 leases and
  the detail line says `DHCP server disabled in AdGuard Home`; no devices are
  touched, so stale data left in the response is ignored.
- Settings: `adguard_url`, `adguard_user`, `adguard_password` (secret,
  encrypted at rest), `adguard_insecure`.

## OPNsense (`internal/opnsense`)

API key and secret as HTTP basic auth.

Lease backends. OPNsense has shipped three DHCP servers over time, and since
24.1 the Kea API exists even on boxes that serve DHCP from ISC, returning an
empty list. "Fall back only on 404" would therefore never reach ISC on a
current install. The client asks the backends in order and uses the first that
answers with leases:

1. Kea — `GET /api/kea/leases4/search` rows `{address, hwaddr, hostname,
   state}` (`hw_address` accepted too); rows whose `state` is not `0` (declined,
   expired-reclaimed) are skipped. Kea reservations,
   `GET /api/kea/dhcpv4/searchReservation` rows `{hw_address, ip_address,
   hostname}`, are the static set (a 404 there is "none").
2. ISC — `GET /api/dhcpv4/leases/searchLease` rows `{address, mac, hostname,
   type, state}`; `type: static` rows are the static set, `dynamic` the leases;
   a dynamic row whose `state` is set and not `active` is skipped.
3. Dnsmasq (25.1+) — `GET /api/dnsmasq/leases/search` rows `{address, hwaddr,
   hostname}`.

A backend that answers 404 (not installed) or 403 (the key lacks that
privilege) is skipped; if every backend is skipped the first error is
returned, so a key without any DHCP privilege shows "authentication failed". A
401 or any other failure fails the run at once. All searches send
`current=1&rowCount=-1` to get every row in one page.

ARP enrichment (routed VLANs). netis reads MACs from the local kernel ARP
table, so hosts on subnets behind a router are discovered MAC-less. OPNsense
sees them at layer 2: `GET /api/diagnostics/interface/getArp` returns
`[{ip, mac, expired, ...}]`. Before leases are applied, each ARP entry that

- is not expired and has a valid MAC,
- has a MAC that appears for exactly one IP in the table (a proxy-ARP or
  multi-homed MAC would otherwise be pinned to the wrong device),
- falls in a configured subnet, and
- whose IP is held by an existing iface that has **no MAC**,

fills that iface's MAC, and only when no other iface already has the MAC
(`SetIfaceMACIfEmpty` checks both in one statement). Nothing is created from
ARP, and a set MAC is never changed. Running it before the leases means a
lease for that MAC then enriches the scanned device instead of creating a
duplicate beside it.

The scanner is unaffected: on a routed subnet it matches hosts by IP, and
`macConflict` ignores an empty local ARP answer, so a filled MAC changes
nothing there. On a local subnet the iface already had its MAC, so nothing is
filled. An ARP failure (for example a key without the diagnostics privilege)
does not fail the run; the detail line says `ARP table unavailable` and the
error is logged.

Settings: `opnsense_url`, `opnsense_key`, `opnsense_secret` (secret, encrypted
at rest), `opnsense_insecure`.

## Wiring

- `cmd/netis/main.go`: `adguard` and `opnsense` closures in
  `newIntegrationRunner` and in `integrationNames`, sharing the runner's per-run
  deadline, per-integration lock, status recording and failure categories.
  Client errors use the `"<name> <path>: HTTP <code>"` shape so 401/403 map to
  "authentication failed".
- Settings → Integrations and the onboarding wizard get an AdGuard Home and an
  OPNsense card with Run now; the viewer status tab lists them. The form's
  secret handling (never echoed, blank keeps) and `_insecure` checkbox
  normalisation are generalised from the hard-coded Pi-hole/Proxmox pairs.
- `store.SecretSettings` gains `adguard_password` and `opnsense_secret`.
- No new routes: saving and Run now use the existing audited
  `POST /settings/integrations` and `POST /settings/integrations/{name}/run`.

## Tests

- `internal/leases`: the apply rules (create, enrich by MAC, reservation beats
  lease, manual static kept, lease move retires the old address, hostname only
  when empty, outside-subnet skipped) on both dialects.
- `internal/adguard`, `internal/opnsense`: httptest fakes for every endpoint,
  basic-auth checks, disabled DHCP, backend fallback, 404/403 skipping, ARP
  enrichment rules.
- `internal/store`: 0015 accepts the new sources on both dialects, keeps the
  0009 unique indexes and every added column; `SetIfaceMACIfEmpty`.
- `cmd/netis`: the new integrations report not-configured without recording a
  status, and record a categorised failure when configured against a dead
  address.

## Not included

- AdGuard Home's DNS rewrites (the Pi-hole DNS-record analogue): easy to add,
  but not asked for.
- DHCPv6 leases from any source.
