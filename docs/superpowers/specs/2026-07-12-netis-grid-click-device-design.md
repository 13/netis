# Netis — Grid Square Click → New/Edit/Open Device (Sub-project G2): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Make a grid v2 square actionable: clicking a **free** square starts a new device
at that IP; clicking an **occupied** square offers Open / Edit (plus the existing
lease toggle).

## Current behavior

`GridFrag` renders one square per IP (network → broadcast):
- **Occupied** (`DeviceID > 0`; states online/offline/reserved/conflict): a
  `<button>` that opens the `CellDetail` popup (device link + Set static / Set
  DHCP).
- **Free** (`DeviceID == 0`, `State == "free"`): a non-clickable `<span>`.
- **Edge** (`State == "edge"`, network/broadcast): a non-clickable `<span>`.

So a free square can't create a device, and an occupied square's popup only links
the device — no explicit Open/Edit affordance.

## Component 1: Free square → New device prefilled

In `GridFrag`, render a free square (`DeviceID == 0 && State == "free"`) as a
button that opens the New-device dialog prefilled with that IP and subnet:

```
<button type="button" class={ "sq", c.State }
	hx-get={ fmt.Sprintf("/devices/new?subnet=%d&ip=%s", sn.ID, c.IP) }
	hx-target="#modal" title={ c.Title + " — click to add a device" }></button>
```

Edge squares stay non-clickable `<span>`s (network/broadcast are not
assignable); occupied squares keep their existing cell-popup button.

`handleDeviceForm` (GET `/devices/new`) reads the new `ip` query param and passes
it to the dialog; on submit, the existing `handleDeviceCreate` already creates
the device + interface and assigns the IP as **static**.

## Component 2: Prefill the New-device dialog's IP field

Thread a `preIP` value into the create dialog. The shared `deviceFormInner`
templ gains a `preIP string` parameter; its interface IP input becomes
`<input type="text" name="ip" placeholder="10.0.0.5" value={ preIP }/>`.

- `DeviceDialog(d, tags, subnets, allDevices, isEdit, preselectSubnet, preIP)` —
  passes `preIP` through (new trailing param).
- `DeviceDrawer(...)` — passes `""` (edit has no first-interface group).
- `handleDeviceForm` — reads `r.URL.Query().Get("ip")` and passes it as `preIP`
  (and the existing `subnet` as `preselectSubnet`, so both the IP field and the
  subnet `<select>` are prefilled).

All `DeviceDialog` call sites (only `handleDeviceForm` in non-test code) are
updated for the new trailing argument.

## Component 3: Occupied square popup → Open / Edit

In `CellDetail`, when `o.DeviceID > 0`, add Open and Edit actions to the `.df`
footer, before the lease buttons:

```
<div class="df">
	<a class="btn" href={ templ.URL(fmt.Sprintf("/devices/%d", o.DeviceID)) }>Open</a>
	<button type="button" hx-get={ fmt.Sprintf("/devices/%d/edit", o.DeviceID) } hx-target="#modal">Edit</button>
	@kindButton(sn.ID, ip, "static", "Set static", o.Kind)
	@kindButton(sn.ID, ip, "dhcp", "Set DHCP", o.Kind)
</div>
```

- **Open** — a link styled as a button (the `.btn` class already exists in
  `app.css`) that navigates to the device detail page.
- **Edit** — `hx-get /devices/{id}/edit` into `#modal`, which replaces the popup
  with the device edit **drawer** (F2) — edit in place from the grid.

No CSS change: `.btn` and `button` share styling already; the drawer/dialog hooks
are unchanged.

## Error handling

- Edge / reserved-with-device squares are unaffected (edge stays inert; occupied
  gets the popup). Only `State == "free"` becomes a new-device trigger.
- A free square's `hx-get` carries the raw IPv4 string (dots need no
  URL-encoding); the subnet id is numeric.
- Prefill is presentational; `handleDeviceCreate`'s existing validation is
  unchanged (blank IP still allowed, MAC optional).

## Testing

- **web** (`internal/web/grid_test.go`):
  - `TestGridFreeCellOpensNewDevice`: seed a `/29` subnet with one device at
    `10.0.0.1`; `GET /subnets/{id}/grid` body contains a button with
    `hx-get="/devices/new?subnet={id}&ip=10.0.0.2"` (a free host IP) and does
    NOT make the network/broadcast edge squares clickable.
  - `TestCellDetailHasOpenAndEdit`: with a device at an IP, `GET
    /subnets/{id}/cell?ip=...` body contains the Open link
    (`href="/devices/{id}"`) and the Edit control
    (`hx-get="/devices/{id}/edit"`).
- **web** (`internal/web/devices_test.go`):
  - `TestNewDevicePrefillsIP`: `GET /devices/new?subnet=1&ip=10.0.0.5` body
    contains `name="ip"` with `value="10.0.0.5"`; a plain `GET /devices/new`
    still renders an empty IP field (`value=""`).
  - Existing `TestNewStaysDialog` / dialog tests stay green (signature change
    only adds a trailing prefill arg).

## Project layout (files added / modified)

- Modify: `internal/web/views/grid.templ` — `GridFrag` free-cell button;
  `CellDetail` Open/Edit actions. Regenerate `grid_templ.go`.
- Modify: `internal/web/views/devices.templ` — `deviceFormInner` + `DeviceDialog`
  gain `preIP`; `DeviceDrawer` passes `""`. Regenerate `devices_templ.go`.
- Modify: `internal/web/devices.go` — `handleDeviceForm` reads `ip` and passes
  `preIP`.
- Test: `internal/web/grid_test.go`, `internal/web/devices_test.go`.

## Out of scope (G2)

- Dragging/reassigning IPs on the grid; multi-select.
- Making edge (network/broadcast) or conflict squares creatable.
- The settings-config additions (separate sub-project).

## Global constraints

- No new dependencies; no store/schema change.
- The web package must not import proxmox/pihole/wireguard.
- Regenerate templ after editing `.templ`
  (`export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`); commit the
  regenerated `*_templ.go` with source.
- Default-dark theme; reuse existing tokens/classes (no new CSS needed).
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Delete stray `netis`/`netis.db*` before finishing.
