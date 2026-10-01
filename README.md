<div align="center">

<img src="docs/images/logo.svg" width="72" height="72" alt="">

# netis

Know what's on your home network.

[![CI](https://github.com/13/netis/actions/workflows/ci.yml/badge.svg)](https://github.com/13/netis/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/13/netis)](https://github.com/13/netis/releases/latest)
[![Licence: MIT](https://img.shields.io/github/license/13/netis)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/13/netis)](go.mod)

</div>

netis is a self-hosted inventory of your home LAN. It scans your subnets, keeps
an editable list of every device (computers, switches, phones, servers, IoT,
VMs, LXCs, WireGuard peers) with its IPs and MACs, and shows which ones are
online right now. It ships as a single static Go binary with an embedded SQLite
database — no external services required.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/dashboard-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/images/dashboard-light.png">
  <img src="docs/images/dashboard-light.png" alt="The netis dashboard: a health strip, a list of things that need attention, subnet usage and recent activity">
</picture>

## What it does

- **Finds devices.** Background ping sweeps of every subnet you add, with MAC
  addresses, vendors and names from reverse DNS and mDNS — including hosts that
  ignore ping.
- **Tracks online and offline.** Live status, last seen, and 30 days of
  availability per device.
- **Flags what's new or odd.** New and unknown devices wait for your approval;
  randomized phone MACs, IP conflicts and devices that vanished upstream are
  called out on the dashboard.
- **Maps your addresses.** A patch-panel grid per subnet shows every IP as
  online, offline, reserved, free or conflicting, lists the free ranges outside
  the DHCP pool and puts the next free IP one click (or `g f`) away.
- **Reads your other tools.** Proxmox (VMs and LXCs), Pi-hole, AdGuard Home and
  OPNsense (DHCP leases and reservations) and WireGuard (peer status over SSH).
  Syncs fill in details but never overwrite your edits.
- **Tells you when it matters.** New devices, outages, IP conflicts and failing
  integrations to a webhook or ntfy, batched so a big scan sends one message.
- **Fits into scripts.** A JSON API with personal tokens, CSV/JSON export, CSV
  import with a preview, and Prometheus metrics.
- **Shares safely.** Admin and viewer roles, single sign-on through OpenID
  Connect, an audit log, and encrypted integration credentials.

## Screenshots

<table>
  <tr>
    <td width="50%"><img src="docs/images/devices.png" alt="Device list"></td>
    <td width="50%"><img src="docs/images/device.png" alt="Device page"></td>
  </tr>
  <tr>
    <td>Every device, searchable and filterable, with bulk approve and tag.</td>
    <td>One device: availability, details, links, interfaces and history.</td>
  </tr>
  <tr>
    <td>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/images/grid-dark.png">
        <img src="docs/images/grid-light.png" alt="Subnet grid">
      </picture>
    </td>
    <td align="center"><img src="docs/images/phone.png" width="200" alt="netis on a phone"></td>
  </tr>
  <tr>
    <td>A subnet as a patch panel: one port per address.</td>
    <td>The same app on a phone, with a bottom tab bar.</td>
  </tr>
</table>

## Quick start

**Docker**

```sh
docker run -d --name netis --network host \
  -v netis-data:/data -e NETIS_DB=/data/netis.db \
  ghcr.io/13/netis:latest
```

> [!NOTE]
> `--network host` lets netis read the host's ARP table, which is where MAC
> addresses and vendors come from. On a bridged network it still pings and
> tracks online/offline, but finds no MACs.

**Release binary** (Linux amd64; other architectures build from source)

```sh
tar -xzf netis_<version>_linux_amd64.tar.gz
NETIS_DB=/var/lib/netis/netis.db NETIS_PRIVILEGED_ICMP=1 ./netis
```

Raw ICMP needs `CAP_NET_RAW` (`sudo setcap cap_net_raw+ep ./netis`); without
`NETIS_PRIVILEGED_ICMP` netis uses unprivileged UDP-ICMP instead.

**Proxmox LXC** — copy the binary to `/opt/netis/netis`, add a `netis` system
user and install the sandboxed unit from `deploy/netis.service`. See
[Proxmox LXC with systemd](docs/install.md#proxmox-lxc-with-systemd).

Then open `http://<host>:8080/setup`: create the admin account, pick the
subnets to scan, optionally connect your integrations, and watch the first scan
come in. More in [docs/install.md](docs/install.md).

## Configuration

Most settings live in the web UI. The environment variables you are most likely
to set:

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_ADDR` | `:8080` | HTTP listen address. |
| `NETIS_DB` | `netis.db` | SQLite path, or a `postgres://` URL for Postgres. |
| `NETIS_PRIVILEGED_ICMP` | unset | `1` sends raw ICMP (needs `CAP_NET_RAW` or root). |
| `NETIS_SECRET_KEY` | unset | Key that encrypts stored integration credentials. |
| `NETIS_BACKUP_DIR` | unset | Directory for scheduled SQLite backups. |

All of them, and every setting, are in [docs/configuration.md](docs/configuration.md).

## Documentation

- [Installing](docs/install.md) — Docker, binary, Proxmox LXC, systemd hardening, reverse proxy
- [Using netis](docs/usage.md) — the pages, keyboard shortcuts, theme
- [Configuration](docs/configuration.md) — environment variables and settings
- [Discovery](docs/discovery.md) — how devices are found, named and marked online
- [Integrations](docs/integrations.md) — Proxmox, Pi-hole, AdGuard Home, OPNsense, WireGuard
- [Notifications](docs/notifications.md) — webhook and ntfy
- [API, export and metrics](docs/api.md) — tokens, endpoints, CSV import, Prometheus
- [Users and security](docs/security.md) — roles, SSO with Authelia or Authentik, audit log, encryption
- [Backups and databases](docs/backups.md) — backups, restore, SQLite or Postgres
- [Development](docs/development.md) — building, tests, design system

## Limitations

- MAC addresses are only discovered on subnets on the same L2 segment as netis;
  routed subnets get ping-only scanning unless a DHCP integration or OPNsense's
  ARP table fills them in.
- A subnet may hold at most 65,536 addresses (an IPv4 `/16`, an IPv6 `/112`).
- Port scans run only on demand, one device at a time.
- WireGuard needs SSH access to the WireGuard host; Proxmox guest IPs need the
  QEMU guest agent.

Details in [Discovery → Limitations](docs/discovery.md#limitations).

## Contributing

Issues and pull requests are welcome. `make build`, `make test` and `make lint`
cover most work; `make e2e` runs the visual regression and accessibility suite
in Docker. See [docs/development.md](docs/development.md) for every target.

## Licence

MIT — see [LICENSE](LICENSE).
