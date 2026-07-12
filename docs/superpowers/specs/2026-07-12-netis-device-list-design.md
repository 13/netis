# Netis — Device List Overhaul (Sub-project B): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Sub-project **B** of the UI/UX modernization (A = design system, done; C =
device dialog, later). B reuses A's components (`.pill`, `.chip`, `.seg`,
`.ic`, `views.KindIcon`).

## Purpose

Make the devices page a real operational table: sortable columns (default
sorted by IP ascending), a list ⇄ grid view toggle, a static/DHCP lease chip
per IP, device icons, and a reviewed/"new" marker with an Approve action so
auto-discovered devices can be acknowledged.

## Architecture

Server-rendered as today (templ + the existing `?q` HTMX filter). Sorting is
server-side (numeric IP order requires Go); the list/grid toggle is
client-side and persisted. One new device flag (`reviewed`) via migration
`0004`; the store's device row carries the per-IP lease kind it already loads
but currently discards. No new dependencies.

## Component 1: Data model

### Migration `0004_device_reviewed.sql`

```sql
ALTER TABLE device ADD COLUMN reviewed INTEGER NOT NULL DEFAULT 0;
UPDATE device SET reviewed = 1 WHERE source != 'scan';
```

This is a plain `ADD COLUMN` + backfill (no table rebuild), safe under the
existing transactional migrate loop. The backfill marks every existing
non-scan device reviewed, so only genuinely auto-discovered (scan) devices
start as "new".

### Store (`internal/store/device.go`)

- `Device` gains `Reviewed bool` (added to `deviceCols`, `scanDevice`,
  `CreateDevice`, `UpdateDevice` column lists).
- `CreateDevice` sets `reviewed` from the source, ignoring any passed value:
  `reviewed = (d.Source != "scan")`. So the scan engine's `createUnknown`
  yields `reviewed=0` and the manual/Proxmox/WireGuard/Pi-hole create paths
  yield `reviewed=1` with **no caller changes**.
- `UpdateDevice` sets `reviewed=1` as part of its existing update (a human
  editing a device has reviewed it).
- `func (s *Store) SetDeviceReviewed(id int64, reviewed bool) error` — for the
  Approve action.
- `type IPInfo struct { IP, Kind string }` (`Kind` is `static`/`dhcp`).
  `DeviceRow.IPs` changes from `[]string` to `[]IPInfo`. `ListDevices` already
  calls `ListIPs` (which returns `IPRow{IP, Kind}`); it now keeps the kind
  instead of flattening to strings. `DeviceRow` also carries `Reviewed`
  (via the embedded `Device`).

Callers of `DeviceRow.IPs` as `[]string` (the `?q` filter's `strings.Join`,
the dashboard occupancy — none read `DeviceRow.IPs` there) are updated: the
device-list filter joins `ip.IP` values.

## Component 2: Sorting (`internal/web/devices.go`, `views.DeviceList`)

`GET /devices?q=&sort=&dir=`:
- `sort ∈ {ip, name, status, kind, seen}`, default `ip`; unknown → `ip`.
- `dir ∈ {asc, desc}`, default `asc`; unknown → `asc`.
- The handler filters by `q` (unchanged), then sorts the rows in Go:
  - `ip` — numeric, by each device's **lowest** IP (`netip.ParseAddr`, compare
    via `Addr.Compare`); devices with no IP sort last.
  - `name` — case-insensitive.
  - `status` — online-first (or last on `desc`).
  - `kind` — lexicographic.
  - `seen` — by `LastSeen` string (RFC3339 sorts chronologically); never-seen
    last.
  - `desc` reverses.
- `DeviceList` receives `sort`, `dir`, and `q`; column `<th>`s are links that
  carry `q` and set `sort`/`dir` (clicking the active column flips `dir`;
  another column sets `asc`). The active header shows a ▲ (asc) / ▼ (desc)
  caret.

## Component 3: List & grid views (`views.DeviceList`, `devices.js`, `app.css`)

Both views render the same sorted rows into the DOM; a `.seg` control toggles
which is shown.

