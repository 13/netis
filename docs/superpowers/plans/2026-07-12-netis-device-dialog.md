# Netis Device Create/Edit Dialog (Sub-project C) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the full-page device create form and the detail page's inline edit/tag forms with one modal dialog (create + edit) featuring a selectable icon with per-kind default, a parent-device picker, and a comma-separated tags field.

**Architecture:** The dialog is a templ fragment (`.dialog-scrim > .dialog`, no `Layout`) served by `GET /devices/new` and `GET /devices/{id}/edit`, injected via HTMX into a persistent `<div id="modal">` in `Layout`. The form submits as a plain POST to the existing `POST /devices` / `POST /devices/{id}` handlers (extended to read icon/parent/tags), whose 303 redirect reloads the page and dismisses the modal. A small `dialog.js` handles close (✕/scrim/Escape) and the icon picker (click-select + per-kind default on Kind change). Tags sync through a new store method; no schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, vanilla JS, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; single static binary, `CGO_ENABLED=0`.
- No schema/migration change — `device.icon` column and `tag`/`device_tag` tables already exist.
- Use `errors.Is(err, sql.ErrNoRows)` for not-found checks (never string compare).
- Not-found on a device id → HTTP 404; bad kind → 400; keep existing `requireAdmin` gating on all mutating POST routes; `GET` fragment routes are read-only (auth-gated by `requireAuth`, no `requireAdmin`).
- Regenerate templ after editing any `.templ`: run `/home/ben/go/bin/templ generate` from the repo root before `go build`/`go test`.
- Commit trailer on every commit: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- The 10 device kinds (order): `computer, switch, phone, server, printer, iot, vm, lxc, wg-peer, other` (already in `views.deviceKinds` and `web.validKinds`).

---

### Task 1: Store — `SetDeviceTags` tag sync

**Files:**
- Modify: `internal/store/meta.go` (add `SetDeviceTags` after `UntagDevice`, ~line 56)
- Test: `internal/store/meta_test.go` (add `TestSetDeviceTags`)

