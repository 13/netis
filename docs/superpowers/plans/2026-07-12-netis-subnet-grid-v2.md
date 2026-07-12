# Netis Subnet Occupancy Grid v2 (Sub-project C3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the subnet page as a full-range occupancy grid (network..broadcast) with lease-aware squares (fill = liveness, border = static/dhcp), and let the user flip an assigned IP static↔dhcp from a click popup.

**Architecture:** A new `scan.AllIPs` returns the full address range; `gridCells` maps each address to a state (adding `edge` for network/broadcast) and carries the lease kind. Assigned squares become buttons that open a `CellDetail` modal fragment whose Set-static/Set-DHCP buttons POST to a new handler that updates `ip_assignment.kind` and republishes the existing `grid:{id}` SSE so the grid re-renders. No schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; single static binary, `CGO_ENABLED=0`.
- No schema/migration change — `ip_assignment.kind` already has the `static`/`dhcp` CHECK.
- WireGuard/other kinds unaffected; only the subnet grid page changes.
- `errors.Is(err, sql.ErrNoRows)` for not-found (house rule); `subnetFromPath` already maps any lookup error to 404.
- Regenerate templ after editing any `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` with its source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing — do not stage them.

---

### Task 1: `scan.AllIPs` (full range) + `HostIPs` refactor

**Files:**
- Modify: `internal/scan/sweep.go` (add `AllIPs`, refactor `HostIPs`)
- Test: `internal/scan/sweep_test.go` (add `TestAllIPs`)

**Interfaces:**
- Produces: `func AllIPs(cidr string) ([]string, error)` — full range network..broadcast. `HostIPs` behavior unchanged.

- [ ] **Step 1: Write the failing test**

Add to `internal/scan/sweep_test.go`:

```go
func TestAllIPs(t *testing.T) {
	ips, err := AllIPs("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"}
	if len(ips) != len(want) {
		t.Fatalf("got %v, want %v", ips, want)
	}
	for i := range want {
		if ips[i] != want[i] {
			t.Fatalf("got %v, want %v", ips, want)
		}
	}
	if _, err := AllIPs("garbage"); err == nil {
		t.Fatal("want error for bad cidr")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/scan/ -run TestAllIPs -v`
Expected: FAIL — `undefined: AllIPs`.

- [ ] **Step 3: Add `AllIPs` and refactor `HostIPs`**

In `internal/scan/sweep.go`, replace the existing `HostIPs` function with these two:

```go
// AllIPs returns every address in cidr from the network address to the
// broadcast address inclusive.
func AllIPs(cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	var out []string
	for addr := prefix.Addr(); prefix.Contains(addr); addr = addr.Next() {
		out = append(out, addr.String())
	}
	return out, nil
}

// HostIPs returns the usable host addresses in cidr: the full range with the
// network and broadcast addresses trimmed for IPv4 subnets shorter than /31.
func HostIPs(cidr string) ([]string, error) {
	out, err := AllIPs(cidr)
	if err != nil {
		return nil, err
	}
	prefix, _ := netip.ParsePrefix(cidr) // already validated by AllIPs
	prefix = prefix.Masked()
	if prefix.Addr().Is4() && prefix.Bits() < 31 && len(out) >= 2 {
		out = out[1 : len(out)-1]
	}
	return out, nil
}
```

(`netip` is already imported in `sweep.go`.)

- [ ] **Step 4: Run both tests to verify they pass**

Run: `go test ./internal/scan/ -run 'TestAllIPs|TestHostIPs' -v`
Expected: PASS (both — `HostIPs` still returns `.1`,`.2` for `/30`).

- [ ] **Step 5: Commit**

```bash
git add internal/scan/sweep.go internal/scan/sweep_test.go
git commit -m "$(printf 'feat: add scan.AllIPs full-range helper; refactor HostIPs to reuse it\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Store — `Occupant.Kind` + `SetIPKind`

**Files:**
- Modify: `internal/store/grid.go` (`Occupant.Kind`, `SubnetOccupancy` select, `SetIPKind`)
- Test: `internal/store/grid_test.go` (add `TestSetIPKindAndOccupancyKind`)

**Interfaces:**
- Produces: `store.Occupant` gains `Kind string`; `SubnetOccupancy` populates it; `func (s *Store) SetIPKind(subnetID int64, ip, kind string) error`.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/grid_test.go`:

