# Integrations

How netis reads devices and leases from Proxmox, Pi-hole, AdGuard Home, OPNsense and WireGuard, and how syncs treat what you have edited.

[← Back to README](../README.md)

- [How syncs run](#how-syncs-run)
- [Syncs never overwrite your edits](#syncs-never-overwrite-your-edits)
- [Proxmox](#proxmox)
- [Pi-hole (v6)](#pi-hole-v6)
- [AdGuard Home and OPNsense](#adguard-home-and-opnsense)
- [WireGuard](#wireguard)
- [Missing upstream](#missing-upstream)

Every integration is set up in **Settings → Integrations** (or the setup
wizard); the keys it stores are listed under
[Settings](configuration.md#settings). Passwords, secrets and tokens are
encrypted at rest when `NETIS_SECRET_KEY` is set (see
[Encrypting stored credentials](security.md#encrypting-stored-credentials)).

## How syncs run

Each configured integration (Proxmox, Pi-hole, AdGuard Home, OPNsense, WireGuard) syncs once a minute
and on demand from Settings → Integrations → Run now. A run is cut off after
30 seconds. Its result — connected with a count, or failing with a category
(timeout, authentication failed, host key rejected, connection failed,
configuration error, sync error) — is shown on the settings page, the dashboard
and in `/api/status`, and the first failure of an outage adds a `scan_error`
event (its end adds a `sync_recovered` one). The full error, which can name key paths and internal addresses, goes
only to the server log. Clicking Run now while that integration is
already syncing reports "already running" instead of starting a second run.

## Syncs never overwrite your edits

Syncs fill in devices but never overwrite your edits. A Proxmox guest or node
takes its name and kind from Proxmox only when it is first imported; renaming
it or changing its kind in netis sticks. A guest's parent follows it when it
moves between Proxmox nodes, but a parent you set to a non-Proxmox device is
kept. Nodes are recognised by the `proxmox_node` custom field the sync adds.
When the scanner found a guest first, the guest takes over that device's
interface (and its IPs) as long as you have not reviewed the discovered device;
the discovered device is deleted if nothing else is left on it. A reviewed
device keeps its interface.

## Proxmox

Settings: `proxmox_url` (base URL of the Proxmox API, e.g.
`https://pve.local:8006`), `proxmox_token_id` (API token ID, e.g.
`user@pam!netis`), `proxmox_secret` (the token secret) and `proxmox_insecure`
(`1` to skip TLS verification for self-signed certs).

The sync imports VMs and LXCs automatically, with each node as the parent
device of its guests (the name, kind and parent rules are under
[Syncs never overwrite your edits](#syncs-never-overwrite-your-edits)).

> [!NOTE]
> Proxmox guest IPs depend on the QEMU guest agent being installed and
> running in the VM; without it, only the guest's configured MAC/bridge
> is known, not its IP.

## Pi-hole (v6)

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

## AdGuard Home and OPNsense

Two more DHCP servers netis can read leases from, set in Settings →
Integrations (or the setup wizard). Their leases and reservations are merged
exactly like Pi-hole's — matched by MAC, reservations `static`, a moved lease
dropping the old DHCP address, nothing you set overwritten — and devices they
discover get source `adguard` or `opnsense`.

### AdGuard Home

Settings: `adguard_url`, `adguard_user`, `adguard_password`,
`adguard_insecure`. netis reads `/control/dhcp/status` with the admin login.
Only AdGuard's built-in DHCP server is read; when it is switched off the
integration reports "DHCP server disabled in AdGuard Home" and changes
nothing.

### OPNsense

Settings: `opnsense_url`, `opnsense_key`, `opnsense_secret`,
`opnsense_insecure`. Create an API key under System → Access → Users. netis
reads leases from Kea, ISC dhcpd or Dnsmasq — whichever has them, in that
order — and Kea reservations and ISC static mappings as reservations. The
key needs the privileges for the DHCP server in use (for example "Services:
Kea DHCP" or "Status: DHCP leases") and, optionally, "Diagnostics: ARP
Table".

With the ARP privilege netis also reads the firewall's ARP table, which
fills in MAC addresses on routed subnets where netis's own scan sees only
IPs: an interface at an address that has no MAC yet gets the one the
firewall saw there. It is careful about it — expired entries, a MAC seen at
several addresses, an address two devices claim and a MAC another device
already has are skipped, nothing is created from ARP, and a MAC that is set
is never changed. Without the privilege the leases still sync and the status
line says the ARP table was unavailable.

## WireGuard

Settings: `wg_ssh_addr` (SSH address of the host running WireGuard,
`host:port`), `wg_ssh_user`, `wg_ssh_key_path` (the SSH private key),
`wg_ssh_known_hosts` (an OpenSSH `known_hosts` file used to verify the host;
unset means the host is **not** verified) and `wg_iface` (default `wg0`).

WireGuard peer status is read by SSHing into the host running WireGuard
and parsing `wg show dump`; it is not a local integration and requires
a reachable SSH endpoint with a configured key.

> [!WARNING]
> Under the sandboxed systemd unit, a key or `known_hosts` file in `/root` or
> `/home` is invisible to netis. Keep them under `/etc/netis` or
> `/var/lib/netis`; see [Sandboxing](install.md#sandboxing).

A WireGuard peer's AllowedIPs are followed on every sync: addresses inside a
WireGuard subnet are added, and ones the sync added that the server no longer
allows are removed. Addresses you recorded yourself are never removed, and
changing an address's lease kind on the subnet grid makes it yours. (When
upgrading, static addresses already on WireGuard peers inside WireGuard subnets
are taken to be the sync's, since that is what it always created.)

## Missing upstream

A Proxmox guest deleted in Proxmox, or a peer removed from the WireGuard
server, is **not** deleted from netis: it gets a red **missing upstream** badge
on its page and in the device list, and one `device_missing` event. If it comes
back (same VMID or public key) the badge clears with a `device_returned` event.
Delete the device yourself once you know it is gone. A device is only marked
after a sync that succeeded and returned a non-empty list: a failed call marks
nothing, and neither does an empty list, since a Proxmox token that lost its
permissions sees no guests at all. The flip side is that removing the very last
guest or peer is not flagged until another one exists. Proxmox nodes are never
marked, because netis only learns of nodes through their guests.
