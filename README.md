# netis

## What is netis

Netis is a self-hosted app that organizes a home LAN: an editable inventory
of devices (computers, switches, phones, servers, IoT, VMs, LXCs, WireGuard
peers), their IPs and MACs, live online/offline status with last-seen
tracking, background network scanning, and a per-subnet grid overview
showing which IPs are online, offline, reserved, free, or conflicting. It
integrates with Proxmox (auto-import of VMs/LXCs) and WireGuard (peer
status via SSH). It ships as a single static Go binary with an embedded
SQLite database — no external services required.

## Quick start

Build:

```sh
templ generate && CGO_ENABLED=0 go build -o netis ./cmd/netis
```

Run:

```sh
NETIS_DB=/var/lib/netis/netis.db ./netis
```

The server listens on `:8080` by default. On first run, visit `/setup` in
a browser to create the initial admin account (username + password). After
setup, log in at `/login`, then go to **Settings** and add your LAN's
subnet (e.g. `192.168.1.0/24`) so netis knows what to scan. Use the
**Scan now** button on the subnet page to run an immediate sweep, or wait
for the background scan loop (every 120s by default).

## Views

- **Dashboard** — per-subnet online/total counts, recent events, quick
  search.
- **Subnet grid** — one square per IP in a subnet: green for online, dark
  for used-but-offline, yellow for reserved, empty for free, red for an
  IP claimed by two devices. Squares update live over SSE during a scan.
- **Device list** — filterable/searchable table of every known device.
- **Device page** — full device detail: interfaces, IPs, open ports,
  uptime, links, tags, custom fields, parent/child devices (e.g. a
  Proxmox host and its guests), event history, and buttons to send a
  Wake-on-LAN packet or run an on-demand TCP port scan.
- **Events** — a filterable log of device-new/online/offline/ip-changed/
  scan-error events.
