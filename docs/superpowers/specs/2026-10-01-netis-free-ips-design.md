# netis free static IPs (F): design

Date: 2026-10-01. Status: implemented in `6fc9073` and `c350056`, merged in
`f87c5ae`; plan in `docs/superpowers/plans/2026-10-01-netis-free-ips.md`.
Written after the merge to record what was built and why.

## Problem

Handing out a static address meant opening the subnet grid and reading it.
"Next free IP" was the lowest unheld address, which on most home LANs lies
inside the router's DHCP pool: assigning it statically collides with a lease
sooner or later. The free count and grid made no difference between the two.
A port's details were a native `title` tooltip: slow, unstyled, and not
reachable from the keyboard.

## Goals

- A subnet can know its DHCP pool, and nothing offered as free lies in it.
- The next free address of any subnet is one click (dashboard) or two keys
  (`g f`, or "free" in the palette) away, and can be copied without opening
  anything.
- On the subnet page, the room for a block of static addresses shows at a
  glance: free ranges, largest first, and a "Free only" view.
- A port shows its address, state, lease and holder in a card, by pointer
  or keyboard, and **C** copies its address.

Non-goals: reading the pool from a DHCP integration, creating reservations
upstream, IPv6 pools beyond full addresses, a free-IP API endpoint.

## DHCP pool

- Two subnet columns, `dhcp_start` and `dhcp_end` (migration `0018_dhcp_pool`
  in both dialects), `TEXT NOT NULL DEFAULT ''`. Both empty means unknown.
- `store.Subnet.InDHCPPool(ip)` is inclusive at both ends and false for every
  address when no pool is set.
- The subnet forms (Settings → Network) take a full address or, for IPv4, a
  last octet ("100"), as routers usually write it. Both blank, or both set,
  inside the subnet, first ≤ last; otherwise an inline error.
- The subnets API returns `dhcp_start` / `dhcp_end` when set.

## Free for static use

One definition everywhere: a host address no device holds and outside the
pool. `freeSummary` (dashboard, palette) and the grid's `free` vs `pool`
cell state both follow it.

- **Dashboard:** each subnet row shows its next free IP, a link that opens
  the subnet with that address selected, and a copy button.
- **Palette:** a query starting with `free` lists each subnet's next free
  address and free count; words after it narrow by name or CIDR. Enter opens,
  Shift-Enter copies. `g f` opens the palette on "free".
- **Subnet page:** the header lists free ranges, largest six first, shown in
  address order, compact labels (`.50–.99`) for IPv4 within a /24. A click
  marks the run and opens its first address. "Free only" fades every port
  that cannot be handed out, remembered per browser. Next free IP gets a
  copy button. Pool addresses are drawn stippled and counted apart.

## Port hover card

One card element per grid, filled from the grid's JSON (`#grid-data`): name,
MAC, last seen and claim count for held addresses. It opens after a short
delay, then follows the pointer without delay; it sits below the port, or
above when there is no room, and stays inside the window. It is
`aria-hidden`, since the port's accessible name already says the same.
**C** copies the address under the pointer or the focused one; copying falls
back to `execCommand` outside a secure context (plain-http LAN installs).

## Testing

Store: pool round-trip and bounds (`TestSubnetDHCPPool`). Web: pool parsing
and the form (`TestParseDHCPPool`, `TestSubnetFormDHCPPool`), free ranges,
the grid with a pool (`TestGridDHCPPool`), the palette query
(`TestSearchFree`). The e2e seed gives LAN a pool `.100–.199`; screenshot
baselines re-taken.
