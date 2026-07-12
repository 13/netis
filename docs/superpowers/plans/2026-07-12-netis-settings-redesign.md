# Netis Settings Redesign + Integration Status (Sub-project D1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show each integration's connection status (connected/failing + last-worked time + detail) on the settings Integrations tab, and give the whole settings page a modern card-based layout.

**Architecture:** Presentation-only rework of `settings.templ` plus a handler change to pass the already-recorded `IntegrationStatus` map. Field markup is split into per-integration templs so the shared onboarding welcome page renders unchanged; the settings tabs wrap those in status cards. No store or schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; no store or schema change (`store.IntegrationStatus` + `ListIntegrationStatus()` already exist).
- Every existing POST endpoint and form field `name` is unchanged; the onboarding welcome page's rendered output stays byte-identical.
- Secrets (`proxmox_secret`, `pihole_password`) never populated into `Values`; password inputs render blank (unchanged).
- Regenerate templ after editing `settings.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `settings_templ.go` with its source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing — do not stage them.

---

### Task 1: Integrations tab status + field-templ split

**Files:**
- Modify: `internal/web/views/settings.templ` (`SettingsData.Statuses`; split `integrationsFields` into `proxmoxFields`/`wireguardFields`/`piholeFields` + thin `integrationsFields` wrapper; `integrationStatus` helper; `integrationsTab`; `SettingsPage` integrations branch)
- Modify: `internal/web/settings.go` (`handleSettingsPage` builds `Statuses`)
- Modify: `internal/web/static/app.css` (`.setting-card`, `.status-line`)
- Test: `internal/web/settings_test.go` (add `TestIntegrationStatusRendered`, `TestWelcomeIntegrationsStillRenders`)

**Interfaces:**
- Consumes: `store.ListIntegrationStatus() ([]store.IntegrationStatus, error)`; `store.IntegrationStatus{Name,LastRun,Detail,OK,ItemCount}`; `views.relTime`.
- Produces: `views.SettingsData.Statuses map[string]store.IntegrationStatus`; templ `integrationsTab(d SettingsData)`, `integrationStatus(name string, configured bool, statuses map[string]store.IntegrationStatus)`, and per-integration field templs `proxmoxFields`/`wireguardFields`/`piholeFields`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/settings_test.go` (ensure `"time"` is imported):

```go
func TestIntegrationStatusRendered(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.SetSetting("pihole_url", "https://pi.hole")
	st.SetSetting("proxmox_url", "https://pve:8006")
	now := time.Now().UTC().Format(time.RFC3339)
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "pihole", OK: true, LastRun: now, Detail: "48 leases, 2 new"})
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "proxmox", OK: false, LastRun: now, Detail: "auth failed"})

	body := authedGet(t, srv, st, "/settings?tab=integrations").Body.String()
	for _, want := range []string{"Pi-hole", "connected", "48 leases, 2 new", "failing", "not configured"} {
		if !strings.Contains(body, want) {
			t.Errorf("integrations tab missing %q", want)
		}
	}
}

func TestWelcomeIntegrationsStillRenders(t *testing.T) {
	srv, st := testServer(t)
	body := authedGet(t, srv, st, "/welcome/integrations").Body.String()
	for _, want := range []string{`name="proxmox_url"`, `name="wg_ssh_addr"`, `name="pihole_url"`, "<legend>"} {
		if !strings.Contains(body, want) {
			t.Errorf("welcome integrations missing %q", want)
		}
	}
}
```

