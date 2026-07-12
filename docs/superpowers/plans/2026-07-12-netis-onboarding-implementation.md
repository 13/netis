# Onboarding & Settings Clarity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Auto-detect LAN subnets from the host, add a first-run quick-setup wizard, and reorganize the settings page into tabs.

**Architecture:** A pure `internal/netdetect` package lists scannable IPv4 subnets from the host interfaces (injectable lister for tests). The settings page becomes server-rendered tabs and reuses detection on the Subnets tab; the integrations save loop and fieldset markup are extracted for reuse. A wizard gated by a new `onboarded` setting runs after admin creation, enforced by a redirect in the auth middleware.

**Tech Stack:** Go 1.26, `net`/`net/netip`, templ + HTMX, `modernc.org/sqlite`.

**Spec:** `docs/superpowers/specs/2026-07-12-netis-onboarding-design.md` — read before starting.

## Global Constraints

- Go module `netis`, `go1.26.5`, `CGO_ENABLED=0`.
- Build `CGO_ENABLED=0 go build ./...`; test `go test ./... -count=1`.
- **templ:** CLI at `/home/ben/go/bin/templ` (v0.3.1020; not on PATH — run `export PATH="$PATH:$(go env GOPATH)/bin"` first). After editing any `.templ`, run `templ generate` then build; commit BOTH the `.templ` and generated `*_templ.go`.
- Subnet defaults created by detection/wizard: `kind=lan`, `scan_enabled=true`, `scan_interval_sec=120`. CIDRs stored masked (`netip.Prefix.Masked().String()`).
- `onboarded` setting: `"1"` once setup is complete; anything else is falsy.
- Virtual-interface name prefixes to skip: `docker`, `veth`, `br-`, `tap`, `cni`, `virbr`, `lo`.
- `errors.Is(err, sql.ErrNoRows)` for not-found lookups (project convention).
- Commit after each task; conventional-commit, body ending with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Shell prints harmless zsh-rc noise on stderr (`command not found: z`); ignore — exit codes are correct.

---

### Task 1: `internal/netdetect` package

**Files:**
- Create: `internal/netdetect/detect.go`, `internal/netdetect/detect_test.go`

**Interfaces:**
- Consumes: stdlib only.
- Produces:
  - `type Detected struct { CIDR, Iface string }`
  - `func DetectSubnets() ([]Detected, error)` — scannable IPv4 subnets from the host, deduped by CIDR, sorted by CIDR.
  - (internal) `detectFrom(lister func() ([]ifaceInfo, error))` — the tested seam; `type ifaceInfo struct { Name string; Up, Loopback bool; Addrs []netip.Prefix }`.

- [ ] **Step 1: Write the failing test**

`internal/netdetect/detect_test.go`:

```go
package netdetect

import (
	"net/netip"
	"testing"
)

func mustPrefix(s string) netip.Prefix {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		panic(err)
	}
	return p
}

func TestDetectFromFiltersAndDedupes(t *testing.T) {
	lister := func() ([]ifaceInfo, error) {
		return []ifaceInfo{
			{Name: "eth0", Up: true, Addrs: []netip.Prefix{mustPrefix("192.168.1.50/24")}},
			{Name: "lo", Up: true, Loopback: true, Addrs: []netip.Prefix{mustPrefix("127.0.0.1/8")}},
			{Name: "eth1", Up: false, Addrs: []netip.Prefix{mustPrefix("10.1.0.5/24")}},          // down → skip
			{Name: "docker0", Up: true, Addrs: []netip.Prefix{mustPrefix("172.17.0.1/16")}},      // virtual prefix → skip
			{Name: "veth123", Up: true, Addrs: []netip.Prefix{mustPrefix("10.9.0.1/24")}},        // virtual prefix → skip
			{Name: "wlan0", Up: true, Addrs: []netip.Prefix{
				mustPrefix("169.254.5.5/16"),   // link-local → skip
				mustPrefix("fe80::1/64"),       // IPv6 → skip
				mustPrefix("10.0.0.2/24"),      // kept
			}},
			{Name: "eth2", Up: true, Addrs: []netip.Prefix{mustPrefix("192.168.1.9/24")}},        // dup CIDR of eth0 → deduped
			{Name: "ptp0", Up: true, Addrs: []netip.Prefix{mustPrefix("203.0.113.7/32")}},        // /32 host route → skip
		}, nil
	}
	got, err := detectFrom(lister)
	if err != nil {
		t.Fatal(err)
	}
	// Expect exactly 10.0.0.0/24 and 192.168.1.0/24, sorted by CIDR.
	if len(got) != 2 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].CIDR != "10.0.0.0/24" || got[0].Iface != "wlan0" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].CIDR != "192.168.1.0/24" || got[1].Iface != "eth0" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestDetectSubnetsRunsAgainstHost(t *testing.T) {
	// Smoke: the real host lister must not error (result contents are
	// environment-dependent, so only the error is asserted).
	if _, err := DetectSubnets(); err != nil {
		t.Fatalf("DetectSubnets on host: %v", err)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/netdetect/ -count=1`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Implement**

