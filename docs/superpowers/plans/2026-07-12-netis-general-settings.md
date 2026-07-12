# Netis General Settings Expansion (Sub-project G3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Defaults for new subnets" card (scan interval, subnet kind, auto-scan) to Settings → General; those defaults prefill the Add-subnet form.

**Architecture:** New fields in the existing General form persist via `handleGeneralSave` (offline_after stays required, new keys optional); `subnetsTab`'s Add-subnet form reads them from `SettingsData.Values`. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `$(go env GOPATH)/bin/templ`), `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change (reuse the `setting` key/value table).
- The web package must NOT import proxmox/pihole/wireguard.
- Regenerate templ after editing `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `settings_templ.go` with source.
- Reuse existing `.setting-card`/form styles — no new CSS.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` before finishing.

---

### Task 1: General-tab new-subnet defaults

**Files:**
- Modify: `internal/web/views/settings.templ`
- Modify: `internal/web/settings.go` (`handleGeneralSave`)
- Test: `internal/web/settings_test.go`

**Interfaces:**
- Produces: settings keys `default_scan_interval_sec`, `default_subnet_kind`, `default_scan_enabled`; templ helpers `settingOr`, `defaultScanEnabled`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/settings_test.go` (uses existing `testServer`, `authedGet`, `authedPost`; imports include `net/url`, `strings`):

```go
func TestGeneralSavesSubnetDefaults(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/settings/general", url.Values{
		"offline_after":            {"3"},
		"default_scan_interval_sec": {"300"},
		"default_subnet_kind":       {"proxmox-bridge"},
		"default_scan_enabled":      {"off"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", rec.Code)
	}
	if v, _ := st.GetSetting("default_scan_interval_sec"); v != "300" {
		t.Fatalf("interval=%q", v)
	}
	if v, _ := st.GetSetting("default_subnet_kind"); v != "proxmox-bridge" {
		t.Fatalf("kind=%q", v)
	}
	if v, _ := st.GetSetting("default_scan_enabled"); v != "off" {
		t.Fatalf("enabled=%q", v)
	}
}

func TestGeneralSaveRejectsBadDefaults(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/settings/general", url.Values{
		"offline_after":            {"3"},
		"default_scan_interval_sec": {"5"}, // < 30
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad interval code=%d, want 400", rec.Code)
	}
}

func TestGeneralTabRendersDefaults(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/settings?tab=general").Body.String()
	for _, want := range []string{`name="default_scan_interval_sec"`, `name="default_subnet_kind"`, `name="default_scan_enabled"`} {
		if !strings.Contains(body, want) {
			t.Errorf("general tab missing %q", want)
		}
	}
}

