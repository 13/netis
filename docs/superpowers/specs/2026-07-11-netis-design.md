# Netis — Home Network Organizer: Design

Date: 2026-07-11
Status: Approved design, pre-implementation

## Purpose

Netis is a self-hosted app that organizes a home LAN: an editable inventory of
devices (computers, switches, phones, servers, IoT, VMs, LXCs, WireGuard
peers), their IPs and MACs, live online/offline status with last-seen
tracking, background network scanning, and a per-subnet grid overview showing
which IPs are online, offline, reserved, free, or conflicting. It integrates
with Proxmox (auto-import of VMs/LXCs) and WireGuard (peer status).

Single home-admin audience, multiple subnets (main LAN, WireGuard subnet,
Proxmox bridges), runs in an LXC on Proxmox and/or a Docker container.

## Architecture

Single Go monolith, one static binary:

- HTTP server: server-rendered UI with templ (typed HTML templates) + HTMX,
  live updates via SSE (HTMX SSE extension).
- Background goroutines: per-subnet scan loops, Proxmox poller, WireGuard
  poller.
- Storage: SQLite via `modernc.org/sqlite` (pure Go, no CGO). Single
  `netis.db` file. Embedded migrations.
- Embedded assets: OUI vendor table, CSS/JS, migrations via `go:embed`.

Rejected alternatives: server + per-segment scanner agents (full MAC coverage
everywhere but two components and more ops — data model stays agent-ready via
the device `source` field); shelling out to nmap (external dependency in a
minimal image).

Known limitation: ARP/MAC discovery only works on L2 segments the netis host
sits on. Remote subnets get ping-only scanning (no MAC).

## Data model (SQLite)

- **subnet** — id, cidr, name, vlan_id (nullable), kind
  (`lan`/`wireguard`/`proxmox-bridge`), scan_enabled, scan_interval_sec
  (default 120).
- **device** — id, name, kind (`computer`/`switch`/`phone`/`server`/
  `printer`/`iot`/`vm`/`lxc`/`wg-peer`/`other`), notes, vendor (from MAC OUI),
  source (`manual`/`scan`/`proxmox`/`wireguard`), parent_device_id (nullable;
  VM/LXC → Proxmox host, device behind bridge), proxmox_vmid (nullable),
  wg_pubkey (nullable), icon. Fully CRUD-editable.
- **interface** — id, device_id, mac (nullable), hostname (nullable). A device
  can have several NICs.
- **ip_assignment** — id, interface_id, subnet_id, ip, assignment type
  (static/dhcp). One grid square per IP in a subnet.
- **status** — per interface: online (bool), first_seen, last_seen,
  last_scan_rtt.
- **availability_history** — downsampled ping results per interface for uptime
  sparklines and "99.2% last 30d" stats.
- **open_port** — interface_id, port, proto (tcp/udp), service_guess,
  first_seen, last_seen.
- **tag** + **device_tag** — free-form labels with color, many-to-many,
  filterable everywhere.
