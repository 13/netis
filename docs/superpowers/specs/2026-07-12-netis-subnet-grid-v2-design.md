# Netis — Subnet Occupancy Grid v2 (Sub-project C3): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Third and final sub-project in the current batch (C1 device model → C2 scanning
UX → **C3 subnet grid v2**). C3 reuses the existing grid page/`GridFrag`,
`SubnetOccupancy`, the `ip_assignment.kind` column, the `#modal` + `dialog.js`
shell, the `#toasts` toast, and the `grid:{id}` SSE refresh.

## Purpose

Turn the subnet page's occupancy grid into a dense, full-range map that shows,
at a glance, which addresses are free / reserved / online / offline / conflicting
**and** whether each taken IP is a static or DHCP lease — and let the user flip a
taken IP between static and DHCP by clicking its square.

## Decisions (locked)

- **Full range.** Render every address network..broadcast (e.g. `.0`–`.255` for a
  /24), not just usable hosts. Network and broadcast are shown as muted,
  non-clickable `edge` squares.
- **Two visual axes.** Fill color = occupancy/liveness (free / reserved / online /
  offline / conflict); border = lease kind (solid = static, dashed = dhcp).
- **Click → popup.** Clicking an assigned square opens a small popup (the modal
  shell) showing the IP, its device (link), current lease, and Set static /
  Set DHCP buttons. Free and edge squares are not clickable.
- **Static ↔ DHCP only.** The popup toggles the lease kind of an already-assigned
  IP. No reserve/free (those need placeholder device/interface handling) — out of
  scope.
- No schema/migration change: `ip_assignment.kind` already carries the
  `static`/`dhcp` CHECK.

## Component 1: Data model

### Store (`internal/store/grid.go`)

