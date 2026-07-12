# Netis Richer Device Model (Sub-project C1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every device first-class `model` and `function` fields plus two new kinds (`router`, `modem`), and surface vendor/model/function in the dialog, detail page, and device list.

**Architecture:** A table-rebuild migration (0005, same idiom as 0002) widens the `device.kind` CHECK and adds `model`/`function` columns; the store `Device` struct and `deviceCols` carry them. Kinds and their default icons are extended across the Go kind lists, the icon maps, and `dialog.js`. The device dialog gains three editable inputs (vendor/model/function) whose values the create/update handlers persist; the detail page and list render them and the `?q` filter matches them.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; single static binary, `CGO_ENABLED=0`.
- Migration is a table rebuild under the existing FK-off transactional migrate loop; the `INSERT` lists columns explicitly (no `SELECT *`).
- `function` is a valid unquoted SQLite identifier (not a reserved keyword).
- Regenerate templ after editing any `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate` from repo root; commit the regenerated `*_templ.go` with its source (no build step).
- The 12 device kinds, in display order: `computer, switch, router, modem, phone, server, printer, iot, vm, lxc, wg-peer, other`.
- Default icons: `router → 🛜`, `modem → 📶`.
- `errors.Is(err, sql.ErrNoRows)` for not-found (house rule); commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Migration 0005 + store model/function fields

**Files:**
- Create: `internal/store/migrations/0005_device_model_function.sql`
- Modify: `internal/store/device.go` (Device struct, `deviceCols`, `scanDevice`, `CreateDevice`, `UpdateDevice`)
- Test: `internal/store/device_test.go` (add `TestDeviceModelFunctionAndKinds`)

