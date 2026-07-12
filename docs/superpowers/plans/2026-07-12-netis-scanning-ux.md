# Netis Scanning UX (Sub-project C2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a subnet keep periodic auto-scan off while still being scannable on demand, and add "Scan now" controls with toast feedback to the dashboard, subnet page, device list, and settings.

**Architecture:** The scan scheduler splits its guard so manual `Trigger` scans bypass the auto-scan flag (WireGuard always skipped). Scan-now handlers return a small toast HTML fragment appended into a `#toasts` container by HTMX; a new `POST /scan` triggers every non-WireGuard subnet. No schema change; scan completion keeps flowing through the existing SSE refresh.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; single static binary, `CGO_ENABLED=0`.
- 2-state model, no migration: `subnet.scan_enabled` means *auto-scan (periodic)*; manual "Scan now" always works for any non-WireGuard subnet.
- WireGuard subnets are never scannable: their scan controls are hidden and a manual trigger is a no-op with an informational toast.
- `errors.Is(err, sql.ErrNoRows)` for not-found (house rule); the existing scan-now handler treats any `subnetFromPath` error as 404.
- Regenerate templ after editing any `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` with its source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Scheduler — manual scan bypasses auto-disable

**Files:**
- Modify: `internal/scan/scheduler.go` (`shouldRunScan`, `run` signature, `Start` per-case flag)
- Test: `internal/scan/engine_test.go` (add `TestShouldRunScan`; rewrite `TestTriggerSkipsDisabledSubnet` → `TestManualTriggerRunsDisabledSubnet`)

**Interfaces:**
- Produces: `func shouldRunScan(sn store.Subnet, manual bool) bool`; `Scheduler.run` now takes a trailing `manual bool`. `Trigger(subnetID int64)` signature unchanged.

- [ ] **Step 1: Write the failing unit test**

Add to `internal/scan/engine_test.go`:

```go
func TestShouldRunScan(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		enabled bool
		manual  bool
		want    bool
	}{
		{"auto enabled lan", "lan", true, false, true},
		{"auto disabled lan", "lan", false, false, false},
		{"manual disabled lan", "lan", false, true, true},
		{"manual wireguard", "wireguard", true, true, false},
		{"auto wireguard", "wireguard", true, false, false},
	}
	for _, c := range cases {
		sn := store.Subnet{Kind: c.kind, ScanEnabled: c.enabled}
		if got := shouldRunScan(sn, c.manual); got != c.want {
			t.Errorf("%s: shouldRunScan=%v want %v", c.name, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/scan/ -run TestShouldRunScan -v`
Expected: FAIL — `undefined: shouldRunScan`.

- [ ] **Step 3: Add `shouldRunScan` and thread `manual` through `run`/`Start`**

In `internal/scan/scheduler.go`, add the helper (above `run`):

```go
// shouldRunScan reports whether a scan should execute for sn. Manual scans
// (user-triggered via Trigger) run regardless of the auto-scan flag; periodic
// scans respect it. WireGuard subnets are never ARP-scannable and are always
// skipped.
func shouldRunScan(sn store.Subnet, manual bool) bool {
	if sn.Kind == "wireguard" {
		return false
	}
	if !manual && !sn.ScanEnabled {
		return false
	}
	return true
}
```

Change `run`'s signature and guard:

```go
func (s *Scheduler) run(ctx context.Context, sn store.Subnet, manual bool) {
	// shouldRunScan is the authoritative guard for every entry point.
	if !shouldRunScan(sn, manual) {
		return
	}
	s.mu.Lock()
	s.lastRun[sn.ID] = time.Now()
	s.mu.Unlock()
	err := s.engine.RunSubnet(ctx, sn)
	if err != nil {
		log.Printf("scan %s: %v", sn.CIDR, err)
	}
	st := store.IntegrationStatus{
		Name:    "scan",
		LastRun: time.Now().UTC().Format(time.RFC3339),
		OK:      err == nil,
		Detail:  "scanned " + sn.CIDR,
	}
	if err != nil {
		st.Detail = sn.CIDR + ": " + err.Error()
	}
	if serr := s.store.SetIntegrationStatus(st); serr != nil {
		log.Printf("scan status write: %v", serr)
	}
	s.engine.Broker.Publish("dashboard", "refresh")
}
```