`internal/netdetect/detect.go`:

```go
package netdetect

import (
	"net"
	"net/netip"
	"sort"
	"strings"
)

type Detected struct {
	CIDR  string
	Iface string
}

type ifaceInfo struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []netip.Prefix
}

var virtualPrefixes = []string{"docker", "veth", "br-", "tap", "cni", "virbr", "lo"}

// DetectSubnets returns scannable IPv4 subnets from the host's interfaces.
func DetectSubnets() ([]Detected, error) {
	return detectFrom(systemInterfaces)
}

func detectFrom(lister func() ([]ifaceInfo, error)) ([]Detected, error) {
	ifaces, err := lister()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []Detected
	for _, ifc := range ifaces {
		if !ifc.Up || ifc.Loopback || hasVirtualPrefix(ifc.Name) {
			continue
		}
		for _, p := range ifc.Addrs {
			a := p.Addr()
			if !a.Is4() || a.IsLinkLocalUnicast() {
				continue
			}
			if p.Bits() > 30 { // skip /31 and /32 host routes
				continue
			}
			cidr := p.Masked().String()
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			out = append(out, Detected{CIDR: cidr, Iface: ifc.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CIDR < out[j].CIDR })
	return out, nil
}

func hasVirtualPrefix(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// systemInterfaces is the production lister wrapping net.Interfaces().
func systemInterfaces() ([]ifaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]ifaceInfo, 0, len(ifaces))
	for _, ifc := range ifaces {
		info := ifaceInfo{
			Name:     ifc.Name,
			Up:       ifc.Flags&net.FlagUp != 0,
			Loopback: ifc.Flags&net.FlagLoopback != 0,
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			out = append(out, info)
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			p, err := netip.ParsePrefix(ipnet.String())
			if err != nil {
				continue
			}
			info.Addrs = append(info.Addrs, p)
		}
		out = append(out, info)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests + build**

Run: `go test ./internal/netdetect/ -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/netdetect/
git commit -m "feat(netdetect): detect scannable IPv4 subnets from host interfaces"
```

---

### Task 2: Settings tabs, detect rows, shared integration save

**Files:**
- Modify: `internal/web/settings.go`, `internal/web/server.go`, `internal/web/views/settings.templ`
- Test: `internal/web/settings_test.go`

**Interfaces:**
- Consumes: `netdetect.DetectSubnets`/`Detected` (Task 1).
- Produces (later the wizard task reuses these):
  - `Server` gains field `detect func() ([]netdetect.Detected, error)`, defaulted to `netdetect.DetectSubnets` in `NewServer`.
  - `func (s *Server) saveIntegrationSettings(r *http.Request) error` — the extracted per-key save loop.
  - templ component `templ integrationsFields(values map[string]string)` — the three integration fieldsets, reused by settings and the wizard.
  - `views.SettingsData` gains `ActiveTab string` and `Detected []netdetect.Detected`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/settings_test.go`:

