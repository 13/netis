# Netis — Device-Detail Overhaul (Sub-project F2): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Second of three sub-projects in this batch (F1 quick fixes ✅ → **F2 device-detail
overhaul** → F3 device-list parent/child + sortable subnet list). F2 makes the
device detail page modern and best-practice, moves the edit form into a
right-side drawer, and adds a per-IP static⇄dhcp lease toggle on the detail page.

## Purpose

The current device detail page (`views.DevicePage`) is a flat vertical stack of
`<h2>` sections with bare tables — functional but dated. Three changes:

1. **Redesign the detail page** into a modern two-column layout: a hero header
   card (icon, name, kind badge, facts, live status, primary actions) over a
   two-column body — interfaces on the left, metadata (tags, links, custom
   fields, relations, event history) in a right-hand aside. Reuse the existing
   design tokens and `.card` idiom.
2. **Edit in a right-side drawer.** Today "Edit" opens a centered modal
   (`views.DeviceDialog`). Move the edit affordance to a panel that slides in
   from the right (a drawer), the modern best-practice for editing a record you
   are looking at. The create flow ("New device") keeps its centered modal.
3. **Per-IP lease toggle.** Each IP on the detail page's interface table gets a
   clickable static⇄dhcp toggle, mirroring the grid-cell toggle (C3). Reuses
   `store.SetIPKind(subnetID, ip, kind)`.

## Component 1: Detail-page redesign

Restructure `views.DevicePage` (no route/handler/view-model change — same
`DeviceDetail` struct, same `GET /devices/{id}`):

- **Hero header** (`.dev-hero` card): large device icon, the name as `<h1>`, the
  kind badge (+ proxmox status badge for vm/lxc as today), the
  vendor · model · function facts line, an aggregate online/offline pill (online
  if any interface is online), and a right-aligned action cluster — **Edit**
  (opens the drawer), **Wake on LAN**, **Scan ports**. Notes render below the
  facts line when present.
- **Two-column body** (`.dev-cols`, CSS grid, collapses to one column under
  ~900px):
  - **Main column** (`.dev-main`): the **Interfaces** section — one `.card` per
    interface with its MAC/hostname/status header, last-seen + 30-day
    availability line, the **IP table** (IP + lease toggle, see Component 3),
    and the open-ports table when non-empty.
  - **Aside column** (`.dev-aside`): **Tags**, **Links** (list + add form),
    **Custom fields** (table + set form), **Parent / children** (parent link +
    children list), and **Event history** (recent events table). Each is a
    `.card` with an `<h2>` heading, preserving all existing forms and actions
    (add/remove link, set/remove field) and their POST targets unchanged.
- The **Delete device** form stays at the bottom of the aside, visually
  de-emphasized (existing button styling; no new danger styling required).

No behavioural change to any existing form action, route, or the view-model.
This is a template + CSS reorganization. All existing `DevicePage` assertions in
tests must continue to pass (name, interfaces, links, fields, events all still
present).

## Component 2: Edit drawer

The edit form is served by `GET /devices/{id}/edit` → `views.DeviceDialog(...,
isEdit=true, ...)` rendered into `#modal`. Change it to render a **drawer**
variant instead, reusing the same form markup so there is one source of truth
for the fields.

**Refactor for DRY.** Extract the shared inner markup — the header (`.dh`) plus
the `<form>` (its `.db` body and `.df` footer) — into an unexported templ
`deviceFormInner(d, tags, subnets, allDevices, isEdit, preselectSubnet)`. Then:

- `DeviceDialog(...)` = `.dialog-scrim` › `.dialog` › `@deviceFormInner(...)`
  (centered modal — used by **create**, `GET /devices/new`).
- `DeviceDrawer(d, tags, subnets, allDevices, preselectSubnet)` =
  `.dialog-scrim.drawer-scrim` › `.dialog.drawer` › `@deviceFormInner(..., isEdit=true, ...)`
  (right-side drawer — used by **edit**, `GET /devices/{id}/edit`).

`handleDeviceEditForm` renders `views.DeviceDrawer(d, tags, subnets, all, 0)`
instead of `DeviceDialog(..., true, 0)`. `handleDeviceNew` is unchanged (still
renders `DeviceDialog`).

**Why the shared class list works with existing JS.** The drawer keeps the
`dialog-scrim` and `dialog` classes (adding `drawer-scrim` / `drawer` only for
positioning), so `dialog.js` needs **no change**: its scrim-backdrop close keys
off `.dialog-scrim`, its Escape close off `#modal .dialog`, and the icon-picker /
kind-sync logic off `.dialog` + `.iconpick` — all still present. `[data-close]`
on the ✕ and Cancel buttons still closes.

**CSS** (`app.css`): `.drawer-scrim` overrides the shell's `place-items:center`
to `place-items: stretch end` (panel pinned to the right edge, full height).
`.drawer` overrides `.dialog`'s `max-width`/`border-radius`: fixed right column
(`width:min(460px,100%)`, full height, left border only, no rounded corners),
sliding in from the right via a short `transform` transition that respects
`prefers-reduced-motion`. The internal `.dh` / `.db` / `.df` layout is inherited
unchanged, and `.db` already scrolls (`overflow-y:auto`) for tall forms.

## Component 3: Per-IP lease toggle

