# Netis Integration "Run now" + Settings Alignment (Sub-project E1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an on-demand "Run now" button per integration on the settings page (runs its poller once and toasts the result) and fix the two misaligned-button problems there.

**Architecture:** A runner registry (interface in `web`, concrete map wired in `main`) lets the settings handler trigger a configured integration's `RunOnce`; the result is read back from the recorded `IntegrationStatus` and toasted. The UI adds a Run-now button per card and fixes button alignment via a `form=`-attribute restructure and a global checkbox-label CSS rule. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change.
- "Run now" uses each integration's existing `RunOnce(ctx) (Stats, error)` against the startup-loaded config; unconfigured integrations return a "not configured" toast.
- `POST /settings/integrations/{name}/run` is admin-only (`requireAdmin`); `name ∈ {proxmox,wireguard,pihole}`.
- The web package never imports proxmox/pihole/wireguard (wiring lives in `main`).
- Regenerate templ after editing `settings.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `settings_templ.go`.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing — do not stage them.

---

### Task 1: Run-now backend — runner registry, handler, route, wiring

**Files:**
- Modify: `internal/web/server.go` (`IntegrationRunner` iface, `runner` field, `NewServer` param, run route)
- Create: `internal/web/integrations_run.go` (`handleIntegrationRun`)
- Modify: `cmd/netis/main.go` (`integrationRunner` map + wire into `NewServer`)
- Modify: `internal/web/auth_test.go` (`testServer` passes nil runner), `internal/web/scan_test.go` (`testServerTrig` passes nil runner)
- Test: `internal/web/integrations_run_test.go` (new)

**Interfaces:**
- Produces: `web.IntegrationRunner interface { Run(ctx context.Context, name string) error }`; `NewServer(st, broker, trigger, runner)`; `handleIntegrationRun` at `POST /settings/integrations/{name}/run`.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/integrations_run_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type recordingRunner struct {
	mu    sync.Mutex
	names []string
}

func (r *recordingRunner) Run(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	return nil
}

func (r *recordingRunner) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.names))
	copy(out, r.names)
	return out
}

func testServerRun(t *testing.T) (*Server, *store.Store, *recordingRunner) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	run := &recordingRunner{}
	return NewServer(st, events.NewBroker(), nil, run), st, run
}

func TestIntegrationRunPihole(t *testing.T) {
	srv, st, run := testServerRun(t)
	st.SetSetting("onboarded", "1")
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "pihole", OK: true, Detail: "48 leases, 2 new"})
	rec := authedPost(t, srv, st, "/settings/integrations/pihole/run", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("run code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Pi-hole", "connected", "48 leases, 2 new"} {
		if !strings.Contains(body, want) {
			t.Errorf("run toast missing %q: %s", want, body)
		}
	}
	if got := run.got(); len(got) != 1 || got[0] != "pihole" {
		t.Fatalf("runner names=%v, want [pihole]", got)
	}
}

func TestIntegrationRunBadName(t *testing.T) {
	srv, st, _ := testServerRun(t)
	st.SetSetting("onboarded", "1")
	if rec := authedPost(t, srv, st, "/settings/integrations/bogus/run", url.Values{}); rec.Code != 400 {
		t.Fatalf("bad name code=%d, want 400", rec.Code)
	}
}

func TestIntegrationRunNilRunner(t *testing.T) {
	srv, st := testServer(t) // nil runner
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/settings/integrations/pihole/run", url.Values{})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "not available") {
		t.Fatalf("nil runner code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationRunRequiresAdmin(t *testing.T) {
	srv, st, _ := testServerRun(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/settings/integrations/pihole/run", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer code=%d, want 403", rec.Code)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run TestIntegrationRun -v`
Expected: FAIL to compile — `NewServer` takes 3 args, no `handleIntegrationRun`/route.

- [ ] **Step 3: Add the interface, field, `NewServer` param, and route**

In `internal/web/server.go`, add `"context"` to the imports, and:

```go
// IntegrationRunner triggers a single on-demand run of a named integration.
type IntegrationRunner interface {
	Run(ctx context.Context, name string) error
}
```

