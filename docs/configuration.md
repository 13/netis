# Configuration

Every environment variable netis reads, and the settings it keeps in its database.

[← Back to README](../README.md)

- [Environment variables](#environment-variables)
- [Settings](#settings)
- [Subnets](#subnets)

## Environment variables

<details open>
<summary>All environment variables</summary>

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_ADDR` | `:8080` | HTTP listen address. |
| `NETIS_BACKUP_DIR` | unset | Directory the server writes scheduled SQLite backups to. Unset turns them off. See [Scheduled backups](backups.md#scheduled-backups). |
| `NETIS_BACKUP_INTERVAL` | `24h` | Time between scheduled backups (a Go duration: `6h`, `90m`). Values under `1m` or unparseable fall back to the default. |
| `NETIS_BACKUP_KEEP` | `7` | Scheduled backups kept; older ones are deleted. |
| `NETIS_DB` | `netis.db` | Database to use. A path selects SQLite; a `postgres://` URL selects Postgres. See [Database backends](backups.md#database-backends). |
| `NETIS_DB_MAX_OPEN_CONNS` | `10` | Postgres connection pool size. Ignored on SQLite, which is held to one connection to avoid `SQLITE_BUSY`. |
| `NETIS_DB_MAX_IDLE_CONNS` | `5` | Postgres idle connections; clamped to the open limit. |
| `NETIS_PRIVILEGED_ICMP` | unset | Set to `1` to send raw ICMP echo requests (requires `CAP_NET_RAW` or root) instead of the unprivileged UDP-ICMP fallback. |
| `NETIS_OIDC_ISSUER`, `NETIS_OIDC_*` | unset | Single sign-on through an OpenID Connect provider. See [Single sign-on (OIDC)](security.md#single-sign-on-oidc). |
| `NETIS_METRICS_TOKEN` | unset | Bearer token a Prometheus scraper presents to read `/metrics`. Unset means `/metrics` needs a logged-in session. See [Prometheus metrics](api.md#prometheus-metrics). |
| `NETIS_SECRET_KEY` | unset | 32-byte key (base64 or hex) that encrypts stored integration credentials. See [Encrypting stored credentials](security.md#encrypting-stored-credentials). |
| `NETIS_TRUSTED_PROXIES` | unset | Comma-separated CIDRs or addresses of reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` headers netis believes. See [Behind a reverse proxy](install.md#behind-a-reverse-proxy). |

</details>

The single sign-on variables (`NETIS_OIDC_CLIENT_ID`, `NETIS_BASE_URL` and the
rest) have their own table under
[Single sign-on (OIDC)](security.md#single-sign-on-oidc).

## Settings

Settings configured in the web UI (Settings page), stored in the
database's key/value settings table:

<details>
<summary>All settings keys</summary>

| Key | Meaning |
| --- | --- |
| `offline_after` | Consecutive missed scan sweeps before a device is marked offline (default 3). |
| `presence_fallback` | `on` (default) or `off`: count hosts that ignore ping but answer ARP as seen. See [Discovery](discovery.md#hosts-that-ignore-ping). |
| `event_retention_days` | Days of event history to keep, swept every 6h; `0` keeps everything (default 30). |
| `availability_retention_days` | Days of availability history to keep (default 365). One row per interface per hour, so this is the fastest-growing table. |
| `audit_retention_days` | Days of audit log to keep (default 180); `0` keeps everything. |
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
| `adguard_url` | Base URL of AdGuard Home, e.g. `http://adguard.lan:3000`. |
| `adguard_user`, `adguard_password` | AdGuard Home admin login. The password is encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `adguard_insecure` | `1` to skip TLS verification. |
| `opnsense_url` | Base URL of the OPNsense web UI, e.g. `https://opnsense.lan`. |
| `opnsense_key`, `opnsense_secret` | OPNsense API key and secret. The secret is encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `opnsense_insecure` | `1` to skip TLS verification. |
| `notify_webhook_url`, `notify_ntfy_url` | Notification channels; blank disables one. See [Notifications](notifications.md). |
| `notify_webhook_auth`, `notify_ntfy_token` | Webhook `Authorization` header value and ntfy access token. Encrypted at rest when `NETIS_SECRET_KEY` is set. |
| `notify_base_url` | netis's own URL, used to link messages to a device or the event log. |
| `notify_device_new`, `notify_offline`, `notify_ip_conflict`, `notify_sync`, `notify_upstream` | `0` switches that kind of notification off (default on). |

</details>

Each integration's settings are explained in [Integrations](integrations.md).

## Subnets

Subnets (CIDR, kind, scan interval, scan enabled) are managed via
Settings, not environment variables — add at least one subnet during or
after first-run setup for scanning to do anything. **Settings → Network** is
the one place subnets are added, edited and removed, next to the offline
threshold, presence without ping and defaults for new subnets.

A subnet may hold at most 65,536 addresses (an IPv4 `/16`, an IPv6 `/112`).
Every address becomes a grid cell and a sweep target, so a wider prefix is
refused when the subnet is saved rather than discovered when the page is
opened.