In `Start`, update the two call sites: the trigger case is manual, the tick case is not:

```go
		case id := <-s.trigger:
			if sn, err := s.store.GetSubnet(id); err == nil {
				s.run(ctx, sn, true)
			}
		case <-tick.C:
			subnets, err := s.store.ListSubnets()
			if err != nil {
				continue
			}
			for _, sn := range subnets {
				if !sn.ScanEnabled || sn.Kind == "wireguard" {
					continue
				}
				s.mu.Lock()
				due := time.Since(s.lastRun[sn.ID]) >= time.Duration(sn.ScanIntervalSec)*time.Second
				s.mu.Unlock()
				if due {
					s.run(ctx, sn, false)
				}
			}
```

- [ ] **Step 4: Run the unit test to verify it passes**

Run: `go test ./internal/scan/ -run TestShouldRunScan -v`
Expected: PASS.

- [ ] **Step 5: Rewrite the now-inverted trigger test**

The existing `TestTriggerSkipsDisabledSubnet` asserts the OLD behavior (manual trigger on a disabled subnet is skipped). Under this task, a manual trigger on a disabled LAN subnet now runs. Replace the whole `TestTriggerSkipsDisabledSubnet` function in `internal/scan/engine_test.go` with:

```go
// TestManualTriggerRunsDisabledSubnet verifies the manual Trigger path bypasses
// the auto-scan flag: a disabled (non-WireGuard) subnet is still scanned on
// demand.
func TestManualTriggerRunsDisabledSubnet(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(snID)
	sn.ScanEnabled = false
	if err := st.UpdateSubnet(sn); err != nil {
		t.Fatal(err)
	}
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}

	sched := NewScheduler(e, st)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		sched.Start(ctx)
		close(done)
	}()

	sched.Trigger(snID)
	<-done // wait for Start to return; establishes happens-before for fs.calls

	if fs.calls == 0 {
		t.Fatal("sweeper was not called for a manually-triggered disabled subnet")
	}
	rows, err := st.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("manual scan of a disabled subnet created no devices")
	}
}
```

- [ ] **Step 6: Run the scan package**

Run: `go test ./internal/scan/`
Expected: PASS (the old `TestTriggerSkipsDisabledSubnet` name is gone; the new tests pass; all other scan tests still pass).

- [ ] **Step 7: Commit**

```bash
git add internal/scan/scheduler.go internal/scan/engine_test.go
git commit -m "$(printf 'feat: manual scan bypasses the auto-scan flag (WireGuard still skipped)\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Toast infrastructure + scan handlers + routes

**Files:**
- Create: `internal/web/static/toasts.js`
- Modify: `internal/web/views/layout.templ` (`#toasts` container + `toasts.js`)
- Modify: `internal/web/views/grid.templ` (add `ScanToast` templ; retarget Scan-now; hide for WireGuard)
- Modify: `internal/web/grid.go` (`handleScanNow` → toast + WireGuard guard; add `handleScanAll`)
- Modify: `internal/web/server.go` (`POST /scan` route)
- Modify: `internal/web/static/app.css` (`.toasts` / `.toast`)
- Test: `internal/web/scan_test.go` (new) with a recording `ScanTrigger`

**Interfaces:**
- Consumes: `Scheduler.Trigger` (via the `ScanTrigger` interface) from Task 1's behavior.
- Produces: templ `ScanToast(msg string)`; handlers `handleScanNow` (returns a toast fragment) and `handleScanAll` at `POST /scan`; a `#toasts` container present on every page.

- [ ] **Step 1: Write the failing test**

Create `internal/web/scan_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type recordingTrigger struct {
	mu  sync.Mutex
	ids []int64
}

func (r *recordingTrigger) Trigger(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
}

func (r *recordingTrigger) got() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.ids))
	copy(out, r.ids)
	return out
}

// testServerTrig builds a server wired to a recording ScanTrigger.
func testServerTrig(t *testing.T) (*Server, *store.Store, *recordingTrigger) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	trig := &recordingTrigger{}
	return NewServer(st, events.NewBroker(), trig), st, trig
}

func TestScanNowLanTriggersAndToasts(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: false, ScanIntervalSec: 120})
	rec := authedPost(t, srv, st, "/subnets/1/scan", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("scan now code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="toast"`) || !strings.Contains(body, "10.0.0.0/24") {
		t.Fatalf("scan-now body missing toast/CIDR: %s", body)
	}
	if ids := trig.got(); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("trigger ids=%v, want [1]", ids)
	}
}