On the detail page, each interface's IP table renders the lease as a clickable
control instead of plain text. Mirrors the C3 grid-cell toggle.

- **View:** an unexported templ `leaseToggle(devID, subnetID int64, ip, kind string)`
  renders a `<button class="chip static?">` (reusing the existing `.chip` /
  `.chip.static` styles from the list view) wrapped in `<span class="lease-cell">`.
  The button posts to `POST /devices/{devID}/ip/kind` with
  `hx-vals` `{subnet_id, ip, kind:<opposite>}`, `hx-target="closest .lease-cell"`,
  `hx-swap="outerHTML"`. Clicking flips static↔dhcp in place.
- **Route:** `POST /devices/{id}/ip/kind`, admin-only
  (`s.requireAdmin(s.handleDeviceIPKind)`), registered alongside the other
  `/devices/{id}/...` mutation routes.
- **Handler `handleDeviceIPKind`:** parse the device id (404 on bad id — keeps
  the route consistent with siblings), read `subnet_id`, `ip`, `kind` from the
  form; reject `kind ∉ {static,dhcp}` with 400; call
  `s.store.SetIPKind(subnetID, ip, kind)`; on success render
  `views.LeaseToggle(devID, subnetID, ip, kind)` (the flipped control) as the
  response fragment. `SetIPKind`'s 0-row update (no such assignment) is not an
  error, matching `handleCellKind`.
- Only IPs that have a subnet assignment (`SubnetID != 0`) get the toggle; an IP
  with no subnet renders as a plain `.chip` (no toggle), since `SetIPKind` keys
  on subnet+ip.

`LeaseToggle` is exported (called from the handler); `leaseToggle` (lowercase) is
its internal call site from `DevicePage` — to avoid two definitions, expose one
exported `LeaseToggle(devID, subnetID int64, ip, kind string)` templ and call it
from both `DevicePage` and the handler.

## Error handling

- Bad device id in any F2 route → `http.NotFound`.
- `kind ∉ {static,dhcp}` → 400 "bad kind" (as `handleCellKind`).
- `SetIPKind` DB error → 500. A no-op update (IP not assigned to that subnet) is
  not an error; the control simply re-renders with the requested kind.
- The drawer and dialog share the same server-rendered form; a validation
  failure on submit is handled by the existing `handleDeviceUpdate` /
  `handleDeviceCreate` paths (unchanged).

## Testing

- **web** (`internal/web/devices_test.go`):
  - `TestDeviceIPKindToggle`: seed device+iface+IP (subnet, static via
    `AssignIP`); `POST /devices/{id}/ip/kind` `{subnet_id, ip, kind:dhcp}` →
    200, response body contains `dhcp` and posts back `kind":"static` (the
    flipped next-state); `store.ListIPs` shows the IP is now `dhcp`. Toggle back
    to `static` → persists `static`.
  - `TestDeviceIPKindBadKind`: `kind=bogus` → 400.
  - `TestEditServesDrawer`: `GET /devices/{id}/edit` body contains the `drawer`
    class and `action="/devices/{id}"` (edit form target), confirming the drawer
    variant is served.
  - `TestNewStaysDialog`: `GET /devices/new` body contains `dialog-scrim` but not
    `drawer` (create still a centered modal).
  - `TestDeviceDetailRenders` (redesign guard): `GET /devices/{id}` still renders
    the device name, an Interfaces heading, and the `dev-hero` container — the
    reorganization keeps content and adds the hero.
  - Existing `DevicePage` / dialog tests must stay green unchanged.

## Project layout (files added / modified)

- Modify: `internal/web/views/devices.templ` — extract `deviceFormInner`; add
  `DeviceDrawer`; add exported `LeaseToggle`; rebuild `DevicePage` into
  hero + two-column cards; render `LeaseToggle` in the IP table. Regenerate
  `devices_templ.go`.
- Modify: `internal/web/views/layout.templ` — **unchanged** (drawer reuses
  `#modal`).
- Modify: `internal/web/devices.go` — `handleDeviceEditForm` renders
  `DeviceDrawer`; add `handleDeviceIPKind`.
- Modify: `internal/web/server.go` — register `POST /devices/{id}/ip/kind`.
- Modify: `internal/web/static/app.css` — `.dev-hero`, `.dev-cols`, `.dev-main`,
  `.dev-aside`, `.drawer-scrim`, `.drawer`, `.lease-cell` (light + dark via
  existing tokens; motion respects `prefers-reduced-motion`).
- Test: `internal/web/devices_test.go` — the cases above.

## Out of scope (F2)

- Device-list parent/child grouping and the sortable subnet device list (F3).
- Editing interfaces/IPs beyond the lease toggle (add/remove IP, rename iface).
- Any store/schema change — `SetIPKind`, `ListIPs`, `GetDevice`, and the
  `DeviceDetail` view-model already exist and are reused as-is.
- `dialog.js` changes — the drawer deliberately reuses the dialog class hooks.

## Global constraints

- No new dependencies; no store/schema change.
- The web package must not import proxmox/pihole/wireguard.
- Regenerate templ after editing `.templ`
  (`export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`); commit the
  regenerated `*_templ.go` with source.
- Default-dark theme; both themes styled via existing tokens; no external assets
  (self-contained CSS).
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Delete stray `netis`/`netis.db*` from the repo root before finishing.