- **Settings** — subnet CRUD, scan interval, offline threshold, Proxmox
  and WireGuard integration credentials, user management (add, delete,
  change your own password, reset someone else's — passwords are at least 8
  characters, and changing one signs that account's other sessions out), and
  an **About**
  tab with the running version, build number, commit, database backend and
  dependency versions. Every page's footer shows the version and links there.

## Install

Every `v*` tag publishes static Linux binaries (amd64 and arm64) to the GitHub
release, alongside a `SHA256SUMS` file:

```sh
tar -xzf netis_<version>_linux_amd64.tar.gz
./netis
```

## Docker

Released images are published to GHCR on every `v*` tag, for `linux/amd64` and
`linux/arm64`:

```sh
docker pull ghcr.io/13/netis:latest
```

Or build locally:

```sh
docker build -t netis .
docker run -d --name netis \
  --network host \
  -v netis-data:/data \
  -e NETIS_DB=/data/netis.db \
  netis
```

`--network host` is required: netis discovers MAC addresses by reading the
host's ARP table (`/proc/net/arp`) after pinging hosts on the subnet, which
only works if the container shares the host's network namespace. Running
netis on a bridged/NAT network will still ping and track online/offline
state, but MAC address (and therefore vendor) discovery will not work for
those subnets.

To allow the unprivileged Go binary to send raw ICMP echo requests instead
of the UDP-ICMP fallback, add `--cap-add=NET_RAW` and set
`NETIS_PRIVILEGED_ICMP=1`.

## Proxmox LXC install

1. Build the static binary as above (or download a prebuilt one) and copy
   it to the LXC as `/opt/netis/netis`.
2. Create a dedicated system user: `useradd -r -s /usr/sbin/nologin netis`.
3. Copy `deploy/netis.service` to `/etc/systemd/system/netis.service`.
4. `systemctl daemon-reload && systemctl enable --now netis`.

The unit sets `AmbientCapabilities=CAP_NET_RAW` so netis can send
privileged ICMP echo requests without running as root, and uses
`StateDirectory=netis` so `/var/lib/netis` exists and is writable by the
`netis` user for the SQLite database.

Because the LXC shares the Proxmox host's bridge, it sees the same L2
segment as everything else on the LAN, so ARP-based MAC discovery works
without any special networking configuration (unlike the Docker bridged
case above).

## Configuration

Environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_ADDR` | `:8080` | HTTP listen address. |
| `NETIS_DB` | `netis.db` | Database to use. A path selects SQLite; a `postgres://` URL selects Postgres. See [Database backends](#database-backends). |
| `NETIS_DB_MAX_OPEN_CONNS` | `10` | Postgres connection pool size. Ignored on SQLite, which is held to one connection to avoid `SQLITE_BUSY`. |
| `NETIS_DB_MAX_IDLE_CONNS` | `5` | Postgres idle connections; clamped to the open limit. |
| `NETIS_PRIVILEGED_ICMP` | unset | Set to `1` to send raw ICMP echo requests (requires `CAP_NET_RAW` or root) instead of the unprivileged UDP-ICMP fallback. |
| `NETIS_METRICS_TOKEN` | unset | Bearer token a Prometheus scraper presents to read `/metrics`. Unset means `/metrics` needs a logged-in session. See [JSON API and metrics](#json-api-and-metrics). |
| `NETIS_SECRET_KEY` | unset | 32-byte key (base64 or hex) that encrypts stored integration credentials. See [Encrypting stored credentials](#encrypting-stored-credentials). |
| `NETIS_TRUSTED_PROXIES` | unset | Comma-separated CIDRs or addresses of reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` headers netis believes. See [Behind a reverse proxy](#behind-a-reverse-proxy). |

Settings configured in the web UI (Settings page), stored in the
database's key/value settings table:

| Key | Meaning |
| --- | --- |
| `offline_after` | Consecutive missed scan sweeps before a device is marked offline (default 3). |
| `event_retention_days` | Days of event history to keep, swept every 6h; `0` keeps everything (default 30). |
| `availability_retention_days` | Days of availability history to keep (default 365). One row per interface per hour, so this is the fastest-growing table. |
| `proxmox_url` | Base URL of the Proxmox API, e.g. `https://pve.local:8006`. |
| `proxmox_token_id` | Proxmox API token ID, e.g. `user@pam!netis`. |
| `proxmox_secret` | Proxmox API token secret. Encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `proxmox_insecure` | `1` to skip TLS verification (self-signed certs). |
| `wg_ssh_addr` | SSH address of the host running WireGuard (`host:port`). |
| `wg_ssh_user` | SSH username for the WireGuard host. |
| `wg_ssh_key_path` | Path to the SSH private key used to connect. |
| `wg_ssh_known_hosts` | Path to an OpenSSH `known_hosts` file used to verify the WireGuard host. Unset means the host is **not** verified. |
| `wg_iface` | WireGuard interface name to poll (default `wg0`). |
| `pihole_url` | Base URL of the Pi-hole admin, e.g. `https://pi.hole`. |
| `pihole_password` | Pi-hole app password (never shown back in the UI). Encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `pihole_insecure` | `1` to skip TLS verification (self-signed certs). |

### Pi-hole (v6)

Set in Settings → Integrations, or as `setting` rows:

- `pihole_url` — base URL of the Pi-hole admin, e.g. `https://pi.hole` (empty disables the integration)
- `pihole_password` — Pi-hole app password (never shown back in the UI)
- `pihole_insecure` — `1` to skip TLS verification (self-signed certs)

When configured, netis polls Pi-hole every minute and merges DHCP leases,
static reservations, and local DNS A records into the device inventory:
devices are matched by MAC (unknown MACs are created with source `pihole`),
reservations mark their IP `static`, and DNS names attach to the matching
device. Pi-hole data never changes a device's online/last-seen status — that
stays driven by the scanner. IPs are only attached when they fall inside a
subnet you've configured in netis.

Subnets (CIDR, kind, scan interval, scan enabled) are managed via
Settings, not environment variables — add at least one subnet after
first-run setup for scanning to do anything.

## JSON API and metrics

A read-only JSON API, authenticated with the same session cookie the pages use:

| Endpoint | Returns |
| --- | --- |
| `GET /api/devices` | every device with its IPs, MACs, tags and online state |
| `GET /api/devices/{id}` | one device |
| `GET /api/subnets` | configured subnets |
| `GET /api/events?limit=N` | recent events, newest first (default 100, max 1000) |
| `GET /api/status` | version, uptime, backend, device/subnet counts, integration results |

Nothing here writes: state changes go through the UI's form posts, where the
role checks and cross-origin protection live.

`GET /metrics` serves the Prometheus text format — device and subnet counts,
how many devices are online, and when each integration last ran and whether it
worked. It needs a session too, unless you set a scrape token:

```sh
NETIS_METRICS_TOKEN="$(openssl rand -hex 16)" ./netis
```

```yaml
scrape_configs:
  - job_name: netis
    static_configs: [{targets: ['netis.lan:8080']}]
    authorization:
      credentials: <the token>
```

The token is accepted on `/metrics` only — it is a scrape credential, not a
login — and only in the `Authorization` header, so it stays out of access logs.
Without it the endpoint is not left open: the metrics name every subnet and
count every device, which is not something to publish to whoever can reach the
port.

## Encrypting stored credentials

The Proxmox API token and the Pi-hole password live in the database's `setting`
table. Set `NETIS_SECRET_KEY` and they are encrypted there with AES-256-GCM
instead:

```sh
NETIS_SECRET_KEY="$(openssl rand -base64 32)" ./netis
```

Only those two rows are encrypted — the rest of the table is configuration, and
stays readable in a SQL client. Values already stored in plaintext are
re-encrypted on the next start, so adding the key to an existing deployment
does not mean re-entering anything.

The key never lives in the database, so keep it with (but not inside) your
backups: a dump restored without it leaves netis unable to read those two
settings, and it says so rather than treating the credential as unset. Changing
the key has the same effect — clear the affected settings and enter them again.
`netis migrate-db` copies the rows as they are, so the same key works on the
Postgres side.

## Behind a reverse proxy

netis ignores `X-Forwarded-For` and `X-Forwarded-Proto` unless you name the
proxy that sends them:

```sh
NETIS_TRUSTED_PROXIES=10.0.0.0/8,192.168.1.5 ./netis
```

Set this when netis sits behind nginx, Caddy, Traefik or similar. Without it
every request is attributed to the proxy's own address, so the login rate
limiter (5 failed attempts per minute) counts all users as one client and five
wrong passwords from anywhere lock everyone out for a minute. With it, the
limiter keys on the real client address — the rightmost forwarded entry that
isn't itself a listed proxy, which is the furthest-left address the proxy chain
can actually vouch for.

`X-Forwarded-Proto: https` from a listed proxy also lets the session cookie
carry the `Secure` flag when TLS terminates at the proxy.

A malformed entry is a startup error rather than a warning: a list that quietly
parsed to nothing would leave the limiter mis-keyed with no sign of it.

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
fresh.

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

## Backups

SQLite: copying `netis.db` with `cp` while netis is running is not safe — under
WAL the committed state is split between the database and its `-wal` sidecar.
Use the built-in snapshot, which works against a live database:

```sh
netis backup --to /backups/netis-$(date +%F).db
```

Postgres: use `pg_dump`, which already handles this properly.

```sh
pg_dump "$NETIS_DB" > /backups/netis-$(date +%F).sql
```

## Limitations

- A subnet may hold at most 65,536 addresses (an IPv4 `/16`, an IPv6 `/112`).
  Every address becomes a grid cell and a sweep target, so a wider prefix is
  refused when the subnet is saved rather than discovered when the page is
  opened.

- MAC address discovery only works for subnets on the same local L2
  segment as the netis host (it reads the kernel ARP table after pinging).
  Remote/routed subnets get ping-only scanning: online/offline status and
  IP tracking work, but no MAC or vendor.
- WireGuard peer status is read by SSHing into the host running WireGuard
  and parsing `wg show dump`; it is not a local integration and requires
  a reachable SSH endpoint with a configured key.
- Port scanning is on-demand per device only (the button on the device
  page); netis never automatically scans ports across a subnet.
- Proxmox guest IPs depend on the QEMU guest agent being installed and
  running in the VM; without it, only the guest's configured MAC/bridge
  is known, not its IP.

## License

MIT — see [LICENSE](LICENSE).