(WireGuard has no status and `wg_ssh_addr` is unset here, so its card renders `not configured` — that's the assertion. `authedGet`'s first `/` hit creates the admin session; `GET /welcome/integrations` is ungated and renders the fields.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'TestIntegrationStatusRendered|TestWelcomeIntegrationsStillRenders' -v`
Expected: FAIL — `Statuses` undefined / no `connected`/`not configured` text.

- [ ] **Step 3: Add `Statuses` to the view-model and build it in the handler**

In `internal/web/views/settings.templ`, add the field to `SettingsData`:

```go
type SettingsData struct {
	Subnets   []store.Subnet
	Users     []store.User
	Values    map[string]string
	ActiveTab string
	Detected  []netdetect.Detected
	Statuses  map[string]store.IntegrationStatus
}
```

In `internal/web/settings.go` `handleSettingsPage`, after the `tab` switch and before the `views.SettingsPage(...)` call, build the map when on the integrations tab:

```go
	var statuses map[string]store.IntegrationStatus
	if tab == "integrations" {
		list, err := s.store.ListIntegrationStatus()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		statuses = make(map[string]store.IntegrationStatus, len(list))
		for _, it := range list {
			statuses[it.Name] = it
		}
	}
```

and add `Statuses: statuses,` to the `views.SettingsData{...}` literal.

- [ ] **Step 4: Split the field markup and add the status helper + integrations tab**

In `internal/web/views/settings.templ`, **replace** the existing `integrationsFields` templ (the whole `templ integrationsFields(values map[string]string) { … }` block) with the three field templs, a thin wrapper, the status helper, and the integrations tab:

```
templ proxmoxFields(values map[string]string) {
	<label>URL <input type="text" name="proxmox_url" value={ values["proxmox_url"] } placeholder="https://proxmox.local:8006"/></label>
	<label>Token ID <input type="text" name="proxmox_token_id" value={ values["proxmox_token_id"] }/></label>
	<label>Secret <input type="password" name="proxmox_secret" placeholder="leave blank to keep current secret"/></label>
	<label>
		<input type="checkbox" name="proxmox_insecure" if values["proxmox_insecure"] == "1" { checked }/>
		Skip TLS verification
	</label>
}

templ wireguardFields(values map[string]string) {
	<label>SSH address <input type="text" name="wg_ssh_addr" value={ values["wg_ssh_addr"] } placeholder="10.0.0.1:22"/></label>
	<label>SSH user <input type="text" name="wg_ssh_user" value={ values["wg_ssh_user"] }/></label>
	<label>SSH key path <input type="text" name="wg_ssh_key_path" value={ values["wg_ssh_key_path"] }/></label>
	<label>Interface <input type="text" name="wg_iface" value={ values["wg_iface"] } placeholder="wg0"/></label>
}

templ piholeFields(values map[string]string) {
	<label>URL <input type="text" name="pihole_url" value={ values["pihole_url"] } placeholder="https://pi.hole"/></label>
	<label>App password <input type="password" name="pihole_password" placeholder="leave blank to keep current password"/></label>
	<label>
		<input type="checkbox" name="pihole_insecure" if values["pihole_insecure"] == "1" { checked }/>
		Skip TLS verification
	</label>
}

// integrationsFields renders the plain fieldsets used by the onboarding welcome
// page; its output must stay identical to the pre-split version.
templ integrationsFields(values map[string]string) {
	<fieldset>
		<legend>Proxmox</legend>
		@proxmoxFields(values)
	</fieldset>
	<fieldset>
		<legend>WireGuard (via SSH)</legend>
		@wireguardFields(values)
	</fieldset>
	<fieldset>
		<legend>Pi-hole (v6)</legend>
		@piholeFields(values)
	</fieldset>
}

// integrationStatus renders a connection-status pill (+ detail) for one
// integration, resolved from the recorded poller status and whether the
// integration is configured.
templ integrationStatus(name string, configured bool, statuses map[string]store.IntegrationStatus) {
	if st, ok := statuses[name]; ok {
		if st.OK {
			<span class="status-line"><span class="pill online"><span class="d"></span>connected</span> <span class="muted">last worked { relTime(st.LastRun) }</span></span>
		} else {
			<span class="status-line"><span class="pill offline"><span class="d"></span>failing</span> <span class="muted">{ relTime(st.LastRun) }</span></span>
		}
		if st.Detail != "" {
			<p class="muted" style="margin:4px 0 0">{ st.Detail }</p>
		}
	} else if configured {
		<span class="status-line"><span class="pill reserved"><span class="d"></span>configured · not run yet</span></span>
	} else {
		<span class="status-line"><span class="muted">not configured</span></span>
	}
}

templ integrationsTab(d SettingsData) {
	<h2>Integrations</h2>
	<p class="muted">restart netis to apply integration changes</p>
	<form method="post" action="/settings/integrations">
		<div class="setting-card">
			<div class="sc-head"><h3>Proxmox</h3>@integrationStatus("proxmox", d.Values["proxmox_url"] != "", d.Statuses)</div>
			@proxmoxFields(d.Values)
		</div>
		<div class="setting-card">
			<div class="sc-head"><h3>WireGuard</h3>@integrationStatus("wireguard", d.Values["wg_ssh_addr"] != "", d.Statuses)</div>
			@wireguardFields(d.Values)
		</div>
		<div class="setting-card">
			<div class="sc-head"><h3>Pi-hole</h3>@integrationStatus("pihole", d.Values["pihole_url"] != "", d.Statuses)</div>
			@piholeFields(d.Values)
		</div>
		<button type="submit" class="primary">Save integrations</button>
	</form>
}
```

- [ ] **Step 5: Point the integrations branch at the new tab**

In `internal/web/views/settings.templ`, in `SettingsPage`, replace the entire inline `if d.ActiveTab == "integrations" { … }` block with:

```
			if d.ActiveTab == "integrations" {
				@integrationsTab(d)
			}
```

(Leave the `subnets`, `users`, `general` branches inline for now — Task 2 extracts and redesigns them.)

- [ ] **Step 6: Add the card + status CSS**

Append to `internal/web/static/app.css`:

```css
.setting-card { background:var(--surface); border:1px solid var(--border); border-radius:var(--radius);
  padding:15px 16px; box-shadow:var(--shadow); margin:0 0 14px; }
.setting-card .sc-head { display:flex; align-items:center; gap:10px; flex-wrap:wrap; margin-bottom:10px; }
.setting-card .sc-head h3 { margin:0; }
.status-line { display:inline-flex; align-items:center; gap:8px; }
```

- [ ] **Step 7: Regenerate templ, run the tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestIntegrationStatusRendered|TestWelcomeIntegrationsStillRenders|TestPiholeSecretNeverEchoed' -v
```
Expected: PASS (the two new tests; the existing secret-never-echoed test still passes against the rebuilt integrations tab).

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/settings.templ internal/web/views/settings_templ.go internal/web/settings.go internal/web/static/app.css internal/web/settings_test.go
git commit -m "$(printf 'feat: show integration connection status on the settings page\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Card redesign of Subnets / Users / General tabs

**Files:**
- Modify: `internal/web/views/settings.templ` (`subnetsTab`/`usersTab`/`generalTab` templs; `SettingsPage` branches)
- Modify: `internal/web/static/app.css` (`.field-grid`, `.sc-actions`, `.chips`/`.chip-add`, `.tabs`/`.tab` pill)
- Test: `internal/web/settings_test.go` (add `TestSettingsTabsRender`)

**Interfaces:**
- Consumes: `.setting-card`/`.status-line` (Task 1); `subnetKinds`, `generalOfflineAfter`, `tabLink` (existing in settings.templ).
- Produces: templ `subnetsTab(d SettingsData)`, `usersTab(d SettingsData)`, `generalTab(d SettingsData)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/settings_test.go`:

```go
func TestSettingsTabsRender(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	subnets := authedGet(t, srv, st, "/settings?tab=subnets").Body.String()
	if !strings.Contains(subnets, "10.0.0.0/24") || !strings.Contains(subnets, "setting-card") {
		t.Error("subnets tab should render the subnet as a card")
	}
	if !strings.Contains(subnets, `hx-post="/subnets/1/scan"`) {
		t.Error("subnets tab should keep the per-subnet scan control")
	}
	users := authedGet(t, srv, st, "/settings?tab=users").Body.String()
	if !strings.Contains(users, `name="username"`) {
		t.Error("users tab missing add-user form")
	}
	gen := authedGet(t, srv, st, "/settings?tab=general").Body.String()
	if !strings.Contains(gen, `name="offline_after"`) {
		t.Error("general tab missing offline_after field")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestSettingsTabsRender -v`
Expected: FAIL — the current subnets tab is a table, no `setting-card`.

- [ ] **Step 3: Add the three tab templs**

In `internal/web/views/settings.templ`, add these templs (near `integrationsTab`). `fmt` is already imported:

```
templ subnetsTab(d SettingsData) {
	<h2>Subnets</h2>
	for _, sn := range d.Subnets {
		<div class="setting-card">
			<div class="sc-head">
				<h3>{ sn.Name }</h3>
				<span class="mono muted">{ sn.CIDR }</span>
				<span class="badge">{ sn.Kind }</span>
				if sn.ScanEnabled {
					<span class="badge online">auto-scan on</span>
				} else {
					<span class="badge">auto-scan off</span>
				}
			</div>
			<form method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d", sn.ID)) }>
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
				<div class="sc-actions">
					<button type="submit" class="primary">Save</button>
					if sn.Kind != "wireguard" {
						<button type="button" class="ghost" hx-post={ fmt.Sprintf("/subnets/%d/scan", sn.ID) } hx-target="#toasts" hx-swap="beforeend">Scan</button>
					}
				</div>
			</form>
			<form method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d/delete", sn.ID)) } class="inline">
				<button type="submit" class="ghost">Delete</button>
			</form>
		</div>
	}

	<h3>Detected subnets</h3>
	if len(d.Detected) == 0 {
		<p class="muted">no new subnets detected</p>
	} else {
		<div class="chips">
			for _, det := range d.Detected {
				<form method="post" action="/settings/subnets" class="inline">
					<input type="hidden" name="cidr" value={ det.CIDR }/>
					<input type="hidden" name="name" value={ det.Iface }/>
					<input type="hidden" name="kind" value="lan"/>
					<input type="hidden" name="scan_interval_sec" value="120"/>
					<input type="hidden" name="scan_enabled" value="on"/>
					<button type="submit" class="chip-add"><span class="mono">{ det.CIDR }</span> <span class="muted">({ det.Iface })</span> +</button>
				</form>
			}
		</div>
	}

	<div class="setting-card">
		<div class="sc-head"><h3>Add subnet</h3></div>
		<form method="post" action="/settings/subnets">
			<div class="field-grid">
				<label>CIDR <input type="text" name="cidr" placeholder="192.168.1.0/24" required/></label>
				<label>Name <input type="text" name="name" required/></label>
				<label>Kind
					<select name="kind">
						for _, k := range subnetKinds {
							<option value={ k }>{ k }</option>
						}
					</select>
				</label>
				<label>Interval (s) <input type="number" name="scan_interval_sec" value="120" min="30"/></label>
				<label class="chk"><input type="checkbox" name="scan_enabled" checked/> Auto-scan (periodic)</label>
			</div>
			<button type="submit" class="primary">Add subnet</button>
		</form>
	</div>
}

templ usersTab(d SettingsData) {
	<h2>Users</h2>
	<div class="setting-card">
		<table>
			<tr><th>Username</th><th>Role</th><th></th></tr>
			for _, u := range d.Users {
				<tr>
					<td>{ u.Username }</td>
					<td><span class="badge">{ u.Role }</span></td>
					<td>
						<form method="post" action={ templ.URL(fmt.Sprintf("/settings/users/%d/delete", u.ID)) } class="inline">
							<button type="submit" class="ghost">delete</button>
						</form>
					</td>
				</tr>
			}
		</table>
	</div>
	<div class="setting-card">
		<div class="sc-head"><h3>Add user</h3></div>
		<form method="post" action="/settings/users">
			<div class="field-grid">
				<label>Username <input type="text" name="username" required/></label>
				<label>Password <input type="password" name="password" minlength="6" required/></label>
				<label>Role
					<select name="role">
						<option value="viewer">viewer</option>
						<option value="admin">admin</option>
					</select>
				</label>
			</div>
			<button type="submit" class="primary">Add user</button>
		</form>
	</div>
}

templ generalTab(d SettingsData) {
	<h2>General</h2>
	<div class="setting-card">
		<div class="sc-head"><h3>Availability</h3></div>
		<form method="post" action="/settings/general">
			<label>Offline after (missed scans) <input type="number" name="offline_after" min="1" max="10" value={ generalOfflineAfter(d.Values) }/></label>
			<p class="muted" style="margin:6px 0 10px">A device is marked offline after this many consecutive missed scans.</p>
			<button type="submit" class="primary">Save</button>
		</form>
	</div>
}
```

- [ ] **Step 4: Replace the inline branches in `SettingsPage`**

In `internal/web/views/settings.templ` `SettingsPage`, replace the three remaining inline blocks (`if d.ActiveTab == "subnets" { … }`, `… "users" { … }`, `… "general" { … }`) with calls:

```
			if d.ActiveTab == "subnets" {
				@subnetsTab(d)
			} else if d.ActiveTab == "integrations" {
				@integrationsTab(d)
			} else if d.ActiveTab == "users" {
				@usersTab(d)
			} else if d.ActiveTab == "general" {
				@generalTab(d)
			}
```

(This supersedes the standalone `if … "integrations"` from Task 1 — the four branches now sit in one if/else-if chain.)

- [ ] **Step 5: Add the layout CSS + pill tabs**

Append to `internal/web/static/app.css`:

```css
.field-grid { display:grid; grid-template-columns:repeat(auto-fit, minmax(180px, 1fr)); gap:11px; margin-bottom:12px; }
.field-grid label { display:flex; flex-direction:column; gap:4px; font-size:12.5px; color:var(--muted); }
.field-grid label.chk { flex-direction:row; align-items:center; gap:7px; }
.sc-actions { display:flex; gap:9px; align-items:center; }
.chips { display:flex; flex-wrap:wrap; gap:8px; margin:6px 0 14px; }
.chip-add { background:var(--surface-2); border:1px solid var(--border); border-radius:999px; padding:4px 11px; cursor:pointer; font-size:12.5px; }
.chip-add:hover { border-color:var(--accent); }
nav.tabs { display:inline-flex; gap:4px; background:var(--surface-2); border:1px solid var(--border); border-radius:999px; padding:3px; margin:0 0 16px; }
nav.tabs .tab { text-decoration:none; color:var(--muted); font-size:13px; font-weight:520; padding:5px 13px; border-radius:999px; }
nav.tabs .tab.active { background:var(--accent-soft); color:var(--accent); }
```

(If any `nav.tabs`/`.tab` rules already exist elsewhere in `app.css`, replace them with these so the pill styling is the single definition.)

- [ ] **Step 6: Regenerate templ, run the test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestSettingsTabsRender -v
```
Expected: PASS.

- [ ] **Step 7: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass (including the existing settings POST-handler tests and the welcome tests — endpoints and field names unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/web/views/settings.templ internal/web/views/settings_templ.go internal/web/static/app.css internal/web/settings_test.go
git commit -m "$(printf 'feat: card-based redesign of settings subnets/users/general tabs\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- View-model `Statuses` + handler build → Task 1. ✅
- `integrationStatus` helper (connected/failing/not-run/not-configured + relTime + detail) → Task 1. ✅
- Field-templ split + `integrationsFields` thin wrapper (welcome unchanged) + integrations cards → Task 1. ✅
- `SettingsPage` split into tab templs → Task 1 (integrations) + Task 2 (subnets/users/general, final if/else-if). ✅
- Subnets/Users/General card redesign → Task 2. ✅
- CSS `.setting-card`/`.status-line` (Task 1) + `.field-grid`/`.chips`/pill tabs (Task 2). ✅
- Tests: status render, welcome unchanged, tabs render; existing POST + secret + welcome tests stay green → Tasks 1-2. ✅
- Out of scope (test-connection button, restart model, dashboard widget, D2 grid nav) → not implemented, matches spec. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `SettingsData.Statuses map[string]store.IntegrationStatus` (Task 1) is populated in `handleSettingsPage` and read by `integrationStatus`/`integrationsTab`. `integrationStatus(name string, configured bool, statuses map[string]store.IntegrationStatus)` is called with those exact arg types. The per-integration field templs `proxmoxFields`/`wireguardFields`/`piholeFields(values map[string]string)` are used by both `integrationsFields` (welcome) and `integrationsTab` (settings) with identical field `name`s. `subnetsTab`/`usersTab`/`generalTab(d SettingsData)` (Task 2) match the `SettingsPage` call sites. `.setting-card` (Task 1 CSS) is reused by Task 2's tabs. `.tabs`/`.tab` pill (Task 2) restyles the existing `tabLink` output.

**Ordering note:** Task 1 introduces `.setting-card` (CSS) and `integrationsTab`, leaving the other three branches inline; Task 2 extracts/redesigns them and consolidates `SettingsPage` into one if/else-if chain. Both edit `settings.templ`/`app.css`/`settings_test.go` and regenerate; sequential execution avoids conflicts.