```go
func TestSetIPKindAndOccupancyKind(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/30", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := s.AddIface(devID, nil, nil)
	if _, err := s.AssignIP(ifID, snID, "10.0.0.1", "static"); err != nil {
		t.Fatal(err)
	}

	occ, _ := s.SubnetOccupancy(snID)
	if occ["10.0.0.1"].Kind != "static" {
		t.Fatalf("kind=%q, want static", occ["10.0.0.1"].Kind)
	}
	if err := s.SetIPKind(snID, "10.0.0.1", "dhcp"); err != nil {
		t.Fatal(err)
	}
	occ2, _ := s.SubnetOccupancy(snID)
	if occ2["10.0.0.1"].Kind != "dhcp" {
		t.Fatalf("kind after SetIPKind=%q, want dhcp", occ2["10.0.0.1"].Kind)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/ -run TestSetIPKindAndOccupancyKind -v`
Expected: FAIL — `Occupant` has no `Kind` field / `SetIPKind` undefined.

- [ ] **Step 3: Add the field, select, and method**

In `internal/store/grid.go`:

Add `Kind string` to the `Occupant` struct (after `Count int`):

```go
type Occupant struct {
	DeviceID   int64
	DeviceName string
	MAC        string
	LastSeen   string
	Online     bool
	EverSeen   bool
	Count      int
	Kind       string
}
```

Add `a.kind` to the `SubnetOccupancy` SELECT (append it as the last selected column) and scan it last:

```go
	rows, err := s.DB.Query(`SELECT a.ip, f.id, d.id, d.name,
			COALESCE(f.mac,''), COALESCE(st.last_seen,''),
			COALESCE(st.online,0), st.first_seen IS NOT NULL, a.kind
		FROM ip_assignment a
		JOIN iface f ON f.id=a.iface_id
		JOIN device d ON d.id=f.device_id
		LEFT JOIN iface_status st ON st.iface_id=f.id
		WHERE a.subnet_id=?`, subnetID)
```

and:

```go
		if err := rows.Scan(&ip, &ifaceID, &o.DeviceID, &o.DeviceName, &o.MAC,
			&o.LastSeen, &o.Online, &o.EverSeen, &o.Kind); err != nil {
			return nil, err
		}
```

Add the setter at the end of `internal/store/grid.go`:

```go
// SetIPKind updates the lease kind (static/dhcp) of an assignment identified by
// its subnet and IP. Callers validate the kind; a 0-row update (no such
// assignment) is not an error.
func (s *Store) SetIPKind(subnetID int64, ip, kind string) error {
	_, err := s.DB.Exec(`UPDATE ip_assignment SET kind=? WHERE subnet_id=? AND ip=?`,
		kind, subnetID, ip)
	return err
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/store/ -run 'TestSetIPKindAndOccupancyKind|TestSubnetOccupancy' -v`
Expected: PASS (both — the existing `TestSubnetOccupancy` still passes with the extra column).

- [ ] **Step 5: Commit**