**Interfaces:**
- Produces: `store.Device` with `Model string` and `Function string`; `CreateDevice`/`UpdateDevice`/`GetDevice`/`ListDevices` persist and return them; `device.kind` CHECK accepts `router` and `modem`.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/device_test.go`:

```go
func TestDeviceModelFunctionAndKinds(t *testing.T) {
	s := openTest(t)

	// New kinds accepted; model/function persist.
	rID, err := s.CreateDevice(Device{Name: "ap", Kind: "router", Source: "manual",
		Model: "ARCHER-A8 v1", Function: "AP Dachboden CH:1,36"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDevice(Device{Name: "m", Kind: "modem", Source: "manual"}); err != nil {
		t.Fatal(err)
	}

	// Garbage kind still rejected by the CHECK.
	if _, err := s.CreateDevice(Device{Name: "x", Kind: "banana", Source: "manual"}); err == nil {
		t.Fatal("expected CHECK to reject kind 'banana'")
	}

	// Round-trip through GetDevice.
	d, err := s.GetDevice(rID)
	if err != nil || d.Model != "ARCHER-A8 v1" || d.Function != "AP Dachboden CH:1,36" {
		t.Fatalf("round-trip model/function: %+v err=%v", d, err)
	}

	// UpdateDevice persists new values.
	d.Model = "ARCHER-C7 v5"
	d.Function = "AP Garten"
	if err := s.UpdateDevice(d); err != nil {
		t.Fatal(err)
	}
	d2, _ := s.GetDevice(rID)
	if d2.Model != "ARCHER-C7 v5" || d2.Function != "AP Garten" {
		t.Fatalf("update model/function: %+v", d2)
	}

	// FK children still attach after the table rebuild.
	fID, err := s.AddIface(rID, strp("aa:bb:cc:dd:ee:01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	if _, err := s.AssignIP(fID, snID, "10.0.0.9", "static"); err != nil {
		t.Fatal(err)
	}
	tID, _ := s.CreateTag("net", "#888888")
	if err := s.TagDevice(rID, tID); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestDeviceModelFunctionAndKinds -v`
Expected: FAIL — `Device` has no `Model`/`Function` field (compile error).

- [ ] **Step 3: Create the migration**

Create `internal/store/migrations/0005_device_model_function.sql`:

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

- [ ] **Step 4: Add the struct fields and column wiring**

In `internal/store/device.go`:

Add the two fields to the `Device` struct (after `Reviewed bool`):

```go
	Icon           string
	Reviewed       bool
	Model          string
	Function       string
```

Change `deviceCols` (append `,model,function`):

```go
const deviceCols = `id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed,model,function`
```

Change `scanDevice` to scan the two extra fields in the same order:

```go
func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Notes, &d.Vendor, &d.Source,
		&d.ParentDeviceID, &d.ProxmoxVMID, &d.WGPubKey, &d.Icon, &d.Reviewed, &d.Model, &d.Function)
	return d, err
}
```

Change `CreateDevice`'s INSERT to include the two columns:

```go
func (s *Store) CreateDevice(d Device) (int64, error) {
	reviewed := d.Source != "scan"
	res, err := s.DB.Exec(`INSERT INTO device (name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,reviewed,model,function)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source, d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon, reviewed, d.Model, d.Function)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
```

Change `UpdateDevice`'s SET to include the two columns:

```go
func (s *Store) UpdateDevice(d Device) error {
	// Editing a device counts as reviewing it.
	_, err := s.DB.Exec(`UPDATE device SET name=?,kind=?,notes=?,vendor=?,source=?,
		parent_device_id=?,proxmox_vmid=?,wg_pubkey=?,icon=?,reviewed=1,model=?,function=? WHERE id=?`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source,
		d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon, d.Model, d.Function, d.ID)
	return err
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestDeviceModelFunctionAndKinds -v`
Expected: PASS.

- [ ] **Step 6: Run the whole store package (guards the rebuild against child-table breakage)**

Run: `go test ./internal/store/`
Expected: PASS (all existing store tests survive the migration).

- [ ] **Step 7: Commit**

```bash
git add internal/store/migrations/0005_device_model_function.sql internal/store/device.go internal/store/device_test.go
git commit -m "$(printf 'feat: add device model/function columns and router/modem kinds (migration 0005)\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Router/modem kinds and icons across the app

**Files:**
- Modify: `internal/web/devices.go` (`validKinds`)
- Modify: `internal/web/views/devices.templ` (`deviceKinds`)
- Modify: `internal/web/views/icons.go` (`kindIcons`, `IconChoices`)
- Modify: `internal/web/static/dialog.js` (`KIND_ICON`)
- Test: `internal/web/views/icons_test.go` (extend `TestKindIcon`)
- Test: `internal/web/devices_test.go` (add `TestRouterModemKind`)

**Interfaces:**
- Consumes: migration 0005 (Task 1) so `POST /devices` with `kind=router` persists.
- Produces: `router`/`modem` accepted by `validKinds`, listed in every kind `<select>` (via `deviceKinds`), with `KindIcon("router")=="🛜"` and `KindIcon("modem")=="📶"`.

- [ ] **Step 1: Extend the icon unit test**

In `internal/web/views/icons_test.go`, add two entries to the `cases` map in `TestKindIcon`:

```go
		"vm": "🧊", "lxc": "📦", "wg-peer": "🔒", "other": "❓",
		"router": "🛜", "modem": "📶",
		"nonsense": "❓", "": "❓",
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/views/ -run TestKindIcon -v`
Expected: FAIL — `KindIcon("router")` returns `❓`.

- [ ] **Step 3: Add the icons**

In `internal/web/views/icons.go`, add the two kinds to `kindIcons`:

```go
	"wg-peer":  "🔒",
	"router":   "🛜",
	"modem":    "📶",
	"other":    "❓",
```

And append the two glyphs to `IconChoices` so they are pickable:

```go
var IconChoices = []string{
	"💻", "🔀", "📱", "🖥️", "🖨️", "💡", "🧊", "📦", "🔒", "❓",
	"🛜", "📶", "📡", "🗄️", "📷", "🔌", "🎮", "📺", "☎️", "🕹️", "🛰️", "⌚",
}
```

- [ ] **Step 4: Run the icon test to verify it passes**

Run: `go test ./internal/web/views/ -run TestKindIcon -v`
Expected: PASS.

- [ ] **Step 5: Add the kinds to the Go/JS kind lists**

In `internal/web/devices.go`, add `router`/`modem` to `validKinds`:

```go
var validKinds = map[string]bool{"computer": true, "switch": true, "phone": true,
	"server": true, "printer": true, "iot": true, "vm": true, "lxc": true,
	"wg-peer": true, "router": true, "modem": true, "other": true}
```

In `internal/web/views/devices.templ`, change `deviceKinds` (insert after `switch`):

```go
var deviceKinds = []string{"computer", "switch", "router", "modem", "phone", "server", "printer",
	"iot", "vm", "lxc", "wg-peer", "other"}
```

In `internal/web/static/dialog.js`, add the two entries to the `KIND_ICON` map:

```javascript
	var KIND_ICON = {
		computer: '💻', switch: '🔀', router: '🛜', modem: '📶', phone: '📱',
		server: '🖥️', printer: '🖨️', iot: '💡', vm: '🧊', lxc: '📦',
		'wg-peer': '🔒', other: '❓'
	};
```

- [ ] **Step 6: Write the failing web test**

Add to `internal/web/devices_test.go`:

```go
func TestRouterModemKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")

	// The dialog offers the new kinds.
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	for _, want := range []string{`<option value="router">`, `<option value="modem">`} {
		if !strings.Contains(body, want) {
			t.Errorf("new dialog missing %q", want)
		}
	}

	// A router device can be created (handler accepts the kind, store persists it).
	rec := authedPost(t, srv, st, "/devices", url.Values{"name": {"ap"}, "kind": {"router"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create router code=%d body=%s", rec.Code, rec.Body.String())
	}
	d, _ := st.GetDevice(1)
	if d.Kind != "router" {
		t.Fatalf("kind=%q, want router", d.Kind)
	}
}
```

- [ ] **Step 7: Regenerate templ, run tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/... ./internal/web/views/... -run 'TestKindIcon|TestRouterModemKind' -v
```
Expected: PASS (both).

- [ ] **Step 8: Commit**

```bash
git add internal/web/devices.go internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/views/icons.go internal/web/views/icons_test.go internal/web/static/dialog.js internal/web/devices_test.go
git commit -m "$(printf 'feat: add router/modem kinds with default icons across the app\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 3: Dialog vendor/model/function inputs + handler persistence

**Files:**
- Modify: `internal/web/views/devices.templ` (`DeviceDialog`: three inputs)
- Modify: `internal/web/devices.go` (`handleDeviceCreate`, `handleDeviceUpdate` read the fields)
- Test: `internal/web/devices_test.go` (add `TestDialogPersistsVendorModelFunction`)

**Interfaces:**
- Consumes: `store.Device.Vendor/Model/Function` (Task 1).
- Produces: the create/edit dialog form carries `vendor`/`model`/`function`; `handleDeviceCreate` and `handleDeviceUpdate` persist them.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestDialogPersistsVendorModelFunction(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")

	// The dialog exposes the three inputs.
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	for _, want := range []string{`name="vendor"`, `name="model"`, `name="function"`} {
		if !strings.Contains(body, want) {
			t.Errorf("new dialog missing %q", want)
		}
	}

	// Create persists all three.
	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"archera8"}, "kind": {"router"},
		"vendor": {"TP-Link"}, "model": {"ARCHER-A8 v1"}, "function": {"AP Dachboden CH:1,36"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create code=%d", rec.Code)
	}
	d, _ := st.GetDevice(1)
	if d.Vendor != "TP-Link" || d.Model != "ARCHER-A8 v1" || d.Function != "AP Dachboden CH:1,36" {
		t.Fatalf("create persisted %+v", d)
	}

	// Update overwrites all three.
	rec = authedPost(t, srv, st, "/devices/1", url.Values{
		"name": {"archera8"}, "kind": {"router"},
		"vendor": {"TP-Link Corp"}, "model": {"ARCHER-C7 v5"}, "function": {"AP Garten"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update code=%d", rec.Code)
	}
	d2, _ := st.GetDevice(1)
	if d2.Vendor != "TP-Link Corp" || d2.Model != "ARCHER-C7 v5" || d2.Function != "AP Garten" {
		t.Fatalf("update persisted %+v", d2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestDialogPersistsVendorModelFunction -v`
Expected: FAIL — dialog lacks the inputs / handlers ignore the fields.

- [ ] **Step 3: Add the three inputs to `DeviceDialog`**

In `internal/web/views/devices.templ`, insert this block immediately after the icon block's closing `</div>` + hidden input (the line `<input type="hidden" name="icon" value={ DeviceIcon(d.Icon, d.Kind) }/>` then `</div>`, at ~line 277-278) and before the `<label> Parent device` block:

```
					<label>
						Vendor
						<input type="text" name="vendor" value={ d.Vendor }/>
					</label>
					<label>
						Model
						<input type="text" name="model" value={ d.Model }/>
					</label>
					<label>
						Function
						<input type="text" name="function" value={ d.Function } placeholder="role, channel…"/>
					</label>
```

- [ ] **Step 4: Read the fields in the handlers**

In `internal/web/devices.go`, in `handleDeviceCreate`, extend the `dev` literal:

```go
	dev := store.Device{
		Name: name, Kind: kind, Notes: r.FormValue("notes"),
		Icon: r.FormValue("icon"), Source: "manual",
		Vendor: r.FormValue("vendor"), Model: r.FormValue("model"), Function: r.FormValue("function"),
	}
```

In `handleDeviceUpdate`, after the existing `d.Icon = r.FormValue("icon")` line, add:

```go
	d.Vendor = r.FormValue("vendor")
	d.Model = r.FormValue("model")
	d.Function = r.FormValue("function")
```

- [ ] **Step 5: Regenerate templ, run test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestDialogPersistsVendorModelFunction -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/devices_test.go
git commit -m "$(printf 'feat: edit vendor, model and function in the device dialog\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 4: List column + subline, detail facts line, `?q` matching

**Files:**
- Modify: `internal/web/views/devices.templ` (`DeviceList` header/cell/column, `DevicePage` facts line, `joinNonEmpty` helper)
- Modify: `internal/web/devices.go` (`handleDeviceList` `?q` filter)
- Test: `internal/web/devices_test.go` (add `TestListShowsAndFiltersNewFields`)

**Interfaces:**
- Consumes: `store.DeviceRow.Vendor/Model/Function` (promoted from `Device`, Task 1).
- Produces: the list shows a `vendor · model` subline and a Function column; the detail page shows a combined facts line; `?q` matches vendor/model/function.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestListShowsAndFiltersNewFields(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "pv", Kind: "iot", Source: "manual",
		Vendor: "Espressif Inc.", Model: "Shelly Plus Plug", Function: "PV Powermeter"})
	st.CreateDevice(store.Device{Name: "printer0", Kind: "printer", Source: "manual",
		Vendor: "Acme"})

	body := authedGet(t, srv, st, "/devices").Body.String()
	// Function value and the "vendor · model" subline render.
	if !strings.Contains(body, "PV Powermeter") {
		t.Error("list missing function value")
	}
	if !strings.Contains(body, "Espressif Inc. · Shelly Plus Plug") {
		t.Error("list missing vendor · model subline")
	}

	// ?q matches the vendor field.
	filtered := authedGet(t, srv, st, "/devices?q=espressif").Body.String()
	if !strings.Contains(filtered, "pv") || strings.Contains(filtered, "printer0") {
		t.Error("?q=espressif should match by vendor and exclude the Acme device")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestListShowsAndFiltersNewFields -v`
Expected: FAIL — no subline/column; `?q` ignores vendor.

- [ ] **Step 3: Add the `joinNonEmpty` helper**

In `internal/web/views/devices.templ`, add this Go function (near `lowestIPStr`, outside any `templ` block):

```go
// joinNonEmpty joins the non-empty parts with sep, used for the device's
// vendor/model/function facts line.
func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
```

(`strings` is already imported in `devices.templ`.)

- [ ] **Step 4: Add the list subline and Function column**

In `internal/web/views/devices.templ` `DeviceList`:

Add a `Function` header after the Kind header (the `@sortHeader("Kind", ...)` line, ~line 106):

```
						@sortHeader("Kind", "kind", sortKey, dir, q)
						<th>Function</th>
						<th>Tags</th>
```

Add the `vendor · model` subline in the device cell, immediately after the MAC `for` loop (after its closing `}` at ~line 131, still inside the inner `<div>`):

```
										for i, mac := range row.MACs {
											if i == 0 {
												<div class="mono muted" style="font-size:11.5px">{ mac }</div>
											}
										}
										if joinNonEmpty(" · ", row.Vendor, row.Model) != "" {
											<div class="muted" style="font-size:11.5px">{ joinNonEmpty(" · ", row.Vendor, row.Model) }</div>
										}
```

Add the Function cell after the Kind cell (the `<td>{ row.Kind }</td>` line, ~line 156):

```
							<td>{ row.Kind }</td>
							<td>{ row.Function }</td>
							<td>
								for _, tn := range row.TagNames {
```

- [ ] **Step 5: Replace the detail-page vendor line with a facts line**

In `internal/web/views/devices.templ` `DevicePage`, replace the existing vendor block (~lines 363-365):

```
			if d.Device.Vendor != "" {
				<p class="muted">{ d.Device.Vendor }</p>
			}
```

with:

```
			if joinNonEmpty(" · ", d.Device.Vendor, d.Device.Model, d.Device.Function) != "" {
				<p class="muted">{ joinNonEmpty(" · ", d.Device.Vendor, d.Device.Model, d.Device.Function) }</p>
			}
```

- [ ] **Step 6: Extend the `?q` filter**

In `internal/web/devices.go` `handleDeviceList`, change the `hay` construction to include the new fields:

```go
			hay := strings.ToLower(row.Name + " " + strings.Join(ips, " ") + " " +
				strings.Join(row.MACs, " ") + " " + strings.Join(row.TagNames, " ") + " " +
				row.Vendor + " " + row.Model + " " + row.Function)
```

- [ ] **Step 7: Regenerate templ, run test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestListShowsAndFiltersNewFields -v
```
Expected: PASS.

- [ ] **Step 8: Full suite + build**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass.

- [ ] **Step 9: Commit**

```bash
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/devices_test.go
git commit -m "$(printf 'feat: show and filter vendor/model/function in the device list and detail\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- Migration 0005 (rebuild, kind CHECK +router/modem, +model/function) → Task 1. ✅
- Store Device.Model/Function, deviceCols, scanDevice, Create/Update → Task 1. ✅
- validKinds/deviceKinds/kindIcons/IconChoices/dialog.js KIND_ICON router/modem → Task 2. ✅
- Dialog vendor/model/function inputs + handler persistence → Task 3. ✅
- List vendor·model subline + Function column + ?q match; detail facts line → Task 4. ✅
- Tests (store kinds/round-trip/FK, icons, dialog options+persist, list render+filter) → Tasks 1-4. ✅
- Out-of-scope (separate channel, scan auto-fill model/function, new sort keys) → not implemented, matches spec. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `Device.Model`/`Function` (Task 1) used identically in Tasks 3 (`r.FormValue`) and 4 (`row.Vendor/Model/Function`, `d.Device.*`). `deviceCols`/`scanDevice`/INSERT/SET all list the two columns in matching order (`…reviewed,model,function`). `joinNonEmpty(sep string, parts ...string) string` (Task 4) is used consistently in both the list subline and the detail facts line. `KindIcon("router")=="🛜"`/`("modem")=="📶"` (Task 2) match the migration's CHECK values and `deviceKinds`. The dialog inputs' `name` attributes (`vendor`/`model`/`function`, Task 3) match the handler `r.FormValue` keys.

**Ordering note:** Task 2 lets `POST /devices` with `kind=router` succeed only because Task 1's migration widened the CHECK — Tasks run in order, so this holds. Tasks 2-4 each edit `devices.templ` and regenerate `devices_templ.go`; sequential execution avoids conflicts.