```go
func TestSettingsTabsShowOneSection(t *testing.T) {
	srv, st := testServer(t)
	// Users tab shows the users section, not the subnet "Add subnet" form.
	rec := authedGet(t, srv, st, "/settings?tab=users")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Add user") {
		t.Fatalf("users tab: code=%d", rec.Code)
	}
	if strings.Contains(body, "Add subnet") {
		t.Fatal("users tab must not render the subnet create form")
	}
	// Unknown tab falls back to subnets.
	rec = authedGet(t, srv, st, "/settings?tab=bogus")
	if !strings.Contains(rec.Body.String(), "Add subnet") {
		t.Fatal("unknown tab should fall back to subnets")
	}
}

func TestSettingsSubnetsTabShowsDetected(t *testing.T) {
	srv, st := testServer(t)
	// Inject a detected subnet not yet configured.
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{{CIDR: "192.168.7.0/24", Iface: "eth0"}}, nil
	}
	rec := authedGet(t, srv, st, "/settings?tab=subnets")
	if !strings.Contains(rec.Body.String(), "192.168.7.0/24") {
		t.Fatal("detected subnet should appear on the subnets tab")
	}
	// Once configured, it is no longer offered.
	st.CreateSubnet(store.Subnet{CIDR: "192.168.7.0/24", Name: "eth0", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
	rec = authedGet(t, srv, st, "/settings?tab=subnets")
	// The configured subnet shows in the table, but not as a fresh "add" row.
	if strings.Contains(rec.Body.String(), "no new subnets detected") == false {
		t.Fatal("expected 'no new subnets detected' once all detected subnets exist")
	}
}
```

Add imports `"netis/internal/netdetect"` and (if missing) `"strings"` to the test file. `srv.detect` is a field on the same-package `*Server`, so the test can set it directly.

- [ ] **Step 2: Run tests, verify failure**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -run Settings -count=1`
Expected: FAIL — `srv.detect` undefined, tabs not implemented.

- [ ] **Step 3: Add the detect field + extract saveIntegrationSettings**

In `internal/web/server.go`, add the import `"netis/internal/netdetect"`, add the field to the `Server` struct:

```go
type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	broker  *events.Broker
	trigger ScanTrigger
	limiter *rateLimiter
	detect  func() ([]netdetect.Detected, error)
}
```

and set the default in `NewServer` (in the struct literal):

```go
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, limiter: newRateLimiter(),
		detect: netdetect.DetectSubnets,
	}
