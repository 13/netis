# netis

## What is netis

Netis is a self-hosted app that organizes a home LAN: an editable inventory
of devices (computers, switches, phones, servers, IoT, VMs, LXCs, WireGuard
peers), their IPs and MACs, live online/offline status with last-seen
tracking, background network scanning, and a per-subnet grid overview
showing which IPs are online, offline, reserved, free, or conflicting. It
integrates with Proxmox (auto-import of VMs/LXCs), Pi-hole (DHCP leases,
reservations and local DNS names) and WireGuard (peer status via SSH). It ships as a single static Go binary with an embedded
SQLite database — no external services required.

## Quick start

Build:

```sh
go tool templ generate && CGO_ENABLED=0 go build -o netis ./cmd/netis
```

(`make build` does the same; templ is pinned in `go.mod` as a tool, so nothing
needs installing first.)

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

## Make targets

| Target | Does |
| --- | --- |
| `make generate` | `go tool templ generate` |
| `make build` | generate, then build a static `./netis` |
| `make run` | build and run it |
| `make test` / `make race` | generate, then `go test ./...` (with `-race`) |
| `make pg` / `make pg-stop` | start / remove a throwaway Postgres on port 55432 |
| `make test-pg` | the tests against that Postgres as well as SQLite |
| `make lint` | `go vet` and staticcheck |
| `make vuln` | govulncheck |
| `make docker` | `docker build -t netis .` |

## Views

- **Dashboard** — per-subnet online/total counts, recent events, quick
  search.
- **Subnet grid** — one square per IP in a subnet: green for online, dark
  for used-but-offline, yellow (marked R) for reserved, empty for free, red
  (marked !) for an IP claimed by two devices. Each square also names its
  address and state for screen readers. Squares update live over SSE during
  a scan.
- **Device list** — filterable/searchable table of every known device, with
  CSV/JSON export and (for admins) a CSV import with a dry-run preview.
- **Device page** — full device detail: interfaces, IPs, open ports,
  uptime, links, tags, custom fields, parent/child devices (e.g. a
  Proxmox host and its guests), event history, and buttons to send a
  Wake-on-LAN packet or run an on-demand TCP port scan.
- **Events** — a filterable log of device-new/online/offline/ip-changed/
  scan-error events.
