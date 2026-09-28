# Netis — Directed Wake-on-LAN and Stale Integration Devices (F6): Design

Date: 2026-09-28
Status: Approved (standing authorization), pre-implementation

## Purpose

Three gaps in how netis follows the network it models:

1. Wake-on-LAN only sends to `255.255.255.255`. The limited broadcast leaves
   through the default-route interface only, so a host on another VLAN or
   subnet is never woken.
2. A Proxmox guest deleted in Proxmox, or a WireGuard peer removed from the
   server, stays in netis looking exactly like a live one.
3. A WireGuard peer's AllowedIPs are read only when the peer is first created;
   later changes on the server never reach netis.

## 1. Directed Wake-on-LAN

- `wol.DirectedBroadcast(prefix)` returns the broadcast address of an IPv4
  prefix (host bits all ones). IPv6 has no broadcast, and `/31` and `/32` have
  no broadcast address, so those return false.
- `wol.Targets(prefixes)` returns `"<bcast>:9"` for each distinct directed
  broadcast, in the order given, followed by `wol.BroadcastAddr`
  (`255.255.255.255:9`) as today.
- `wol.SendAll(mac, addrs, send)` sends to every address with the injected
  `send` (production: `wol.SendTo`) and returns the addresses that worked. It
  fails only when none did, joining the per-address errors. A bad MAC fails
  before anything is sent.
- The handler takes the device's first interface with a MAC (unchanged), looks
  up the subnet of each IP assigned to that interface, and sends to the
  directed broadcast of each subnet plus the limited broadcast. The subnet's
  prefix is what defines the broadcast, not the IP.
- The Server gets a `wolSend` field (defaults to `wol.SendTo`) so tests use a
  fake sender and never put a packet on the network.
- The button becomes an htmx post answered with a toast:
  `Magic packet for aa:bb:… sent to 10.0.20.255, 255.255.255.255`. Without
  htmx (no JS) the handler still redirects back to the device page.
- Directed broadcasts to a subnet that is not directly attached only arrive if
  the router forwards them (often off by default); the README says so.

## 2. Vanished upstream devices

### Storage (migration `0012_upstream_missing`, both dialects)

- `device.upstream_missing_since TEXT NULL` — RFC3339 UTC time the device was
  first found missing from its integration's list; NULL when present. A column
  rather than a custom field: the device list needs it for every row, and a
  custom field would be editable (and deletable) by the user, which would
  silently re-arm the event.
- `event.type` CHECK gains `device_missing` and `device_returned`, on top of
  the list migration 0010 (alerts) left: SQLite rebuilds the event table;
  Postgres drops and re-adds `event_type_check`.
- `UpdateDevice` does not write the column, so the edit form cannot clear it;
  only the syncs do.

### Detection

- `Store.ReconcileUpstream(ctx, scope, seen, now)` — in one transaction, for
  every device in scope: in `seen` and marked → clear, reported as returned;
  not in `seen` and unmarked → set to `now`, reported as gone. Already-marked
  and still-missing devices are untouched (so the event fires once).
  Scopes: Proxmox guests (`source='proxmox' AND proxmox_vmid IS NOT NULL`) and
  WireGuard peers (`source='wireguard' AND wg_pubkey IS NOT NULL`). Proxmox
  nodes are not reconciled: the node list is derived from the guest list, so a
  node with no guests is not evidence that it is gone.
- The syncs collect the ids of the devices they resolved (found or created)
  during the run and reconcile only when the run is **authoritative**:
  - the upstream call succeeded (errors return before this point, as today);
  - every entry resolved to a device id (a store error while looking one up
    would otherwise mark it missing);
  - the list is **non-empty**. An empty list is treated as "cannot tell": a
    Proxmox API token that lost its privileges gets HTTP 200 with an empty
    list, and a misconfigured `wg` command can print nothing. The cost is that
    removing the *last* guest or peer is not flagged until another exists.
    Flagging every device on a transient permission problem would be worse.
- Events: `device_missing` ("proxmox guest web (101) is no longer in Proxmox",
  "wireguard peer 10.6.0.2 was removed from the server") on the transition to
  missing, `device_returned` on the transition back. Emitted after the
  transaction commits.
- Devices are **never deleted** automatically. A missing device keeps all its
  data; the user deletes it if it is really gone. A missing device that comes
  back upstream is re-linked by its VMID / public key as before and the mark
  clears.

### UI

- Device page header and device list row (table and tile view) show a
  `missing upstream` pill (red) with the since-time as its title.
- Events page filter and badge styles include the two new types.

### Notifications

- The two types get their own notification group, "Missing upstream"
  (`notify_upstream`, on by default like the others), with their own titles in
  single messages and summaries. A deleted guest is worth hearing about, and a
  separate toggle lets it be muted without muting new devices or errors.

## 3. WireGuard AllowedIPs refresh

- `ip_assignment.source TEXT NOT NULL DEFAULT ''` (same migration). `''` means
  user/unknown; `'wireguard'` marks rows the WireGuard sync owns. Existing
  static rows of WireGuard-sourced devices inside WireGuard-kind subnets are
  backfilled to `'wireguard'` — that is exactly what the sync has always
  created. (A static IP a user added by hand in that same place is
  indistinguishable and is adopted; this is documented.)
- `Store.SyncIntegrationIPs(ctx, ifaceID, source, want)` in one transaction:
  inserts each wanted (subnet, ip) the interface lacks, as `static` with that
  source; deletes rows of that source that are no longer wanted; never touches
  a row with another source, and never re-labels an existing row (an IP the
  user already recorded stays theirs).
- The WireGuard sync calls it on every run, for new and existing peers, with
  the peer's AllowedIPs that fall inside WireGuard-kind subnets.
- Changing an IP's lease kind from the subnet grid (`SetIPKind`) is a user
  edit, so it also clears `source` and the row becomes the user's.
- If the user deletes the peer's whole device nothing is recreated beyond what
  the sync already did (a new device for an unknown key).

## Testing

- `internal/wol`: broadcast computation (/24, /23, /16, /31, /32, IPv6, a
  non-canonical prefix), target ordering and de-duplication, `SendAll` with a
  fake sender (partial failure, total failure, bad MAC sends nothing).
- Web: WOL handler with a fake sender — targets include the subnet broadcast
  and the limited broadcast, toast lists them, non-htmx still redirects.
- Store (`storetest.EachDialect` / dialect helpers): `ReconcileUpstream`
  transitions and scopes; `SyncIntegrationIPs` add/remove/leave-user-rows;
  migration backfill.
- Syncs on both dialects: guest/peer disappears → marked + one event; second
  run → no new event; reappears → cleared + `device_returned`; empty upstream
  list → nothing marked. WireGuard AllowedIPs change → IP added/removed, user
  row kept.