```

In `internal/web/settings.go`, replace `handleIntegrationsSave` with the extracted helper + a thin handler:

```go
// saveIntegrationSettings writes the integration settings from a submitted
// form: a blank secret keeps the stored value, and the *_insecure checkboxes
// normalize "on" to "1". Shared by the settings page and the setup wizard.
func (s *Server) saveIntegrationSettings(r *http.Request) error {
	for _, k := range settingsKeys {
		v := r.FormValue(k)
		if (k == "proxmox_secret" || k == "pihole_password") && v == "" {
			continue
		}
		if k == "proxmox_insecure" || k == "pihole_insecure" {
			if v == "on" {
				v = "1"
			} else {
				v = ""
			}
		}
		if err := s.store.SetSetting(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleIntegrationsSave(w http.ResponseWriter, r *http.Request) {
	if err := s.saveIntegrationSettings(r); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings?tab=integrations", http.StatusSeeOther)
}
```

- [ ] **Step 4: Add active-tab + detected list to the settings handler**

Replace the tail of `handleSettingsPage` (the `values["offline_after"]` block onward) so it computes the active tab and the detected-minus-existing list, and passes them:

```go
	values["offline_after"] = offlineAfter

	tab := r.URL.Query().Get("tab")
	switch tab {
	case "subnets", "integrations", "users", "general":
	default:
		tab = "subnets"
	}

	var newDetected []netdetect.Detected
	if detected, err := s.detect(); err == nil {
		have := make(map[string]bool, len(subnets))
		for _, sn := range subnets {
			have[sn.CIDR] = true
		}
		for _, d := range detected {
			if !have[d.CIDR] {
				newDetected = append(newDetected, d)
			}
		}
	}

	u, _ := userFrom(r)
	views.SettingsPage(u.Username, views.SettingsData{
		Subnets: subnets, Users: users, Values: values,
		ActiveTab: tab, Detected: newDetected,
	}).Render(r.Context(), w)
}
```

Add `"netis/internal/netdetect"` to `settings.go` imports.

- [ ] **Step 5: Update the POST redirects to their tabs**

In `internal/web/settings.go`, change the success redirects:
- `handleSubnetCreate`, `handleSubnetUpdate`, `handleSubnetDelete`: redirect to `/settings?tab=subnets`.
- `handleUserCreate`, `handleUserDelete`: redirect to `/settings?tab=users`.
- `handleGeneralSave`: redirect to `/settings?tab=general`.

Each currently ends `http.Redirect(w, r, "/settings", http.StatusSeeOther)` — change the path to include the `?tab=` for that section. (Leave the error responses and the last-admin 400 in `handleUserDelete` unchanged.)

- [ ] **Step 6: Rewrite the settings template with tabs**

Replace `internal/web/views/settings.templ` entirely:

```templ
package views

import (
	"fmt"

	"netis/internal/netdetect"
	"netis/internal/store"
)

var subnetKinds = []string{"lan", "wireguard", "proxmox-bridge"}

// SettingsData is the view-model for the settings page. Values never contains
// proxmox_secret or pihole_password — those fields always render blank so the
// stored secrets are never echoed back.
type SettingsData struct {
	Subnets   []store.Subnet
	Users     []store.User
	Values    map[string]string
	ActiveTab string
	Detected  []netdetect.Detected
}

templ tabLink(active, tab, label string) {
	if active == tab {
		<a class="tab active" href={ templ.URL("/settings?tab=" + tab) }>{ label }</a>
	} else {
		<a class="tab" href={ templ.URL("/settings?tab=" + tab) }>{ label }</a>
	}
}

templ integrationsFields(values map[string]string) {
	<fieldset>
		<legend>Proxmox</legend>
		<label>URL <input type="text" name="proxmox_url" value={ values["proxmox_url"] } placeholder="https://proxmox.local:8006"/></label>
		<label>Token ID <input type="text" name="proxmox_token_id" value={ values["proxmox_token_id"] }/></label>
		<label>Secret <input type="password" name="proxmox_secret" placeholder="leave blank to keep current secret"/></label>
		<label>
			<input type="checkbox" name="proxmox_insecure" if values["proxmox_insecure"] == "1" { checked }/>
			Skip TLS verification
		</label>
	</fieldset>
	<fieldset>
		<legend>WireGuard (via SSH)</legend>
		<label>SSH address <input type="text" name="wg_ssh_addr" value={ values["wg_ssh_addr"] } placeholder="10.0.0.1:22"/></label>
		<label>SSH user <input type="text" name="wg_ssh_user" value={ values["wg_ssh_user"] }/></label>
		<label>SSH key path <input type="text" name="wg_ssh_key_path" value={ values["wg_ssh_key_path"] }/></label>
		<label>Interface <input type="text" name="wg_iface" value={ values["wg_iface"] } placeholder="wg0"/></label>
	</fieldset>
	<fieldset>
		<legend>Pi-hole (v6)</legend>
		<label>URL <input type="text" name="pihole_url" value={ values["pihole_url"] } placeholder="https://pi.hole"/></label>
		<label>App password <input type="password" name="pihole_password" placeholder="leave blank to keep current password"/></label>
		<label>
			<input type="checkbox" name="pihole_insecure" if values["pihole_insecure"] == "1" { checked }/>
			Skip TLS verification
		</label>
	</fieldset>
}

templ SettingsPage(username string, d SettingsData) {
	@Layout("Settings", username) {
		<h1>Settings</h1>
		<nav class="tabs">
			@tabLink(d.ActiveTab, "subnets", "Subnets")
			@tabLink(d.ActiveTab, "integrations", "Integrations")
			@tabLink(d.ActiveTab, "users", "Users")
			@tabLink(d.ActiveTab, "general", "General")
		</nav>

		if d.ActiveTab == "subnets" {
			<h2>Subnets</h2>
			<table>
				<tr><th>CIDR</th><th>Name</th><th>Kind</th><th>Scan enabled</th><th>Interval (s)</th><th></th></tr>
				for _, sn := range d.Subnets {
					<tr>
						<td class="mono">{ sn.CIDR }</td>
						<td>{ sn.Name }</td>
						<td>{ sn.Kind }</td>
						<td>
							if sn.ScanEnabled {
								<span class="ok">yes</span>
							} else {
								<span class="muted">no</span>
							}
						</td>
						<td>{ fmt.Sprint(sn.ScanIntervalSec) }</td>
						<td>
							<form method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d/delete", sn.ID)) } class="inline">
								<button type="submit">delete</button>
							</form>
						</td>
					</tr>
					<tr>
						<td colspan="6">
							<form method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d", sn.ID)) } class="inline">
								<input type="text" name="cidr" value={ sn.CIDR } placeholder="192.168.1.0/24" required/>
								<input type="text" name="name" value={ sn.Name } required/>
								<select name="kind">
									for _, k := range subnetKinds {
										if k == sn.Kind {
											<option value={ k } selected>{ k }</option>
										} else {
											<option value={ k }>{ k }</option>
										}
									}
								</select>
								<input type="number" name="scan_interval_sec" value={ fmt.Sprint(sn.ScanIntervalSec) } min="30"/>
								<label>
									<input type="checkbox" name="scan_enabled" if sn.ScanEnabled { checked }/>
									Scan enabled
								</label>
								<button type="submit">save</button>
							</form>
						</td>
					</tr>
				}
			</table>

			<h3>Detected subnets</h3>
			if len(d.Detected) == 0 {
				<p class="muted">no new subnets detected</p>
			} else {
				for _, det := range d.Detected {
					<form method="post" action="/settings/subnets" class="inline detected">
						<input type="hidden" name="cidr" value={ det.CIDR }/>
						<input type="hidden" name="name" value={ det.Iface }/>
						<input type="hidden" name="kind" value="lan"/>
						<input type="hidden" name="scan_interval_sec" value="120"/>
						<input type="hidden" name="scan_enabled" value="on"/>
						<span class="mono">{ det.CIDR }</span> <span class="muted">({ det.Iface })</span>
						<button type="submit">add</button>
					</form>
				}
			}

			<h3>Add subnet</h3>
			<form method="post" action="/settings/subnets" class="auth-card">
				<label>CIDR <input type="text" name="cidr" placeholder="192.168.1.0/24" required/></label>
				<label>Name <input type="text" name="name" required/></label>
				<label>Kind
					<select name="kind">
						for _, k := range subnetKinds {
							<option value={ k }>{ k }</option>
						}
					</select>
				</label>
				<label>Scan interval (s) <input type="number" name="scan_interval_sec" value="120" min="30"/></label>
				<label><input type="checkbox" name="scan_enabled" checked/> Scan enabled</label>
				<button type="submit">Add subnet</button>
			</form>
		}

		if d.ActiveTab == "integrations" {
			<h2>Integrations</h2>
			<p class="muted">restart netis to apply integration changes</p>
			<form method="post" action="/settings/integrations" class="auth-card">
				@integrationsFields(d.Values)
				<button type="submit">Save integrations</button>
			</form>
		}

		if d.ActiveTab == "users" {
			<h2>Users</h2>
			<table>
				<tr><th>Username</th><th>Role</th><th></th></tr>
				for _, u := range d.Users {
					<tr>
						<td>{ u.Username }</td>
						<td>{ u.Role }</td>
						<td>
							<form method="post" action={ templ.URL(fmt.Sprintf("/settings/users/%d/delete", u.ID)) } class="inline">
								<button type="submit">delete</button>
							</form>
						</td>
					</tr>
				}
			</table>
			<form method="post" action="/settings/users" class="auth-card">
				<label>Username <input type="text" name="username" required/></label>
				<label>Password <input type="password" name="password" minlength="6" required/></label>
				<label>Role
					<select name="role">
						<option value="viewer">viewer</option>
						<option value="admin">admin</option>
					</select>
				</label>
				<button type="submit">Add user</button>
			</form>
		}

		if d.ActiveTab == "general" {
			<h2>General</h2>
			<form method="post" action="/settings/general" class="auth-card">
				<label>Offline after (missed scans) <input type="number" name="offline_after" min="1" max="10" value={ generalOfflineAfter(d.Values) }/></label>
				<button type="submit">Save</button>
			</form>
		}
	}
}

func generalOfflineAfter(values map[string]string) string {
	if v := values["offline_after"]; v != "" {
		return v
	}
	return "3"
}
```

- [ ] **Step 7: Add tab CSS**

Append to `internal/web/static/app.css`:

```css
.tabs { display:flex; gap:.25rem; border-bottom:1px solid var(--free); margin-bottom:1rem; }
.tab { padding:.5rem 1rem; color:var(--muted); text-decoration:none; border-bottom:2px solid transparent; }
.tab.active { color:var(--fg); border-bottom-color:var(--ok); }
.detected { display:block; margin:.25rem 0; }
```

- [ ] **Step 8: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS. (The existing `TestPiholeSecretNeverEchoedAndBlankKeeps` still passes — it now loads `?tab` defaulting to subnets, but posts to `/settings/integrations` which is unchanged; if that test asserts the *page* contains the integrations fields, update it to request `/settings?tab=integrations`.)

- [ ] **Step 9: Commit**

```bash
git add internal/web/
git commit -m "feat(web): tabbed settings, detected-subnet add rows, shared integration save"
```

---

### Task 3: First-run quick-setup wizard

**Files:**
- Create: `internal/web/welcome.go`, `internal/web/views/welcome.templ`, `internal/web/welcome_test.go`
- Modify: `internal/web/auth.go` (onboarding redirect in `requireAuth`), `internal/web/server.go` (welcome routes)

**Interfaces:**
- Consumes: `s.detect` (Task 2), `s.saveIntegrationSettings` (Task 2), `views.integrationsFields` (Task 2), `netdetect.Detected` (Task 1), `store.CreateSubnet`, `store.ListSubnets`, `store.GetSetting`/`SetSetting`.
- Produces: routes `GET /welcome`, `POST /welcome/subnets`, `GET /welcome/integrations`, `POST /welcome/integrations`, `POST /welcome/skip`; the `onboarded` gating.

- [ ] **Step 1: Write the failing tests**

`internal/web/welcome_test.go`:

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"netis/internal/netdetect"
)

// Note: authedGet/authedPost and testServer are defined in the package's other
// _test.go files (dashboard_test.go, devices_test.go, auth_test.go).

func TestOnboardingRedirect(t *testing.T) {
	srv, st := testServer(t)
	srv.detect = func() ([]netdetect.Detected, error) { return nil, nil }
	// Un-onboarded authed user hitting "/" is redirected to /welcome.
	rec := authedGet(t, srv, st, "/")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome" {
		t.Fatalf("want redirect to /welcome, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// /welcome itself is reachable (not redirected).
	rec = authedGet(t, srv, st, "/welcome")
	if rec.Code != 200 {
		t.Fatalf("/welcome code=%d", rec.Code)
	}
	// After onboarding, "/" is served normally.
	st.SetSetting("onboarded", "1")
	rec = authedGet(t, srv, st, "/")
	if rec.Code != 200 {
		t.Fatalf("post-onboarding / code=%d", rec.Code)
	}
}

func TestWelcomeShowsDetectedAndCreates(t *testing.T) {
	srv, st := testServer(t)
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{{CIDR: "192.168.5.0/24", Iface: "eth0"}}, nil
	}
	rec := authedGet(t, srv, st, "/welcome")
	if !strings.Contains(rec.Body.String(), "192.168.5.0/24") {
		t.Fatal("welcome should list the detected subnet")
	}
	// Submit the checked subnet.
	rec = authedPost(t, srv, st, "/welcome/subnets", url.Values{"subnet": {"192.168.5.0/24|eth0"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome/integrations" {
		t.Fatalf("subnets post: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	subnets, _ := st.ListSubnets()
	if len(subnets) != 1 || subnets[0].CIDR != "192.168.5.0/24" || subnets[0].Name != "eth0" ||
		!subnets[0].ScanEnabled || subnets[0].ScanIntervalSec != 120 || subnets[0].Kind != "lan" {
		t.Fatalf("subnet not created with defaults: %+v", subnets)
	}
}

func TestWelcomeSkipSetsOnboarded(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/welcome/skip", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("skip: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if v, _ := st.GetSetting("onboarded"); v != "1" {
		t.Fatalf("onboarded not set: %q", v)
	}
}

var _ = httptest.NewRequest // keep httptest imported if unused elsewhere
```

(If `httptest` ends up used by the helpers already, drop the trailing `var _` line.)

- [ ] **Step 2: Run tests, verify failure**

Run: `templ generate && go test ./internal/web/ -run 'Onboarding|Welcome' -count=1`
Expected: FAIL — `/welcome` 404 / no onboarding redirect.

- [ ] **Step 3: Add the onboarding redirect to requireAuth**

In `internal/web/auth.go`, inside `requireAuth`, replace the valid-session branch so it enforces onboarding:

```go
		if c, err := r.Cookie("netis_session"); err == nil {
			if u, ok, _ := s.store.GetSession(c.Value); ok {
				if !onboardingAllowed(r.URL.Path) {
					if v, _ := s.store.GetSetting("onboarded"); v != "1" {
						http.Redirect(w, r, "/welcome", http.StatusSeeOther)
						return
					}
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
```

And add the helper (in `auth.go`):

```go
// onboardingAllowed reports whether a path is reachable before onboarding is
// complete (so the wizard and logout don't get caught by the redirect).
func onboardingAllowed(path string) bool {
	return path == "/welcome" || strings.HasPrefix(path, "/welcome/") || path == "/logout"
}
```

(`/healthz`, `/login`, `/setup`, `/static/` are already handled by the skip list at the top of `requireAuth`, so they don't need to appear here.)

- [ ] **Step 4: Implement the wizard handlers**

`internal/web/welcome.go`:

```go
package web

import (
	"net/http"
	"net/netip"
	"strings"

	"netis/internal/netdetect"
	"netis/internal/store"
	"netis/internal/web/views"
)

func (s *Server) availableDetected() []netdetect.Detected {
	detected, err := s.detect()
	if err != nil {
		return nil
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return nil
	}
	have := make(map[string]bool, len(subnets))
	for _, sn := range subnets {
		have[sn.CIDR] = true
	}
	var out []netdetect.Detected
	for _, d := range detected {
		if !have[d.CIDR] {
			out = append(out, d)
		}
	}
	return out
}

func (s *Server) handleWelcome(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	views.WelcomeSubnets(u.Username, s.availableDetected()).Render(r.Context(), w)
}

func (s *Server) createDetectedSubnet(cidr, iface string) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return
	}
	name := iface
	if name == "" {
		name = prefix.Masked().String()
	}
	s.store.CreateSubnet(store.Subnet{
		CIDR: prefix.Masked().String(), Name: name, Kind: "lan",
		ScanEnabled: true, ScanIntervalSec: 120,
	})
}

func (s *Server) handleWelcomeSubnets(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	for _, v := range r.Form["subnet"] {
		cidr, iface, _ := strings.Cut(v, "|")
		s.createDetectedSubnet(cidr, iface)
	}
	if m := strings.TrimSpace(r.FormValue("manual_cidr")); m != "" {
		s.createDetectedSubnet(m, "")
	}
	http.Redirect(w, r, "/welcome/integrations", http.StatusSeeOther)
}

func (s *Server) handleWelcomeIntegrationsPage(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	views.WelcomeIntegrations(u.Username, map[string]string{}).Render(r.Context(), w)
}

func (s *Server) handleWelcomeIntegrations(w http.ResponseWriter, r *http.Request) {
	if err := s.saveIntegrationSettings(r); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.finishOnboarding(w, r)
}

func (s *Server) handleWelcomeSkip(w http.ResponseWriter, r *http.Request) {
	s.finishOnboarding(w, r)
}

func (s *Server) finishOnboarding(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSetting("onboarded", "1"); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
```

- [ ] **Step 5: Implement the wizard templates**

`internal/web/views/welcome.templ`:

```templ
package views

import "netis/internal/netdetect"

templ WelcomeSubnets(username string, detected []netdetect.Detected) {
	@Layout("Welcome", username) {
		<h1>Welcome to netis</h1>
		<p>Step 1 of 2 — pick the subnets to scan.</p>
		<form method="post" action="/welcome/subnets" class="auth-card">
			if len(detected) == 0 {
				<p class="muted">No subnets auto-detected. Add one manually below.</p>
			} else {
				<p>Detected on this host:</p>
				for _, d := range detected {
					<label>
						<input type="checkbox" name="subnet" value={ d.CIDR + "|" + d.Iface } checked/>
						<span class="mono">{ d.CIDR }</span> <span class="muted">({ d.Iface })</span>
					</label>
				}
			}
			<label>Add another CIDR <input type="text" name="manual_cidr" placeholder="192.168.1.0/24"/></label>
			<button type="submit">Continue</button>
		</form>
	}
}

templ WelcomeIntegrations(username string, values map[string]string) {
	@Layout("Welcome", username) {
		<h1>Welcome to netis</h1>
		<p>Step 2 of 2 — connect an integration (optional).</p>
		<form method="post" action="/welcome/integrations" class="auth-card">
			@integrationsFields(values)
			<button type="submit">Save &amp; finish</button>
		</form>
		<form method="post" action="/welcome/skip">
			<button type="submit">Skip for now</button>
		</form>
	}
}
```

- [ ] **Step 6: Register the routes**

In `internal/web/server.go` `NewServer`, add (near the dashboard routes):

```go
	s.mux.HandleFunc("GET /welcome", s.handleWelcome)
	s.mux.HandleFunc("POST /welcome/subnets", s.handleWelcomeSubnets)
	s.mux.HandleFunc("GET /welcome/integrations", s.handleWelcomeIntegrationsPage)
	s.mux.HandleFunc("POST /welcome/integrations", s.handleWelcomeIntegrations)
	s.mux.HandleFunc("POST /welcome/skip", s.handleWelcomeSkip)
```

(These are auth-gated by the outer `requireAuth`; they are admin-only in practice because onboarding runs before any second user exists, and `requireAuth` allows `/welcome*` through the onboarding gate.)

- [ ] **Step 7: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web/
git commit -m "feat(web): first-run quick-setup wizard with subnet detection"
```

---

## Final verification (after Task 3)

- [ ] `CGO_ENABLED=0 go test ./... -count=1` — all green.
- [ ] `go test -race ./internal/web/ ./internal/netdetect/ -count=1` — clean.
- [ ] `CGO_ENABLED=0 go build -o netis ./cmd/netis` — builds.
- [ ] Manual smoke: fresh DB → `/setup` creates admin → login → redirected to `/welcome` → detected subnet(s) listed → Continue → integrations step → Skip → dashboard now shows the created subnet. Revisit `/settings` → tabs switch; Subnets tab shows detected-minus-existing.
- [ ] Use superpowers:finishing-a-development-branch.