```bash
git add internal/store/grid.go internal/store/grid_test.go
git commit -m "$(printf 'feat: carry lease kind in Occupant and add store.SetIPKind\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 3: Grid rendering — full range, edge, lease border

**Files:**
- Modify: `internal/web/grid.go` (`gridCells` → `AllIPs` + edge + kind)
- Modify: `internal/web/views/grid.templ` (`GridCell.Kind`, `GridFrag` buttons/lease/edge, legend)
- Modify: `internal/web/static/app.css` (`.sq.static`/`.dhcp`/`.edge`, cursor, denser grid)
- Test: `internal/web/grid_test.go` (update `TestGridStates`)

**Interfaces:**
- Consumes: `scan.AllIPs` (Task 1), `store.Occupant.Kind` (Task 2).
- Produces: `views.GridCell` gains `Kind string`; assigned squares render as `<button hx-get="/subnets/{id}/cell?ip=…" hx-target="#modal">` (the route is added in Task 4).

- [ ] **Step 1: Update the failing test**

The full-range change makes a `/29` render 8 squares (network + 6 hosts + broadcast) instead of 6, and adds an `edge` state. Replace the assertions at the end of `TestGridStates` in `internal/web/grid_test.go`:

```go
	body := rec.Body.String()
	for _, want := range []string{"sq conflict", "sq offline", "sq reserved", "sq free", "sq edge", "static"} {
		if !strings.Contains(body, want) {
			t.Errorf("grid missing %q", want)
		}
	}
	// /29 full range → 8 squares (network + 6 hosts + broadcast)
	if n := strings.Count(body, `class="sq`); n != 8 {
		t.Errorf("squares=%d, want 8", n)
	}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestGridStates -v`
Expected: FAIL — currently 6 squares, no `edge`/`static` classes.

- [ ] **Step 3: Add `Kind` to `GridCell` and rebuild `gridCells`**

In `internal/web/views/grid.templ`, add `Kind string` to the `GridCell` struct:

```go
type GridCell struct {
	IP       string
	State    string
	DeviceID int64
	Title    string
	Kind     string
}
```

In `internal/web/grid.go`, add `"net/netip"` to the imports, and replace `gridCells` with:

```go
func (s *Server) gridCells(sn store.Subnet) ([]views.GridCell, error) {
	occ, err := s.store.SubnetOccupancy(sn.ID)
	if err != nil {
		return nil, err
	}
	ips, err := scan.AllIPs(sn.CIDR)
	if err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(sn.CIDR)
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	hasEdges := prefix.Addr().Is4() && prefix.Bits() < 31
	cells := make([]views.GridCell, 0, len(ips))
	for i, ip := range ips {
		c := views.GridCell{IP: ip, State: "free", Title: ip}
		if hasEdges && (i == 0 || i == len(ips)-1) {
			c.State = "edge"
			if i == 0 {
				c.Title = ip + " — network address"
			} else {
				c.Title = ip + " — broadcast address"
			}
			cells = append(cells, c)
			continue
		}
		if o, ok := occ[ip]; ok {
			c.DeviceID = o.DeviceID
			c.Kind = o.Kind
			c.Title = fmt.Sprintf("%s — %s %s last seen %s", ip, o.DeviceName, o.MAC, o.LastSeen)
			switch {
			case o.Count > 1:
				c.State = "conflict"
			case !o.EverSeen:
				c.State = "reserved"
			case o.Online:
				c.State = "online"
			default:
				c.State = "offline"
			}
		}
		cells = append(cells, c)
	}
	return cells, nil
}
```

- [ ] **Step 4: Rebuild `GridFrag` (clickable buttons + lease class) and the legend**

In `internal/web/views/grid.templ`, replace the `GridFrag` templ with:

```
templ GridFrag(sn store.Subnet, cells []GridCell) {
	<div class="grid">
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
	</div>
}
```

In `GridPage`, replace the legend `<p class="muted">…</p>` with:

```
			<p class="muted">
				<span class="sq online"></span> online
				<span class="sq offline"></span> offline
				<span class="sq reserved"></span> reserved
				<span class="sq"></span> free
				<span class="sq conflict"></span> conflict
				<span class="sq edge"></span> network/broadcast
				· border: <span class="sq static"></span> static
				<span class="sq dhcp"></span> dhcp
			</p>
```

- [ ] **Step 5: Add the CSS**

In `internal/web/static/app.css`, change the `.grid` rule (currently `grid-template-columns:repeat(16,26px)`) to a denser auto-fill and add the new square styles:

```css
.grid { display:grid; grid-template-columns:repeat(auto-fill, 20px); gap:3px; }
.grid .sq { width:20px; height:20px; }
button.sq { padding:0; cursor:pointer; }
.sq.static { border-width:2px; border-style:solid; border-color:var(--accent); }
.sq.dhcp { border-width:2px; border-style:dashed; border-color:var(--muted); }
.sq.edge { cursor:default; background:var(--surface);
  background-image:repeating-linear-gradient(45deg, transparent, transparent 3px, var(--border) 3px, var(--border) 4px); }
