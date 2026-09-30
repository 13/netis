# Discovery

How netis finds devices on your subnets, decides whether they are online, and names them — and what it cannot see.

[← Back to README](../README.md)

- [Scanning](#scanning)
- [Hosts that ignore ping](#hosts-that-ignore-ping)
- [Randomized MACs](#randomized-macs)
- [Names](#names)
- [Limitations](#limitations)

## Scanning

The background scan loop pings every address of each enabled subnet (every
120s by default; each subnet has its own scan interval under **Settings →
Network**), then reads the kernel ARP table for MAC addresses. A device is
marked offline after `offline_after` consecutive missed sweeps (default 3).
Raw ICMP needs `NETIS_PRIVILEGED_ICMP=1` and `CAP_NET_RAW` (see
[Installing netis](install.md)); if most probes in a sweep cannot be sent at
all, the sweep is reported as a scan error and device states are left alone.

## Hosts that ignore ping

Sleeping phones, Windows with its firewall on and a lot of IoT gear drop ICMP.
On a directly attached subnet the sweep's pings still make the kernel ARP for
every address, so a host that answers ARP ends up with a complete entry in
`/proc/net/arp`. Such a host is counted as seen when a TCP connect to one of
ports 22, 80, 443, 445, 62078 or 8080 is accepted or refused (400 ms), or,
failing that, when its ARP entry still resolves to the same MAC about 9 seconds
after the sweep, by which time the kernel has re-probed a stale entry and
dropped it if nobody answered. A host that left within the last half minute can
therefore look present for one more sweep. Turn it off with **Presence without
ping** in Settings → Network (the `presence_fallback` setting). Routed subnets
have no ARP entries, so it does nothing there.

## Randomized MACs

A MAC with the locally administered bit set (phones' "Private Wi-Fi Address")
gets a *private MAC* badge on the device list and page, and `private_mac` in
the API. A new device on one is named `private-<mac>` and its event says it may
be a phone. The QEMU/KVM (`52:54:00`) and Docker (`02:42`) prefixes are not
counted.

## Names

Reverse DNS first, then an mDNS reverse lookup (a PTR query to
`224.0.0.251:5353` asking for a unicast answer), which is where Apple devices,
printers and Avahi hosts name themselves. Lookups run 16 at a time; an address
that returned no name is retried after 30 minutes. A discovered name only fills
an interface hostname that is empty.

## Filling in device details

netis fills in vendor, model, kind, icon, function and tags from what it can
see: the maker registered for the MAC address (the IEEE registry, built in),
the hostname (`BRW3C2AF4A1B2C3` is a Brother printer, `Galaxy-S23` a Samsung
phone) and open ports found by a port scan (9100 is a printer, 8006 Proxmox).

On subnets the netis host is directly attached to, netis also listens for
what devices announce about themselves on the local network: mDNS (Bonjour)
names Chromecasts, AirPlay speakers, printers, HomeKit and ESPHome devices,
and UPnP names TVs, speakers and routers. It asks after a sweep, at most every
15 minutes per subnet. In Docker this needs host networking, which netis
already requires (see [Docker](install.md#docker)). Devices answer these
questions straight back to a random port on the netis host, so a stateful
host firewall there (ufw, firewalld) can drop the answers.

It only fills a field that is empty (kind counts as empty while it is Other),
and it never changes a value you set: once you edit or clear a value netis
filled, that field is yours. A tag you remove stays removed. On a device's
page, **Detected** marks values netis filled, and **What netis detected**
lists every clue and whether it was used. When you edit a device, the vendor,
model and function fields suggest values as you type, and "Use detected"
fills in what netis detected where it differs from what the device holds.

Turn it off under Settings, Network, "Fill in device details automatically".
Every change is recorded in the audit log as user `netis`.

The MAC registry is refreshed with `make oui`.

## Limitations

- A subnet may hold at most 65,536 addresses (an IPv4 `/16`, an IPv6 `/112`).
  Every address becomes a grid cell and a sweep target, so a wider prefix is
  refused when the subnet is saved rather than discovered when the page is
  opened.
- MAC address discovery only works for subnets on the same local L2
  segment as the netis host (it reads the kernel ARP table after pinging).
  Remote/routed subnets get ping-only scanning: online/offline status and
  IP tracking work, but no MAC or vendor — unless a DHCP integration leases
  those addresses, or the [OPNsense integration](integrations.md#opnsense) can
  read the router's ARP table. In Docker this needs `--network host` (see
  [Docker](install.md#docker)).
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
