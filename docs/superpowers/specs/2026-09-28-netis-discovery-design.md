# netis discovery improvements (F3)

Date: 2026-09-28. Status: implemented on branch `f3-discovery`.

Three independent improvements to what a sweep learns about a host.

## 1. Presence without ICMP

**Problem.** Phones asleep, Windows with its default firewall and plenty of IoT
gear drop ICMP echo. A sweep counts them as misses, so they flap or sit offline
while perfectly present on the LAN.

**Signal.** The sweep already pings every host address and then reads
`/proc/net/arp`. Pinging an address on a directly attached subnet forces the
kernel to resolve it, so a host that ignores ICMP but answers ARP ends up with
a COMPLETE entry (`ATF_COM`, flag `0x2`). Routed subnets never get ARP entries
for their hosts, so the fallback is naturally limited to attached subnets and
does nothing elsewhere.

**Staleness.** `/proc/net/arp` does not show the neighbour (NUD) state. An entry
for a host that left a few minutes ago can still read COMPLETE while it is
STALE. The ping helps here: sending a packet to a STALE entry moves it to
DELAY, then after `delay_first_probe_time` (5 s) to PROBE, where the kernel
sends `ucast_solicit` (3) unicast ARP requests `retrans_time` (1 s) apart and
marks the entry FAILED (shown as flags `0x0`) if none is answered. So roughly
8 s after the ping a stale entry for a departed host stops reading COMPLETE; a
REACHABLE entry means the host confirmed itself within the last ~30 s.

**Decision.** For every address that missed ICMP (no probe error) and has a
COMPLETE ARP entry:

1. Confirm with a TCP connect to 22, 80, 443, 445, 62078 (iOS lockdownd) and
   8080, all ports in parallel, 400 ms timeout. A completed handshake *or* a
   refusal (RST) proves the host is up. Hosts are probed with bounded
   parallelism (16).
2. Hosts TCP did not confirm are re-checked against the ARP table once at
   least 9 s have passed since the sweep ended (the wait only happens when such
   hosts exist, and overlaps the TCP probes). If the entry is still COMPLETE
   with the same MAC, the kernel's own unicast re-probe was answered: the host
   is present.

A confirmed address is then treated exactly as if it had answered ping, so the
existing identity logic applies unchanged: a known IP with a matching MAC is
marked seen, a MAC at a new IP is an `ip_changed` move, an unknown MAC is a
new device. RTT is the TCP connect time when TCP confirmed, otherwise 0.

**Trade-offs.**
- A host that left within the last ~30 s (REACHABLE entry) can be counted
  present for one more sweep. Offline detection already needs several
  consecutive misses (`offline_after`), so this delays it by at most a sweep.
- The wait makes a sweep of a subnet with ICMP-silent hosts ~9 s longer. Sweeps
  run in their own goroutines and default to 120 s intervals, so this is cheap.
- TCP probes touch at most six ports on hosts that already answered ARP, never
  a whole subnet.
- Reading NUD state via netlink (`RTM_GETNEIGH`) would avoid the wait but adds
  a netlink parser for little gain; not done.

**Setting.** `presence_fallback` (`on`/`off`, default on) in Settings >
General, read on every sweep like `offline_after`.

## 2. Randomized (private) MACs

A MAC whose first octet has bit `0x02` set is locally administered. Phones use
such addresses for "Private Wi-Fi Address" / "Randomized MAC". Computed from
the MAC, no schema change (`internal/macaddr.IsPrivate`).

Virtualization prefixes that are locally administered but stable are excluded
so VMs do not all read as phones: `52:54:00` (QEMU/KVM) and `02:42` (Docker).

- Device list and device page show a small `private MAC` chip.
- JSON API devices gain `"private_mac": true|false` (any of its MACs private).
- A new device discovered with a private MAC and no resolved name is named
  `private-<mac>` instead of `unknown-<mac>`, and its `device_new` event text
  adds "(randomized MAC — may be a phone with Private Wi-Fi Address)".

## 3. Name discovery

- `ResolveName` keeps reverse DNS (500 ms) and, when it returns nothing, sends
  an mDNS PTR query for `d.c.b.a.in-addr.arpa.` to `224.0.0.251:5353` with the
  unicast-response (QU) bit, from an ephemeral port, and waits 400 ms for a
  unicast answer. The query goes out of the interface whose address covers the
  target, when one does. Packets are built and parsed with
  `golang.org/x/net/dns/dnsmessage` (x/net was already an indirect dependency).
- Names are resolved concurrently (16 at a time) before the sweep's writes, for
  alive addresses that would become new devices and for known interfaces with
  no hostname. Known interfaces are filled with `SetIfaceHostnameIfEmpty`, so a
  user- or integration-set hostname is never overwritten; device names are not
  touched after creation.
- An address that resolved to nothing is not retried for 30 minutes, so
  nameless hosts do not cost a lookup every sweep.
