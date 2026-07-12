# Netis Subnets Index Page (Sub-project D2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a top-nav "Subnets" page listing every subnet as an occupancy card that opens its grid v2, reusing the dashboard's occupancy computation and card markup.

**Architecture:** Extract the dashboard's subnet-row loop into a shared `subnetRows()` method and its card markup into a `subnetCard` templ; a new `GET /subnets` handler renders `SubnetsPage` from those rows. A top-nav link makes it discoverable. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change (reuses `ListSubnets`/`SubnetOccupancy`).
- `GET /subnets` is auth-gated only (read-only; viewers may view). No `requireAdmin`.
- Dashboard output stays unchanged by the extraction (`subnetRows`/`subnetCard` reproduce the current numbers and markup).
- Regenerate templ after editing `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` with source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing — do not stage them.

---

### Task 1: Subnets index page (shared row helper + card templ + page + nav + route)

**Files:**
- Modify: `internal/web/dashboard.go` (extract `subnetRows()`; refactor `assembleDashboard`; add `handleSubnetsIndex`)
- Modify: `internal/web/views/dashboard.templ` (extract `subnetCard`; add `SubnetsPage`; dashboard loop calls `@subnetCard`)
- Modify: `internal/web/views/layout.templ` (nav `Subnets` link)
- Modify: `internal/web/server.go` (`GET /subnets` route)
- Test: `internal/web/subnets_test.go` (new)

**Interfaces:**
- Consumes: `store.ListSubnets`, `store.SubnetOccupancy`, `scan.HostIPs`, `views.DashRow{Subnet,Online,Reserved,Offline,Used,Free,Hosts}`, `views.AttentionConflict{IP,SubnetID,SubnetName}`, `views.BarPct`.
- Produces: `func (s *Server) subnetRows() ([]views.DashRow, []views.AttentionConflict, error)`; `func (s *Server) handleSubnetsIndex(w,r)`; templ `subnetCard(r DashRow)` and `SubnetsPage(username string, rows []DashRow)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/subnets_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netis/internal/store"
)

func TestSubnetsIndexListsSubnets(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	body := authedGet(t, srv, st, "/subnets").Body.String()
	for _, want := range []string{"lan", "10.0.0.0/24", `href="/subnets/1"`, `href="/subnets"`} {
		if !strings.Contains(body, want) {
			t.Errorf("subnets index missing %q", want)
		}
	}
}

func TestSubnetsIndexEmptyState(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/subnets").Body.String()
	if !strings.Contains(body, "No subnets yet") || !strings.Contains(body, "/settings?tab=subnets") {
		t.Errorf("empty state missing: %s", body)
	}
}

func TestSubnetsIndexViewerOK(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("GET", "/subnets", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("viewer GET /subnets = %d, want 200", rec.Code)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run TestSubnetsIndex -v`
Expected: FAIL — `GET /subnets` 404 / `SubnetsPage` undefined.

- [ ] **Step 3: Extract `subnetRows()` and refactor `assembleDashboard`**

In `internal/web/dashboard.go`, add the helper (above `assembleDashboard`):

```go
// subnetRows builds the per-subnet occupancy rows (shared by the dashboard and
// the subnets index) plus any IP conflicts found while scanning occupancy.
func (s *Server) subnetRows() ([]views.DashRow, []views.AttentionConflict, error) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return nil, nil, err
	}
	var rows []views.DashRow
	var conflicts []views.AttentionConflict
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(sn.ID)
		if err != nil {
			return nil, nil, err
		}
		hosts, _ := scan.HostIPs(sn.CIDR)
		free := len(hosts) - len(occ)
		if free < 0 {
			free = 0
		}
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: free, Hosts: len(hosts)}
		for ip, o := range occ {
			switch {
			case o.Online:
				row.Online++
			case !o.EverSeen:
				row.Reserved++
			default:
				row.Offline++
			}
			if o.Count > 1 {
				conflicts = append(conflicts, views.AttentionConflict{IP: ip, SubnetID: sn.ID, SubnetName: sn.Name})
			}
		}
		rows = append(rows, row)
	}
	return rows, conflicts, nil
}
```

In `assembleDashboard`, replace the whole subnet block (the standalone
`subnets, err := s.store.ListSubnets()` through the `sort.Slice(data.Conflicts …)`
line — currently the `subnets`-list fetch, `data.Stats.Subnets = len(subnets)`,
the `for _, sn := range subnets { … }` loop, and the conflicts sort) with:

```go
	rows, conflicts, err := s.subnetRows()
	if err != nil {
		return data, err
	}
	data.Rows = rows
	data.Stats.Subnets = len(rows)
	data.Conflicts = conflicts
	sort.Slice(data.Conflicts, func(i, j int) bool { return data.Conflicts[i].IP < data.Conflicts[j].IP })
```

(`len(rows)` equals the subnet count — one row per subnet — so the separate
`ListSubnets` call is no longer needed in `assembleDashboard`. `sort` and `scan`
imports remain used.)

- [ ] **Step 4: Add the index handler**

In `internal/web/dashboard.go`, add:

```go
func (s *Server) handleSubnetsIndex(w http.ResponseWriter, r *http.Request) {
	rows, _, err := s.subnetRows()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u, _ := userFrom(r)
	views.SubnetsPage(u.Username, rows).Render(r.Context(), w)
}
```