```

(Leave the existing `.sq` base and `.sq.online/.offline/.reserved/.conflict` rules; the new `.sq.static/.dhcp` come after them so the lease border wins by source order.)

- [ ] **Step 6: Regenerate templ, run the test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestGridStates -v
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web/grid.go internal/web/views/grid.templ internal/web/views/grid_templ.go internal/web/static/app.css internal/web/grid_test.go
git commit -m "$(printf 'feat: full-range subnet grid with edge cells and static/dhcp borders\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 4: Cell popup — CellDetail fragment + handlers + routes

**Files:**
- Modify: `internal/web/views/grid.templ` (`CellDetail` + `kindButton` templs)
- Modify: `internal/web/grid.go` (`handleCellDetail`, `handleCellKind`)
- Modify: `internal/web/server.go` (two `/subnets/{id}/cell` routes)
- Test: `internal/web/grid_test.go` (add cell tests)

**Interfaces:**
- Consumes: `store.SetIPKind` + `store.Occupant.Kind` (Task 2); `s.broker.Publish`; `views.ScanToast` (existing).
- Produces: `templ CellDetail(sn store.Subnet, ip string, o store.Occupant)`; handlers at `GET`/`POST /subnets/{id}/cell`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/grid_test.go` (add imports `net/http`, `net/http/httptest`, `net/url` as needed):

```go
func TestCellDetailAndSetKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")

	det := authedGet(t, srv, st, "/subnets/1/cell?ip=10.0.0.1").Body.String()
	for _, want := range []string{"/devices/1", "Set static", "Set DHCP", `class="dialog"`} {
		if !strings.Contains(det, want) {
			t.Errorf("cell detail missing %q", want)
		}
	}

	rec := authedPost(t, srv, st, "/subnets/1/cell", url.Values{"ip": {"10.0.0.1"}, "kind": {"dhcp"}})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "10.0.0.1 → dhcp") {
		t.Fatalf("set kind code=%d body=%s", rec.Code, rec.Body.String())
	}
	occ, _ := st.SubnetOccupancy(snID)
	if occ["10.0.0.1"].Kind != "dhcp" {
		t.Fatalf("kind not updated: %+v", occ["10.0.0.1"])
	}
}

func TestSetKindBadKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	if rec := authedPost(t, srv, st, "/subnets/1/cell", url.Values{"ip": {"10.0.0.1"}, "kind": {"bogus"}}); rec.Code != 400 {
		t.Fatalf("bad kind code=%d, want 400", rec.Code)
	}
	_ = snID
}

func TestSetKindRequiresAdmin(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/subnets/1/cell", strings.NewReader("ip=10.0.0.1&kind=dhcp"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer code=%d, want 403", rec.Code)
	}
	_ = snID
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'TestCellDetailAndSetKind|TestSetKindBadKind|TestSetKindRequiresAdmin' -v`
Expected: FAIL — routes 404 / `CellDetail` undefined.

- [ ] **Step 3: Add the `CellDetail` + `kindButton` templs**

In `internal/web/views/grid.templ`, add at the end of the file:

```
templ CellDetail(sn store.Subnet, ip string, o store.Occupant) {
	<div class="dialog-scrim">
		<div class="dialog">
			<div class="dh">
				<h3>{ ip }</h3>
				<button type="button" class="theme-toggle" data-close aria-label="Close">✕</button>
			</div>
			<div class="db">
				if o.DeviceID > 0 {
					<p>
						<a href={ templ.URL(fmt.Sprintf("/devices/%d", o.DeviceID)) }>{ o.DeviceName }</a>
						if o.MAC != "" {
							{ " " }<span class="mono muted">{ o.MAC }</span>
						}
					</p>
					<p class="muted">Last seen: { o.LastSeen } · Lease: { o.Kind }</p>
				} else {
					<p class="muted">Free — no assignment.</p>
				}
			</div>
			if o.DeviceID > 0 {
				<div class="df">
					@kindButton(sn.ID, ip, "static", "Set static", o.Kind)
					@kindButton(sn.ID, ip, "dhcp", "Set DHCP", o.Kind)
				</div>
			}
		</div>
	</div>
}

templ kindButton(subnetID int64, ip, kind, label, current string) {
	if kind == current {
		<button type="button" class="primary" disabled>{ label }</button>
	} else {
		<button
			type="button"
			class="ghost"
			hx-post={ fmt.Sprintf("/subnets/%d/cell", subnetID) }
			hx-vals={ fmt.Sprintf(`{"ip":%q,"kind":%q}`, ip, kind) }
			hx-target="#toasts"
			hx-swap="beforeend"
			hx-on::after-request="document.getElementById('modal').innerHTML=''"
		>{ label }</button>
	}
}
```