Add a `runner` field to `Server`:

```go
type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	broker  *events.Broker
	trigger ScanTrigger
	runner  IntegrationRunner
	limiter *rateLimiter
	detect  func() ([]netdetect.Detected, error)
}
```

Change `NewServer` to take the runner and set it:

```go
func NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger, runner IntegrationRunner) *Server {
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, runner: runner, limiter: newRateLimiter(),
		detect: netdetect.DetectSubnets,
	}
```

After the existing `POST /settings/integrations` route line, add:

```go
	s.mux.HandleFunc("POST /settings/integrations/{name}/run", s.requireAdmin(s.handleIntegrationRun))
```

- [ ] **Step 4: Add the run handler**

Create `internal/web/integrations_run.go`:

```go
package web

import (
	"context"
	"net/http"
	"time"

	"netis/internal/store"
	"netis/internal/web/views"
)

var integrationTitles = map[string]string{
	"proxmox":   "Proxmox",
	"wireguard": "WireGuard",
	"pihole":    "Pi-hole",
}

// handleIntegrationRun triggers a single on-demand run of an integration and
// toasts the result read back from the recorded status.
func (s *Server) handleIntegrationRun(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	title, ok := integrationTitles[name]
	if !ok {
		http.Error(w, "unknown integration", 400)
		return
	}
	if s.runner == nil {
		views.ScanToast("integration run not available").Render(r.Context(), w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	runErr := s.runner.Run(ctx, name)

	var st *store.IntegrationStatus
	if list, err := s.store.ListIntegrationStatus(); err == nil {
		for i := range list {
			if list[i].Name == name {
				st = &list[i]
				break
			}
		}
	}
	var msg string
	switch {
	case st == nil && runErr != nil:
		msg = title + ": not configured"
	case st == nil:
		msg = title + ": ran"
	case st.OK:
		msg = title + ": connected"
		if st.Detail != "" {
			msg += " · " + st.Detail
		}
	default:
		msg = title + ": failing"
		if st.Detail != "" {
			msg += " · " + st.Detail
		}
	}
	views.ScanToast(msg).Render(r.Context(), w)
}
```

- [ ] **Step 5: Update the two test constructors**

In `internal/web/auth_test.go`, change `testServer`'s return:

```go
	return NewServer(st, events.NewBroker(), nil, nil), st
```

In `internal/web/scan_test.go`, change `testServerTrig`'s return:

```go
	return NewServer(st, events.NewBroker(), trig, nil), st, trig
```

- [ ] **Step 6: Wire the runner in `main`**

In `cmd/netis/main.go`, ensure `"context"` and `"fmt"` are imported (add any missing). Add the runner type (top-level, near other declarations):

```go
type integrationRunner map[string]func(context.Context) error

func (r integrationRunner) Run(ctx context.Context, name string) error {
	fn, ok := r[name]
	if !ok {
		return fmt.Errorf("%s not configured", name)
	}
	return fn(ctx)
}
```

Build the registry and register each configured integration. Declare it before
the integration `if` blocks:

```go
	runNow := integrationRunner{}
```

In the proxmox block, capture the sync and register it:

```go
		px := proxmox.NewSync(st, proxmox.NewClient(pxURL, tokenID, secret, insecure == "1"), evs)
		runNow["proxmox"] = func(ctx context.Context) error { _, err := px.RunOnce(ctx); return err }
		go px.Start(ctx, time.Minute)
```

In the wireguard block, name the sync (rename the local SSH runner variable to
avoid clashing with `runNow` — call it `sshRunner`):

```go
		if sshRunner, err := wireguard.NewSSHRunner(wgAddr, wgUser, wgKey); err != nil {
			log.Printf("wireguard ssh setup: %v", err)
		} else {
			wgSync := wireguard.NewSync(st, sshRunner, evs, wgIface)
			runNow["wireguard"] = func(ctx context.Context) error { _, err := wgSync.RunOnce(ctx); return err }
			go wgSync.Start(ctx, time.Minute)
		}
```

