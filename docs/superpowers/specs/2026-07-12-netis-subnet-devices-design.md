# Netis — Subnet Page Devices List + CRUD (Sub-project E2): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Second of three sub-projects in this batch (E1 done → **E2 subnet devices** → E3
onboarding). E2 adds a scoped devices list with the same CRUD as the devices page
to the subnet page (grid v2), and clarifies why the grid shows fewer devices than
the full `/devices` total.

## Purpose

On `/subnets/{id}`, below the occupancy grid, show **"Devices in this subnet
(N)"** — the devices that have an IP assigned in this subnet — as a list with the
same CRUD affordances as the devices page (new / edit / delete / approve), and add
a note clarifying that the grid (and this list) is scoped to IPs assigned here, so
it is smaller than the full devices page.

## Decisions (locked)

- The list shows **devices with an IP assigned in this subnet** (matches the
  grid). Reuse `ListDevices()` + `ListSubnetIfaceIPs(subnetID)` to filter — no new
  store query.
- Reuse the exact devices-list row markup via a shared `deviceRow` templ (DRY).
- CRUD is the same as `/devices`: rows link to `/devices/{id}` (detail → Edit
  modal / Delete), per-row Approve, and a **New device** button that opens the
  create dialog pre-selecting this subnet (`GET /devices/new?subnet={id}`).
- The subnet device list is a static scoped table — no sort/filter (it is small).
- No store/schema change.

## Component 1: Devices-in-subnet data

- New helper in `internal/web/grid.go`:
  ```go
  func (s *Server) devicesInSubnet(subnetID int64) ([]store.DeviceRow, error)
  ```
  It calls `s.store.ListDevices()` (all rows) and `s.store.ListSubnetIfaceIPs(subnetID)`
  (returns `[]SubnetIfaceIP{IfaceID,DeviceID,IP,MAC}` for the subnet), builds a
  `map[int64]bool` of the subnet's device IDs, and returns the `DeviceRow`s whose
  `ID` is in that set, preserving `ListDevices()` order. A store error propagates.
- `handleSubnetPage` calls `devicesInSubnet(sn.ID)` (500 on error) and passes the
  rows to `GridPage`.

## Component 2: Shared `deviceRow` templ

Extract the device-list table row (`internal/web/views/devices.templ`, the `<tr>`
currently inside `DeviceList`'s tbody loop — device cell with icon/name link/
reviewed marker/MAC/`vendor · model`; IPs; lease chips; status pill; kind;
function; tags; last-seen; Approve form) into:

```go
templ deviceRow(row store.DeviceRow)
```

`DeviceList`'s tbody loop becomes `for _, row := range rows { @deviceRow(row) }` —
byte-identical output (the existing devices-list tests guard this). The subnet
page reuses `@deviceRow` for identical rows.

## Component 3: Subnet page devices section

`GridPage` (`internal/web/views/grid.templ`) gains a `devices []store.DeviceRow`
param. After the existing grid `#grid` div and the legend, render:

- A muted scope note:
  > The grid shows every IP assigned in this subnet. Devices without an IP here
  > (or on other subnets) aren't listed — see the full [Devices](/devices) page.

  (the `Devices` word links to `/devices`.)
- `<h2>Devices in this subnet ({ fmt.Sprint(len(devices)) })</h2>` with a **New
  device** button:
  ```
  <button type="button" class="primary" hx-get={ fmt.Sprintf("/devices/new?subnet=%d", sn.ID) } hx-target="#modal">New device</button>
  ```
- If `len(devices) == 0` → a muted "No devices with an IP in this subnet yet."
- Else a `<table>` with a static header row (`Device`, `IP`, `Lease`, `Status`,
  `Kind`, `Function`, `Tags`, `Last seen`, and a blank actions column) and a tbody
  of `@deviceRow(d)` for each device.

The `#modal` container and `dialog.js` are already present via `Layout`, so the
New-device dialog and the detail-page Edit dialog work here unchanged.

## Component 4: New-device subnet pre-select

- `DeviceDialog` (`devices.templ`) gains a trailing `preselectSubnet int64` param;
  in the "First interface (optional)" subnet `<select>`, an `<option>` renders
  `selected` when `sn.ID == preselectSubnet` (only on create; edit passes 0).
- `handleDeviceForm` (`GET /devices/new`) reads `subnet := parse(r.URL.Query().Get("subnet"))`
  (0 when absent/non-numeric) and passes it to `DeviceDialog(...)`.
- `handleDeviceEditForm` passes `0` for `preselectSubnet`.
- No new route; the query param rides the existing `/devices/new`.

## Error handling

- Bad/nonexistent subnet id on `/subnets/{id}` → 404 (existing `subnetFromPath`).
- `?subnet=` non-numeric → 0 → no pre-selection (harmless).
- Empty subnet → the device list renders its empty state.
- `devicesInSubnet` store error → 500.

## Testing

- **web** (`internal/web/grid_test.go`):
  - Seed a subnet `A` with a device `in` that has an IP in `A`, and a device `out`
    with no IP (or an IP in a different subnet `B`). `GET /subnets/{A}` body
    contains `Devices in this subnet`, the `in` device's name, a
    `hx-get="/devices/new?subnet={A}"` New-device button, and an `href="/devices"`
    link; and does **not** contain the `out` device's name in the devices section.
  - `GET /devices/new?subnet={A}` renders the subnet `A` `<option ... selected>` in
    the dialog's subnet select.
  - Regression: `GET /devices` still renders the device list rows (an existing
    devices-list assertion — e.g. a seeded device's name + a lease chip — stays
    green after the `deviceRow` extraction).

## Project layout (files added / modified)

- Modify: `internal/web/grid.go` (`devicesInSubnet` helper; `handleSubnetPage`
  passes devices) + `internal/web/grid_test.go`.
- Modify: `internal/web/views/grid.templ` (`GridPage` gains `devices` param +
  the devices section + scope note).
- Modify: `internal/web/views/devices.templ` (extract `deviceRow`; `DeviceList`
  uses it; `DeviceDialog` gains `preselectSubnet`) + regenerated
  `internal/web/views/devices_templ.go` and `grid_templ.go`.
- Modify: `internal/web/devices.go` (`handleDeviceForm` reads `?subnet=`;
  `handleDeviceEditForm` passes 0 to `DeviceDialog`).

## Out of scope (E2)

- Sort/filter on the subnet device list (static scoped table).
- Moving a device between subnets / reassigning IPs from this page.
- Any change to the grid squares (unchanged from C3).
- Onboarding redesign (E3).