- **Toolbar:** the `?q` search input, the `.seg` list/grid toggle, and the
  "New device" button.
- **List (`#dev-list`):** a table with sortable headers. Columns: device
  (`.ic` icon + name + reviewed marker), IP(s) (each with a `.chip.static` or
  `.chip` dhcp), status (`.pill.online/.offline`), kind, tags (`.tag`), last
  seen, actions (Approve on unreviewed rows).
- **Grid (`#dev-grid`):** device tiles — `.ic` icon, name, the lowest IP,
  a status pill, and the reviewed/new marker. Links to the device page.
- **Reviewed marker:** `reviewed=false` → an amber "new" pill; `reviewed=true`
  → a muted ✓ next to the name. Approve button appears only on unreviewed
  rows (list view).

`internal/web/static/devices.js` (loaded by the DeviceList template):
- On load, reads `localStorage["netis-devices-view"]` (default `list`), shows
  `#dev-list` or `#dev-grid`, and marks the active `.seg` button.
- Clicking a `.seg` button switches the view and writes `localStorage`.
- Because the `?q` input uses `hx-target="body"` (full re-render), the script
  re-runs on each filter/sort navigation and restores the chosen view.

`app.css` gains the `.devgrid`/`.devtile` tile styles and a `.caret` style
(all token-driven, matching the style tile). These are additive.

## Component 4: Approve action

`POST /devices/{id}/approve` (admin, via `requireAdmin`):
- Parse the id (bad → 404), `SetDeviceReviewed(id, true)`, redirect 303 to
  `/devices`.
- Registered in `NewServer` alongside the other `/devices/{id}/…` routes.

## Error handling

- Unknown/empty `sort`/`dir` fall back to `ip`/`asc` (never a 500).
- A device with no parseable IP sorts last under `ip` sort rather than
  erroring.
- Approve on a non-numeric or nonexistent id → 404.
- `devices.js` guards `localStorage` in try/catch (private-mode) and no-ops if
  the containers aren't present.

## Testing

- **store** (`device_test.go`): after `Open`, migration `0004` is applied and
  a pre-seeded `source='scan'` row has `reviewed=0` while a `source='manual'`
  row has `reviewed=1`; `CreateDevice{Source:"scan"}` → `reviewed=false`,
  `Source:"manual"` → `true`; `SetDeviceReviewed` flips it; `UpdateDevice`
  sets `reviewed=1`; `ListDevices` returns `IPInfo` entries with the right
  `Kind` (a static and a dhcp IP on one device) and the `Reviewed` value.
- **web** (`devices_test.go`): seed devices at `.2`, `.10`, `.100`; `GET
  /devices` (default) renders them in numeric-IP order (`.2` before `.10`
  before `.100`, which a string sort would get wrong); `?sort=name` reorders;
  a device with a static and a dhcp IP renders both a `static` chip and a
  `dhcp` chip; an unreviewed scan device renders the Approve control, and
  `POST /devices/{id}/approve` (admin) sets `reviewed=1` so a re-fetch no
  longer shows Approve for it; the page contains the `#dev-grid` container and
  the `.seg` toggle. A viewer gets 403 on the approve route.

## Project layout (files added / modified)

- Create: `internal/store/migrations/0004_device_reviewed.sql`,
  `internal/web/static/devices.js`.
- Modify: `internal/store/device.go` (Reviewed, IPInfo, ListDevices,
  CreateDevice/UpdateDevice, SetDeviceReviewed) + `internal/store/device_test.go`;
  `internal/web/devices.go` (sort + approve handlers, filter uses `IPInfo`);
  `internal/web/server.go` (approve route); `internal/web/views/devices.templ`
  (DeviceList rewrite) + `internal/web/devices_test.go`;
  `internal/web/static/app.css` (tile + caret styles).

## Out of scope (B)

- The device create/edit dialog, icon picker, searchable parent, tags/category
  fields (sub-project C).
- Bulk actions (approve-all, multi-select).
- Server-side pagination (home-network device counts don't need it).
- Column show/hide or reordering.
