# Netis Subnet Page Devices List + CRUD (Sub-project E2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On the subnet page, show a "Devices in this subnet (N)" list (devices with an IP assigned here) with the same CRUD as the devices page, plus a note clarifying the grid's scope.

**Architecture:** Extract the devices-list table row into a shared `deviceRow` templ; the subnet page renders a scoped table of those rows from a `devicesInSubnet` filter (reusing `ListDevices` + `ListSubnetIfaceIPs`) and a New-device button that pre-selects the subnet in the create dialog. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change (reuses `ListDevices`/`ListSubnetIfaceIPs`).
- The subnet device list shows only devices with an IP assigned in that subnet; CRUD affordances match `/devices` (rows link to `/devices/{id}`; per-row Approve; New-device dialog).
- Regenerate templ after editing `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` with source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing — do not stage them.

---

### Task 1: Extract `deviceRow`; dialog subnet pre-select

**Files:**
- Modify: `internal/web/views/devices.templ` (extract `deviceRow`; `DeviceList` uses it; `DeviceDialog` gains `preselectSubnet`)
- Modify: `internal/web/devices.go` (`handleDeviceForm` reads `?subnet=`; both dialog callers pass the new arg)
- Test: `internal/web/devices_test.go` (add `TestDeviceRowRegression`, `TestNewDeviceSubnetPreselect`)

**Interfaces:**
- Produces: `templ deviceRow(row store.DeviceRow)`; `DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool, preselectSubnet int64)`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceRowRegression(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	d, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	f, _ := st.AddIface(d, nil, nil)
	st.AssignIP(f, snID, "10.0.0.5", "static")
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "nas") || !strings.Contains(body, "chip static") {
		t.Fatal("devices list should still render device rows after deviceRow extraction")
	}
}

