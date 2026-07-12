# Netis — Device Create/Edit Dialog (Sub-project C): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Sub-project **C** of the UI/UX modernization (A = design system, done; B =
device-list overhaul, done). C reuses A's `.dialog` shell and B's `KindIcon`
/ `.pill` / `.chip` / `.tag` components. No schema change: the `icon` column
and the `tag` / `device_tag` tables already exist.

## Purpose

Replace the awkward full-page "New device" form (the strange-bordered
`<fieldset><legend>Optional first interface</legend>` block) and the device
detail page's inline edit form with a single **modal dialog** used for both
create and edit. The dialog adds the fields the user asked for — a selectable
**icon** with a per-kind default (phone → 📱), a **parent device** picker, and
**tags** — inline in one form, so device metadata is edited in one place
instead of scattered per-field forms.

## Architecture

Server-rendered as today (templ + HTMX). The dialog is delivered as an HTMX
**fragment** (`.dialog-scrim > .dialog`, no `Layout`) injected into a
persistent `<div id="modal">` in the layout. The form submits as a **plain
POST** to the existing create/update handlers, which already 303-redirect; the
resulting full-page navigation reloads the list/detail and dismisses the modal.
A small `dialog.js` handles close (✕ / scrim / Escape) and the icon-picker
interaction (click-to-select plus per-kind default on Kind change). Sorting,
filtering, list/grid toggle from B are untouched.

## Component 1: Store — tag sync

### `SetDeviceTags(deviceID int64, names []string) error` (`internal/store/meta.go`)

Syncs a device's tags to exactly `names`:

1. Normalize the input: `strings.TrimSpace` each name, drop empties,
   de-duplicate (case-sensitive match of the existing `tag.name`).
2. Load current tags via `DeviceTags(deviceID)` (returns `[]Tag`).
3. For each desired name not currently attached: find-or-create the tag
   (reuse the pattern in `web.findOrCreateTag` — scan `ListTags()` for an
   exact-name match, else `CreateTag(name, "#888888")`), then
   `TagDevice(deviceID, tagID)`.
4. For each currently-attached tag whose name is **not** in the desired set:
   `UntagDevice(deviceID, tag.ID)`.

This is idempotent and order-independent. It lives in the store (not the web
layer) because it is pure persistence logic; the web layer passes the parsed
`[]string`. `web.findOrCreateTag` and the per-tag `handleTagAdd` /
`handleTagRemove` handlers + routes are removed (the detail page no longer has
inline tag controls — see Component 4).

## Component 2: Views — icon helper

### `DeviceIcon(icon, kind string) string` (`internal/web/views/icons.go`)

```go
// DeviceIcon returns the device's chosen icon, or the kind default when the
// device has no explicit icon set.
func DeviceIcon(icon, kind string) string {
	if icon != "" {
		return icon
	}
	return KindIcon(kind)
}
```

B's device list, grid tile, and the detail header currently render
`KindIcon(row.Kind)` and ignore the stored `Icon`. All three render sites
switch to `DeviceIcon(row.Icon, row.Kind)` so a user-picked icon actually
shows.

### Icon palette

A package-level ordered list of emoji offered in the picker, defined once in
`icons.go` and consumed by the dialog template and `dialog.js`:

```go
// IconChoices is the emoji palette shown in the device dialog's icon picker.
// The first ten mirror the kind defaults; the rest are common extras.
var IconChoices = []string{
	"💻", "🔀", "📱", "🖥️", "🖨️", "💡", "🧊", "📦", "🔒", "❓",
	"📡", "🗄️", "📷", "🔌", "🎮", "📺", "☎️", "🕹️", "🛰️", "⌚",
}
```

The kind→default map (`kindIcons`) stays the single source of per-kind
defaults; `dialog.js` gets its own copy of that map (10 entries) for the
client-side default-on-kind-change behavior.

## Component 3: The dialog fragment (`DeviceDialog` templ)

One template renders both modes:

```
templ DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool)
```

Structure — `.dialog-scrim` (click closes) wrapping `.dialog`:

- **Header (`.dh`):** title ("New device" / "Edit device") + ✕ close button.
- **Body (`.db`) — a single `<form>`** posting to `/devices` (create) or
  `/devices/{id}` (edit), `method="post"`:
  - **Name** — `<input name="name" required>`, prefilled `d.Name`.
  - **Kind** — `<select name="kind">` over the 10 kinds (the "category"),
    `d.Kind` selected; defaults to `computer` on create.
  - **Icon** — a `.iconpick` row of `<button type="button" class="ic-swatch">`
    per `IconChoices` entry, plus a hidden `<input name="icon">`. Initial
    value/highlight = `DeviceIcon(d.Icon, d.Kind)`. `data-kind-default` on the
    picker seeds the "untouched" comparison.
  - **Parent device** — `<select name="parent_device_id">`: first option
    `— none —` (value `""`), then one option per `allDevices` entry rendered
    `name — ip` (value = id, using `lowestIPStr` from B for the ip; empty ip
    omitted). On edit, `d.ParentDeviceID` is pre-selected and the device's own
    id is skipped (no self-parent). `d`'s existing children are **not**
    excluded here — the update handler already guards `pid != d.ID`, and a
    deeper cycle is out of scope (see Out of scope).
  - **Notes** — `<textarea name="notes">`, prefilled `d.Notes`.
  - **Tags** — `<input name="tags">`, comma-separated, prefilled on edit with
    the device's current tag names joined `", "`.
  - **Optional first interface** (create only, `!isEdit`) — MAC / IP / subnet
    (`<select name="subnet_id">` over `subnets`) as clean labeled `.dialog`
    rows, not a raw fieldset. Hidden on edit.
- **Footer (`.df`):** Cancel (closes) + Submit ("Create device" / "Save").

The fragment includes no `Layout`, nav, or `<script>` — `dialog.js` is loaded
once by `Layout` and is already present when the fragment lands.

## Component 4: Handlers & routes

### Fragment handlers (`internal/web/devices.go`)

- `handleDeviceForm` (already wired to `GET /devices/new`) is **repurposed**
  to render the create-mode `DeviceDialog` fragment: it loads `ListSubnets()`
  and `ListDevices()` (for the parent select) and renders
  `DeviceDialog(store.Device{Kind:"computer"}, nil, subnets, allDevices, false)`.
- **New** `handleDeviceEditForm` for `GET /devices/{id}/edit`: parse id (404 on
  bad/`sql.ErrNoRows`), load the device (`GetDevice`), its tags
  (`DeviceTags`), subnets, and all devices; render
  `DeviceDialog(d, tags, subnets, allDevices, true)`.

Both are auth-gated (they are `GET`, under `requireAuth` like the rest); they
are read-only so they need no `requireAdmin`.

### Mutating handlers (existing, extended)

- `handleDeviceCreate` (`POST /devices`, `requireAdmin`) — additionally reads
  `icon`, `parent_device_id`, and `tags`:
  - set `Icon` on the created `store.Device`;
  - parse `parent_device_id` (non-empty, valid int) into `ParentDeviceID`
    before `CreateDevice`;
  - after create, parse the `tags` field (`strings.Split(",")`) and call
    `SetDeviceTags(devID, names)`.
  - The optional-first-interface block is unchanged.
- `handleDeviceUpdate` (`POST /devices/{id}`, `requireAdmin`) — already reads
  `icon` + `parent_device_id`; add: parse `tags` and call
  `SetDeviceTags(id, names)` after `UpdateDevice`.

A shared helper `parseTags(s string) []string` (in `devices.go`) does the
split/trim/drop-empty so create and update agree; `SetDeviceTags` de-dups
defensively regardless.

### Routes (`internal/web/server.go`)

- Add `GET /devices/{id}/edit` → `handleDeviceEditForm`.
- **Remove** `POST /devices/{id}/tags` and
  `POST /devices/{id}/tags/{tagID}/delete` (inline tag controls gone).
- The `#modal` container is added to `Layout`; `dialog.js` is loaded once
  there.

### Detail page (`DevicePage` in `views/devices.templ`)

- Remove the inline edit `<form>` (name/kind/notes/icon/parent) and the
  per-tag add/remove forms.
- Add an **Edit** button: `hx-get="/devices/{id}/edit" hx-target="#modal"`.
- Tags render as read-only `.tag` chips (edited via the dialog).
- Header icon uses `DeviceIcon(d.Device.Icon, d.Device.Kind)`.
- Everything else stays: interfaces / IPs / open ports, custom fields controls,
  links controls, availability, parent/children, events, WOL / port-scan,
  delete. `DeviceDetail.AllTags` becomes unused and is removed from the struct
  and the handler.

## Component 5: Client script (`internal/web/static/dialog.js`)

Loaded once by `Layout`. Uses event delegation on `document` so it works for
fragments injected after load:

- **Close:** click on `.dialog-scrim` (only when the target is the scrim
  itself, not a child), click on `[data-close]` (✕ / Cancel), or `Escape` →
  set `#modal` `innerHTML = ''`.
- **Icon picker:** click on `.ic-swatch` → write its emoji to the hidden
  `input[name=icon]`, move the `.selected` class. Guarded to the picker's
  scope.
