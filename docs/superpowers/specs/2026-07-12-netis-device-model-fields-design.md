# Netis — Richer Device Model (Sub-project C1): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

First of three sub-projects in the next batch (C1 device model → C2 scanning UX
→ C3 subnet occupancy grid v2). C1 is the data foundation the later two display
on. It reuses the existing OUI vendor lookup, the device dialog (sub-project C),
and the migration table-rebuild idiom (migration 0002).

## Purpose

Model each device the way the user's inventory table does: add the two device
kinds it needs (`router`, `modem`) and give every device first-class **model**
and **function** fields, alongside the already-captured MAC **vendor**. Surface
all of these — vendor, model, function — in the dialog, the detail page, and the
device list, and make them searchable.

Concretely, this lets a row like
`192.168.22.27 | archera8 | TP-Link | ARCHER-A8 v1 | AP Dachboden CH:1,36`
be represented: name `archera8`, kind `router`, vendor `TP-Link…`, model
`ARCHER-A8 v1`, function `AP Dachboden CH:1,36`.

## Decisions (locked)

- **Function** is a single free-text field holding role + channel together
  (e.g. `AP Dachboden CH:1,36`, `PV Inverter`, `3D Print`). No separate channel
  field.
- **Vendor** is user-editable in the dialog (a normal text input). Scan fills it
  only when it *creates* an unknown device (`engine.go` `createUnknown`), and
  never updates an existing device's vendor — so a user edit is never clobbered
  (verified: no scan path calls `UpdateDevice` on existing rows).
- **Model** and **function** are first-class device columns (not custom fields):
  sortable/filterable and shown as dedicated inputs.
- Router/modem default icons: **router → 🛜**, **modem → 📶**.

## Component 1: Migration `0005_device_model_function.sql`

Widening the `kind` CHECK requires a table rebuild (the CHECK is baked into the
table definition), so the two new columns are added in the same rebuild — the
same drop-and-rename idiom migration 0002 already used on `device`.

```sql
CREATE TABLE device_new (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'other' CHECK (kind IN
    ('computer','switch','router','modem','phone','server','printer','iot','vm','lxc','wg-peer','other')),
  notes TEXT NOT NULL DEFAULT '',
  vendor TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','scan','proxmox','wireguard','pihole')),
  parent_device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  proxmox_vmid INTEGER,
  wg_pubkey TEXT,
  icon TEXT NOT NULL DEFAULT '',
  reviewed INTEGER NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  function TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
INSERT INTO device_new
  (id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed,created_at)
  SELECT
  id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed,created_at
  FROM device;
DROP TABLE device;
ALTER TABLE device_new RENAME TO device;
```

Notes:
- The `INSERT` lists columns explicitly (the shape changed); `model`/`function`
  take their `''` default for existing rows. This guards against silent column
  drift.
- `parent_device_id REFERENCES device(id)` resolves to the renamed table exactly
  as it did in 0002.
- The migrate loop runs with `foreign_keys = OFF` around the pass, so the
  drop/rename does not cascade-delete iface/ip/tag children; a per-migration
  `foreign_key_check` still validates integrity afterward (existing behavior).
- `function` is not a SQLite reserved keyword; it is a valid identifier
  unquoted.

## Component 2: Store (`internal/store/device.go`)

- `Device` struct gains `Model string` and `Function string`.
- `deviceCols` becomes
  `id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed,model,function`
  (append the two; keep `created_at` out of `deviceCols` as today).
- `scanDevice` scans the two extra fields in the same order.
- `CreateDevice` INSERT and `UpdateDevice` SET include `model`, `function`.
- `ListDevices` and `ListChildren` select `deviceCols`, so they carry the new
  fields with no further change.

## Component 3: Kinds & icons

- `validKinds` (`internal/web/devices.go`) gains `router:true, modem:true`.
- `deviceKinds` (`internal/web/views/devices.templ`) becomes
  `["computer","switch","router","modem","phone","server","printer","iot","vm","lxc","wg-peer","other"]`
  (display order in every kind `<select>`).
- `kindIcons` (`internal/web/views/icons.go`) gains `"router":"🛜"`,
  `"modem":"📶"`.