- [ ] **Step 4: Add the handlers**

In `internal/web/grid.go`, add:

```go
func (s *Server) handleCellDetail(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ip := r.URL.Query().Get("ip")
	occ, err := s.store.SubnetOccupancy(sn.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.CellDetail(sn, ip, occ[ip]).Render(r.Context(), w)
}

func (s *Server) handleCellKind(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ip := r.FormValue("ip")
	kind := r.FormValue("kind")
	if kind != "static" && kind != "dhcp" {
		http.Error(w, "bad kind", 400)
		return
	}
	if err := s.store.SetIPKind(sn.ID, ip, kind); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	views.ScanToast(ip + " → " + kind).Render(r.Context(), w)
}
```

(`occ[ip]` returns a zero `store.Occupant` when the IP has no assignment, so `CellDetail` renders the read-only "Free" variant.)

- [ ] **Step 5: Register the routes**

In `internal/web/server.go`, after the `POST /subnets/{id}/scan` line, add:

```go
	s.mux.HandleFunc("GET /subnets/{id}/cell", s.handleCellDetail)
	s.mux.HandleFunc("POST /subnets/{id}/cell", s.requireAdmin(s.handleCellKind))
```

- [ ] **Step 6: Regenerate templ, run the tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestCellDetailAndSetKind|TestSetKindBadKind|TestSetKindRequiresAdmin' -v
```
Expected: PASS (all three).

- [ ] **Step 7: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass.

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/grid.templ internal/web/views/grid_templ.go internal/web/grid.go internal/web/server.go internal/web/grid_test.go
git commit -m "$(printf 'feat: cell popup to flip an IP static/dhcp from the subnet grid\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `AllIPs` + `HostIPs` refactor → Task 1. ✅
- `Occupant.Kind` + `SubnetOccupancy` select + `SetIPKind` → Task 2. ✅
- `gridCells` full range + edge + kind; `GridCell.Kind`; `GridFrag` clickable/lease/edge; legend; `.sq.static/.dhcp/.edge` CSS → Task 3. ✅
- `CellDetail` popup; `handleCellDetail`/`handleCellKind`; broker grid refresh; toast; two routes → Task 4. ✅
- Tests: AllIPs range, SetIPKind + Occupant.Kind, grid states+edge+border count, cell detail render, POST sets kind, bad kind 400, viewer 403 → Tasks 1-4. ✅
- Out of scope (reserve/free, move/renumber, virtualization, dashboard bars) → not implemented, matches spec. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `AllIPs(cidr string) ([]string, error)` (Task 1) is called by `gridCells` (Task 3). `Occupant.Kind` (Task 2) is read by `gridCells` (Task 3) and `CellDetail` (Task 4). `GridCell.Kind` (Task 3) feeds `GridFrag`'s class list. `SetIPKind(int64, string, string) error` (Task 2) is called by `handleCellKind` (Task 4). `CellDetail(sn store.Subnet, ip string, o store.Occupant)` (Task 4) is called by `handleCellDetail`. The `#modal`/`#toasts` targets and `grid:{id}` SSE topic already exist (from earlier sub-projects) and match between the buttons, handlers, and `GridPage`'s existing `sse:grid:{id}` trigger.

**Ordering note:** Task 3's `GridFrag` buttons `hx-get` the `/subnets/{id}/cell` route that Task 4 registers; Task 3's tests assert markup only (they don't exercise the route), so the sequential order is fine. Tasks 3 and 4 both edit `grid.templ`/`grid.go` and regenerate; sequential execution avoids conflicts.