func TestScanNowWireGuardDoesNotTrigger(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.9.0.0/24", Name: "wg", Kind: "wireguard", ScanIntervalSec: 120})
	rec := authedPost(t, srv, st, "/subnets/1/scan", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not scannable") {
		t.Fatalf("expected not-scannable toast: %s", rec.Body.String())
	}
	if ids := trig.got(); len(ids) != 0 {
		t.Fatalf("wireguard must not trigger, ids=%v", ids)
	}
}

func TestScanNowNonexistent404(t *testing.T) {
	srv, st, _ := testServerTrig(t)
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/subnets/999/scan", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d, want 404", rec.Code)
	}
}

func TestScanAllTriggersNonWireGuard(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	st.CreateSubnet(store.Subnet{CIDR: "10.9.0.0/24", Name: "wg", Kind: "wireguard", ScanIntervalSec: 120})
	rec := authedPost(t, srv, st, "/scan", url.Values{})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "all subnets") {
		t.Fatalf("scan-all code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ids := trig.got(); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("scan-all should trigger only the LAN subnet, ids=%v", ids)
	}
}

func TestScanRoutesRequireAdmin(t *testing.T) {
	srv, st, _ := testServerTrig(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	for _, path := range []string{"/subnets/1/scan", "/scan"} {
		req := httptest.NewRequest("POST", path, nil)
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer POST %s = %d, want 403", path, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestScan' -v`
Expected: FAIL — `ScanToast` undefined / `POST /scan` 404 / handler still returns 204.

- [ ] **Step 3: Add the `ScanToast` templ**

In `internal/web/views/grid.templ`, add at the end of the file:

```
templ ScanToast(msg string) {
	<div class="toast">{ msg }</div>
}
```

- [ ] **Step 4: Rewrite `handleScanNow` and add `handleScanAll`**

In `internal/web/grid.go`, replace `handleScanNow` with:

```go
func (s *Server) handleScanNow(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if sn.Kind == "wireguard" {
		views.ScanToast(sn.CIDR + " is WireGuard — not scannable").Render(r.Context(), w)
		return
	}
	if s.trigger != nil {
		s.trigger.Trigger(sn.ID)
	}
	views.ScanToast("Scanning " + sn.CIDR + "…").Render(r.Context(), w)
}

func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if s.trigger != nil {
		for _, sn := range subnets {
			if sn.Kind != "wireguard" {
				s.trigger.Trigger(sn.ID)
			}
		}
	}
	views.ScanToast("Scanning all subnets…").Render(r.Context(), w)
}
```

- [ ] **Step 5: Register the `POST /scan` route**

In `internal/web/server.go`, after the existing `POST /subnets/{id}/scan` line, add:

```go
	s.mux.HandleFunc("POST /scan", s.requireAdmin(s.handleScanAll))
```

- [ ] **Step 6: Add the `#toasts` container + toasts.js to Layout**

In `internal/web/views/layout.templ`, change the modal/script tail:

```
			<div id="modal"></div>
			<div id="toasts" class="toasts"></div>
			<script src="/static/theme.js"></script>
			<script src="/static/dialog.js"></script>
			<script src="/static/toasts.js"></script>
```

- [ ] **Step 7: Create `internal/web/static/toasts.js`**

```javascript
(function () {
	function arm(el) {
		if (el.getAttribute('data-armed')) { return; }
		el.setAttribute('data-armed', '1');
		el.addEventListener('click', function () { el.remove(); });
		setTimeout(function () { el.remove(); }, 4000);
	}
	function init() {
		var box = document.getElementById('toasts');
		if (!box) { return; }
		box.querySelectorAll('.toast').forEach(arm);
		new MutationObserver(function (muts) {
			muts.forEach(function (m) {
				m.addedNodes.forEach(function (n) {
					if (n.nodeType !== 1) { return; }
					if (n.classList && n.classList.contains('toast')) {
						arm(n);
					} else if (n.querySelectorAll) {
						n.querySelectorAll('.toast').forEach(arm);
					}
				});
			});
		}).observe(box, { childList: true, subtree: true });
	}
	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', init);
	} else {
		init();
	}
})();
```

- [ ] **Step 8: Retarget the grid page Scan-now button + hide for WireGuard**

In `internal/web/views/grid.templ`, replace the existing Scan-now button line:

```
		<button hx-post={ fmt.Sprintf("/subnets/%d/scan", sn.ID) } hx-swap="none">Scan now</button>
```

with:

```
		if sn.Kind != "wireguard" {
			<button hx-post={ fmt.Sprintf("/subnets/%d/scan", sn.ID) } hx-target="#toasts" hx-swap="beforeend">Scan now</button>
		}
```

- [ ] **Step 9: Add the toast styles**

Append to `internal/web/static/app.css`:

```css
.toasts { position:fixed; top:64px; right:18px; z-index:60; display:flex; flex-direction:column; gap:8px; max-width:min(360px, 90vw); }
.toast { background:var(--surface); border:1px solid var(--border-strong); border-radius:var(--radius-sm);
  box-shadow:var(--shadow-lg); padding:9px 13px; font-size:13px; color:var(--fg); cursor:pointer;
  animation:toastin .15s ease-out; }
@keyframes toastin { from { opacity:0; transform:translateY(-6px); } to { opacity:1; transform:none; } }
.ghost { background:var(--surface-2); border:1px solid var(--border); color:var(--muted);
  border-radius:var(--radius-sm); padding:3px 9px; font-size:12.5px; font-weight:540; cursor:pointer; }
.ghost:hover { color:var(--fg); border-color:var(--border-strong); }
```

- [ ] **Step 10: Regenerate templ, run tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestScan' -v
```
Expected: PASS (all five scan tests).

- [ ] **Step 11: Commit**

```bash
git add internal/web/static/toasts.js internal/web/static/app.css internal/web/views/layout.templ internal/web/views/layout_templ.go internal/web/views/grid.templ internal/web/views/grid_templ.go internal/web/grid.go internal/web/server.go internal/web/scan_test.go
git commit -m "$(printf 'feat: scan-now toast feedback + scan-all route + WireGuard guard\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 3: Scan buttons across dashboard, device list, settings

**Files:**
- Modify: `internal/web/views/dashboard.templ` (subnet card restructure + per-card Scan + Scan all)
- Modify: `internal/web/views/devices.templ` (toolbar Scan all)
- Modify: `internal/web/views/settings.templ` (per-row scan + `Auto-scan` relabel)
- Test: `internal/web/scan_test.go` (add render checks)

**Interfaces:**
- Consumes: the `POST /scan` and `POST /subnets/{id}/scan` routes and `#toasts` container (Task 2).

- [ ] **Step 1: Write the failing render test**

Add to `internal/web/scan_test.go`:

```go
func TestScanButtonsRendered(t *testing.T) {
	srv, st, _ := testServerTrig(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	dash := authedGet(t, srv, st, "/").Body.String()
	if !strings.Contains(dash, `hx-post="/scan"`) {
		t.Error("dashboard missing Scan all button")
	}
	if !strings.Contains(dash, `hx-post="/subnets/1/scan"`) {
		t.Error("dashboard subnet card missing Scan button")
	}

	devs := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(devs, `hx-post="/scan"`) {
		t.Error("device list toolbar missing Scan all button")
	}

	settings := authedGet(t, srv, st, "/settings").Body.String()
	if !strings.Contains(settings, `hx-post="/subnets/1/scan"`) {
		t.Error("settings row missing scan button")
	}
	if !strings.Contains(settings, "Auto-scan") {
		t.Error("settings should relabel Scan enabled -> Auto-scan")
	}
}
```

Ensure `internal/web/scan_test.go` imports `"strings"` (used here).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestScanButtonsRendered -v`
Expected: FAIL — buttons/label absent.

- [ ] **Step 3: Restructure the dashboard subnet cards + Scan all**

In `internal/web/views/dashboard.templ`, change the `<h2>Subnets</h2>` line to include a Scan-all button:

```
	<h2>Subnets <button hx-post="/scan" hx-target="#toasts" hx-swap="beforeend" class="ghost">Scan all</button></h2>
```

Replace the card block (the `<a class="card" …> … </a>` element) with a `<div>`-based card whose title is the link and whose header carries a Scan button:

```
		for _, r := range d.Rows {
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
```

- [ ] **Step 4: Add a Scan-all button to the device-list toolbar**

In `internal/web/views/devices.templ`, in the `<div class="toolbar">`, add a Scan-all button after the New device button:

```
			<button type="button" class="primary" hx-get="/devices/new" hx-target="#modal">New device</button>
			<button type="button" class="ghost" hx-post="/scan" hx-target="#toasts" hx-swap="beforeend">Scan all</button>
```

- [ ] **Step 5: Add per-row scan button + relabel in settings**

In `internal/web/views/settings.templ`, change the subnet table header cell `Scan enabled` to `Auto-scan`:

```
				<tr><th>CIDR</th><th>Name</th><th>Kind</th><th>Auto-scan</th><th>Interval (s)</th><th></th></tr>
```

In the row actions cell (currently just the delete form), add a scan button before the delete form (for non-WireGuard rows):

```
						<td>
							if sn.Kind != "wireguard" {
								<button hx-post={ fmt.Sprintf("/subnets/%d/scan", sn.ID) } hx-target="#toasts" hx-swap="beforeend">scan</button>
							}
							<form method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d/delete", sn.ID)) } class="inline">
								<button type="submit">delete</button>
							</form>
						</td>
```

Change the edit form's checkbox label from `Scan enabled` to `Auto-scan (periodic)`:

```
								<label>
									<input type="checkbox" name="scan_enabled" if sn.ScanEnabled { checked }/>
									Auto-scan (periodic)
								</label>
```

- [ ] **Step 6: Regenerate templ, run test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestScanButtonsRendered -v
```
Expected: PASS.

- [ ] **Step 7: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass.

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/dashboard.templ internal/web/views/dashboard_templ.go internal/web/views/devices.templ internal/web/views/devices_templ.go internal/web/views/settings.templ internal/web/views/settings_templ.go internal/web/scan_test.go
git commit -m "$(printf 'feat: scan-now/scan-all buttons on dashboard, device list and settings\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- Manual bypass (`shouldRunScan` + `run(manual)` + per-case flag) + rewritten test → Task 1. ✅
- Toast component (`#toasts`, `toasts.js`, `ScanToast`, `.toast` CSS) → Task 2. ✅
- `handleScanNow` → toast + WireGuard guard; `handleScanAll` + `POST /scan` → Task 2. ✅
- Buttons: dashboard cards (restructured) + Scan all, grid page, device-list toolbar, settings rows; `Auto-scan` relabel → Tasks 2 (grid) + 3. ✅
- Tests: `shouldRunScan` table, rewritten trigger test, scan-handler behavior with recording trigger, admin-gating, render checks → Tasks 1-3. ✅
- Out-of-scope (progress tracking, scan history, rate-limit UI, grid v2) → not implemented, matches spec. ✅

**Placeholder scan:** none — every code step shows complete code. The Task-2 test uses `strings.Contains` directly (imported). The `.ghost` button class used by the Task-2/Task-3 buttons is defined in Task 2's `app.css` step.

**Type consistency:** `shouldRunScan(sn store.Subnet, manual bool) bool` (Task 1) and `run(ctx, sn, manual bool)` are used consistently in `Start`. `ScanToast(msg string)` (Task 2) is called by both handlers and asserted by tests. `recordingTrigger.Trigger(int64)` satisfies the existing `web.ScanTrigger` interface (`Trigger(subnetID int64)`). The routes `POST /scan` and `POST /subnets/{id}/scan` and the `#toasts` target string match between handlers (Task 2), buttons (Tasks 2-3), and tests. `.ghost` is defined in Task 2's app.css step and used by the Task 2/3 buttons.

**Ordering note:** Task 1 changes only the scan package. Task 2 adds the toast/handlers/routes the buttons in Task 3 post to. Tasks 2 and 3 each edit `.templ` files and regenerate; sequential execution avoids conflicts.