- [ ] **Step 5: Extract `subnetCard` + add `SubnetsPage`; dashboard uses the card**

In `internal/web/views/dashboard.templ`, add the extracted card templ and the
new page (place them after the existing `DashboardBody` templ):

```
templ subnetCard(r DashRow) {
	<div class="card">
		<div style="display:flex;align-items:baseline;justify-content:space-between;gap:10px">
			<h3><a href={ templ.URL(fmt.Sprintf("/subnets/%d", r.Subnet.ID)) }>{ r.Subnet.Name }</a></h3>
			<span class="mono muted">{ r.Subnet.CIDR }</span>
			if r.Subnet.Kind != "wireguard" {
				<button hx-post={ fmt.Sprintf("/subnets/%d/scan", r.Subnet.ID) } hx-target="#toasts" hx-swap="beforeend" class="ghost">Scan</button>
			}
		</div>
		<div class="occ">
			<i class="on" style={ "width:" + BarPct(r.Online, r.Hosts) }></i>
			<i class="res" style={ "width:" + BarPct(r.Reserved, r.Hosts) }></i>
			<i class="off" style={ "width:" + BarPct(r.Offline, r.Hosts) }></i>
		</div>
		<div class="legend">
			<span><span class="sw" style="background:var(--ok)"></span><b>{ fmt.Sprint(r.Online) }</b> online</span>
			<span><span class="sw" style="background:var(--warn)"></span><b>{ fmt.Sprint(r.Reserved) }</b> reserved</span>
			<span><span class="sw" style="background:var(--surface-2);border:1px solid var(--border-strong)"></span><b>{ fmt.Sprint(r.Free) }</b> free</span>
		</div>
	</div>
}

templ SubnetsPage(username string, rows []DashRow) {
	@Layout("Subnets", username) {
		<h1>Subnets</h1>
		<p class="muted">Open a subnet to see its occupancy grid — free, reserved, online/offline, and static/DHCP.</p>
		if len(rows) == 0 {
			<p class="muted">No subnets yet — add one in <a href="/settings?tab=subnets">Settings</a>.</p>
		} else {
			<div class="cards">
				for _, r := range rows {
					@subnetCard(r)
				}
			</div>
		}
	}
}
```

In the same file, in `DashboardBody`, replace the inline subnet-card loop body
with the extracted card. Change:

```
		for _, r := range d.Rows {
			<div class="card">
				…the full inline card markup…
			</div>
		}
```

to:

```
		for _, r := range d.Rows {
			@subnetCard(r)
		}
```

- [ ] **Step 6: Add the nav link**

In `internal/web/views/layout.templ`, add the Subnets link after Dashboard in
`nav.top .links`:

```
					<a href="/">Dashboard</a>
					<a href="/subnets">Subnets</a>
					<a href="/devices">Devices</a>
					<a href="/events">Events</a>
					<a href="/settings">Settings</a>
```

- [ ] **Step 7: Register the route**

In `internal/web/server.go`, before the existing `GET /subnets/{id}` line, add:

```go
	s.mux.HandleFunc("GET /subnets", s.handleSubnetsIndex)
```

- [ ] **Step 8: Regenerate templ, run the tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestSubnetsIndex|TestDashboard' -v
```
Expected: PASS — the three new tests, and the existing dashboard tests (the
extraction left dashboard output unchanged).

- [ ] **Step 9: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass.

- [ ] **Step 10: Commit**

```bash
git add internal/web/dashboard.go internal/web/views/dashboard.templ internal/web/views/dashboard_templ.go internal/web/views/layout.templ internal/web/views/layout_templ.go internal/web/server.go internal/web/subnets_test.go
git commit -m "$(printf 'feat: add a Subnets index page linking to each occupancy grid\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- Shared `subnetRows()` helper reused by dashboard + index → Step 3. ✅
- Shared `subnetCard` templ reused by dashboard + index → Step 5. ✅
- `SubnetsPage` (heading + intro + `.cards` grid + empty state) → Step 5. ✅
- `handleSubnetsIndex` + `GET /subnets` route (auth-only) → Steps 4, 7. ✅
- Nav `Subnets` link after Dashboard → Step 6. ✅
- Tests (lists subnets + grid links + nav link; empty state; viewer OK; dashboard unchanged) → Steps 1, 8. ✅
- Out of scope (grid page itself, extra per-subnet actions, new store query) → not touched. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `subnetRows() ([]views.DashRow, []views.AttentionConflict, error)` (Step 3) is consumed by `assembleDashboard` (rows+conflicts) and `handleSubnetsIndex` (rows only, Step 4). `subnetCard(r DashRow)` and `SubnetsPage(username string, rows []DashRow)` (Step 5) match the `DashRow` fields (`Subnet`,`Online`,`Reserved`,`Offline`,`Free`,`Hosts`) and the handler's `views.SubnetsPage(u.Username, rows)` call. The route string `/subnets` is distinct from `/subnets/{id}` (Go 1.22 ServeMux). `BarPct`/`fmt`/`store`/`templ` are already imported in `dashboard.templ`.

**Regression note:** extracting the dashboard's card into `subnetCard` and its row loop into `subnetRows` must not change dashboard output — the card markup is copied verbatim, and `subnetRows` reproduces the exact same `DashRow`/conflict computation (only the redundant second `ListSubnets` call is dropped; `Stats.Subnets` now derives from `len(rows)`, which equals the subnet count). The existing dashboard tests guard this.
