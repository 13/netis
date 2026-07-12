# Netis Device-List Parent/Child + Sortable Subnet List (Sub-project F3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group child devices under their parent (indented) in the Devices list, and make the subnet page's device list the same sortable table as `/devices`.

**Architecture:** Extract the Devices-page sortable `<table>` into a reusable `deviceTable` templ whose sort links take a `base` path, add a `GroupByParent` reordering helper and a per-row `depth` for indentation, then reuse `deviceTable` on the subnet page (flat) and wire subnet sorting through a shared `parseDeviceSort` helper. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `$(go env GOPATH)/bin/templ`), `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change (`ParentDeviceID` already on `Device`/`DeviceRow`).
- The web package must NOT import proxmox/pihole/wireguard.
- Regenerate templ after editing any `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` in the same commit as its source.
- Default-dark theme; style via existing tokens; no external assets.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` before finishing.

---

### Task 1: Shared `deviceTable`, `base`-param sort links, parent/child grouping

**Files:**
- Modify: `internal/web/views/devices.templ`
- Modify: `internal/web/static/app.css`
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Produces: `templ deviceTable(rows []store.DeviceRow, q, sortKey, dir, base string, grouped bool)`; `func GroupByParent(rows []store.DeviceRow) []DeviceGroupRow`; `type DeviceGroupRow struct { Row store.DeviceRow; Depth int }`; `templ deviceRow(row store.DeviceRow, depth int)` (depth added); `sortHeader(label, col, curSort, curDir, q, base string)` and `sortURL(base, q, col, curSort, curDir string)` (base added).

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceListParentChildGrouping(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	pid, _ := st.CreateDevice(store.Device{Name: "aaa-parent", Kind: "switch", Source: "manual"})
	st.CreateDevice(store.Device{Name: "mmm-mid", Kind: "computer", Source: "manual"})
	st.CreateDevice(store.Device{Name: "zzz-child", Kind: "computer", Source: "manual", ParentDeviceID: &pid})

	body := authedGet(t, srv, st, "/devices?sort=name&dir=asc").Body.String()
	iParent := strings.Index(body, "aaa-parent")
	iMid := strings.Index(body, "mmm-mid")
	iChild := strings.Index(body, "zzz-child")
	if iParent < 0 || iMid < 0 || iChild < 0 {
		t.Fatalf("rows missing: parent=%d mid=%d child=%d", iParent, iMid, iChild)
	}
	// Grouping pulls the child up under its parent, ahead of the later root.
	if !(iParent < iChild && iChild < iMid) {
		t.Fatalf("expected parent<child<mid, got parent=%d child=%d mid=%d", iParent, iChild, iMid)
	}
	if !strings.Contains(body, "tree-branch") {
		t.Fatalf("child row missing tree-branch marker")
	}
}

