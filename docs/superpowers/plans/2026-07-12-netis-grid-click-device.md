# Netis Grid Square Click → New/Edit/Open Device (Sub-project G2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A free grid square opens a New-device dialog prefilled with its IP+subnet; an occupied square's popup gains Open (to the device page) and Edit (the F2 drawer) actions.

**Architecture:** `GridFrag` renders free squares as new-device buttons; `handleDeviceForm` + `deviceFormInner`/`DeviceDialog` thread a `preIP` prefill; `CellDetail` gains Open/Edit. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `$(go env GOPATH)/bin/templ`), HTMX, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change.
- The web package must NOT import proxmox/pihole/wireguard.
- Regenerate templ after editing any `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` in the same commit as its source.
- Reuse existing tokens/classes — no new CSS.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` before finishing.

---

### Task 1: Grid click — free → new, occupied → open/edit

**Files:**
- Modify: `internal/web/views/grid.templ`
- Modify: `internal/web/views/devices.templ`
- Modify: `internal/web/devices.go`
- Test: `internal/web/grid_test.go`, `internal/web/devices_test.go`

**Interfaces:**
- `deviceFormInner(d, tags, subnets, allDevices, isEdit bool, preselectSubnet int64, preIP string)` and `DeviceDialog(d, tags, subnets, allDevices, isEdit bool, preselectSubnet int64, preIP string)` gain a trailing `preIP`. `DeviceDrawer` passes `""`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/grid_test.go`:

```go
func TestGridFreeCellOpensNewDevice(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	dev, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	f, _ := st.AddIface(dev, nil, nil)
	st.AssignIP(f, snID, "10.0.0.1", "static")

	body := authedGet(t, srv, st, "/subnets/"+strconv.FormatInt(snID, 10)+"/grid").Body.String()
	// A free host IP is a clickable new-device button.
	if !strings.Contains(body, `hx-get="/devices/new?subnet=`+strconv.FormatInt(snID, 10)+`&ip=10.0.0.2"`) {
		t.Fatalf("free cell should open new-device dialog: %q", body)
	}
	// The network/broadcast edges are NOT new-device buttons.
	if strings.Contains(body, `ip=10.0.0.0"`) || strings.Contains(body, `ip=10.0.0.7"`) {
		t.Fatalf("edge cells must not be clickable new-device buttons")
	}
}

func TestCellDetailHasOpenAndEdit(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	dev, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	f, _ := st.AddIface(dev, nil, nil)
	st.AssignIP(f, snID, "10.0.0.1", "static")

	body := authedGet(t, srv, st, "/subnets/"+strconv.FormatInt(snID, 10)+"/cell?ip=10.0.0.1").Body.String()
	did := strconv.FormatInt(dev, 10)
	if !strings.Contains(body, `href="/devices/`+did+`"`) {
		t.Fatalf("cell popup missing Open link: %q", body)
	}
	if !strings.Contains(body, `hx-get="/devices/`+did+`/edit"`) {
		t.Fatalf("cell popup missing Edit action: %q", body)
	}
}
```

Add to `internal/web/devices_test.go`:

```go
func TestNewDevicePrefillsIP(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanIntervalSec: 120})

	body := authedGet(t, srv, st, "/devices/new?subnet=1&ip=10.0.0.5").Body.String()
	if !strings.Contains(body, `name="ip"`) || !strings.Contains(body, `value="10.0.0.5"`) {
		t.Fatalf("new dialog should prefill ip: %q", body)
	}
	// No ip param → empty IP field.
	plain := authedGet(t, srv, st, "/devices/new").Body.String()
	if !strings.Contains(plain, `name="ip"`) || !strings.Contains(plain, `value=""`) {
		t.Fatalf("plain new dialog should have empty ip field")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web/ -run 'TestGridFreeCellOpensNewDevice|TestCellDetailHasOpenAndEdit|TestNewDevicePrefillsIP' -v`
Expected: FAIL — free cells are spans, cell popup lacks Open/Edit, IP not prefilled.

- [ ] **Step 3: Free-cell button + Open/Edit in `grid.templ`**

In `internal/web/views/grid.templ`, replace the `GridFrag` loop body. Change:

```go
		for _, c := range cells {
			if c.DeviceID > 0 {
				<button
					type="button"
					class={ "sq", c.State, c.Kind }
					hx-get={ fmt.Sprintf("/subnets/%d/cell?ip=%s", sn.ID, c.IP) }
					hx-target="#modal"
					title={ c.Title }
				></button>
			} else {
				<span class={ "sq", c.State } title={ c.Title }></span>
			}
		}
```

to:

```go
		for _, c := range cells {
			if c.DeviceID > 0 {
				<button
					type="button"
					class={ "sq", c.State, c.Kind }
					hx-get={ fmt.Sprintf("/subnets/%d/cell?ip=%s", sn.ID, c.IP) }
					hx-target="#modal"
					title={ c.Title }
				></button>
			} else if c.State == "free" {
				<button
					type="button"
					class={ "sq", c.State }
					hx-get={ fmt.Sprintf("/devices/new?subnet=%d&ip=%s", sn.ID, c.IP) }
					hx-target="#modal"
					title={ c.Title + " — click to add a device" }
				></button>
			} else {
				<span class={ "sq", c.State } title={ c.Title }></span>
			}
		}
```

Then in the same file, update `CellDetail`'s occupied footer. Change:

```go
			if o.DeviceID > 0 {
				<div class="df">
					@kindButton(sn.ID, ip, "static", "Set static", o.Kind)
					@kindButton(sn.ID, ip, "dhcp", "Set DHCP", o.Kind)
				</div>
			}
```

to:

```go
			if o.DeviceID > 0 {
				<div class="df">
					<a class="btn" href={ templ.URL(fmt.Sprintf("/devices/%d", o.DeviceID)) }>Open</a>
					<button type="button" hx-get={ fmt.Sprintf("/devices/%d/edit", o.DeviceID) } hx-target="#modal">Edit</button>
					@kindButton(sn.ID, ip, "static", "Set static", o.Kind)
					@kindButton(sn.ID, ip, "dhcp", "Set DHCP", o.Kind)
				</div>
			}
```

- [ ] **Step 4: Thread `preIP` through the create dialog in `devices.templ`**

In `internal/web/views/devices.templ`:

(a) `deviceFormInner` signature — add a trailing `preIP string`:

```go
templ deviceFormInner(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool, preselectSubnet int64, preIP string) {
```

(b) Its interface IP input — add `value={ preIP }`. Change:

```go
					<label>
						IP
						<input type="text" name="ip" placeholder="10.0.0.5"/>
					</label>
```

to:

```go
					<label>
						IP
						<input type="text" name="ip" placeholder="10.0.0.5" value={ preIP }/>
					</label>
```

(c) `DeviceDialog` — add trailing `preIP` and pass it through:

```go
templ DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool, preselectSubnet int64, preIP string) {
	<div class="dialog-scrim">
		<div class="dialog">
			@deviceFormInner(d, tags, subnets, allDevices, isEdit, preselectSubnet, preIP)
		</div>
	</div>
}
```

(d) `DeviceDrawer` — pass `""` for `preIP`:

```go
			@deviceFormInner(d, tags, subnets, allDevices, true, preselectSubnet, "")
```

- [ ] **Step 5: Pass the `ip` query through `handleDeviceForm`**

In `internal/web/devices.go`, `handleDeviceForm`, change the render line:

```go
	views.DeviceDialog(store.Device{Kind: "computer"}, nil, subnets, all, false, subnetID).Render(r.Context(), w)
```

to:

```go
	preIP := r.URL.Query().Get("ip")
	views.DeviceDialog(store.Device{Kind: "computer"}, nil, subnets, all, false, subnetID, preIP).Render(r.Context(), w)
```

- [ ] **Step 6: Regenerate templ, run tests + full build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean; the three new tests pass; existing grid/device/dialog tests stay green.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/web/devices.go && go vet ./...
git add internal/web/views/grid.templ internal/web/views/grid_templ.go internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/grid_test.go internal/web/devices_test.go
git commit -m "$(printf 'feat: grid squares open a device — free adds, occupied opens/edits\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- Free square → new-device button prefilled ip+subnet; edges stay inert → Task 1. ✅
- `preIP` threaded through `deviceFormInner`/`DeviceDialog`; `handleDeviceForm` reads `ip` → Task 1. ✅
- Occupied cell popup gains Open (link) + Edit (drawer) → Task 1. ✅
- Tests: free-cell new-device link (edges excluded), cell Open/Edit, ip prefill → Task 1. ✅
- Out of scope (drag/reassign, edge/conflict creatable, settings config) → untouched. ✅

**Placeholder scan:** none — every step shows complete markup/code.

**Type consistency:** `deviceFormInner(..., preselectSubnet int64, preIP string)` and `DeviceDialog(..., preselectSubnet int64, preIP string)` gain the same trailing param; the only `DeviceDialog` call site (`handleDeviceForm`) is updated; `DeviceDrawer` passes `""`. `GridCell` fields `IP`/`State`/`DeviceID` (used in `GridFrag`) exist; `store.Occupant.DeviceID`/`Kind` (used in `CellDetail`) exist. `.btn` class is pre-existing in `app.css`. `strconv`/`strings`/`store` already imported in the test files.

**Ordering note:** single task; the `preIP` signature change and the free-cell/Open-Edit markup are interdependent (one commit), and both templ files regenerate together in Step 6.