- **Kind default:** on `change` of `select[name=kind]`, if the current icon
  value equals the *previous* kind's default (i.e. "untouched"), replace it
  with the new kind's default and move the highlight. The previous default is
  tracked in a `data-kind-default` attribute updated on each change. The
  kind→emoji map is embedded in the script (10 entries mirroring `kindIcons`).

`app.css` gains `.iconpick` / `.ic-swatch` / `.ic-swatch.selected` styles
(token-driven, matching the design system). Additive only.

## Error handling

- `GET /devices/{id}/edit` on a bad or nonexistent id → 404 (parse error or
  `errors.Is(err, sql.ErrNoRows)`), consistent with the other `/devices/{id}`
  handlers.
- `SetDeviceTags` with an empty/whitespace `tags` field detaches all tags
  (desired set empty). This is the intended "clear tags" behavior on edit.
- `parent_device_id` that is empty, non-numeric, or equal to the device's own
  id → no parent set (existing `handleDeviceUpdate` guard; create mirrors it).
- Scrim/Escape close is pure client JS; the fragment degrades to a plain page
  if JS is off (direct nav to `/devices/new` renders the fragment standalone —
  the form still submits and redirects).

## Testing

- **store** (`meta_test.go`): `SetDeviceTags` — attaches new names (creating
  tags that don't exist), detaches names no longer present, leaves unchanged
  names attached, and de-dups/trims input (`["web"," web ","",]` → one `web`
  tag). Verify via `DeviceTags` after each call.
- **views** (`icons_test.go`): `DeviceIcon("🎮","phone") == "🎮"`;
  `DeviceIcon("","phone") == "📱"`; `DeviceIcon("","nope") == "❓"`.
- **web** (`devices_test.go`):
  - `GET /devices/new` returns a fragment containing `class="dialog"`, the icon
    picker (`ic-swatch`), the parent `<select name="parent_device_id">`, and
    the `tags` field; and **no** `<nav` (fragment, not full page).
  - `GET /devices/{id}/edit` for a seeded device is prefilled: name value, the
    current tags in the `tags` input, the device's parent pre-selected, and the
    device's own id absent from the parent options. Bad id → 404.
  - `POST /devices` with `icon`, `parent_device_id`, and `tags=a,b` creates a
    device whose stored `Icon`, `ParentDeviceID`, and tag set match.
  - `POST /devices/{id}` with `tags=a` on a device previously tagged `a,b`
    leaves only `a` attached (sync detaches `b`).
  - The detail page renders an Edit button (`hx-get`…`/edit`) and no longer
    contains the old inline edit form (`name="notes"` textarea absent from the
    detail page — it now lives only in the dialog fragment).
  - A device with a stored icon renders that icon on the list page (not the
    kind default).
  - Viewers still get 403 on `POST /devices` and `POST /devices/{id}`
    (unchanged `requireAdmin`).

## Project layout (files added / modified)

- Create: `internal/web/static/dialog.js`.
- Modify:
  - `internal/store/meta.go` (`SetDeviceTags`) + `internal/store/meta_test.go`.
  - `internal/web/views/icons.go` (`DeviceIcon`, `IconChoices`) +
    `internal/web/views/icons_test.go`.
  - `internal/web/views/devices.templ` (`DeviceDialog` fragment; `DeviceList`
    + grid use `DeviceIcon`; `DevicePage` edit button + read-only tags, inline
    edit/tag forms removed; header uses `DeviceIcon`).
  - `internal/web/devices.go` (`handleDeviceForm` → fragment,
    `handleDeviceEditForm`, `parseTags`, create/update extended,
    `findOrCreateTag`/`handleTagAdd`/`handleTagRemove` removed) +
    `internal/web/devices_test.go`.
  - `internal/web/server.go` (add `/devices/{id}/edit`, remove the two tag
    routes).
  - `internal/web/views/layout.templ` (`#modal` container, `dialog.js`).
  - `internal/web/static/app.css` (`.iconpick` / `.ic-swatch` styles).
  - `internal/web/views/devices.templ` — the `DeviceDetail` struct (defined at
    `devices.templ:29`): drop the now-unused `AllTags` field, and
    `handleDevicePage` stops loading/passing `allTags`.

## Out of scope (C)

- Deep parent-cycle prevention (A→B→A); the handler blocks only direct
  self-parent. Home-network topologies are shallow.
- Tag colors / a tag manager UI (tags get the default `#888888`).
- Editing interfaces/IPs from the dialog on edit (managed on the detail page).
- Autocomplete/type-ahead widget for tags (plain comma field).
- HTMX-swapped modal close without a full navigation (the 303 reload is the
  close mechanism on submit).