- `IconChoices` palette gains 🛜 and 📶 so they are pickable in the icon grid.
- `dialog.js` `KIND_ICON` map gains `router: '🛜'`, `modem: '📶'` so selecting
  those kinds sets the default icon client-side (matching the existing
  per-kind-default behavior).

## Component 4: UI

### Dialog (`DeviceDialog` in `devices.templ`)

Add three text inputs after the Icon picker, before Notes:

- **Vendor** — `<input name="vendor" value={ d.Vendor }>` (editable).
- **Model** — `<input name="model" value={ d.Model }>`.
- **Function** — `<input name="function" value={ d.Function }
  placeholder="role, channel…">`.

`handleDeviceCreate` and `handleDeviceUpdate` (`devices.go`) read `vendor`,
`model`, `function` from the form and set them on the `store.Device` before
Create/Update. (Create currently hard-sets `Vendor:""`; it now reads the form
value. Update currently sets `d.Vendor` from the loaded row unchanged; it now
overwrites from the form.)

### Detail page (`DevicePage`)

Under the `<h1>`, render a muted facts line listing the non-empty ones among
Vendor, Model, Function (e.g. `TP-Link · ARCHER-A8 v1 · AP Dachboden CH:1,36`).
The existing standalone vendor `<p>` is replaced by this combined line.

### Device list (`DeviceList`)

- Device cell: below the existing MAC line, add a muted line
  `vendor · model` (rendered only when at least one is present; join with ` · `
  skipping empties).
- Add a **Function** column (header `Function`, after the Kind column) showing
  `row.Function`.
- `handleDeviceList`'s `?q` filter also matches `vendor`, `model`, and
  `function` (lower-cased), so `?q=shelly` or `?q=AP` finds devices by those
  fields.

## Error handling

- Model / function / vendor are free text; empty is allowed; no validation.
- Unknown `kind` still returns 400 via `validKinds` (router/modem now pass).
- Migration failure aborts the transactional pass (existing behavior). The
  explicit `INSERT` column list prevents silent column-order drift.

## Testing

- **store** (`device_test.go`): `CreateDevice{Kind:"router"}` and `{Kind:"modem"}`
  succeed (CHECK widened); `Model`/`Function` round-trip through Create→GetDevice
  and through UpdateDevice; a nonsense kind (e.g. `"banana"`) still fails the
  CHECK; a device created after migration accepts an iface + IP + tag (rebuild
  left FKs and child tables intact).
- **views** (`icons_test.go`): `KindIcon("router") == "🛜"`,
  `KindIcon("modem") == "📶"`.
- **web** (`devices_test.go`):
  - `GET /devices/new` fragment contains `name="vendor"`, `name="model"`,
    `name="function"`, and `<option value="router">` / `<option value="modem">`.
  - `POST /devices` with `vendor`/`model`/`function` persists all three
    (verify via `GetDevice`).
  - `POST /devices/{id}` updates all three.
  - The list page renders a device's `Function` value and its `vendor · model`
    line.
  - Filter by a new field: seed a device with vendor `Espressif Inc.` and
    another with vendor `Acme`; `GET /devices?q=espressif` includes the first
    and excludes the second (proves `?q` matches vendor).

## Project layout (files added / modified)

- Create: `internal/store/migrations/0005_device_model_function.sql`.
- Modify:
  - `internal/store/device.go` (Model/Function, deviceCols, scanDevice,
    Create/Update) + `internal/store/device_test.go`.
  - `internal/web/devices.go` (validKinds router/modem; create/update read
    vendor/model/function; `?q` matches them) + `internal/web/devices_test.go`.
  - `internal/web/views/devices.templ` (deviceKinds; DeviceDialog inputs;
    DeviceList cell line + Function column; DevicePage facts line) +
    regenerated `internal/web/views/devices_templ.go`.
  - `internal/web/views/icons.go` (kindIcons router/modem; IconChoices +🛜📶) +
    `internal/web/views/icons_test.go`.
  - `internal/web/static/dialog.js` (KIND_ICON router/modem).

## Out of scope (C1)

- Separate channel field (folded into Function).
- Scan auto-filling model/function (not derivable; only vendor is).
- Any scan-config or grid changes (C2 and C3).
- Sorting the list by the new columns (existing sort keys unchanged; filtering
  covers discovery).