func TestAddSubnetFormUsesDefaults(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.SetSetting("default_scan_interval_sec", "300")
	st.SetSetting("default_subnet_kind", "proxmox-bridge")
	body := authedGet(t, srv, st, "/settings?tab=subnets").Body.String()
	if !strings.Contains(body, `value="300"`) {
		t.Errorf("add-subnet form should default interval to 300")
	}
	if !strings.Contains(body, `<option value="proxmox-bridge" selected>`) {
		t.Errorf("add-subnet form should default kind to proxmox-bridge")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web/ -run 'TestGeneralSavesSubnetDefaults|TestGeneralSaveRejectsBadDefaults|TestGeneralTabRendersDefaults|TestAddSubnetFormUsesDefaults' -v`
Expected: FAIL — new fields not rendered/saved; add-subnet form still hardcodes 120/first-kind.

- [ ] **Step 3: Add templ helpers**

In `internal/web/views/settings.templ`, add to the Go block (near `generalOfflineAfter`):

```go
func settingOr(values map[string]string, key, def string) string {
	if v := values[key]; v != "" {
		return v
	}
	return def
}

func defaultScanEnabled(values map[string]string) bool {
	return values["default_scan_enabled"] != "off"
}
```

- [ ] **Step 4: Extend `generalTab` with the defaults card**

In `internal/web/views/settings.templ`, replace the `generalTab` templ body's form so it contains both the Availability card and a new Defaults card inside the one form. Change:

```go
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

to:

```go
templ generalTab(d SettingsData) {
	<h2>General</h2>
	<form method="post" action="/settings/general">
		<div class="setting-card">
			<div class="sc-head"><h3>Availability</h3></div>
			<label>Offline after (missed scans) <input type="number" name="offline_after" min="1" max="10" value={ generalOfflineAfter(d.Values) }/></label>
			<p class="muted" style="margin:6px 0 4px">A device is marked offline after this many consecutive missed scans.</p>
		</div>
		<div class="setting-card">
			<div class="sc-head"><h3>Defaults for new subnets</h3></div>
			<div class="field-grid">
				<label>Default scan interval (s) <input type="number" name="default_scan_interval_sec" min="30" value={ settingOr(d.Values, "default_scan_interval_sec", "120") }/></label>
				<label>Default kind
					<select name="default_subnet_kind">
						for _, k := range subnetKinds {
							if k == settingOr(d.Values, "default_subnet_kind", "lan") {
								<option value={ k } selected>{ k }</option>
							} else {
								<option value={ k }>{ k }</option>
							}
						}
					</select>
				</label>
				<label>Default auto-scan
					<select name="default_scan_enabled">
						if defaultScanEnabled(d.Values) {
							<option value="on" selected>Enabled</option>
							<option value="off">Disabled</option>
						} else {
							<option value="on">Enabled</option>
							<option value="off" selected>Disabled</option>
						}
					</select>
				</label>
			</div>
			<p class="muted" style="margin:6px 0 4px">Prefilled into the Add-subnet form.</p>
		</div>
		<button type="submit" class="primary">Save</button>
	</form>
}
```

- [ ] **Step 5: Prefill the Add-subnet form from the defaults**

In `internal/web/views/settings.templ`, in `subnetsTab`'s **Add subnet** form, change:

```go
				<label>Kind
					<select name="kind">
						for _, k := range subnetKinds {
							<option value={ k }>{ k }</option>
						}
					</select>
				</label>
				<label>Interval (s) <input type="number" name="scan_interval_sec" value="120" min="30"/></label>
				<label class="chk"><input type="checkbox" name="scan_enabled" checked/> Auto-scan (periodic)</label>
```

to:

```go
				<label>Kind
					<select name="kind">
						for _, k := range subnetKinds {
							if k == settingOr(d.Values, "default_subnet_kind", "lan") {
								<option value={ k } selected>{ k }</option>
							} else {
								<option value={ k }>{ k }</option>
							}
						}
					</select>
				</label>
				<label>Interval (s) <input type="number" name="scan_interval_sec" value={ settingOr(d.Values, "default_scan_interval_sec", "120") } min="30"/></label>
				<label class="chk"><input type="checkbox" name="scan_enabled" if defaultScanEnabled(d.Values) { checked }/> Auto-scan (periodic)</label>
```

Also update the **Adopt detected subnet** hidden interval to the default. Change:

```go
					<input type="hidden" name="scan_interval_sec" value="120"/>
```

to:

```go
					<input type="hidden" name="scan_interval_sec" value={ settingOr(d.Values, "default_scan_interval_sec", "120") }/>
```

(The adopt form's `kind="lan"` and `scan_enabled="on"` hidden fields stay as-is.)

- [ ] **Step 6: Persist the new keys in `handleGeneralSave`**

In `internal/web/settings.go`, replace the body of `handleGeneralSave` with:

```go
func (s *Server) handleGeneralSave(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.FormValue("offline_after"))
	if err != nil || n < 1 || n > 10 {
		http.Error(w, "offline_after must be an integer 1-10", 400)
		return
	}
	if err := s.store.SetSetting("offline_after", strconv.Itoa(n)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if v := r.FormValue("default_scan_interval_sec"); v != "" {
		iv, err := strconv.Atoi(v)
		if err != nil || iv < 30 {
			http.Error(w, "default scan interval must be an integer >= 30", 400)
			return
		}
		if err := s.store.SetSetting("default_scan_interval_sec", strconv.Itoa(iv)); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if v := r.FormValue("default_subnet_kind"); v != "" {
		if v != "lan" && v != "wireguard" && v != "proxmox-bridge" {
			http.Error(w, "bad subnet kind", 400)
			return
		}
		if err := s.store.SetSetting("default_subnet_kind", v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if v := r.FormValue("default_scan_enabled"); v != "" {
		if v != "on" && v != "off" {
			http.Error(w, "bad default scan enabled", 400)
			return
		}
		if err := s.store.SetSetting("default_scan_enabled", v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/settings?tab=general", http.StatusSeeOther)
}
```

- [ ] **Step 7: Regenerate templ, run tests + full build**

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean; the four new tests pass; existing settings tests (including the viewer-forbidden and offline_after tests) stay green.

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/web/settings.go && go vet ./...
git add internal/web/views/settings.templ internal/web/views/settings_templ.go internal/web/settings.go internal/web/settings_test.go
git commit -m "$(printf 'feat: configurable new-subnet defaults in general settings\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- General "Defaults for new subnets" card (interval/kind/auto-scan) in the one General form → Task 1. ✅
- `handleGeneralSave` persists the new keys (offline_after still required, new keys optional+validated) → Task 1. ✅
- Add-subnet form + adopt-detected prefill from the defaults; helpers `settingOr`/`defaultScanEnabled` → Task 1. ✅
- Tests: save persists, bad interval → 400, general renders fields, add-form uses defaults → Task 1. ✅
- Out of scope (instance name, retention, retroactive apply, schema) → untouched. ✅

**Placeholder scan:** none — every step shows complete markup/code.

**Type consistency:** `settingOr(map[string]string, string, string) string` and `defaultScanEnabled(map[string]string) bool` are used in both `generalTab` and `subnetsTab`, which each receive `SettingsData` (has `Values map[string]string`). `subnetKinds` (lan/wireguard/proxmox-bridge) is the existing views var; `handleGeneralSave`'s kind whitelist matches `handleSubnetCreate`'s. New settings keys are plain `setting` rows via `SetSetting`/`GetSetting`. `handleSettingsPage` already populates `d.Values` from all settings rows, so no handler change is needed to surface them.

**Ordering note:** single task; the one General `<form>` submits offline_after + the three new fields together, so no field is ever ambiguously absent in normal use, and the `default_scan_enabled` `<select>` (not a checkbox) is always submitted.