- **Settings** — subnet CRUD, scan interval, offline threshold, Proxmox
  and WireGuard integration credentials, user management (add, delete,
  change your own password, reset someone else's — passwords are 8 to 72
  bytes long, and changing one signs that account's other sessions out), your
  own session list with per-session revoke and a sign-out-everywhere-else
  button, an **API tokens** tab for your own tokens (admins see and can
  revoke everyone's), and an **About**
  tab with the running version, build number, commit, database backend and
  dependency versions. Every page's footer shows the version and links there.

The theme follows the operating system's light/dark preference, including
when it changes, until you pick one with the theme button; that choice is then
remembered in the browser. Deleting a device, subnet, user, link or custom
field, and revoking sessions, asks for confirmation first. The device search
and the New/Edit device forms also work with JavaScript turned off.

Users are either `admin` or `viewer`. Viewers see the inventory, the subnets,
events and each integration's status, and manage their own password, API tokens and
sessions; they do not see integration settings, the user list, or any of the
edit, delete, scan, Wake-on-LAN and port-scan controls.

## Install

Every `v*` tag publishes static Linux binaries (amd64 and arm64) to the GitHub
release, alongside a `SHA256SUMS` file. The container images are amd64 only, so
on an arm64 machine use the binary:

```sh
tar -xzf netis_<version>_linux_amd64.tar.gz
./netis
```

## Docker

Released images are published to GHCR on every `v*` tag, for `linux/amd64`:

```sh
docker pull ghcr.io/13/netis:latest
```

Publishing needs this repository's Actions token to be allowed to write
packages (Settings → Actions → General → Workflow permissions → "Read and
write permissions"). Without it the release workflow builds the image and then
fails the push with `denied: permission_denied: write_package`.

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

The image runs netis as root and sets `NETIS_PRIVILEGED_ICMP=1`, so it sends
raw ICMP echo requests. Docker grants root in a container `CAP_NET_RAW` by
default, so no `--cap-add` is needed. A runtime that drops it (Podman's
defaults, `--cap-drop=ALL`, some hardened setups) needs `--cap-add=NET_RAW`;
without it the sweeps fail and show up as scan errors.

If most probes in a sweep cannot be sent at all (for example the process
lacks permission to open ICMP sockets), the sweep is reported as a scan
error on the dashboard and in Events, and device states are left as they
were rather than counted as misses.

## Discovery

- **Hosts that ignore ping.** Sleeping phones, Windows with its firewall on
  and a lot of IoT gear drop ICMP. On a directly attached subnet the sweep's
  pings still make the kernel ARP for every address, so a host that answers
  ARP ends up with a complete entry in `/proc/net/arp`. Such a host is
  counted as seen when a TCP connect to one of ports 22, 80, 443, 445, 62078
  or 8080 is accepted or refused (400 ms), or, failing that, when its ARP
  entry still resolves to the same MAC about 9 seconds after the sweep, by
  which time the kernel has re-probed a stale entry and dropped it if nobody
  answered. A host that left within the last half minute can therefore look
  present for one more sweep. Turn it off with **Presence without ping** in
  Settings > General. Routed subnets have no ARP entries, so it does nothing
  there.
- **Randomized MACs.** A MAC with the locally administered bit set (phones'
  "Private Wi-Fi Address") gets a *private MAC* badge on the device list and
  page, and `private_mac` in the API. A new device on one is named
  `private-<mac>` and its event says it may be a phone. The QEMU/KVM
  (`52:54:00`) and Docker (`02:42`) prefixes are not counted.
- **Names.** Reverse DNS first, then an mDNS reverse lookup (a PTR query to
  `224.0.0.251:5353` asking for a unicast answer), which is where Apple
  devices, printers and Avahi hosts name themselves. Lookups run 16 at a time;
  an address that returned no name is retried after 30 minutes. A discovered
  name only fills an interface hostname that is empty.

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

Secrets such as `NETIS_SECRET_KEY` belong in `/etc/netis/env` (read through
`EnvironmentFile=`, optional), not in the unit file, which any local user can
read:

```sh
install -d -m 0755 /etc/netis
install -m 0600 /dev/null /etc/netis/env
echo "NETIS_SECRET_KEY=$(openssl rand -base64 32)" >> /etc/netis/env
```

The unit is sandboxed: the filesystem is read-only except `/var/lib/netis`,
`/home` and `/root` are hidden, the capability set is limited to
`CAP_NET_RAW`, and system calls, address families and namespaces are
restricted (`systemd-analyze security netis` shows the details). Because of
`ProtectHome=yes`, put the WireGuard SSH key and `known_hosts` file under
`/etc/netis` or `/var/lib/netis`, readable by the `netis` user, and point the
`wg_ssh_key_path` / `wg_ssh_known_hosts` settings there; a key in
`/root/.ssh` is invisible to the service. If a key must stay where it is, add
it with a drop-in (`systemctl edit netis`) containing
`BindReadOnlyPaths=/home/you/.ssh/netis_wg:/etc/netis/wg_key`.

Because the LXC shares the Proxmox host's bridge, it sees the same L2
segment as everything else on the LAN, so ARP-based MAC discovery works
without any special networking configuration (unlike the Docker bridged
case above).

## Configuration

Environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_ADDR` | `:8080` | HTTP listen address. |
| `NETIS_BACKUP_DIR` | unset | Directory the server writes scheduled SQLite backups to. Unset turns them off. See [Scheduled backups](#scheduled-backups). |
| `NETIS_BACKUP_INTERVAL` | `24h` | Time between scheduled backups (a Go duration: `6h`, `90m`). Values under `1m` or unparseable fall back to the default. |
| `NETIS_BACKUP_KEEP` | `7` | Scheduled backups kept; older ones are deleted. |
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
| `presence_fallback` | `on` (default) or `off`: count hosts that ignore ping but answer ARP as seen. See [Discovery](#discovery). |
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
| `wg_iface` | WireGuard interface name to poll (default `wg0`): 1-15 letters, digits, `.`, `_` or `-`. It is part of the command run over SSH, so anything else is refused. |
| `pihole_url` | Base URL of the Pi-hole admin, e.g. `https://pi.hole`. |
| `pihole_password` | Pi-hole app password (never shown back in the UI). Encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `pihole_insecure` | `1` to skip TLS verification (self-signed certs). |
| `notify_webhook_url`, `notify_ntfy_url` | Notification channels; blank disables one. See [Notifications](#notifications). |
| `notify_webhook_auth`, `notify_ntfy_token` | Webhook `Authorization` header value and ntfy access token. Encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `notify_base_url` | netis's own URL, used to link messages to a device or the event log. |
| `notify_device_new`, `notify_offline`, `notify_ip_conflict`, `notify_sync` | `0` switches that kind of notification off (default on). |

### Pi-hole (v6)

Set in Settings → Integrations, or as `setting` rows:

- `pihole_url` — base URL of the Pi-hole admin, e.g. `https://pi.hole` (empty disables the integration)
- `pihole_password` — Pi-hole app password (never shown back in the UI)
- `pihole_insecure` — `1` to skip TLS verification (self-signed certs)

When configured, netis polls Pi-hole every minute and merges DHCP leases,
static reservations, and local DNS A records into the device inventory:
devices are matched by MAC (unknown MACs are created with source `pihole`),
reservations mark their IP `static`, and DNS names attach to the matching
device. When a lease moves, the device's old DHCP address is dropped, and an
address leased to a new MAC is taken off whichever device held it by DHCP
before; static addresses are never removed. Pi-hole data never changes a device's online/last-seen status — that
stays driven by the scanner. IPs are only attached when they fall inside a
subnet you've configured in netis.

Each configured integration (Proxmox, Pi-hole, WireGuard) syncs once a minute
and on demand from Settings → Integrations → Run now. A run is cut off after
30 seconds. Its result — connected with a count, or failing with a category
(timeout, authentication failed, host key rejected, connection failed,
configuration error, sync error) — is shown on the settings page, the dashboard
and in `/api/status`, and the first failure of an outage adds a `scan_error`
event (its end adds a `sync_recovered` one). The full error, which can name key paths and internal addresses, goes
only to the server log. Clicking Run now while that integration is
already syncing reports "already running" instead of starting a second run.

Syncs fill in devices but never overwrite your edits. A Proxmox guest or node
takes its name and kind from Proxmox only when it is first imported; renaming
it or changing its kind in netis sticks. A guest's parent follows it when it
moves between Proxmox nodes, but a parent you set to a non-Proxmox device is
kept. Nodes are recognised by the `proxmox_node` custom field the sync adds.
When the scanner found a guest first, the guest takes over that device's
interface (and its IPs) as long as you have not reviewed the discovered device;
the discovered device is deleted if nothing else is left on it. A reviewed
device keeps its interface.

Subnets (CIDR, kind, scan interval, scan enabled) are managed via
Settings, not environment variables — add at least one subnet after
first-run setup for scanning to do anything.

## JSON API and metrics

The JSON API under `/api/` accepts either the session cookie the pages use or
a personal **API token**. Create one under Settings > API tokens: it is shown
once (`netis_` followed by 43 characters), only its SHA-256 digest is stored,
and it acts with your role: a viewer's token can read, an admin's can also
write. Tokens can expire (default 90 days, 0 = never), are revoked from the
same tab, and are deleted with their user. Expired tokens are removed by the
retention sweep.

```sh
export NETIS=http://netis.lan:8080 TOKEN=netis_...
curl -H "Authorization: Bearer $TOKEN" $NETIS/api/devices
```

| Endpoint | Role | Does |
| --- | --- | --- |
| `GET /api/devices` | any | every device with its IPs, MACs, tags and online state; `private_mac` is true when a MAC is randomized |
| `GET /api/devices/{id}` | any | one device |
| `GET /api/subnets` | any | configured subnets |
| `GET /api/events?limit=N` | any | recent events, newest first (default 100, max 1000) |
| `GET /api/status` | any | version, uptime, backend, device/subnet counts, integration results |
| `GET /api/export/devices.csv` | any | inventory as CSV (one row per device; MACs, IPs, tags `;`-joined) |
| `GET /api/export/devices.json` | any | inventory as JSON, with each interface's MAC and addresses |
| `POST /api/devices` | admin | create a device |
| `PATCH /api/devices/{id}` | admin | change some of a device's fields |
| `DELETE /api/devices/{id}` | admin | delete a device |

```sh
# Create: name and kind are required; mac, ip, subnet_id, tags, notes,
# vendor, model, function, icon and parent_device_id are optional. Without
# subnet_id the IP goes into the narrowest configured subnet that holds it.
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"nas","kind":"server","mac":"aa:bb:cc:00:00:01","ip":"192.168.1.20","tags":["core"]}' \
  $NETIS/api/devices

# Update only what you send; "parent_device_id": null clears the parent and
# "tags" replaces the set.
curl -X PATCH -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"notes":"rack 2","tags":["core","storage"]}' $NETIS/api/devices/42

curl -X DELETE -H "Authorization: Bearer $TOKEN" $NETIS/api/devices/42

curl -H "Authorization: Bearer $TOKEN" -o devices.csv $NETIS/api/export/devices.csv
```

Writes take `Content-Type: application/json` and validate exactly what the
device form does. Errors come back as `{"error": "..."}`: 400 for a bad value
or unknown parent/subnet, 401 for a missing, unknown or expired token, 403 for
a viewer, 404 for no such device, 409 for a MAC that already belongs to another
device. Token requests skip the browser cross-origin checks (they carry no
cookie to forge), but 20 failed token attempts a minute from one address get
429.

**CSV import** (admins, Devices > Import CSV) creates and updates devices by
MAC address. The first row names the columns: `mac` is required; `name`,
`kind`, `ip`, `tags`, `notes`, `vendor`, `model` and `function` are optional,
and anything else (such as the export's `id` or `last_seen`) is ignored, so an
export can be edited and imported as it is. A preview lists what each line
will create, update or skip before anything is written. A new MAC creates a
manual device (it needs a name, and an IP must fall in a configured subnet).
A known MAC only fills in what is missing: notes, vendor, model and function
where empty, and tags are added, never removed. Names and kinds change only on
unreviewed scan discoveries, so devices from integrations and devices you have
reviewed keep theirs. Uploads may be up to 5 MB.

`GET /metrics` serves the Prometheus text format — device and subnet counts,
how many devices are online, when each integration last ran and whether it
worked, and — with [scheduled backups](#scheduled-backups) on —
`netis_last_backup_timestamp_seconds` and `netis_last_backup_success`. It needs a session too, unless you set a scrape token:

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

## Notifications

Settings → Notifications (admins only) sends the events worth hearing about to
a generic webhook, an [ntfy](https://ntfy.sh) topic, or both:

| Kind | When |
| --- | --- |
| New devices | A scan or an integration finds a device netis has not seen before. |
| Offline / online | A device goes offline or comes back — **only** for devices you mark with **Alert when offline** on their page. The mark is off for every device by default, so phones and laptops coming and going stay quiet. |
| IP conflicts | After a subnet sweep, an address is newly claimed by more than one interface. Announced once per conflict; one still present after a restart is announced again. |
| Scan and integration errors | A subnet sweep fails (the same error at most once an hour), an integration starts failing (once per outage), and when it works again. |

Each kind can be switched off. Events are collected for 30 seconds and sent
together, so a scan that turns up fifty devices sends one summary instead of
fifty messages. Sending never holds up a scan: events wait in a bounded queue,
and if it fills (an endpoint down for a long time during a busy period) the
overflow is dropped and logged. Each channel gets 5 seconds per attempt and
three attempts; a 4xx answer other than 429 is not retried.

The webhook receives a JSON `POST`:

```json
{"event": "offline", "device": {"id": 3, "name": "nas"},
 "details": "nas (192.168.1.5) went offline",
 "time": "2026-09-28T10:00:00Z", "url": "https://netis.lan/devices/3"}
```

A batch has `"event": "batch"`, a summary in `details` ("12 new devices, 1 went
offline") and the individual events in `events`. `device` is `null` for events
not about one device, and `url` is empty unless a base URL is set. An optional
`Authorization` header value is sent as given.

ntfy gets the details as the message body, with `Title`, `Tags` (an emoji per
kind), `Priority` (4 for offline, conflicts and errors) and, with a base URL,
`Click` headers. Set the topic URL (e.g. `https://ntfy.sh/my-netis-topic` or
your own server) and, for a protected topic, an access token.

**Send test** posts a test message with the saved settings and shows each
channel's result. The webhook header and ntfy token are never shown back in the
form; leave the field blank to keep the stored value or tick *clear* to remove
it.

## Encrypting stored credentials

The Proxmox API token, the Pi-hole password and the notification credentials
(webhook header, ntfy token) live in the database's `setting` table. Set
`NETIS_SECRET_KEY` and they are encrypted there with AES-256-GCM instead:

```sh
NETIS_SECRET_KEY="$(openssl rand -base64 32)" ./netis
```

Only those rows are encrypted — the rest of the table is configuration, and
stays readable in a SQL client. Values already stored in plaintext are
re-encrypted on the next start, so adding the key to an existing deployment
does not mean re-entering anything.

The key never lives in the database, so keep it with (but not inside) your
backups: a dump restored without it leaves netis unable to read those
settings, and it says so rather than treating the credential as unset. Changing
the key has the same effect — clear the affected settings and enter them again.
`netis migrate-db` copies the rows as they are, so the same key works on the
Postgres side.

Session tokens need no key: the database only ever holds their SHA-256
digest, so a copy of it cannot be used to sign in as anyone. Sessions from
before netis stored them that way are dropped on upgrade, and everyone signs in
once more.

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

The per-address limiter keys IPv6 clients by their /64. Separately, each
account allows 10 wrong passwords per 15 minutes, whatever address they come
from, so rotating addresses does not buy unlimited guesses, and the
current-password check when changing your own password has the same limit. It is a
sliding window, not a lockout: the account opens up again once old failures age
out.

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

### Scheduled backups

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
About shows the last backup (or the failure), and `/metrics` exports
`netis_last_backup_timestamp_seconds` and `netis_last_backup_success` for
alerting on stale backups.

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

With Postgres the setting is ignored with a warning at startup: use `pg_dump`
(below). Restore a scheduled backup exactly like a manual one; see
[Restoring a backup](#restoring-a-backup).

### Restoring a backup

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
   set now (see [Encrypting stored credentials](#encrypting-stored-credentials)).

With the Docker example from [Scheduled backups](#scheduled-backups), mount
both volumes in step 2:
`docker run --rm -v netis-data:/data -v /srv/backups/netis:/backups alpine sh -c 'cp /backups/netis-20260928-031500.db /data/netis.db && rm -f /data/netis.db-wal /data/netis.db-shm'`.

A backup from an older release is fine: netis migrates it forward on start.

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
  page); netis never automatically scans ports across a subnet. The only
  automatic TCP connects are the six-port presence checks on hosts that
  answered ARP but not ping.
- Proxmox guest IPs depend on the QEMU guest agent being installed and
  running in the VM; without it, only the guest's configured MAC/bridge
  is known, not its IP.

## License

MIT — see [LICENSE](LICENSE).
