# Netis Device-Detail Overhaul (Sub-project F2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modernize the device detail page (hero + two-column cards), move the edit form into a right-side drawer, and add a per-IP static⇄dhcp lease toggle.

**Architecture:** Three sequential tasks on one branch. (1) A reusable `LeaseToggle` templ + `POST /devices/{id}/ip/kind` handler, wired into the existing IP table. (2) Refactor the device form into a shared `deviceFormInner`, add a `DeviceDrawer` variant that reuses the dialog class hooks so `dialog.js` is untouched, and serve it from the edit route. (3) Rebuild `DevicePage` into a hero header + two-column card layout, plus its CSS. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `$(go env GOPATH)/bin/templ`), HTMX, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change (`SetIPKind`, `ListIPs`, `GetDevice`, `DeviceDetail` already exist).
- The web package must NOT import proxmox/pihole/wireguard.
- Regenerate templ after editing any `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` in the same commit as its source.
- Default-dark theme; style via existing tokens (`--surface`, `--border`, `--accent`, `--muted`, `--radius`, …); no external assets; motion respects `prefers-reduced-motion`.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing (`.gitignore` already covers these).

---

### Task 1: Per-IP lease toggle

**Files:**
- Modify: `internal/web/views/devices.templ` (add `LeaseToggle`; use it in the IP table)
- Modify: `internal/web/devices.go` (`handleDeviceIPKind`)
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Produces: `templ LeaseToggle(devID, subnetID int64, ip, kind string)`; `func (s *Server) handleDeviceIPKind(w http.ResponseWriter, r *http.Request)`.
- Consumes: existing `store.SetIPKind(subnetID int64, ip, kind string) error`; `store.IPRow{ID, IP, SubnetID, Kind}`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceIPKindToggle(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")

	// Toggle static -> dhcp.
	rec := authedPost(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/ip/kind", url.Values{
		"subnet_id": {strconv.FormatInt(snID, 10)}, "ip": {"10.0.0.1"}, "kind": {"dhcp"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle code=%d", rec.Code)
	}
	body := rec.Body.String()
	// The re-rendered control shows the new kind and offers the reverse next step.
	if !strings.Contains(body, "dhcp") || !strings.Contains(body, `"kind":"static"`) {
		t.Fatalf("fragment did not reflect flip: %q", body)
	}
	ips, _ := st.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "dhcp" {
		t.Fatalf("kind not persisted: %+v", ips)
	}

	// Toggle back dhcp -> static.
	authedPost(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/ip/kind", url.Values{
		"subnet_id": {strconv.FormatInt(snID, 10)}, "ip": {"10.0.0.1"}, "kind": {"static"},
	})
	ips, _ = st.ListIPs(ifID)
	if ips[0].Kind != "static" {
		t.Fatalf("toggle back failed: %+v", ips)
	}
}

func TestDeviceIPKindBadKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	rec := authedPost(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/ip/kind", url.Values{
		"subnet_id": {strconv.FormatInt(snID, 10)}, "ip": {"10.0.0.1"}, "kind": {"bogus"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind code=%d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run 'TestDeviceIPKind' -v`
Expected: FAIL — route not registered (404) / handler undefined.

- [ ] **Step 3: Add the `LeaseToggle` templ**

In `internal/web/views/devices.templ`, add these near `deviceRow` (top-level `func` + `templ`). `oppositeKind` is a plain helper:

```go
// oppositeKind returns the other lease kind, for the toggle's next click.
func oppositeKind(kind string) string {
	if kind == "static" {
		return "dhcp"
	}
	return "static"
}
```

```go
// LeaseToggle renders an IP's lease as a clickable chip that flips
// static<->dhcp in place (POST /devices/{devID}/ip/kind, htmx outerHTML swap).
templ LeaseToggle(devID, subnetID int64, ip, kind string) {
	<span class="lease-cell">
		<button
			type="button"
			if kind == "static" {
				class="chip static"
			} else {
				class="chip"
			}
			hx-post={ fmt.Sprintf("/devices/%d/ip/kind", devID) }
			hx-vals={ fmt.Sprintf(`{"subnet_id":"%d","ip":%q,"kind":%q}`, subnetID, ip, oppositeKind(kind)) }
			hx-target="closest .lease-cell"
			hx-swap="outerHTML"
			title="toggle lease static/dhcp"
		>{ kind }</button>
	</span>
}
```

- [ ] **Step 4: Use `LeaseToggle` in the current IP table**

In `internal/web/views/devices.templ`, inside `DevicePage`'s interface loop, replace the IP table's Kind cell. Change:

```go
					<tr>
						<td class="mono">{ ip.IP }</td>
						<td>{ ip.Kind }</td>
					</tr>
```

to:

```go
					<tr>
						<td class="mono">{ ip.IP }</td>
						<td>
							if ip.SubnetID != 0 {
								@LeaseToggle(d.Device.ID, ip.SubnetID, ip.IP, ip.Kind)
							} else {
								<span class="chip">{ ip.Kind }</span>
							}
						</td>
					</tr>
```

(`ip` here is a `store.IPRow`, which has `SubnetID`.)

- [ ] **Step 5: Add the handler**

In `internal/web/devices.go`, add:

```go
// handleDeviceIPKind flips an IP assignment's lease kind (static/dhcp) from the
// device detail page and returns the re-rendered toggle control.
func (s *Server) handleDeviceIPKind(w http.ResponseWriter, r *http.Request) {
	devID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	subnetID, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad subnet", 400)
		return
	}
	ip := r.FormValue("ip")
	kind := r.FormValue("kind")
	if kind != "static" && kind != "dhcp" {
		http.Error(w, "bad kind", 400)
		return
	}
	if err := s.store.SetIPKind(subnetID, ip, kind); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.LeaseToggle(devID, subnetID, ip, kind).Render(r.Context(), w)
}
```

- [ ] **Step 6: Register the route**

In `internal/web/server.go`, next to the other `/devices/{id}/...` POST routes (e.g. after the `/portscan` line), add:

```go
	s.mux.HandleFunc("POST /devices/{id}/ip/kind", s.requireAdmin(s.handleDeviceIPKind))
```

- [ ] **Step 7: Regenerate templ, run tests + build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./internal/web/ -run 'TestDeviceIPKind' -v
```
Expected: builds clean; both tests PASS.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/web/devices.go && go vet ./...
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/server.go internal/web/devices_test.go
git commit -m "$(printf 'feat: per-IP static/dhcp lease toggle on device detail\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Edit drawer (right-side)

**Files:**
- Modify: `internal/web/views/devices.templ` (extract `deviceFormInner`; add `DeviceDrawer`)
- Modify: `internal/web/devices.go` (`handleDeviceEditForm` renders the drawer)
- Modify: `internal/web/static/app.css` (`.drawer-scrim`, `.drawer`)
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Produces: `templ DeviceDrawer(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, preselectSubnet int64)`; unexported `templ deviceFormInner(...)` shared by `DeviceDialog` and `DeviceDrawer`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/devices_test.go`:

```go
func TestEditServesDrawer(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	body := authedGet(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/edit").Body.String()
	if !strings.Contains(body, "drawer") {
		t.Fatalf("edit form is not a drawer: %q", body)
	}
	if !strings.Contains(body, `action="/devices/`+strconv.FormatInt(devID, 10)+`"`) {
		t.Fatalf("edit form action missing: %q", body)
	}
}

func TestNewStaysDialog(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	if !strings.Contains(body, "dialog-scrim") {
		t.Fatalf("new form lost dialog shell: %q", body)
	}
	if strings.Contains(body, "drawer") {
		t.Fatalf("new form should be a centered dialog, not a drawer")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run 'TestEditServesDrawer|TestNewStaysDialog' -v`
Expected: `TestEditServesDrawer` FAILs (edit still renders `DeviceDialog`, no `drawer` class); `TestNewStaysDialog` passes already.

- [ ] **Step 3: Extract `deviceFormInner` and add `DeviceDrawer`**

In `internal/web/views/devices.templ`, replace the entire `DeviceDialog` templ (the block from `templ DeviceDialog(...) {` through its closing `}`) with the following three definitions. `deviceFormInner` is the current inner markup verbatim (the `.dh` header + the `<form>`); `DeviceDialog` and `DeviceDrawer` wrap it.

```go
// deviceFormInner is the shared header + form used by both the centered create
// dialog and the right-side edit drawer.
templ deviceFormInner(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool, preselectSubnet int64) {
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
								if sn.ID == preselectSubnet {
									<option value={ fmt.Sprint(sn.ID) } selected>{ sn.Name } { sn.CIDR }</option>
								} else {
									<option value={ fmt.Sprint(sn.ID) }>{ sn.Name } { sn.CIDR }</option>
								}
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
}

// DeviceDialog is the centered modal used for creating a device.
templ DeviceDialog(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, isEdit bool, preselectSubnet int64) {
	<div class="dialog-scrim">
		<div class="dialog">
			@deviceFormInner(d, tags, subnets, allDevices, isEdit, preselectSubnet)
		</div>
	</div>
}

// DeviceDrawer is the right-side sliding panel used for editing a device.
templ DeviceDrawer(d store.Device, tags []store.Tag, subnets []store.Subnet, allDevices []store.DeviceRow, preselectSubnet int64) {
	<div class="dialog-scrim drawer-scrim">
		<div class="dialog drawer">
			@deviceFormInner(d, tags, subnets, allDevices, true, preselectSubnet)
		</div>
	</div>
}
```

- [ ] **Step 4: Serve the drawer from the edit route**

In `internal/web/devices.go`, in `handleDeviceEditForm`, change the final render line from:

```go
	views.DeviceDialog(d, tags, subnets, all, true, 0).Render(r.Context(), w)
```

to:

```go
	views.DeviceDrawer(d, tags, subnets, all, 0).Render(r.Context(), w)
```

(`handleDeviceNew` is unchanged — it still renders `DeviceDialog`.)

- [ ] **Step 5: Add drawer CSS**

In `internal/web/static/app.css`, immediately after the `.dialog .df { … }` line (end of the dialog-shell block), add:

```css
/* ---- edit drawer (right-side; reuses .dialog internals) ---- */
.drawer-scrim { place-items:stretch end; padding:0; }
.drawer { max-width:min(460px,100%); width:min(460px,100%); max-height:100vh; height:100vh;
  border-radius:0; border-width:0 0 0 1px; box-shadow:var(--shadow-lg); animation:drawerIn .18s ease; }
@keyframes drawerIn { from { transform:translateX(24px); opacity:.6; } to { transform:none; opacity:1; } }
@media (prefers-reduced-motion: reduce) { .drawer { animation:none; } }
```

- [ ] **Step 6: Regenerate templ, run tests + build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./internal/web/ -run 'TestEditServesDrawer|TestNewStaysDialog|TestDialog' -v
```
Expected: builds clean; all listed tests PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/web/devices.go && go vet ./...
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/devices.go internal/web/static/app.css internal/web/devices_test.go
git commit -m "$(printf 'feat: edit devices in a right-side drawer\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 3: Detail-page redesign (hero + two columns)

**Files:**
- Modify: `internal/web/views/devices.templ` (`DevicePage`)
- Modify: `internal/web/static/app.css` (`.dev-hero`, `.dev-cols`, `.dev-main`, `.dev-aside`, `.lease-cell`)
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Consumes: `DeviceDetail`, `LeaseToggle` (Task 1), the design tokens.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestDeviceDetailRedesign(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual", Vendor: "TP-Link"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")

	body := authedGet(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)).Body.String()
	for _, want := range []string{"dev-hero", "dev-cols", "gw", "TP-Link", "Interfaces", "/devices/" + strconv.FormatInt(devID, 10) + "/ip/kind"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run TestDeviceDetailRedesign -v`
Expected: FAIL — `dev-hero` / `dev-cols` absent.

- [ ] **Step 3: Rebuild `DevicePage`**

In `internal/web/views/devices.templ`, replace the entire `DevicePage` templ (from `templ DevicePage(username string, d DeviceDetail) {` through its closing `}`) with:

```go
// anyOnline reports whether any interface of the device is currently online.
func anyOnline(ifds []IfaceDetail) bool {
	for _, f := range ifds {
		if f.Online {
			return true
		}
	}
	return false
}

templ DevicePage(username string, d DeviceDetail) {
	@Layout(d.Device.Name, username) {
		<div class="dev-hero card">
			<div class="dev-hero-main">
				<span class="ic ic-lg">{ DeviceIcon(d.Device.Icon, d.Device.Kind) }</span>
				<div class="dev-hero-head">
					<h1>
						{ d.Device.Name }
						<span class="badge">{ d.Device.Kind }</span>
						if d.Device.Kind == "vm" || d.Device.Kind == "lxc" {
							for _, f := range d.Fields {
								if f.Key == "proxmox_status" {
									<span class="badge">{ f.Value }</span>
								}
							}
						}
						if anyOnline(d.Ifaces) {
							<span class="pill online"><span class="d"></span>online</span>
						} else {
							<span class="pill offline"><span class="d"></span>offline</span>
						}
					</h1>
					if joinNonEmpty(" · ", d.Device.Vendor, d.Device.Model, d.Device.Function) != "" {
						<p class="muted">{ joinNonEmpty(" · ", d.Device.Vendor, d.Device.Model, d.Device.Function) }</p>
					}
					if d.Device.Notes != "" {
						<p class="dev-notes">{ d.Device.Notes }</p>
					}
				</div>
			</div>
			<div class="dev-hero-actions">
				<button type="button" class="primary" hx-get={ fmt.Sprintf("/devices/%d/edit", d.Device.ID) } hx-target="#modal">Edit</button>
				<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/wol", d.Device.ID)) } class="inline">
					<button type="submit">Wake on LAN</button>
				</form>
				<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/portscan", d.Device.ID)) } class="inline">
					<button type="submit">Scan ports</button>
				</form>
			</div>
		</div>

		<div class="dev-cols">
			<div class="dev-main">
				<div class="card">
					<h2>Interfaces</h2>
					for _, ifd := range d.Ifaces {
						<div class="iface">
							<p class="mono">
								if ifd.Iface.MAC != nil {
									{ *ifd.Iface.MAC }
								}
								if ifd.Iface.Hostname != nil {
									{ " " } { *ifd.Iface.Hostname }
								}
								if ifd.Online {
									<span class="pill online"><span class="d"></span>online</span>
								} else {
									<span class="pill offline"><span class="d"></span>offline</span>
								}
							</p>
							<p class="muted">Last seen: <span class="mono">{ ifd.LastSeen }</span> · Availability (30d): { fmt.Sprintf("%.1f%%", ifd.AvailabilityPct) }</p>
							<table>
								<tr>
									<th>IP</th>
									<th>Lease</th>
								</tr>
								for _, ip := range ifd.IPs {
									<tr>
										<td class="mono">{ ip.IP }</td>
										<td>
											if ip.SubnetID != 0 {
												@LeaseToggle(d.Device.ID, ip.SubnetID, ip.IP, ip.Kind)
											} else {
												<span class="chip">{ ip.Kind }</span>
											}
										</td>
									</tr>
								}
							</table>
							if len(ifd.Ports) > 0 {
								<table>
									<tr>
										<th>Port</th>
										<th>Proto</th>
										<th>Service</th>
										<th>Last seen</th>
									</tr>
									for _, p := range ifd.Ports {
										<tr>
											<td>{ fmt.Sprint(p.Port) }</td>
											<td>{ p.Proto }</td>
											<td>{ p.ServiceGuess }</td>
											<td class="mono">{ p.LastSeen }</td>
										</tr>
									}
								</table>
							}
						</div>
					}
				</div>
			</div>

			<div class="dev-aside">
				<div class="card">
					<h2>Tags</h2>
					<p>
						for _, t := range d.Tags {
							<span class="tag">{ t.Name }</span>{ " " }
						}
					</p>
				</div>

				<div class="card">
					<h2>Links</h2>
					<ul class="linklist">
						for _, l := range d.Links {
							<li>
								<a href={ templ.URL(l.URL) } target="_blank" rel="noopener">{ l.Label }</a>
								<form method="post" action={ templ.URL(fmt.Sprintf("/links/%d/delete", l.ID)) } class="inline">
									<input type="hidden" name="device_id" value={ fmt.Sprint(d.Device.ID) }/>
									<button type="submit">remove</button>
								</form>
							</li>
						}
					</ul>
					<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/links", d.Device.ID)) } class="rowform">
						<input type="text" name="label" placeholder="label" required/>
						<input type="text" name="url" placeholder="https://…" required/>
						<button type="submit">Add link</button>
					</form>
				</div>

				<div class="card">
					<h2>Custom fields</h2>
					<table>
						<tr>
							<th>Key</th>
							<th>Value</th>
							<th></th>
						</tr>
						for _, f := range d.Fields {
							<tr>
								<td class="mono">{ f.Key }</td>
								<td>{ f.Value }</td>
								<td>
									<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/fields/delete", d.Device.ID)) } class="inline">
										<input type="hidden" name="key" value={ f.Key }/>
										<button type="submit">remove</button>
									</form>
								</td>
							</tr>
						}
					</table>
					<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/fields", d.Device.ID)) } class="rowform">
						<input type="text" name="key" placeholder="key" required/>
						<input type="text" name="value" placeholder="value"/>
						<button type="submit">Set field</button>
					</form>
				</div>

				<div class="card">
					<h2>Parent / children</h2>
					if d.Parent != nil {
						<p>Parent: <a href={ templ.URL(fmt.Sprintf("/devices/%d", d.Parent.ID)) }>{ d.Parent.Name }</a></p>
					}
					if len(d.Children) > 0 {
						<ul>
							for _, c := range d.Children {
								<li><a href={ templ.URL(fmt.Sprintf("/devices/%d", c.ID)) }>{ c.Name }</a></li>
							}
						</ul>
					}
					if d.Parent == nil && len(d.Children) == 0 {
						<p class="muted">No parent or children.</p>
					}
				</div>

				<div class="card">
					<h2>Event history</h2>
					<table>
						for _, e := range d.Events {
							<tr>
								<td class="mono">{ e.TS }</td>
								<td><span class={ "badge", e.Type }>{ e.Type }</span></td>
								<td>{ e.Details }</td>
							</tr>
						}
					</table>
				</div>

				<div class="card">
					<h2>Danger zone</h2>
					<form method="post" action={ templ.URL(fmt.Sprintf("/devices/%d/delete", d.Device.ID)) }>
						<button type="submit">Delete device</button>
					</form>
				</div>
			</div>
		</div>
	}
}
```

- [ ] **Step 4: Add layout CSS**

In `internal/web/static/app.css`, append at the end of the file:

```css
/* ---- device detail (F2) ---- */
.dev-hero { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; flex-wrap:wrap; margin-bottom:16px; }
.dev-hero-main { display:flex; align-items:flex-start; gap:14px; min-width:0; }
.dev-hero .ic-lg { width:52px; height:52px; font-size:27px; border-radius:14px; flex:none; }
.dev-hero-head { min-width:0; }
.dev-hero-head h1 { display:flex; align-items:center; gap:9px; flex-wrap:wrap; margin:0; font-size:22px; }
.dev-hero-head p { margin:5px 0 0; }
.dev-hero-head .dev-notes { color:var(--fg); }
.dev-hero-actions { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.dev-cols { display:grid; grid-template-columns:minmax(0,1.6fr) minmax(0,1fr); gap:16px; align-items:start; }
.dev-main, .dev-aside { display:flex; flex-direction:column; gap:16px; min-width:0; }
.dev-main .iface + .iface { margin-top:14px; padding-top:14px; border-top:1px solid var(--border); }
.dev-main .iface p { margin:0 0 6px; }
.linklist { list-style:none; padding:0; margin:0 0 10px; display:flex; flex-direction:column; gap:6px; }
.linklist li { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.rowform { display:flex; gap:8px; flex-wrap:wrap; margin-top:8px; }
.rowform input { flex:1 1 120px; min-width:0; }
.lease-cell button.chip { cursor:pointer; background:var(--surface-2); border:1px solid var(--border); }
.lease-cell button.chip.static { background:var(--accent-soft); border-color:color-mix(in srgb, var(--accent) 40%, var(--border)); }
.lease-cell button.chip:hover { border-color:var(--accent); }
@media (max-width:900px) { .dev-cols { grid-template-columns:1fr; } }
```

- [ ] **Step 5: Regenerate templ, run tests + full build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean; every package passes (including the earlier F2 tests and all pre-existing `DevicePage` tests).

- [ ] **Step 6: Commit**

```bash
go vet ./...
git add internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/static/app.css internal/web/devices_test.go
git commit -m "$(printf 'feat: modern two-column device detail page with hero header\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- Per-IP lease toggle (`LeaseToggle` + `POST /devices/{id}/ip/kind` + `SetIPKind`) → Task 1. ✅
- Edit drawer (`deviceFormInner` refactor + `DeviceDrawer` reusing dialog hooks + edit route) → Task 2. ✅
- Detail redesign (hero + two-column cards, all existing forms/actions preserved) → Task 3. ✅
- `dialog.js` untouched (drawer keeps `dialog-scrim`/`dialog` classes) → Task 2. ✅
- Tests: toggle persist + flip fragment + bad kind; drawer served + new-stays-dialog; redesign guard → Tasks 1-3. ✅
- Out of scope (F3 list grouping, iface CRUD, schema) → untouched. ✅

**Placeholder scan:** none — every code step is complete markup/code.

**Type consistency:** `LeaseToggle(devID, subnetID int64, ip, kind string)` is called identically from `DevicePage` (Tasks 1 & 3) and `handleDeviceIPKind` (Task 1). `store.IPRow` exposes `SubnetID`/`IP`/`Kind` (checked). `DeviceDrawer(d, tags, subnets, allDevices, preselectSubnet)` matches the `handleDeviceEditForm` call `DeviceDrawer(d, tags, subnets, all, 0)`. `deviceFormInner(d, tags, subnets, allDevices, isEdit, preselectSubnet)` matches both wrapper call sites. `anyOnline([]IfaceDetail) bool` uses `IfaceDetail.Online` (exists). Helpers `parentLabel`, `tagNamesJoin`, `deviceKinds`, `IconChoices`, `KindIcon`, `DeviceIcon` are all pre-existing and unchanged by the extraction.

**Ordering note:** Task 3 depends on `LeaseToggle` (Task 1) and the drawer edit affordance (Task 2), so execute in order. Task 1 wires `LeaseToggle` into the *current* IP table so it is testable before the Task 3 redesign; Task 3 re-renders the same call in the new layout.