- **custom_field** — device_id, key, value. Arbitrary per-device data.
- **device_link** — device_id, label, url (web UI, ssh://, docs). One-click
  admin pages.
- **event** — id, ts, type (`device_new`/`online`/`offline`/`ip_changed`/
  `scan_error`), device_id (nullable), details. Feeds the in-app alert log.
- **user** — id, username, password_hash (bcrypt), role (`admin`/`viewer`);
  plus a session table.

Unknown-device flow: a scan hit with no matching MAC/IP auto-creates a device
(source=`scan`, name = hostname or `unknown-<mac>`) plus a `device_new` event.
The user renames/classifies it later.

Grid square states derived from ip_assignment + status: used+online,
used+offline, reserved (assigned but never seen), free, conflict (two devices
claim the same IP).

## Scanning engine

Per-subnet sweep loop on a ticker (`scan_interval_sec`, default 120 s):

1. ICMP echo to every host IP in the CIDR — worker pool (~64 concurrent),
   unprivileged UDP-ICMP fallback when CAP_NET_RAW is absent.
2. Read `/proc/net/arp` to map IP → MAC for local-segment hits.
3. Best-effort reverse DNS (PTR) and mDNS name lookup.
4. MAC → vendor via embedded OUI table.
5. Diff against DB: new device → create + `device_new` event; state flip →
   `online`/`offline` event + status update; IP/MAC mismatch → `ip_changed`
   event or conflict state.

Offline rule: a device goes offline only after N consecutive missed sweeps
(default 3, configurable) to avoid flapping from sleeping phones.

Manual scan: per-subnet "scan now" button runs the same pipeline immediately.
SSE pushes grid/list updates live while scanning.

Port scan: manual per device (or opt-in via tag) — TCP connect scan of the
top ~1000 ports, results into open_port. Never automatic across a subnet.

WireGuard subnets are not swept; peer status comes from the WireGuard
integration (online = last handshake < 3 min).

## Integrations

### Proxmox

Poller every 60 s (configurable), API token auth
(`PVEAPIToken=user@realm!name=secret`):

- `GET /cluster/resources?type=vm` — all VMs and LXCs: name, vmid, node,
  status (running/stopped).
- Per-guest config for MACs and bridge; QEMU guest agent (when present) for
  actual IPs.
- Upsert devices: kind=`vm`/`lxc`, source=`proxmox`,
  parent_device_id = the node's device. Proxmox nodes are auto-created as
  devices too.
- A stopped guest is shown as "stopped" (its own state), not spammed as
  `offline` events.
- Config: base URL, token, TLS-verify toggle (self-signed certs are common).

### WireGuard (SSH remote only)

WireGuard runs on a remote host (router / Proxmox host). Netis connects via
SSH with a configured key and runs `wg show dump`, parsing peers: pubkey,
allowed-ips, endpoint, last handshake. Upsert devices kind=`wg-peer`,
source=`wireguard`. Peer online = handshake < 3 min.

## UI & views

Server-rendered templ + HTMX, SSE for live refresh, dark-mode-friendly.

- **Dashboard** — per-subnet online/total counts, recent events, quick search.
- **Subnet grid** (the signature view) — one grid per subnet, one square per
  IP: green online, dark used+offline, yellow reserved, empty free, red
  conflict. Hover shows IP/name/MAC/last seen; click opens the device page or
  "create device here" for free/unknown squares. A /24 renders as a 16×16
  grid; larger subnets paginate by /24 blocks. Squares flip live via SSE
  during scans.
- **Device list** — table of name, kind icon, IPs, MAC, vendor, tags, online,
  last seen. Filter by tag/kind/subnet/status, full-text search, sort, inline
  quick-edit.
- **Device page** — all fields, interfaces + IPs, links, tags, custom fields,
  open ports, uptime sparkline, parent/children (Proxmox host ⇄ guests),
  Wake-on-LAN button (magic packet to stored MAC), event history. Everything
  editable.
- **Events** — filterable log, unseen-count badge in the nav.
- **Settings** — subnet CRUD, scan intervals, offline threshold,
  Proxmox/WireGuard credentials, user management.

## Auth

Username/password with bcrypt hashes, server-side sessions (cookie,
SQLite-backed). Roles: `admin` (everything) and `viewer` (read-only; no
settings, no scan triggers). First-run setup page creates the initial admin.
Login is rate-limited.

## Error handling

- Scan errors (permission denied, unreachable subnet) become `scan_error`
  events, visible in the log; the loop continues on the next tick.
- Proxmox/WireGuard poll failures are logged as events once per outage (not
  per tick) and shown as an integration-status indicator in Settings;
  previously imported devices keep their last state.
- SSE clients reconnect automatically (HTMX default); the UI stays usable
  without live updates.

## Testing

- Store layer: unit tests against in-memory SQLite.
- Scan diff logic: pure-function tests with fake sweep results (no live
  network in tests; scanner behind an interface, mocked).
- Proxmox and WireGuard parsers: recorded fixture responses.
- HTTP handlers: `httptest` with a seeded test DB.

## Project layout

- `cmd/netis` — main.
- `internal/store` — SQLite repositories + migrations.
- `internal/scan` — sweep pipeline, ARP/DNS/OUI, diff logic.
- `internal/proxmox` — API client + sync.
- `internal/wireguard` — SSH client + `wg show dump` parser + sync.
- `internal/events` — event creation + SSE fan-out.
- `internal/web` — handlers, templ views, static assets.

## Deploy

- Docker: distroless/scratch image (~15 MB), optional `NET_RAW` capability
  for privileged ICMP; host or bridged network so ARP works.
- LXC: plain binary + systemd unit.
- Config: env vars, optional `config.yaml`. All state in one `netis.db`
  volume/file.

## Out of scope (for now)

- Per-segment scanner agents (data model ready via `source`).
- External push notifications (ntfy/Telegram/email) — in-app events only.
- Automatic subnet-wide port scanning.
- Switch-port topology mapping.
