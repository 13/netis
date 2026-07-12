# Netis — Device-List Parent/Child + Sortable Subnet List (Sub-project F3): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Third and final sub-project in this batch (F1 quick fixes ✅ → F2 device-detail
overhaul ✅ → **F3 list tree + sortable subnet list**). F3 groups child devices
under their parent in the Devices list, and makes the subnet page's device list
the same sortable table as the Devices page.

## Purpose

1. **Parent/child in the Devices list.** A device with a parent should appear
   indented directly below that parent (item 5 of the user's list). Children
   follow their parent regardless of the active column sort; siblings and roots
   are sorted normally.
2. **Sortable subnet device list.** The subnet (grid v2) page's "Devices in this
   subnet" table is currently static; make it the same sortable table as
   `/devices` (item 2), with its sort links staying on the subnet page.

Both hinge on one refactor: extract the Devices-page sortable `<table>` into a
reusable `deviceTable` component whose sort links take a **base path**, so it can
render on both `/devices` and `/subnets/{id}`.

## Component 1: Shared sortable `deviceTable` + parameterized sort links

Today `sortHeader` / `sortURL` (in `devices.templ`) hardcode `/devices`. Add a
`base` parameter so a sort link targets whatever page hosts the table:

- `func sortURL(base, q, col, curSort, curDir string) string` →
  `base + "?q=" + url.QueryEscape(q) + "&sort=" + col + "&dir=" + nextDir(...)`.
- `templ sortHeader(label, col, curSort, curDir, q, base string)` → uses
  `sortURL(base, ...)`.

Extract the table (thead with sort headers + tbody) into:

```
templ deviceTable(rows []store.DeviceRow, q, sortKey, dir, base string, grouped bool)
```

- thead: `@sortHeader(…, base)` for Device/IP/Status/Kind/Last-seen; plain `<th>`
  for Lease/Function/Tags/actions (unchanged columns).
- tbody: when `grouped`, iterate `GroupByParent(rows)` emitting
  `@deviceRow(g.Row, g.Depth)`; otherwise iterate rows flat as
  `@deviceRow(row, 0)`.

`deviceRow` gains a depth argument: `templ deviceRow(row store.DeviceRow, depth int)`.
The name cell renders `depth` indent spacers plus a "↳" branch marker when
`depth > 0`; everything else is unchanged. `deviceRow`'s only call site becomes
`deviceTable` (both grouped and flat paths), so there is a single row template.

`DeviceList` renders `@deviceTable(rows, q, sortKey, dir, "/devices", true)`
inside its existing `#dev-list` wrapper; the toolbar, the list/grid segmented
control, and the `#dev-grid` tile view are unchanged (tiles stay flat — a
spatial overview, not a tree).

## Component 2: Parent/child grouping (`GroupByParent`)

Add to the `views` package (co-located with `deviceRow`, which consumes it):

```go
type DeviceGroupRow struct {
	Row   store.DeviceRow
	Depth int
}

func GroupByParent(rows []store.DeviceRow) []DeviceGroupRow
```

Algorithm (stable, preserves the incoming sort within each sibling set):

- Build `byParent map[int64][]store.DeviceRow` and a set of present IDs, iterating
  `rows` in order (so children lists inherit the already-applied sort).
- **Roots** = rows whose `ParentDeviceID` is nil, OR whose parent id is not in
  the present set (an orphan whose parent is filtered out / on another view is
  treated as a root, so no row is ever dropped). Roots keep their incoming order.
- DFS each root, emitting the row at its depth then recursing into
  `byParent[row.ID]` at `depth+1`. Arbitrary nesting depth is supported.
- A `visited` set guards against a cyclic `parent_device_id` (data corruption):
  a row is emitted at most once; a back-edge is skipped. Guarantees termination.

The result length equals `len(rows)` (every input row emitted exactly once).

Grouping applies **only** to the Devices page (`grouped=true`). The subnet list
is a filtered subset where a parent is often not in-subnet, so it renders flat
(`grouped=false`).

## Component 3: Subnet device list wired to `deviceTable`

- `GridPage` gains `sortKey, dir string` params:
  `GridPage(username string, sn store.Subnet, cells []GridCell, devices []store.DeviceRow, sortKey, dir string)`.
  Its static "Devices in this subnet" table is replaced by
  `@deviceTable(devices, "", sortKey, dir, fmt.Sprintf("/subnets/%d", sn.ID), false)`.
  The heading, count, "New device" button, empty-state, and scope note are kept.
- `handleSubnetPage` parses and validates the sort params (shared with the
  Devices handler) and sorts the in-subnet rows before rendering:

```go
sortKey, dir := parseDeviceSort(r)      // new shared helper in devices.go
sortDeviceRows(devices, sortKey, dir)
views.GridPage(u.Username, sn, cells, devices, sortKey, dir).Render(...)
```

- `parseDeviceSort(r *http.Request) (string, string)` extracts the existing
  validation from `handleDeviceList` (whitelist `ip/name/status/kind/seen`
  default `ip`; `dir` default `asc`, only `desc` honored) and both handlers call
  it. Sort links on the subnet page are plain `<a href="/subnets/{id}?sort=…">`
  full-page navigations, identical in behavior to the Devices page.

The grid itself, its SSE refresh (`GridFrag`), and the cell toggle are untouched.

## Error handling

- Cyclic/self parent reference → `visited` set prevents infinite recursion; the
  cycle's back-edge row is emitted once at its first-reached position.
- Orphan child (parent not present) → treated as a root; never dropped.
- Bad sort/dir query values → normalized to defaults by `parseDeviceSort` (no
  error surfaced), matching current `/devices` behavior.
- Bad subnet id → existing `http.NotFound` (unchanged).

## Testing

- **web** (`internal/web/devices_test.go`):
  - `TestDeviceListParentChildGrouping`: create parent `aaa-parent`, a root
    `mmm-mid`, and a child `zzz-child` whose `ParentDeviceID` = parent. `GET
    /devices?sort=name&dir=asc`: assert `index(zzz-child) < index(mmm-mid)` in
    the body (grouping pulls the child up under its parent, ahead of the
    alphabetically-later root) and that the child row carries the `tree-branch`
    marker class. A flat name sort would place `zzz-child` last, so the assertion
    is non-vacuous.
  - `TestDeviceListOrphanChildIsRoot`: a device whose `parent_device_id` points
    at a non-existent id still renders (treated as a root; row count intact).
- **web** (`internal/web/grid_test.go`):
  - `TestSubnetDeviceListSortable`: seed two in-subnet devices; `GET
    /subnets/{id}` body contains sort-header links whose href starts with
    `/subnets/{id}?sort=` (proves the table is the sortable component with the
    correct base). `GET /subnets/{id}?sort=name` orders the rows by name.
  - Existing `TestSubnetPageDevicesList` (E2) must stay green (device still
    listed, out-of-subnet device excluded).

## Project layout (files added / modified)

- Modify: `internal/web/views/devices.templ` — `base` param on
  `sortHeader`/`sortURL`; `deviceRow(row, depth)`; new `deviceTable`;
  `DeviceGroupRow` + `GroupByParent`; `DeviceList` uses `deviceTable`. Regenerate
  `devices_templ.go`.
- Modify: `internal/web/views/grid.templ` — `GridPage` gains `sortKey,dir`; its
  device table becomes `@deviceTable(..., false)`. Regenerate `grid_templ.go`.
- Modify: `internal/web/devices.go` — extract `parseDeviceSort`; `handleDeviceList`
  uses it.
- Modify: `internal/web/grid.go` — `handleSubnetPage` sorts + passes sort params.
- Modify: `internal/web/static/app.css` — `.tree-indent`, `.tree-branch` (indent
  spacer + branch marker; existing tokens, both themes).
- Test: `internal/web/devices_test.go`, `internal/web/grid_test.go`.

## Out of scope (F3)

- Grouping the subnet list or the `#dev-grid` tile view (both stay flat).
- Collapsible/expandable tree nodes (static indent only).
- Drag-to-reparent (parent is set via the edit form's Parent field, unchanged).
- Any store/schema change — `ParentDeviceID` already exists on `Device`/`DeviceRow`.

## Global constraints

- No new dependencies; no store/schema change.
- The web package must not import proxmox/pihole/wireguard.
- Regenerate templ after editing `.templ`
  (`export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`); commit the
  regenerated `*_templ.go` with source.
- Default-dark theme; style via existing tokens; self-contained CSS.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Delete stray `netis`/`netis.db*` before finishing.