func TestDeviceListOrphanChildIsRoot(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	missing := int64(9999)
	st.CreateDevice(store.Device{Name: "orphan", Kind: "computer", Source: "manual", ParentDeviceID: &missing})
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "orphan") {
		t.Fatalf("orphan (parent absent) should still render as a root")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run 'TestDeviceListParentChildGrouping|TestDeviceListOrphanChildIsRoot' -v`
Expected: FAIL — grouping produces flat name order (`iMid < iChild`), no `tree-branch`.

- [ ] **Step 3: Add `DeviceGroupRow` + `GroupByParent`**

In `internal/web/views/devices.templ`, add to the Go section (near `lowestIPStr`):

```go
// DeviceGroupRow is a device row plus its nesting depth for the tree list.
type DeviceGroupRow struct {
	Row   store.DeviceRow
	Depth int
}

// GroupByParent reorders rows so each child immediately follows its parent,
// indented one level deeper, while preserving the incoming sort within each
// sibling set. A child whose parent is absent from rows is treated as a root
// (never dropped); a cyclic parent_device_id is broken by the visited guard.
func GroupByParent(rows []store.DeviceRow) []DeviceGroupRow {
	present := make(map[int64]bool, len(rows))
	for _, r := range rows {
		present[r.ID] = true
	}
	byParent := make(map[int64][]store.DeviceRow)
	var roots []store.DeviceRow
	for _, r := range rows {
		if r.ParentDeviceID != nil && present[*r.ParentDeviceID] {
			byParent[*r.ParentDeviceID] = append(byParent[*r.ParentDeviceID], r)
		} else {
			roots = append(roots, r)
		}
	}
	out := make([]DeviceGroupRow, 0, len(rows))
	visited := make(map[int64]bool, len(rows))
	var walk func(r store.DeviceRow, depth int)
	walk = func(r store.DeviceRow, depth int) {
		if visited[r.ID] {
			return
		}
		visited[r.ID] = true
		out = append(out, DeviceGroupRow{Row: r, Depth: depth})
		for _, c := range byParent[r.ID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	// Any rows unreached (e.g. a cycle among non-roots) are appended flat.
	for _, r := range rows {
		if !visited[r.ID] {
			out = append(out, DeviceGroupRow{Row: r, Depth: 0})
		}
	}
	return out
}
```

- [ ] **Step 4: Add `depth` to `deviceRow`**

In `internal/web/views/devices.templ`, change the `deviceRow` signature and its first `<td>` (the `.dev` name cell). Change:

```go
templ deviceRow(row store.DeviceRow) {
	<tr>
		<td>
			<div class="dev">
				<span class="ic">{ DeviceIcon(row.Icon, row.Kind) }</span>
```

to:

```go
templ deviceRow(row store.DeviceRow, depth int) {
	<tr>
		<td>
			<div class="dev">
				for i := 0; i < depth; i++ {
					<span class="tree-indent"></span>
				}
				if depth > 0 {
					<span class="tree-branch">↳</span>
				}
				<span class="ic">{ DeviceIcon(row.Icon, row.Kind) }</span>
```

(The rest of `deviceRow` is unchanged.)

- [ ] **Step 5: Add `base` to `sortHeader`/`sortURL` and the `deviceTable` component**

In `internal/web/views/devices.templ`, replace `sortHeader` with:

```go
templ sortHeader(label, col, curSort, curDir, q, base string) {
	<th>
		<a href={ templ.URL(sortURL(base, q, col, curSort, curDir)) }>
			{ label }
			if curSort == col {
				if curDir == "asc" {
					{ " ▲" }
				} else {
					{ " ▼" }
				}
			}
		</a>
	</th>
}
```

and replace `sortURL` with:

```go
// sortURL builds a link on `base` that preserves the filter (URL-encoded) and
// toggles the sort direction for the clicked column.
func sortURL(base, q, col, curSort, curDir string) string {
	return base + "?q=" + url.QueryEscape(q) + "&sort=" + col + "&dir=" + nextDir(col, curSort, curDir)
}
```

Then add a new `deviceTable` templ (place it just before `DeviceList`):

```go
// deviceTable renders the sortable devices table. `base` is the page hosting
// the table (its sort links point there); when `grouped`, child rows are nested
// under their parent.
templ deviceTable(rows []store.DeviceRow, q, sortKey, dir, base string, grouped bool) {
	<table>
		<thead>
			<tr>
				@sortHeader("Device", "name", sortKey, dir, q, base)
				@sortHeader("IP", "ip", sortKey, dir, q, base)
				<th>Lease</th>
				@sortHeader("Status", "status", sortKey, dir, q, base)
				@sortHeader("Kind", "kind", sortKey, dir, q, base)
				<th>Function</th>
				<th>Tags</th>
				@sortHeader("Last seen", "seen", sortKey, dir, q, base)
				<th></th>
			</tr>
		</thead>
		<tbody>
			if grouped {
				for _, g := range GroupByParent(rows) {
					@deviceRow(g.Row, g.Depth)
				}
			} else {
				for _, row := range rows {
					@deviceRow(row, 0)
				}
			}
		</tbody>
	</table>
}
```

- [ ] **Step 6: Point `DeviceList` at `deviceTable`**

In `internal/web/views/devices.templ`, replace the `#dev-list` block in `DeviceList`. Change:

```go
			<div id="dev-list">
				<table>
					<thead>
						<tr>
							@sortHeader("Device", "name", sortKey, dir, q)
							@sortHeader("IP", "ip", sortKey, dir, q)
							<th>Lease</th>
							@sortHeader("Status", "status", sortKey, dir, q)
							@sortHeader("Kind", "kind", sortKey, dir, q)
							<th>Function</th>
							<th>Tags</th>
							@sortHeader("Last seen", "seen", sortKey, dir, q)
							<th></th>
						</tr>
					</thead>
					<tbody>
						for _, row := range rows {
							@deviceRow(row)
						}
					</tbody>
				</table>
			</div>
```

to:

```go
			<div id="dev-list">
				@deviceTable(rows, q, sortKey, dir, "/devices", true)
			</div>
```

- [ ] **Step 7: Add tree CSS**

In `internal/web/static/app.css`, in the device-list area (near the `.dev` rules), add:

```css
.tree-indent { display:inline-block; width:16px; flex:none; }
.tree-branch { color:var(--muted); flex:none; margin-right:-3px; font-size:13px; }
```

- [ ] **Step 8: Regenerate templ, run tests + build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./internal/web/ -run 'TestDeviceList' -v
```
Expected: builds clean; grouping + orphan tests PASS; pre-existing `TestDeviceList*` still pass.

- [ ] **Step 9: Commit**

```bash
go vet ./...
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/static/app.css internal/web/devices_test.go
git commit -m "$(printf 'feat: group child devices under their parent in the devices list\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Sortable subnet device list

**Files:**
- Modify: `internal/web/devices.go` (`parseDeviceSort`; `handleDeviceList` uses it)
- Modify: `internal/web/views/grid.templ` (`GridPage` sort params; use `deviceTable`)
- Modify: `internal/web/grid.go` (`handleSubnetPage` sorts + passes params)
- Test: `internal/web/grid_test.go`

**Interfaces:**
- Produces: `func parseDeviceSort(r *http.Request) (sortKey, dir string)`; `GridPage(username string, sn store.Subnet, cells []GridCell, devices []store.DeviceRow, sortKey, dir string)`.
- Consumes: `deviceTable` (Task 1); existing `sortDeviceRows`.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/grid_test.go`:

```go
func TestSubnetDeviceListSortable(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "A", Kind: "lan", ScanIntervalSec: 120})
	for _, x := range []struct {
		name, ip string
	}{{"beta", "10.0.0.9"}, {"alpha", "10.0.0.3"}} {
		d, _ := st.CreateDevice(store.Device{Name: x.name, Kind: "server", Source: "manual"})
		f, _ := st.AddIface(d, nil, nil)
		st.AssignIP(f, snID, x.ip, "static")
	}
	base := "/subnets/" + strconv.FormatInt(snID, 10)

	// The table is the sortable component: sort-header links target this subnet.
	body := authedGet(t, srv, st, base).Body.String()
	if !strings.Contains(body, `href="`+base+`?`) {
		t.Fatalf("subnet device list is not sortable (no %s sort links): %q", base, body)
	}

	// Sorting by name orders alpha before beta.
	sorted := authedGet(t, srv, st, base+"?sort=name&dir=asc").Body.String()
	ia, ib := strings.Index(sorted, "alpha"), strings.Index(sorted, "beta")
	if ia < 0 || ib < 0 || ia > ib {
		t.Fatalf("name sort failed: alpha=%d beta=%d", ia, ib)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run TestSubnetDeviceListSortable -v`
Expected: FAIL — the static table has no `/subnets/{id}?` sort links.

- [ ] **Step 3: Extract `parseDeviceSort`**

In `internal/web/devices.go`, add:

```go
// parseDeviceSort reads and normalizes the ?sort=&dir= query used by the
// devices table (whitelist ip/name/status/kind/seen, default ip; dir asc unless
// desc). Shared by the devices list and the subnet page.
func parseDeviceSort(r *http.Request) (sortKey, dir string) {
	sortKey = r.URL.Query().Get("sort")
	switch sortKey {
	case "ip", "name", "status", "kind", "seen":
	default:
		sortKey = "ip"
	}
	dir = r.URL.Query().Get("dir")
	if dir != "desc" {
		dir = "asc"
	}
	return sortKey, dir
}
```

Then in `handleDeviceList`, replace the inline sort-parsing block:

```go
	sortKey := r.URL.Query().Get("sort")
	switch sortKey {
	case "ip", "name", "status", "kind", "seen":
	default:
		sortKey = "ip"
	}
	dir := r.URL.Query().Get("dir")
	if dir != "desc" {
		dir = "asc"
	}
	sortDeviceRows(rows, sortKey, dir)
```

with:

```go
	sortKey, dir := parseDeviceSort(r)
	sortDeviceRows(rows, sortKey, dir)
```

- [ ] **Step 4: `GridPage` — sort params + `deviceTable`**

In `internal/web/views/grid.templ`, change the signature:

```go
templ GridPage(username string, sn store.Subnet, cells []GridCell, devices []store.DeviceRow) {
```

to:

```go
templ GridPage(username string, sn store.Subnet, cells []GridCell, devices []store.DeviceRow, sortKey, dir string) {
```

Then replace the device-list table block. Change:

```go
		if len(devices) == 0 {
			<p class="muted">No devices with an IP in this subnet yet.</p>
		} else {
			<table>
				<thead>
					<tr><th>Device</th><th>IP</th><th>Lease</th><th>Status</th><th>Kind</th><th>Function</th><th>Tags</th><th>Last seen</th><th></th></tr>
				</thead>
				<tbody>
					for _, d := range devices {
						@deviceRow(d)
					}
				</tbody>
			</table>
		}
```

to:

```go
		if len(devices) == 0 {
			<p class="muted">No devices with an IP in this subnet yet.</p>
		} else {
			@deviceTable(devices, "", sortKey, dir, fmt.Sprintf("/subnets/%d", sn.ID), false)
		}
```

- [ ] **Step 5: `handleSubnetPage` — sort + pass params**

In `internal/web/grid.go`, in `handleSubnetPage`, after `devices, err := s.devicesInSubnet(sn.ID)` (and its error check), add sorting and update the render call. Change:

```go
	devices, err := s.devicesInSubnet(sn.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u, _ := userFrom(r)
	views.GridPage(u.Username, sn, cells, devices).Render(r.Context(), w)
```

to:

```go
	devices, err := s.devicesInSubnet(sn.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	sortKey, dir := parseDeviceSort(r)
	sortDeviceRows(devices, sortKey, dir)
	u, _ := userFrom(r)
	views.GridPage(u.Username, sn, cells, devices, sortKey, dir).Render(r.Context(), w)
```

- [ ] **Step 6: Regenerate templ, run tests + full build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean; `TestSubnetDeviceListSortable` PASSes; the E2 `TestSubnetPageDevicesList` and all other packages stay green.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/web/devices.go internal/web/grid.go && go vet ./...
git add internal/web/devices.go internal/web/grid.go internal/web/views/grid.templ internal/web/views/grid_templ.go internal/web/grid_test.go
git commit -m "$(printf 'feat: sortable device list on the subnet page\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `base`-param sort links + `deviceTable` extraction → Task 1. ✅
- `GroupByParent` + `deviceRow` depth + `DeviceList` grouped → Task 1. ✅
- `parseDeviceSort` shared; subnet list uses `deviceTable` (flat) + sorted handler → Task 2. ✅
- Tree CSS (`.tree-indent`, `.tree-branch`) → Task 1. ✅
- Tests: grouping order + marker, orphan-as-root, subnet sortable links + name sort → Tasks 1-2. ✅
- Out of scope (tile view, collapsible nodes, reparent, schema) → untouched. ✅

**Placeholder scan:** none — every step shows complete code.

**Type consistency:** `deviceTable(rows, q, sortKey, dir, base string, grouped bool)` is called from `DeviceList` (`"/devices", true`) and `GridPage` (`fmt.Sprintf("/subnets/%d", sn.ID), false`). `deviceRow(row, depth int)` is called only from `deviceTable` (both branches). `sortHeader(..., q, base string)` / `sortURL(base, q, col, curSort, curDir string)` updated at their sole call sites inside `deviceTable`. `GroupByParent([]store.DeviceRow) []DeviceGroupRow` uses `DeviceRow.ParentDeviceID` (`*int64`, from embedded `Device`) and `.ID`. `GridPage(..., sortKey, dir string)` matches the `handleSubnetPage` call. `parseDeviceSort(r) (string,string)` feeds both `sortDeviceRows` calls. `fmt` and `url` are already imported in the respective templ/Go files (grid.templ uses `fmt`; devices.templ uses `url`).

**Ordering note:** Task 2 depends on `deviceTable` and `parseDeviceSort`/depth from Task 1 — execute in order. Both `grid.templ` and `devices.templ` must be regenerated after their edits; Task 2's `templ generate` covers `grid_templ.go`.