**Interfaces:**
- Consumes: existing `DeviceTags(deviceID int64) ([]Tag, error)`, `ListTags() ([]Tag, error)`, `CreateTag(name, color string) (int64, error)`, `TagDevice(deviceID, tagID int64) error`, `UntagDevice(deviceID, tagID int64) error`.
- Produces: `func (s *Store) SetDeviceTags(deviceID int64, names []string) error` — syncs the device's tags to exactly the (trimmed, de-duplicated, non-empty) `names`, creating tags that don't exist with color `#888888`.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/meta_test.go`:

```go
func TestSetDeviceTags(t *testing.T) {
	s := openTest(t)
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "manual"})

	names := func() []string {
		tags, _ := s.DeviceTags(devID)
		out := make([]string, 0, len(tags))
		for _, tg := range tags {
			out = append(out, tg.Name)
		}
		return out
	}

	// Attach two, creating tags that don't exist. Input has dupes/blanks/spaces.
	if err := s.SetDeviceTags(devID, []string{"web", " web ", "", "db"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 2 || got[0] != "db" || got[1] != "web" {
		t.Fatalf("after first sync tags=%v, want [db web]", got)
	}

	// Re-sync to a set that drops "db", keeps "web", adds "nas".
	if err := s.SetDeviceTags(devID, []string{"web", "nas"}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 2 || got[0] != "nas" || got[1] != "web" {
		t.Fatalf("after second sync tags=%v, want [nas web]", got)
	}

	// Empty set detaches everything.
	if err := s.SetDeviceTags(devID, []string{"  ", ""}); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 0 {
		t.Fatalf("after clear tags=%v, want []", got)
	}

	// A tag reused across syncs is not duplicated in the tag table.
	all, _ := s.ListTags()
	seen := map[string]int{}
	for _, tg := range all {
		seen[tg.Name]++
	}
	if seen["web"] != 1 {
		t.Fatalf("tag 'web' should exist exactly once, got %d", seen["web"])
	}
}
```

(`DeviceTags` orders by name, so assertions use alphabetical order.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSetDeviceTags -v`
Expected: FAIL — `s.SetDeviceTags undefined`.

- [ ] **Step 3: Implement `SetDeviceTags`**

Add to `internal/store/meta.go` after the `UntagDevice` function:

```go
// SetDeviceTags syncs a device's tags to exactly the given names: names are
// trimmed, blanks dropped, and de-duplicated; tags that don't exist are
// created (color #888888); tags no longer present are detached. Idempotent
// and order-independent.
func (s *Store) SetDeviceTags(deviceID int64, names []string) error {
	// Build the desired set (trim, drop empty, dedup).
	want := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			want[n] = true
		}
	}

	current, err := s.DeviceTags(deviceID)
	if err != nil {
		return err
	}
	have := map[string]int64{} // name -> tag id, currently attached
	for _, t := range current {
		have[t.Name] = t.ID
	}

	// Detach tags no longer wanted.
	for name, id := range have {
		if !want[name] {
			if err := s.UntagDevice(deviceID, id); err != nil {
				return err
			}
		}
	}

	// Attach wanted tags not already present (find-or-create).
	for name := range want {
		if _, ok := have[name]; ok {
			continue
		}
		id, err := s.findOrCreateTag(name)
		if err != nil {
			return err
		}
		if err := s.TagDevice(deviceID, id); err != nil {
			return err
		}
	}
	return nil
}

// findOrCreateTag returns the id of the tag with the given name, creating it
// with a neutral color if it does not exist yet.
func (s *Store) findOrCreateTag(name string) (int64, error) {
	tags, err := s.ListTags()
	if err != nil {
		return 0, err
	}
	for _, t := range tags {
		if t.Name == name {
			return t.ID, nil
		}
	}
	return s.CreateTag(name, "#888888")
}
```

Add `"strings"` to the import block of `internal/store/meta.go` (the file currently has no imports — add `import "strings"` under the `package store` line).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSetDeviceTags -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
/home/ben/go/bin/templ generate >/dev/null 2>&1 || true
git add internal/store/meta.go internal/store/meta_test.go
git commit -m "$(printf 'feat: add store.SetDeviceTags tag sync\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Views — `DeviceIcon` + `IconChoices`, render device icon in list/grid

**Files:**
- Modify: `internal/web/views/icons.go` (add `DeviceIcon`, `IconChoices`)
- Modify: `internal/web/views/devices.templ` (list row + grid tile: `KindIcon(row.Kind)` → `DeviceIcon(row.Icon, row.Kind)`)
- Test: `internal/web/views/icons_test.go` (add `TestDeviceIcon`)
- Test: `internal/web/devices_test.go` (add `TestDeviceListShowsStoredIcon`)

**Interfaces:**
- Consumes: existing `KindIcon(kind string) string`, `kindIcons` map, `store.DeviceRow` (embeds `Device` with `Icon`, `Kind`).
- Produces: `func DeviceIcon(icon, kind string) string`; `var IconChoices []string` (ordered emoji palette for the dialog picker, consumed in Task 4).

- [ ] **Step 1: Write the failing unit test**

Add to `internal/web/views/icons_test.go`:

```go
func TestDeviceIcon(t *testing.T) {
	if got := DeviceIcon("🎮", "phone"); got != "🎮" {
		t.Errorf("explicit icon should win, got %q", got)
	}
	if got := DeviceIcon("", "phone"); got != "📱" {
		t.Errorf("empty icon falls back to kind default, got %q", got)
	}
	if got := DeviceIcon("", "nonsense"); got != "❓" {
		t.Errorf("unknown kind falls back to ❓, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/views/ -run TestDeviceIcon -v`
Expected: FAIL — `undefined: DeviceIcon`.

- [ ] **Step 3: Implement `DeviceIcon` + `IconChoices`**

Append to `internal/web/views/icons.go`:

```go
// DeviceIcon returns the device's chosen icon, or the kind default when the
// device has no explicit icon set.
func DeviceIcon(icon, kind string) string {
	if icon != "" {
		return icon
	}
	return KindIcon(kind)
}

// IconChoices is the ordered emoji palette shown in the device dialog's icon
// picker. The first ten mirror the kind defaults; the rest are common extras.
var IconChoices = []string{
	"💻", "🔀", "📱", "🖥️", "🖨️", "💡", "🧊", "📦", "🔒", "❓",
	"📡", "🗄️", "📷", "🔌", "🎮", "📺", "☎️", "🕹️", "🛰️", "⌚",
}
```

- [ ] **Step 4: Run unit test to verify it passes**

Run: `go test ./internal/web/views/ -run TestDeviceIcon -v`
Expected: PASS.

- [ ] **Step 5: Swap the two render sites in `devices.templ`**

In `internal/web/views/devices.templ`, the list row (currently line ~117):

```
<span class="ic">{ KindIcon(row.Kind) }</span>
```
→
```
<span class="ic">{ DeviceIcon(row.Icon, row.Kind) }</span>
```

And the grid tile (currently line ~190):

```
<span class="ic">{ KindIcon(row.Kind) }</span>
```
→
```
<span class="ic">{ DeviceIcon(row.Icon, row.Kind) }</span>
```

(`row.Icon`/`row.Kind` are promoted from the embedded `store.Device`.)

- [ ] **Step 6: Write the failing render test**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceListShowsStoredIcon(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "console", Kind: "other", Icon: "🎮", Source: "manual"})
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "🎮") {
		t.Fatal("device list should render the stored icon")
	}
}
```

- [ ] **Step 7: Regenerate templ, run tests**

Run:
```bash
/home/ben/go/bin/templ generate
go test ./internal/web/... ./internal/web/views/... -run 'TestDeviceIcon|TestDeviceListShowsStoredIcon' -v
```
Expected: PASS (both).

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/icons.go internal/web/views/icons_test.go internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices_test.go
git commit -m "$(printf 'feat: render per-device icon in list/grid; add IconChoices palette\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 3: Layout `#modal` container + `dialog.js`