- `Occupant` gains `Kind string`. `SubnetOccupancy`'s query selects `a.kind`; the
  first occupant found for an IP represents the square (same rule already used for
  the occupant's other fields). Add `a.kind` to the `SELECT` and scan it into
  `o.Kind`.
- New `func (s *Store) SetIPKind(subnetID int64, ip, kind string) error`:
  ```go
  _, err := s.DB.Exec(`UPDATE ip_assignment SET kind=? WHERE subnet_id=? AND ip=?`,
      kind, subnetID, ip)
  return err
  ```
  The web layer validates `kind ∈ {static,dhcp}` before calling. A 0-row update
  (free/unknown IP) is harmless.

### Scan (`internal/scan/sweep.go`)

- New `func AllIPs(cidr string) ([]string, error)` — the full range
  network..broadcast, no edge trim.
- Refactor `HostIPs` to call `AllIPs` then drop the network+broadcast edges for
  IPv4 subnets with `bits < 31` (behavior unchanged; its existing test still
  passes). Concretely, `AllIPs` holds the current range-generation loop, and
  `HostIPs` becomes:
  ```go
  func HostIPs(cidr string) ([]string, error) {
      out, err := AllIPs(cidr)
      if err != nil {
          return nil, err
      }
      prefix, _ := netip.ParsePrefix(cidr)
      prefix = prefix.Masked()
      if prefix.Addr().Is4() && prefix.Bits() < 31 && len(out) >= 2 {
          out = out[1 : len(out)-1]
      }
      return out, nil
  }
  ```

## Component 2: Grid rendering

### `gridCells` (`internal/web/grid.go`)

Switch the range source from `scan.HostIPs` to `scan.AllIPs`. For each address:

- Determine `edge`: an address is an edge when the subnet is IPv4 with
  `bits < 31` and the address is the first (network) or last (broadcast) of the
  full range. Edge cells get `State = "edge"`, no device link, no click.
- Non-edge addresses keep the existing state logic against `SubnetOccupancy`:
  `conflict` (`o.Count > 1`) · `reserved` (`!o.EverSeen`) · `online` (`o.Online`) ·
  `offline` (otherwise) · `free` (no occupant).
- Carry `Kind` (the occupant's `o.Kind`) onto the cell for assigned squares.

`views.GridCell` gains `Kind string`.

### `GridFrag` (`internal/web/views/grid.templ`)

Each square renders as `.sq` with its state class plus, when assigned, the lease
class (`static` / `dhcp`) for the border. Interaction:

- **Assigned cells** (occupant present, i.e. `c.DeviceID > 0`) render as a
  `<button>` with `class={ "sq", c.State, c.Kind }`,
  `hx-get="/subnets/{id}/cell?ip={c.IP}"`, `hx-target="#modal"`, `title={c.Title}`.
- **Free and edge cells** render as a plain `<span class={ "sq", c.State }>`
  (no `hx-get`).

The device→square navigation moves into the popup (which links to the device), so
nothing is lost by dropping the direct `<a>` link.

### Legend (`GridPage`)

Keep the fill swatches (online / offline / reserved / free / conflict) and add two
border swatches: a solid-bordered square labeled **static** and a dashed-bordered
square labeled **dhcp**.

### `app.css`

Additive: `.sq.static` (solid accent border), `.sq.dhcp` (dashed muted border),
`.sq.edge` (muted/striped fill, `cursor:default`), `cursor:pointer` on `button.sq`,
and a slightly denser default `.grid` cell size so a full /24 reads compactly.

## Component 3: Cell popup + handlers

### `CellDetail` fragment (`grid.templ`)

A `.dialog-scrim > .dialog` (reuses the modal shell; `dialog.js` handles ✕ /
scrim / Escape close):

- **Header:** the IP + a ✕ close button (`data-close`).
- **Body:** the device name linking to `/devices/{id}`, MAC, last-seen, the
  current state, and the current lease kind.
- **Footer:** `Set static` and `Set DHCP` buttons, each a `<form>`-free
  `hx-post="/subnets/{id}/cell"` carrying hidden `ip` and `kind`, with
  `hx-target="#toasts" hx-swap="beforeend"` and
  `hx-on::after-request="document.getElementById('modal').innerHTML=''"` to close
  the popup. The button matching the current kind is marked active (disabled).

Signature: `templ CellDetail(sn store.Subnet, ip string, o store.Occupant)`.

### Handlers (`internal/web/grid.go`)

- `handleCellDetail` (`GET /subnets/{id}/cell?ip=…`, auth): load the subnet (404
  on bad/nonexistent id via `subnetFromPath`), read `ip` from the query, look it
  up in `SubnetOccupancy`; render `CellDetail` with the occupant (or a read-only
  "free — no assignment" variant when the IP has no occupant).
- `handleCellKind` (`POST /subnets/{id}/cell`, `requireAdmin`): read `ip` and
  `kind`; if `kind` is not `static` or `dhcp` → 400; `SetIPKind(sn.ID, ip, kind)`;
  `s.broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")` so the open grid
  page reloads `GridFrag` (border flips) via its existing `sse:grid:{id}` trigger;
  render a toast (`ScanToast(ip + " → " + kind)`).

### Routes (`internal/web/server.go`)

```go
s.mux.HandleFunc("GET /subnets/{id}/cell", s.handleCellDetail)
s.mux.HandleFunc("POST /subnets/{id}/cell", s.requireAdmin(s.handleCellKind))
```

## Data flow (toggle)

click assigned square → `GET …/cell` renders popup into `#modal` → click **Set
static** → `POST …/cell` → `SetIPKind` → `broker.Publish("grid:{id}")` → the grid
page's SSE trigger reloads `GridFrag` with the new border, a toast appears in
`#toasts`, and `hx-on::after-request` clears `#modal`. All via existing infra.

## Error handling

- `POST …/cell` with a `kind` other than `static`/`dhcp` → 400.
- Bad/nonexistent subnet id → 404 (via `subnetFromPath`).
- `SetIPKind` on a free/unknown IP updates 0 rows → the toast still renders
  ("… → static"); no error. (Normal cells for free IPs aren't clickable, so this
  is only reachable by a crafted request.)
- `handleCellDetail` for an IP with no occupant → read-only "free" fragment.

## Testing

- **scan** (`internal/scan/sweep_test.go`): `AllIPs("192.168.1.0/30")` returns
  `["192.168.1.0","192.168.1.1","192.168.1.2","192.168.1.3"]` (network +
  broadcast included); the existing `HostIPs` test (`.1`,`.2`) still passes.
- **store** (`internal/store/grid_test.go`): seed device + iface + IP with kind
  `static`; `SetIPKind(subnetID, ip, "dhcp")` then `SubnetOccupancy[ip].Kind ==
  "dhcp"`; confirm `SubnetOccupancy` populates `Kind`.
- **web** (`internal/web/grid_test.go`):
  - The grid page for a `/24` (or `/30`) renders an `edge`-classed square for the
    network address and, for an assigned static IP, a square whose class contains
    `static`.
  - `GET /subnets/{id}/cell?ip=<assigned>` renders the device link
    (`/devices/{id}`) and both `Set static` and `Set DHCP` controls.
  - `POST /subnets/{id}/cell` with `ip=<assigned>&kind=dhcp` returns 200 with a
    toast and flips the stored kind to `dhcp` (verify via `SubnetOccupancy`).
  - `POST …/cell` with `kind=bogus` → 400.
  - A viewer session → 403 on `POST …/cell`.

## Project layout (files added / modified)

- Modify: `internal/store/grid.go` (`Occupant.Kind`, `SubnetOccupancy` select,
  `SetIPKind`) + `internal/store/grid_test.go`.
- Modify: `internal/scan/sweep.go` (`AllIPs`, `HostIPs` refactor) +
  `internal/scan/sweep_test.go`.
- Modify: `internal/web/grid.go` (`gridCells` → `AllIPs` + edge/kind,
  `handleCellDetail`, `handleCellKind`) + `internal/web/grid_test.go`.
- Modify: `internal/web/server.go` (two `/subnets/{id}/cell` routes).
- Modify: `internal/web/views/grid.templ` (`GridCell.Kind`, `GridFrag`
  clickable/lease/edge, legend, `CellDetail`) + regenerated
  `internal/web/views/grid_templ.go`.
- Modify: `internal/web/static/app.css` (`.sq.static`/`.dhcp`/`.edge`, cursors,
  denser grid).

## Out of scope (C3)

- Reserve a free IP / free an assigned IP (needs a placeholder device/interface).
- Moving or renumbering an IP; bulk range edits.
- Grid virtualization for very large subnets (all cells render, as today).
- Any change to the dashboard occupancy bars (they keep using `HostIPs`).