func TestNewDeviceSubnetPreselect(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	body := authedGet(t, srv, st, "/devices/new?subnet=1").Body.String()
	if !strings.Contains(body, `value="1" selected`) {
		t.Fatalf("new device dialog should preselect subnet 1: %s", body)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'TestDeviceRowRegression|TestNewDeviceSubnetPreselect' -v`
Expected: `TestNewDeviceSubnetPreselect` FAILs (no `selected` on the subnet option); `TestDeviceRowRegression` passes already (guards the refactor stays green).

- [ ] **Step 3: Extract the `deviceRow` templ**

In `internal/web/views/devices.templ`, add this templ (place it just before `templ DeviceList(...)`):

```
templ deviceRow(row store.DeviceRow) {
	<tr>
		<td>
			<div class="dev">
				<span class="ic">{ DeviceIcon(row.Icon, row.Kind) }</span>
				<div>
					<div>
						<a href={ templ.URL(fmt.Sprintf("/devices/%d", row.ID)) }>{ row.Name }</a>
						if row.Reviewed {
							<span class="muted" title="reviewed">✓</span>
						} else {
							<span class="pill reserved"><span class="d"></span>new</span>
						}
					</div>
					for i, mac := range row.MACs {
						if i == 0 {
							<div class="mono muted" style="font-size:11.5px">{ mac }</div>
						}
					}
					if joinNonEmpty(" · ", row.Vendor, row.Model) != "" {
						<div class="muted" style="font-size:11.5px">{ joinNonEmpty(" · ", row.Vendor, row.Model) }</div>
					}
				</div>
			</div>
		</td>
		<td class="mono">
			for _, ip := range row.IPs {
				<span>{ ip.IP }</span>{ " " }
			}
		</td>
		<td>
			for _, ip := range row.IPs {
				if ip.Kind == "static" {
					<span class="chip static">static</span>{ " " }
				} else {
					<span class="chip">dhcp</span>{ " " }
				}
			}
		</td>
		<td>
			if row.Online {
				<span class="pill online"><span class="d"></span>online</span>
			} else {
				<span class="pill offline"><span class="d"></span>offline</span>
			}
		</td>
		<td>{ row.Kind }</td>
		<td>{ row.Function }</td>
		<td>
			for _, tn := range row.TagNames {
				<span class="tag">{ tn }</span>{ " " }
			}
		</td>
		<td class="mono muted">
			if row.LastSeen != nil {
				{ *row.LastSeen }
			}
		</td>
		<td>
			if !row.Reviewed {
				<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/approve", row.ID)) } class="inline">
					<button type="submit">Approve</button>
				</form>
			}
		</td>
	</tr>
}
```

Then in `DeviceList`, replace the `<tbody>` loop body — the entire `<tr>…</tr>`
currently rendered per row — with a call:

```
					<tbody>
						for _, row := range rows {
							@deviceRow(row)
						}
					</tbody>
```

- [ ] **Step 4: Add `preselectSubnet` to `DeviceDialog`**

In `internal/web/views/devices.templ`, change the `DeviceDialog` signature to add
a trailing `preselectSubnet int64`:

```
templ DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool, preselectSubnet int64) {
```

and change the first-interface subnet `<option>` loop to mark the pre-selected
subnet:

```
									<select name="subnet_id">
										<option value="">—</option>
										for _, sn := range subnets {
											if sn.ID == preselectSubnet {
												<option value={ fmt.Sprint(sn.ID) } selected>{ sn.Name } { sn.CIDR }</option>
											} else {
												<option value={ fmt.Sprint(sn.ID) }>{ sn.Name } { sn.CIDR }</option>
											}
										}
									</select>
```

- [ ] **Step 5: Update the two dialog callers in `devices.go`**

In `internal/web/devices.go`, replace `handleDeviceForm`'s body render + add the
`?subnet=` read:

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
	var subnetID int64
	if v := r.URL.Query().Get("subnet"); v != "" {
		subnetID, _ = strconv.ParseInt(v, 10, 64)
	}
	views.DeviceDialog(store.Device{Kind: "computer"}, nil, subnets, all, false, subnetID).Render(r.Context(), w)
}
```

In `handleDeviceEditForm`, change the render call to pass `0`:

```go
	views.DeviceDialog(d, tags, subnets, all, true, 0).Render(r.Context(), w)
```

(`strconv` is already imported in `devices.go`.)

- [ ] **Step 6: Regenerate templ, run the tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestDeviceRowRegression|TestNewDeviceSubnetPreselect|TestDeviceListDefaultSortIPNumeric' -v
```
Expected: PASS (both new tests + an existing devices-list test, confirming the row extraction preserved output).

- [ ] **Step 7: Commit**

```bash
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/devices_test.go
git commit -m "$(printf 'refactor: extract deviceRow templ; add subnet pre-select to new-device dialog\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Subnet page devices section

**Files:**
- Modify: `internal/web/grid.go` (`devicesInSubnet` helper; `handleSubnetPage` passes devices)
- Modify: `internal/web/views/grid.templ` (`GridPage` gains `devices` param + scope note + devices section)
- Test: `internal/web/grid_test.go` (add `TestSubnetPageDevicesList`)

**Interfaces:**
- Consumes: `deviceRow` (Task 1); `store.ListDevices`, `store.ListSubnetIfaceIPs`.
- Produces: `func (s *Server) devicesInSubnet(subnetID int64) ([]store.DeviceRow, error)`; `GridPage(username string, sn store.Subnet, cells []GridCell, devices []store.DeviceRow)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/grid_test.go`:

```go
func TestSubnetPageDevicesList(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snA, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "A", Kind: "lan", ScanIntervalSec: 120})
	snB, _ := st.CreateSubnet(store.Subnet{CIDR: "10.1.0.0/24", Name: "B", Kind: "lan", ScanIntervalSec: 120})

	din, _ := st.CreateDevice(store.Device{Name: "insub", Kind: "server", Source: "manual"})
	fin, _ := st.AddIface(din, nil, nil)
	st.AssignIP(fin, snA, "10.0.0.5", "static")

	dout, _ := st.CreateDevice(store.Device{Name: "outsub", Kind: "server", Source: "manual"})
	fout, _ := st.AddIface(dout, nil, nil)
	st.AssignIP(fout, snB, "10.1.0.5", "static")

	body := authedGet(t, srv, st, "/subnets/1").Body.String()
	for _, want := range []string{"Devices in this subnet", "insub", `hx-get="/devices/new?subnet=1"`, "Devices without an IP here"} {
		if !strings.Contains(body, want) {
			t.Errorf("subnet page missing %q", want)
		}
	}
	if strings.Contains(body, "outsub") {
		t.Error("subnet page should not list a device from another subnet")
	}
	_ = snA
	_ = snB
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestSubnetPageDevicesList -v`
Expected: FAIL — no devices section on the subnet page.

- [ ] **Step 3: Add the `devicesInSubnet` helper + pass devices**

In `internal/web/grid.go`, add the helper:

```go
// devicesInSubnet returns the DeviceRows that have an IP assigned in subnetID,
// in ListDevices order.
func (s *Server) devicesInSubnet(subnetID int64) ([]store.DeviceRow, error) {
	all, err := s.store.ListDevices()
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListSubnetIfaceIPs(subnetID)
	if err != nil {
		return nil, err
	}
	inSubnet := make(map[int64]bool, len(links))
	for _, l := range links {
		inSubnet[l.DeviceID] = true
	}
	var out []store.DeviceRow
	for _, d := range all {
		if inSubnet[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}
```

Change `handleSubnetPage` to build and pass the devices:

```go
func (s *Server) handleSubnetPage(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cells, err := s.gridCells(sn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	devices, err := s.devicesInSubnet(sn.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u, _ := userFrom(r)
	views.GridPage(u.Username, sn, cells, devices).Render(r.Context(), w)
}
```

- [ ] **Step 4: Add the devices section to `GridPage`**

In `internal/web/views/grid.templ`, change the `GridPage` signature to add the
`devices` param:

```
templ GridPage(username string, sn store.Subnet, cells []GridCell, devices []store.DeviceRow) {
```

After the legend `<p class="muted"> … </p>` block (the one listing online/offline/
reserved/free/conflict/edge/static/dhcp), and before the closing `}` of the
`@Layout` block, add:

```
		<p class="muted">
			The grid shows every IP assigned in this subnet. Devices without an IP here (or on other subnets) aren't listed — see the full <a href="/devices">Devices</a> page.
		</p>
		<h2>
			{ "Devices in this subnet (" + fmt.Sprint(len(devices)) + ")" }
			<button type="button" class="primary" hx-get={ fmt.Sprintf("/devices/new?subnet=%d", sn.ID) } hx-target="#modal">New device</button>
		</h2>
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

(`fmt` and `store` are already imported in `grid.templ`; `deviceRow` is in the
same `views` package from Task 1.)

- [ ] **Step 5: Regenerate templ, run the test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestSubnetPageDevicesList -v
```
Expected: PASS.

- [ ] **Step 6: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass (the existing grid/devices tests included — the `GridPage` signature change is internal to `handleSubnetPage`).

- [ ] **Step 7: Commit**

```bash
git add internal/web/grid.go internal/web/views/grid.templ internal/web/views/grid_templ.go internal/web/grid_test.go
git commit -m "$(printf 'feat: list the subnet devices with CRUD on the subnet page\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `devicesInSubnet` (ListDevices ∩ ListSubnetIfaceIPs device IDs) → Task 2. ✅
- `handleSubnetPage` passes devices → Task 2. ✅
- `deviceRow` extraction (DRY, DeviceList reuse) → Task 1. ✅
- `GridPage` devices section + scope note + New-device button + empty state → Task 2. ✅
- `DeviceDialog.preselectSubnet` + `handleDeviceForm` `?subnet=` + edit passes 0 → Task 1. ✅
- Tests: subnet devices scoping (in vs out), new-device preselect, devices-list regression → Tasks 1-2. ✅
- Out of scope (sort/filter on subnet list, moving devices, grid squares, onboarding) → not touched. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `deviceRow(row store.DeviceRow)` (Task 1) is called by `DeviceList` and `GridPage` (Task 2). `DeviceDialog(..., isEdit bool, preselectSubnet int64)` (Task 1) — both callers updated (`handleDeviceForm` with the parsed subnet, `handleDeviceEditForm` with 0). `devicesInSubnet(int64) ([]store.DeviceRow, error)` (Task 2) feeds `GridPage(username, sn, cells, devices)` — matching the new 4-arg signature in `handleSubnetPage`. `store.SubnetIfaceIP.DeviceID` is the field keyed into the `inSubnet` set. The New-device button URL `"/devices/new?subnet=%d"` matches `handleDeviceForm`'s `?subnet=` read and the test's expected `hx-get`.

**Ordering note:** Task 1 defines `deviceRow` and the new `DeviceDialog` signature; Task 2's `GridPage` calls `deviceRow` and `handleSubnetPage` builds the devices — sequential execution ensures `deviceRow` exists when Task 2 compiles. Both regenerate templ (Task 1 `devices_templ.go`, Task 2 `grid_templ.go`).