In the pihole block:

```go
		ph := pihole.NewSync(st, pihole.NewClient(phURL, phPass, phInsecure == "1"), evs)
		runNow["pihole"] = func(ctx context.Context) error { _, err := ph.RunOnce(ctx); return err }
		go ph.Start(ctx, time.Minute)
```

Change the `NewServer` call:

```go
	srv := web.NewServer(st, broker, sched, runNow)
```

- [ ] **Step 7: Run the tests**

Run:
```bash
CGO_ENABLED=0 go build ./...
go test ./internal/web/ -run TestIntegrationRun -v
```
Expected: builds clean (all `NewServer` callers updated), the four run tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web/server.go internal/web/integrations_run.go internal/web/auth_test.go internal/web/scan_test.go internal/web/integrations_run_test.go cmd/netis/main.go
git commit -m "$(printf 'feat: on-demand integration run-now endpoint + runner registry\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Settings UI — Run-now button + button alignment

**Files:**
- Modify: `internal/web/views/settings.templ` (Run-now button in the three integration card headers; subnet-card `form=` restructure)
- Modify: `internal/web/static/app.css` (checkbox-label `:has` row rule)
- Test: `internal/web/settings_test.go` (add `TestSettingsRunButtonAndAlignment`)

**Interfaces:**
- Consumes: the `POST /settings/integrations/{name}/run` route (Task 1); existing `#toasts` container.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/settings_test.go`:

```go
func TestSettingsRunButtonAndAlignment(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	integ := authedGet(t, srv, st, "/settings?tab=integrations").Body.String()
	for _, want := range []string{
		`hx-post="/settings/integrations/proxmox/run"`,
		`hx-post="/settings/integrations/wireguard/run"`,
		`hx-post="/settings/integrations/pihole/run"`,
	} {
		if !strings.Contains(integ, want) {
			t.Errorf("integrations tab missing %q", want)
		}
	}

	subs := authedGet(t, srv, st, "/settings?tab=subnets").Body.String()
	if !strings.Contains(subs, `form="sn-edit-1"`) {
		t.Error("subnet card Save button should link to the edit form via form=")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestSettingsRunButtonAndAlignment -v`
Expected: FAIL — no Run-now buttons / no `form="sn-edit-1"`.

- [ ] **Step 3: Add the Run-now buttons to the integration card headers**

In `internal/web/views/settings.templ` `integrationsTab`, replace the three
`sc-head` lines:

```
			<div class="sc-head"><h3>Proxmox</h3>@integrationStatus("proxmox", d.Values["proxmox_url"] != "", d.Statuses)</div>
```
→
```
			<div class="sc-head"><h3>Proxmox</h3>@integrationStatus("proxmox", d.Values["proxmox_url"] != "", d.Statuses)<button type="button" class="ghost" hx-post="/settings/integrations/proxmox/run" hx-target="#toasts" hx-swap="beforeend">Run now</button></div>
```

```
			<div class="sc-head"><h3>WireGuard</h3>@integrationStatus("wireguard", d.Values["wg_ssh_addr"] != "", d.Statuses)</div>
```
→
```
			<div class="sc-head"><h3>WireGuard</h3>@integrationStatus("wireguard", d.Values["wg_ssh_addr"] != "", d.Statuses)<button type="button" class="ghost" hx-post="/settings/integrations/wireguard/run" hx-target="#toasts" hx-swap="beforeend">Run now</button></div>
```

```
			<div class="sc-head"><h3>Pi-hole</h3>@integrationStatus("pihole", d.Values["pihole_url"] != "", d.Statuses)</div>
```
→
```
			<div class="sc-head"><h3>Pi-hole</h3>@integrationStatus("pihole", d.Values["pihole_url"] != "", d.Statuses)<button type="button" class="ghost" hx-post="/settings/integrations/pihole/run" hx-target="#toasts" hx-swap="beforeend">Run now</button></div>
```

- [ ] **Step 4: Restructure the subnet-card actions**

In `internal/web/views/settings.templ`, replace the subnet card's edit form +
actions + delete form (the block currently at ~lines 129-156, from the
`<form method="post" action=…/subnets/%d>` through the standalone delete
`<form>`) with:

```
			<form id={ fmt.Sprintf("sn-edit-%d", sn.ID) } method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d", sn.ID)) }>
				<div class="field-grid">
					<label>CIDR <input type="text" name="cidr" value={ sn.CIDR } placeholder="192.168.1.0/24" required/></label>
					<label>Name <input type="text" name="name" value={ sn.Name } required/></label>
					<label>Kind
						<select name="kind">
							for _, k := range subnetKinds {
								if k == sn.Kind {
									<option value={ k } selected>{ k }</option>
								} else {
									<option value={ k }>{ k }</option>
								}
							}
						</select>
					</label>
					<label>Interval (s) <input type="number" name="scan_interval_sec" value={ fmt.Sprint(sn.ScanIntervalSec) } min="30"/></label>
					<label class="chk"><input type="checkbox" name="scan_enabled" if sn.ScanEnabled { checked }/> Auto-scan (periodic)</label>
				</div>
			</form>
			<div class="sc-actions">
				<button type="submit" form={ fmt.Sprintf("sn-edit-%d", sn.ID) } class="primary">Save</button>
				if sn.Kind != "wireguard" {
					<button type="button" class="ghost" hx-post={ fmt.Sprintf("/subnets/%d/scan", sn.ID) } hx-target="#toasts" hx-swap="beforeend">Scan</button>
				}
				<form method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d/delete", sn.ID)) } class="inline">
					<button type="submit" class="ghost">Delete</button>
				</form>
			</div>
```

(The fields stay inside the `sn-edit-{id}` form; the Save button submits it via
the `form=` attribute. All endpoints/field names are unchanged.)

- [ ] **Step 5: Add the checkbox-label CSS rule**

Append to `internal/web/static/app.css`:

```css
label:has(> input[type="checkbox"]) { flex-direction:row; align-items:center; gap:7px; }
```

- [ ] **Step 6: Regenerate templ, run the test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestSettingsRunButtonAndAlignment -v
```
Expected: PASS.

- [ ] **Step 7: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass (existing settings/welcome/scan tests included — endpoints and field names unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/settings.templ internal/web/views/settings_templ.go internal/web/static/app.css internal/web/settings_test.go
git commit -m "$(printf 'feat: integration Run-now buttons; fix settings button alignment\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- Runner interface + field + `NewServer` param + wiring → Task 1. ✅
- `handleIntegrationRun` (name validation 400, nil-runner toast, 20s timeout, RunOnce, status-read toast connected/failing/not-configured/ran) + route → Task 1. ✅
- `main` `integrationRunner` map wiring for proxmox/wireguard/pihole → Task 1. ✅
- Run-now button per card → Task 2. ✅
- Checkbox-label `:has` fix + subnet-card `form=` restructure → Task 2. ✅
- Tests: run happy path + toast, bad name 400, nil-runner 200, viewer 403, render (button + form=) → Tasks 1-2. ✅
- Out of scope (async/progress, in-place pill refresh, concurrency guard, E2/E3) → not implemented. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `IntegrationRunner.Run(ctx context.Context, name string) error` (Task 1) is implemented by `main.integrationRunner` and the test `recordingRunner`, and called by `handleIntegrationRun`. `NewServer(st, broker, trigger, runner)` — all four callers updated (auth_test nil, scan_test trig+nil, main sched+runNow). The toast titles map (`proxmox`/`wireguard`/`pihole` → `Proxmox`/`WireGuard`/`Pi-hole`) matches the route `{name}` values and the poller `IntegrationStatus.Name`s. `form="sn-edit-1"` (Task 2 test) matches `id={ fmt.Sprintf("sn-edit-%d", sn.ID) }` for subnet id 1.

**Ordering note:** Task 1 changes `NewServer`'s signature (all callers updated in the same task, so the tree compiles). Task 2's render test needs Task 1's route to exist; sequential order satisfies that. Both are independently testable.