**Files:**
- Create: `internal/web/static/dialog.js`
- Modify: `internal/web/views/layout.templ` (add `<div id="modal">` and `<script src="/static/dialog.js">`)
- Test: `internal/web/devices_test.go` (add `TestLayoutHasModalContainer`)

**Interfaces:**
- Produces: an always-present `<div id="modal"></div>` HTMX target on every authenticated page, and `dialog.js` providing close (✕/scrim/Escape) + icon-picker + kind-default behavior (behavior verified by visual smoke, not unit tests).

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestLayoutHasModalContainer(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{`id="modal"`, "/static/dialog.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("layout missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestLayoutHasModalContainer -v`
Expected: FAIL — body lacks `id="modal"`.

- [ ] **Step 3: Add the modal container + script to `layout.templ`**

In `internal/web/views/layout.templ`, change the `<main>`/script tail (currently lines ~45-48):

```
			<main>
				{ children... }
			</main>
			<script src="/static/theme.js"></script>
```
→
```
			<main>
				{ children... }
			</main>
			<div id="modal"></div>
			<script src="/static/theme.js"></script>
			<script src="/static/dialog.js"></script>
```

- [ ] **Step 4: Create `internal/web/static/dialog.js`**

```javascript
(function () {
	// Per-kind default icons — mirrors views.kindIcons. Used to keep the icon
	// picker's selection in sync with the Kind field until the user overrides.
	var KIND_ICON = {
		computer: '💻', switch: '🔀', phone: '📱', server: '🖥️', printer: '🖨️',
		iot: '💡', vm: '🧊', lxc: '📦', 'wg-peer': '🔒', other: '❓'
	};

	function closeModal() {
		var m = document.getElementById('modal');
		if (m) { m.innerHTML = ''; }
	}

	// Close on ✕ / Cancel ([data-close]) or a click on the scrim backdrop itself.
	document.addEventListener('click', function (e) {
		if (e.target.closest('[data-close]')) { closeModal(); return; }
		if (e.target.classList && e.target.classList.contains('dialog-scrim')) { closeModal(); }
	});

	// Close on Escape when a dialog is open.
	document.addEventListener('keydown', function (e) {
		if (e.key === 'Escape' && document.querySelector('#modal .dialog')) { closeModal(); }
	});

	// Icon picker: click a swatch to select it.
	document.addEventListener('click', function (e) {
		var sw = e.target.closest('.ic-swatch');
		if (!sw) { return; }
		var pick = sw.closest('.iconpick');
		if (!pick) { return; }
		e.preventDefault();
		selectSwatch(pick, sw.dataset.icon);
	});

	// Kind change: if the icon is still the previous kind's default (untouched),
	// switch it to the new kind's default; always remember the new default.
	document.addEventListener('change', function (e) {
		if (!e.target.matches || !e.target.matches('select[name=kind]')) { return; }
		var dialog = e.target.closest('.dialog');
		if (!dialog) { return; }
		var pick = dialog.querySelector('.iconpick');
		var hidden = dialog.querySelector('input[name=icon]');
		if (!pick || !hidden) { return; }
		var newDefault = KIND_ICON[e.target.value] || '❓';
		if (hidden.value === pick.dataset.kindDefault) {
			selectSwatch(pick, newDefault);
		}
		pick.dataset.kindDefault = newDefault;
	});

	function selectSwatch(pick, icon) {
		var hidden = pick.parentNode.querySelector('input[name=icon]') ||
			pick.closest('.dialog').querySelector('input[name=icon]');
		if (hidden) { hidden.value = icon; }
		pick.querySelectorAll('.ic-swatch').forEach(function (b) {
			b.classList.toggle('selected', b.dataset.icon === icon);
		});
	}
})();
```

- [ ] **Step 5: Regenerate templ, run test**

Run:
```bash
/home/ben/go/bin/templ generate
go test ./internal/web/ -run TestLayoutHasModalContainer -v
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/dialog.js internal/web/views/layout.templ internal/web/views/layout_templ.go
git commit -m "$(printf 'feat: add modal container and dialog.js (close + icon picker)\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 4: `DeviceDialog` fragment + fragment handlers + routes + CSS

**Files:**
- Modify: `internal/web/views/devices.templ` (add `DeviceDialog` templ + `tagNamesJoin` helper; remove the now-unused `DeviceForm` templ)
- Modify: `internal/web/devices.go` (`handleDeviceForm` → renders `DeviceDialog`; add `handleDeviceEditForm`)
- Modify: `internal/web/server.go` (add `GET /devices/{id}/edit`)
- Modify: `internal/web/static/app.css` (add `.iconpick` / `.ic-swatch` styles)
- Test: `internal/web/devices_test.go` (add `TestDeviceNewDialogFragment`, `TestDeviceEditDialogPrefilled`, `TestDeviceEditDialogBadID404`)

**Interfaces:**
- Consumes: `views.DeviceIcon`, `views.IconChoices`, `views.KindIcon`, `views.deviceKinds`, `views.lowestIPStr` (Task 2 / existing); `store.ListSubnets() ([]Subnet, error)`, `store.ListDevices() ([]DeviceRow, error)`, `store.GetDevice`, `store.DeviceTags`.
- Produces:
  - templ `DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool)`
  - `func (s *Server) handleDeviceEditForm(w http.ResponseWriter, r *http.Request)` at `GET /devices/{id}/edit`
  - `handleDeviceForm` now renders the create-mode `DeviceDialog` fragment at `GET /devices/new`.

- [ ] **Step 1: Add the `DeviceDialog` templ + helper**

In `internal/web/views/devices.templ`, **replace** the entire `DeviceForm` templ (currently lines ~220-263) with the following `DeviceDialog` templ and helper (this removes `DeviceForm`, which becomes unused):

```
// tagNamesJoin renders a device's tags as a comma-separated string for the
// dialog's tags input.
func tagNamesJoin(tags []store.Tag) string {
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

templ DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool) {
	<div class="dialog-scrim">
		<div class="dialog">
			<div class="dh">
				if isEdit {
					<h3>Edit device</h3>
				} else {
					<h3>New device</h3>
				}
				<button type="button" class="theme-toggle" data-close aria-label="Close">✕</button>
			</div>
			<form
				method="post"
				if isEdit {
					action={ templ.URL(fmt.Sprintf("/devices/%d", d.ID)) }
				} else {
					action="/devices"
				}
			>
				<div class="db">
					<label>
						Name
						<input type="text" name="name" value={ d.Name } required autofocus/>
					</label>
					<label>
						Kind
						<select name="kind">
							for _, k := range deviceKinds {
								if k == d.Kind {
									<option value={ k } selected>{ k }</option>
								} else {
									<option value={ k }>{ k }</option>
								}
							}
						</select>
					</label>
					<div>
						<div class="fieldlabel">Icon</div>
						<div class="iconpick" data-kind-default={ KindIcon(d.Kind) }>
							for _, ic := range IconChoices {
								if ic == DeviceIcon(d.Icon, d.Kind) {
									<button type="button" class="ic-swatch selected" data-icon={ ic }>{ ic }</button>
								} else {
									<button type="button" class="ic-swatch" data-icon={ ic }>{ ic }</button>
								}
							}
						</div>
						<input type="hidden" name="icon" value={ DeviceIcon(d.Icon, d.Kind) }/>
					</div>
					<label>
						Parent device
						<select name="parent_device_id">
							<option value="">— none —</option>
							for _, pd := range allDevices {
								if pd.ID != d.ID {
									if d.ParentDeviceID != nil && *d.ParentDeviceID == pd.ID {
										<option value={ fmt.Sprint(pd.ID) } selected>{ parentLabel(pd) }</option>
									} else {
										<option value={ fmt.Sprint(pd.ID) }>{ parentLabel(pd) }</option>
									}
								}
							}
						</select>
					</label>
					<label>
						Notes
						<textarea name="notes">{ d.Notes }</textarea>
					</label>
					<label>
						Tags
						<input type="text" name="tags" value={ tagNamesJoin(tags) } placeholder="comma,separated"/>
					</label>
					if !isEdit {
						<div class="ifgroup">
							<div class="fieldlabel">First interface (optional)</div>
							<label>
								MAC
								<input type="text" name="mac" placeholder="aa:bb:cc:dd:ee:ff"/>
							</label>
							<label>
								IP
								<input type="text" name="ip" placeholder="10.0.0.5"/>
							</label>
							<label>
								Subnet
								<select name="subnet_id">
									<option value="">—</option>
									for _, sn := range subnets {
										<option value={ fmt.Sprint(sn.ID) }>{ sn.Name } { sn.CIDR }</option>
									}
								</select>
							</label>
						</div>
					}
				</div>
				<div class="df">
					<button type="button" class="theme-toggle" data-close>Cancel</button>
					if isEdit {
						<button type="submit" class="primary">Save</button>
					} else {
						<button type="submit" class="primary">Create device</button>
					}
				</div>
			</form>
		</div>
	</div>
}

// parentLabel formats a device as "name — ip" for the parent <select>,
// omitting the dash when the device has no IP.
func parentLabel(pd store.DeviceRow) string {
	ip := lowestIPStr(pd.IPs)
	if ip == "" {
		return pd.Name
	}
	return pd.Name + " — " + ip
}
```

Add `"strings"` to the import block at the top of `internal/web/views/devices.templ` (it currently imports `fmt`, `net/netip`, `net/url`, `netis/internal/store` — add `"strings"`).

- [ ] **Step 2: Repurpose `handleDeviceForm` and add `handleDeviceEditForm`**

In `internal/web/devices.go`, **replace** `handleDeviceForm` (currently lines ~127-135) with:

```go
func (s *Server) handleDeviceForm(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	all, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.DeviceDialog(store.Device{Kind: "computer"}, nil, subnets, all, false).Render(r.Context(), w)
}

func (s *Server) handleDeviceEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	tags, err := s.store.DeviceTags(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	all, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.DeviceDialog(d, tags, subnets, all, true).Render(r.Context(), w)
}
```

(`handleDeviceForm` no longer needs `userFrom(r)` — the fragment has no `Layout`/username.)

- [ ] **Step 3: Register the edit route**

In `internal/web/server.go`, after the `GET /devices/new` line (~line 55) add:

```go
	s.mux.HandleFunc("GET /devices/{id}/edit", s.handleDeviceEditForm)
```

- [ ] **Step 4: Add the picker CSS**

Append to `internal/web/static/app.css`:

```css
.fieldlabel { font-size:12.5px; font-weight:540; color:var(--muted); margin-bottom:6px; }
.iconpick { display:flex; flex-wrap:wrap; gap:6px; }
.iconpick .ic-swatch { width:34px; height:34px; padding:0; font-size:17px; line-height:1;
  display:inline-flex; align-items:center; justify-content:center;
  background:var(--surface-2); border:1px solid var(--border); border-radius:var(--radius-sm); cursor:pointer; }
.iconpick .ic-swatch.selected { border-color:var(--accent); background:var(--accent-soft); box-shadow:0 0 0 1px var(--accent) inset; }
.ifgroup { border:1px solid var(--border); border-radius:var(--radius-sm); padding:12px 13px; display:flex; flex-direction:column; gap:11px; }
```

- [ ] **Step 5: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceNewDialogFragment(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	for _, want := range []string{`class="dialog"`, "ic-swatch", `name="parent_device_id"`, `name="tags"`, `name="mac"`} {
		if !strings.Contains(body, want) {
			t.Errorf("new dialog fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<nav") {
		t.Error("new dialog should be a fragment, not a full page with <nav>")
	}
}

func TestDeviceEditDialogPrefilled(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	parentID, _ := st.CreateDevice(store.Device{Name: "core-switch", Kind: "switch", Source: "manual"})
	pid := parentID
	devID, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Notes: "shelf", ParentDeviceID: &pid, Source: "manual"})
	st.SetDeviceTags(devID, []string{"storage"})

	body := authedGet(t, srv, st, "/devices/2/edit").Body.String()
	if !strings.Contains(body, `value="nas"`) {
		t.Error("edit dialog missing prefilled name")
	}
	if !strings.Contains(body, `value="storage"`) {
		t.Error("edit dialog missing prefilled tags")
	}
	// Parent device (id 1) must be the pre-selected option. Kind-select
	// "selected" options carry string values (e.g. "server"), so match the
	// numeric parent value specifically.
	if !strings.Contains(body, "core-switch") || !strings.Contains(body, `value="1" selected`) {
		t.Error("edit dialog should pre-select the parent device")
	}
	// The edited device must not appear as a selectable parent of itself.
	if strings.Contains(body, "nas — ") || strings.Contains(body, ">nas<") {
		t.Error("edit dialog should exclude the device itself from parent options")
	}
	_ = devID
}

func TestDeviceEditDialogBadID404(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	if rec := authedGet(t, srv, st, "/devices/999/edit"); rec.Code != http.StatusNotFound {
		t.Fatalf("edit unknown device = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 6: Regenerate templ, run tests**

Run:
```bash
/home/ben/go/bin/templ generate
go test ./internal/web/... -run 'TestDeviceNewDialogFragment|TestDeviceEditDialogPrefilled|TestDeviceEditDialogBadID404' -v
```
Expected: PASS (all three).

- [ ] **Step 7: Full build to catch the removed `DeviceForm`**

Run: `CGO_ENABLED=0 go build ./... && go test ./internal/web/... ./internal/web/views/...`
Expected: builds clean (no lingering `DeviceForm` references), tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/server.go internal/web/static/app.css internal/web/devices_test.go
git commit -m "$(printf 'feat: device create/edit modal dialog fragment + routes\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 5: Extend create/update handlers with icon + parent + tags

**Files:**
- Modify: `internal/web/devices.go` (`handleDeviceCreate`, `handleDeviceUpdate`, add `parseTags`)
- Test: `internal/web/devices_test.go` (add `TestCreateDeviceWithIconParentTags`, `TestUpdateDeviceSyncsTags`)

**Interfaces:**
- Consumes: `store.SetDeviceTags(deviceID int64, names []string) error` (Task 1); existing `store.CreateDevice`, `store.UpdateDevice`, `store.GetDevice`.
- Produces: `func parseTags(s string) []string` (split on `,`, trim, drop empty); create/update now persist `icon`, `parent_device_id`, and `tags`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestCreateDeviceWithIconParentTags(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	parentID, _ := st.CreateDevice(store.Device{Name: "rack", Kind: "switch", Source: "manual"})

	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"nas"}, "kind": {"server"}, "icon": {"🗄️"},
		"parent_device_id": {strconv.FormatInt(parentID, 10)},
		"tags":             {"storage, media"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	// The new device is id 2 (parent is id 1).
	d, err := st.GetDevice(2)
	if err != nil {
		t.Fatal(err)
	}
	if d.Icon != "🗄️" {
		t.Errorf("icon=%q, want 🗄️", d.Icon)
	}
	if d.ParentDeviceID == nil || *d.ParentDeviceID != parentID {
		t.Errorf("parent=%v, want %d", d.ParentDeviceID, parentID)
	}
	tags, _ := st.DeviceTags(2)
	if len(tags) != 2 {
		t.Fatalf("tags=%v, want 2", tags)
	}
}

func TestUpdateDeviceSyncsTags(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	st.SetDeviceTags(devID, []string{"a", "b"})

	rec := authedPost(t, srv, st, "/devices/1", url.Values{
		"name": {"nas"}, "kind": {"server"}, "tags": {"a"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update code=%d", rec.Code)
	}
	tags, _ := st.DeviceTags(devID)
	if len(tags) != 1 || tags[0].Name != "a" {
		t.Fatalf("after update tags=%v, want [a]", tags)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestCreateDeviceWithIconParentTags|TestUpdateDeviceSyncsTags' -v`
Expected: FAIL — create ignores icon/parent/tags; update ignores tags.

- [ ] **Step 3: Add `parseTags` and extend the handlers**

In `internal/web/devices.go`, add near the top (after `normMAC`):

```go
// parseTags splits a comma-separated tags field into trimmed, non-empty names.
func parseTags(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

Replace the body of `handleDeviceCreate` (currently lines ~137-168) with:

```go
func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	kind := r.FormValue("kind")
	if !validKinds[kind] {
		http.Error(w, "bad kind", 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	dev := store.Device{
		Name: name, Kind: kind, Notes: r.FormValue("notes"),
		Icon: r.FormValue("icon"), Source: "manual",
	}
	if p := r.FormValue("parent_device_id"); p != "" {
		if pid, err := strconv.ParseInt(p, 10, 64); err == nil {
			dev.ParentDeviceID = &pid
		}
	}
	devID, err := s.store.CreateDevice(dev)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetDeviceTags(devID, parseTags(r.FormValue("tags"))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if mac := normMAC(r.FormValue("mac")); mac != "" || r.FormValue("ip") != "" {
		var macP *string
		if mac != "" {
			macP = &mac
		}
		ifID, err := s.store.AddIface(devID, macP, nil)
		if err == nil && r.FormValue("ip") != "" {
			if snID, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64); err == nil {
				s.store.AssignIP(ifID, snID, r.FormValue("ip"), "static")
			}
		}
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(devID, 10), http.StatusSeeOther)
}
```

In `handleDeviceUpdate`, after the successful `UpdateDevice` call (currently lines ~200-203), insert the tag sync before the redirect:

```go
	if err := s.store.UpdateDevice(d); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetDeviceTags(d.ID, parseTags(r.FormValue("tags"))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestCreateDeviceWithIconParentTags|TestUpdateDeviceSyncsTags' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
/home/ben/go/bin/templ generate >/dev/null 2>&1 || true
git add internal/web/devices.go internal/web/devices_test.go
git commit -m "$(printf 'feat: persist icon, parent and tags from device dialog submit\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 6: Detail page rework — Edit button, read-only tags, remove inline forms & tag routes

**Files:**
- Modify: `internal/web/views/devices.templ` (`DevicePage`: header uses `DeviceIcon`, add Edit button, read-only tags, remove inline edit form + inline tag add/remove forms; drop `AllTags` from `DeviceDetail`)
- Modify: `internal/web/devices.go` (`handleDevicePage` stops loading/passing `allTags`; remove `handleTagAdd`, `handleTagRemove`, `findOrCreateTag`)
- Modify: `internal/web/server.go` (remove `POST /devices/{id}/tags` and `POST /devices/{id}/tags/{tagID}/delete`)
- Test: `internal/web/devices_test.go` (add `TestDetailPageHasEditButtonNoInlineForm`)

**Interfaces:**
- Consumes: `views.DeviceIcon` (Task 2); the `GET /devices/{id}/edit` route (Task 4).
- Produces: detail page renders an `hx-get="/devices/{id}/edit" hx-target="#modal"` Edit button and read-only tag chips; the `DeviceDetail` struct no longer has an `AllTags` field; the two tag routes and their handlers are gone.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestDetailPageHasEditButtonNoInlineForm(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "nas", Kind: "server", Notes: "shelf", Source: "manual"})
	body := authedGet(t, srv, st, "/devices/1").Body.String()

	if !strings.Contains(body, `hx-get="/devices/1/edit"`) {
		t.Error("detail page should have an Edit button targeting the edit fragment")
	}
	// The old inline edit form had a notes <textarea>; it now lives only in the dialog.
	if strings.Contains(body, "<textarea") {
		t.Error("detail page should no longer contain the inline edit form")
	}
	// The old per-tag add form posted to /devices/1/tags; it is gone.
	if strings.Contains(body, `/devices/1/tags`) {
		t.Error("detail page should no longer contain inline tag forms")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestDetailPageHasEditButtonNoInlineForm -v`
Expected: FAIL — detail page still has the inline edit form / tag forms.

- [ ] **Step 3: Drop `AllTags` from the `DeviceDetail` struct**

In `internal/web/views/devices.templ`, remove the `AllTags  []store.Tag` line from the `DeviceDetail` struct (currently line ~33).

- [ ] **Step 4: Rework `DevicePage` — header icon + Edit button, read-only tags, remove inline forms**

In `internal/web/views/devices.templ`, change the `<h1>` at the top of `DevicePage` (currently lines ~267-277) to show the icon and an Edit button:

```
		<h1>
			<span class="ic">{ DeviceIcon(d.Device.Icon, d.Device.Kind) }</span>
			{ d.Device.Name }
			<span class="badge">{ d.Device.Kind }</span>
			if d.Device.Kind == "vm" || d.Device.Kind == "lxc" {
				for _, f := range d.Fields {
					if f.Key == "proxmox_status" {
						<span class="badge">{ f.Value }</span>
					}
				}
			}
			<button type="button" class="primary" hx-get={ fmt.Sprintf("/devices/%d/edit", d.Device.ID) } hx-target="#modal">Edit</button>
		</h1>
```

Replace the **Tags** section (currently lines ~353-372, the `<h2>Tags</h2>` block with its add/remove forms) with read-only chips:

```
		<h2>Tags</h2>
		<p>
			for _, t := range d.Tags {
				<span class="tag">{ t.Name }</span>{ " " }
			}
		</p>
```

Remove the entire **Edit** section (currently lines ~433-468: the `<h2>Edit</h2>` heading and the `auth-card` form). Leave the Delete form that follows it intact.

- [ ] **Step 5: Stop loading `allTags` in `handleDevicePage`**

In `internal/web/devices.go`, in `handleDevicePage` remove the `allTags` fetch (currently lines ~302-306):

```go
	allTags, err := s.store.ListTags()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
```

and remove `AllTags: allTags,` from the `views.DeviceDetail{...}` literal (currently line ~337).

- [ ] **Step 6: Remove the tag handlers and routes**

In `internal/web/devices.go`, delete `handleTagAdd` (lines ~367-382), `findOrCreateTag` (lines ~384-395), and `handleTagRemove` (lines ~397-405). (The store now owns find-or-create via `store.findOrCreateTag` from Task 1.)

In `internal/web/server.go`, delete these two route registrations (lines ~63-64):

```go
	s.mux.HandleFunc("POST /devices/{id}/tags", s.requireAdmin(s.handleTagAdd))
	s.mux.HandleFunc("POST /devices/{id}/tags/{tagID}/delete", s.requireAdmin(s.handleTagRemove))
```

- [ ] **Step 7: Regenerate templ, build, run tests**

Run:
```bash
/home/ben/go/bin/templ generate
CGO_ENABLED=0 go build ./...
go test ./internal/web/... -run 'TestDetailPageHasEditButtonNoInlineForm|TestCreateAndShowDevice' -v
```
Expected: builds clean (no references to removed handlers), tests PASS.

- [ ] **Step 8: Full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/server.go internal/web/devices_test.go
git commit -m "$(printf 'feat: device detail Edit button + read-only tags; drop inline forms and tag routes\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 7: Visual smoke (both themes)

**Files:** none (verification only).

- [ ] **Step 1: Build and run the server on a scratch DB**

```bash
CGO_ENABLED=0 go build -o /tmp/netis-c ./cmd/netis
NETIS_DB=/tmp/netis-c.db /tmp/netis-c &
```
(Stop it at the end with `kill %1`.)

- [ ] **Step 2: Headless-browser check (playwright MCP)**

Log in / complete onboarding as needed, create a device, then:
- Open `/devices`, click **New device** → confirm the modal opens over the page (scrim + centered `.dialog`), the icon palette renders, picking a swatch highlights it, changing **Kind** to `phone` flips the selected icon to 📱 (until a swatch is clicked), and Cancel/✕/scrim/Escape all close it.
- On a device detail page click **Edit** → confirm the dialog opens prefilled (name, tags, parent selected), Save closes it and the change shows on reload.
- Capture screenshots of the open dialog in **dark** (default) and **light** themes; confirm the swatches, borders, and footer are legible in both.

- [ ] **Step 3: Record the result in the progress ledger**

No code change; note "visual smoke passed (dark+light)" in the ledger. If a visual defect appears, fix in the relevant task's files and re-run.

---

## Self-Review

**Spec coverage:**
- SetDeviceTags sync → Task 1. ✅
- DeviceIcon + IconChoices + list/grid render swap → Task 2. ✅
- `#modal` + dialog.js (close/icon-picker/kind-default) → Task 3. ✅
- DeviceDialog fragment + `GET /devices/new` (repurposed) + `GET /devices/{id}/edit` + parent select + tags field + optional-first-interface (create only) + CSS → Task 4. ✅
- handleDeviceCreate/Update extended with icon+parent+tags + parseTags → Task 5. ✅
- Detail page Edit button, read-only tags, remove inline edit/tag forms, drop AllTags, remove tag routes/handlers → Task 6. ✅
- Visual smoke both themes → Task 7. ✅
- Out-of-scope items (deep cycle prevention, tag colors, dialog interface editing, tag autocomplete) → not implemented, matches spec. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `SetDeviceTags(int64, []string) error` (Task 1) is called identically in Task 5. `DeviceDialog(store.Device, []store.Tag, []store.Subnet, []store.DeviceRow, bool)` (Task 4) is called with those exact argument types by `handleDeviceForm`/`handleDeviceEditForm`. `DeviceIcon(icon, kind string)` (Task 2) used in Tasks 2/4/6. `parseTags(string) []string` (Task 5) used in create+update. `store.findOrCreateTag` (Task 1) replaces the removed `web.findOrCreateTag` (Task 6) — no remaining caller of the web version.

Note on ordering: Task 6 removes `handleTagAdd`/`handleTagRemove`/`findOrCreateTag` from `devices.go`; nothing added in Tasks 4-5 depends on them (Task 5 uses `store.SetDeviceTags`). The `DeviceForm` templ removed in Task 4 has no remaining caller after `handleDeviceForm` is repurposed in the same task.
