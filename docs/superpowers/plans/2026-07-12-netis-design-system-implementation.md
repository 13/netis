# Design System Foundation (Sub-project A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace netis's ad-hoc dark-only stylesheet with a token-based light/dark design system, add a persisted theme toggle (default dark), and redesign the dashboard subnet card — without changing the app's no-build, server-rendered model.

**Architecture:** `app.css` is rewritten as CSS custom-property tokens with light/dark palettes (`data-theme` + `prefers-color-scheme`). A no-flash inline bootstrap plus a small embedded `theme.js` drive the toggle and active-nav. The dashboard subnet card gains an occupancy bar fed by two new computed counts on the view-model. No store/route/migration changes.

**Tech Stack:** Go 1.26, templ, HTMX (unchanged), plain CSS + ~25 lines of vanilla JS.

**Spec:** `docs/superpowers/specs/2026-07-12-netis-design-system-design.md`. **Visual reference:** the style tile at `<scratchpad>/netis-styletile.html` (published artifact) — match its palette, spacing, and components.

## Global Constraints

- Go module `netis`, `go1.26.5`, `CGO_ENABLED=0`. Build `CGO_ENABLED=0 go build ./...`; test `go test ./... -count=1`.
- **templ:** CLI at `/home/ben/go/bin/templ` (v0.3.1020; run `export PATH="$PATH:$(go env GOPATH)/bin"` first). After editing any `.templ`, run `templ generate` then build; commit BOTH the `.templ` and generated `*_templ.go`. (Editing `.css`/`.js` needs no generate — they are embedded static assets served as-is.)
- Token values are the exact hexes in the spec's token table (light `:root`; dark under `@media (prefers-color-scheme: dark)`, `:root[data-theme="dark"]`, and `:root[data-theme="light"]`). Components reference tokens only — never redefine a token inside a component rule.
- Default theme is **dark**: when nothing is stored, use dark unless the OS explicitly prefers light (`matchMedia('(prefers-color-scheme: light)')`).
- Device-kind → emoji map (exactly netis's 10 kinds): `computer 💻, switch 🔀, phone 📱, server 🖥️, printer 🖨️, iot 💡, vm 🧊, lxc 📦, wg-peer 🔒, other ❓`.
- Preserve every existing template's class names so all pages restyle by cascade with no markup edits (except the two files this plan explicitly changes: `layout.templ`, `dashboard.templ`).
- Commit after each task; conventional-commit, body ending with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Shell prints harmless zsh-rc noise on stderr (`command not found: z`); ignore — exit codes are correct.

---

### Task 1: Token-based `app.css` + component set + kind-icon map

**Files:**
- Modify: `internal/web/static/app.css` (full rewrite)
- Create: `internal/web/views/icons.go`, `internal/web/views/icons_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: the full token system + component classes used by every page and (later) B/C; `func views.KindIcon(kind string) string` returning the kind's emoji (default `❓`).

- [ ] **Step 1: Write the failing test**

`internal/web/views/icons_test.go`:

```go
package views

import "testing"

func TestKindIcon(t *testing.T) {
	cases := map[string]string{
		"phone": "📱", "computer": "💻", "server": "🖥️",
		"switch": "🔀", "printer": "🖨️", "iot": "💡",
		"vm": "🧊", "lxc": "📦", "wg-peer": "🔒", "other": "❓",
		"nonsense": "❓", "": "❓",
	}
	for kind, want := range cases {
		if got := KindIcon(kind); got != want {
			t.Errorf("KindIcon(%q) = %q, want %q", kind, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/web/views/ -run KindIcon -count=1`
Expected: FAIL — `undefined: KindIcon`.

- [ ] **Step 3: Implement the icon map**

`internal/web/views/icons.go`:

```go
package views

// kindIcons maps a device kind to its default emoji. The device dialog
// (sub-project C) lets the user override; this is the fallback shown in the
// device list, grid, and detail views.
var kindIcons = map[string]string{
	"computer": "💻",
	"switch":   "🔀",
	"phone":    "📱",
	"server":   "🖥️",
	"printer":  "🖨️",
	"iot":      "💡",
	"vm":       "🧊",
	"lxc":      "📦",
	"wg-peer":  "🔒",
	"other":    "❓",
}

// KindIcon returns the default emoji for a device kind, or ❓ if unknown.
func KindIcon(kind string) string {
	if e, ok := kindIcons[kind]; ok {
		return e
	}
	return "❓"
}
```

- [ ] **Step 4: Rewrite `app.css`**

Replace `internal/web/static/app.css` entirely with the token system + components. This styles the existing class names AND the new nav/components Task 2/3 use:

```css
/* ---- tokens ---- */
:root {
  --bg:#eef1f5; --surface:#ffffff; --surface-2:#f3f6f9; --border:#d5dce4;
  --border-strong:#c2ccd6; --fg:#141a21; --muted:#5a6674;
  --accent:#0e8ba8; --accent-fg:#ffffff; --accent-soft:#d5f0f7;
  --ok:#1a7f37; --ok-soft:#d6f2dd; --bad:#c8202f; --bad-soft:#fbe0e2;
  --warn:#8a5a00; --warn-soft:#f7ead0;
  --shadow:0 1px 2px rgba(16,24,32,.06), 0 4px 16px rgba(16,24,32,.06);
  --radius:10px; --radius-sm:6px;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg:#0b0f14; --surface:#131a22; --surface-2:#1a232d; --border:#26313d;
    --border-strong:#34414f; --fg:#e6edf3; --muted:#8a97a6;
    --accent:#38bdf8; --accent-fg:#04141d; --accent-soft:#0e2b3a;
    --ok:#3fb950; --ok-soft:#10261a; --bad:#f85149; --bad-soft:#2c1416;
    --warn:#d8a12b; --warn-soft:#2b2411;
    --shadow:0 1px 2px rgba(0,0,0,.4), 0 6px 20px rgba(0,0,0,.3);
  }
}
:root[data-theme="dark"] {
  --bg:#0b0f14; --surface:#131a22; --surface-2:#1a232d; --border:#26313d;
  --border-strong:#34414f; --fg:#e6edf3; --muted:#8a97a6;
  --accent:#38bdf8; --accent-fg:#04141d; --accent-soft:#0e2b3a;
  --ok:#3fb950; --ok-soft:#10261a; --bad:#f85149; --bad-soft:#2c1416;
  --warn:#d8a12b; --warn-soft:#2b2411;
  --shadow:0 1px 2px rgba(0,0,0,.4), 0 6px 20px rgba(0,0,0,.3);
}
:root[data-theme="light"] {
  --bg:#eef1f5; --surface:#ffffff; --surface-2:#f3f6f9; --border:#d5dce4;
  --border-strong:#c2ccd6; --fg:#141a21; --muted:#5a6674;
  --accent:#0e8ba8; --accent-fg:#ffffff; --accent-soft:#d5f0f7;
  --ok:#1a7f37; --ok-soft:#d6f2dd; --bad:#c8202f; --bad-soft:#fbe0e2;
  --warn:#8a5a00; --warn-soft:#f7ead0;
  --shadow:0 1px 2px rgba(16,24,32,.06), 0 4px 16px rgba(16,24,32,.06);
}

/* ---- base ---- */
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--fg);
       font:14px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif;
       -webkit-font-smoothing:antialiased; }
.mono { font-family:ui-monospace,"SF Mono",Menlo,monospace; font-variant-numeric:tabular-nums; }
.muted { color:var(--muted); }
.ok { color:var(--ok); } .error { color:var(--bad); }
h1,h2,h3 { letter-spacing:-.01em; }
a { color:var(--accent); }

/* ---- nav ---- */
nav.top { position:sticky; top:0; z-index:10; display:flex; align-items:center; gap:20px;
          height:54px; padding:0 20px; background:color-mix(in srgb, var(--surface) 90%, transparent);
          backdrop-filter:blur(8px); border-bottom:1px solid var(--border); }
nav.top .brand { display:flex; align-items:center; gap:9px; font-weight:680; color:var(--fg); text-decoration:none; }
nav.top .brand .dot { width:9px; height:9px; border-radius:50%; background:var(--accent); box-shadow:0 0 0 3px var(--accent-soft); }
nav.top .links { display:flex; gap:4px; }
nav.top .links a { color:var(--muted); text-decoration:none; font-weight:520; font-size:13.5px; padding:6px 11px; border-radius:var(--radius-sm); }
nav.top .links a:hover { color:var(--fg); }
nav.top .links a.active { color:var(--fg); background:var(--surface-2); }
nav.top .right { margin-left:auto; display:flex; align-items:center; gap:12px; }
nav.top .who { font-size:12.5px; color:var(--muted); }
.theme-toggle { display:inline-flex; align-items:center; gap:7px; cursor:pointer; background:var(--surface-2);
                border:1px solid var(--border); color:var(--fg); border-radius:999px; padding:5px 11px; font-size:12.5px; font-weight:540; }
.theme-toggle:hover { border-color:var(--border-strong); }
.logout { display:inline; }
.logout button { padding:5px 11px; font-size:12.5px; }

main { padding:20px; max-width:1080px; margin:0 auto; }

/* ---- cards / stats / panels ---- */
.cards, .grid2 { display:grid; grid-template-columns:repeat(auto-fill,minmax(300px,1fr)); gap:14px; }
.card { background:var(--surface); border:1px solid var(--border); border-radius:var(--radius);
        padding:15px 16px; box-shadow:var(--shadow); color:inherit; text-decoration:none; display:block; }
a.card { transition:border-color .12s, transform .12s; }
a.card:hover { border-color:var(--accent); transform:translateY(-1px); }
.card h2, .card h3 { margin:0 0 2px; font-size:15px; }
.stats { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:12px; margin-bottom:18px; }
.stat { background:var(--surface); border:1px solid var(--border); border-radius:var(--radius); padding:13px 15px; box-shadow:var(--shadow); }
.stat .num { display:block; font-size:26px; font-weight:700; letter-spacing:-.02em; font-variant-numeric:tabular-nums; }
.stat .num.ok { color:var(--ok); } .stat .num.warn { color:var(--warn); }
.band { display:grid; grid-template-columns:repeat(auto-fit,minmax(300px,1fr)); gap:14px; margin-bottom:18px; }
.panel { background:var(--surface); border:1px solid var(--border); border-radius:var(--radius); padding:15px 16px; box-shadow:var(--shadow); }
.panel h2 { margin-top:0; font-size:15px; }
.panel ul { margin:0; padding-left:1.1rem; }

/* ---- occupancy bar (dashboard subnet card) ---- */
.occ { display:flex; height:8px; border-radius:4px; overflow:hidden; margin:12px 0 10px;
       background:var(--surface-2); border:1px solid var(--border); }
.occ i { display:block; height:100%; }
.occ .on { background:var(--ok); } .occ .off { background:var(--border-strong); } .occ .res { background:var(--warn); }
.legend { display:flex; flex-wrap:wrap; gap:14px; font-size:12px; color:var(--muted); }
.legend b { color:var(--fg); font-variant-numeric:tabular-nums; }
.legend .sw { display:inline-block; width:8px; height:8px; border-radius:2px; margin-right:5px; vertical-align:middle; }

/* ---- pills / chips / tags ---- */
.pill { display:inline-flex; align-items:center; gap:5px; font-size:11.5px; font-weight:560; padding:2px 8px 2px 7px; border-radius:999px; }
.pill .d { width:6px; height:6px; border-radius:50%; }
.pill.online { color:var(--ok); background:var(--ok-soft); } .pill.online .d { background:var(--ok); }
.pill.offline { color:var(--muted); background:var(--surface-2); } .pill.offline .d { background:var(--border-strong); }
.pill.reserved { color:var(--warn); background:var(--warn-soft); } .pill.reserved .d { background:var(--warn); }
.chip { display:inline-flex; align-items:center; font-size:11px; font-weight:560; padding:1px 7px; border-radius:var(--radius-sm);
        border:1px solid var(--border); color:var(--muted); background:var(--surface-2); }
.chip.static { color:var(--accent); border-color:color-mix(in srgb, var(--accent) 40%, var(--border)); background:var(--accent-soft); }
.tag { display:inline-flex; font-size:11px; padding:1px 8px; border-radius:999px; background:var(--surface-2); border:1px solid var(--border); color:var(--muted); }

/* ---- event-type badges (dashboard + events pages) ---- */
.badge { display:inline-flex; align-items:center; padding:1px 8px; border-radius:999px; font-size:11.5px; font-weight:540;
         background:var(--surface-2); border:1px solid var(--border); color:var(--muted); }
.badge.online { color:var(--ok); background:var(--ok-soft); }
.badge.offline { color:var(--bad); background:var(--bad-soft); }
.badge.device_new { color:var(--warn); background:var(--warn-soft); }
.badge.scan_error { color:var(--bad); background:var(--bad-soft); }
.badge.ip_changed { color:var(--accent); background:var(--accent-soft); }

/* ---- buttons / inputs ---- */
button, .btn { cursor:pointer; border:1px solid var(--border); background:var(--surface); color:var(--fg);
               border-radius:var(--radius-sm); padding:6px 12px; font-size:13px; font-weight:540; }
button:hover, .btn:hover { border-color:var(--border-strong); }
.btn.primary, button.primary { background:var(--accent); border-color:var(--accent); color:var(--accent-fg); }
.btn.primary:hover, button.primary:hover { filter:brightness(1.06); border-color:var(--accent); }
input, select, textarea { background:var(--surface-2); border:1px solid var(--border); border-radius:var(--radius-sm);
                          padding:7px 10px; color:var(--fg); font:inherit; font-size:13.5px; outline:none; }
input:focus, select:focus, textarea:focus { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-soft); }
form.inline, .inline { display:inline-block; margin-right:.4rem; }

/* ---- segmented control / icon tile (used by B and C) ---- */
.seg { display:inline-flex; border:1px solid var(--border); border-radius:var(--radius-sm); overflow:hidden; }
.seg button { border:0; border-radius:0; background:var(--surface); color:var(--muted); padding:5px 10px; }
.seg button.on { background:var(--accent-soft); color:var(--accent); }
.ic { width:30px; height:30px; border-radius:8px; background:var(--surface-2); border:1px solid var(--border);
      display:inline-grid; place-items:center; font-size:15px; flex:none; }

/* ---- tables ---- */
table { width:100%; border-collapse:collapse; }
thead th { text-align:left; font-size:11px; letter-spacing:.05em; text-transform:uppercase; color:var(--muted);
           font-weight:600; padding:9px 12px; background:var(--surface-2); border-bottom:1px solid var(--border); }
td, th { padding:9px 12px; border-bottom:1px solid var(--border); text-align:left; font-size:13.5px; }
tbody tr:hover { background:var(--surface-2); }

/* ---- fieldsets (settings integrations) ---- */
fieldset { border:1px solid var(--border); border-radius:var(--radius); padding:14px 16px; margin:0 0 14px; }
legend { padding:0 6px; font-size:12px; font-weight:600; color:var(--muted); text-transform:uppercase; letter-spacing:.04em; }
label { display:flex; flex-direction:column; gap:5px; font-size:12px; color:var(--muted); margin-bottom:10px; }
label input, label select { color:var(--fg); }

/* ---- subnet grid squares ---- */
.grid { display:grid; grid-template-columns:repeat(16,26px); gap:4px; }
.sq { width:26px; height:26px; border-radius:5px; background:var(--surface-2); border:1px solid var(--border); display:block; }
.sq.online { background:var(--ok); border-color:var(--ok); }
.sq.offline { background:var(--border-strong); border-color:var(--border-strong); }
.sq.reserved { background:var(--warn); border-color:var(--warn); }
.sq.conflict { background:var(--bad); border-color:var(--bad); }

/* ---- tabs (settings) ---- */
.tabs { display:flex; gap:2px; border-bottom:1px solid var(--border); margin-bottom:18px; }
.tab { padding:8px 14px; color:var(--muted); text-decoration:none; border-bottom:2px solid transparent; font-weight:520; font-size:13.5px; }
.tab:hover { color:var(--fg); }
.tab.active { color:var(--fg); border-bottom-color:var(--accent); }
.detected { display:block; margin:.35rem 0; }

/* ---- auth / wizard card ---- */
.auth { display:grid; place-items:center; min-height:100vh; }
.auth-card { background:var(--surface); border:1px solid var(--border); box-shadow:var(--shadow);
             padding:22px; border-radius:var(--radius); display:flex; flex-direction:column; gap:12px; width:320px; max-width:100%; }

/* ---- dialog shell (used by C) ---- */
.dialog-scrim { position:fixed; inset:0; background:rgba(4,10,16,.55); display:grid; place-items:center; padding:20px; z-index:50; }
.dialog { width:100%; max-width:460px; background:var(--surface); border:1px solid var(--border-strong);
          border-radius:12px; box-shadow:0 12px 40px rgba(0,0,0,.35); overflow:hidden; }
.dialog .dh { display:flex; align-items:center; justify-content:space-between; padding:14px 16px; border-bottom:1px solid var(--border); }
.dialog .dh h3 { margin:0; font-size:15px; }
.dialog .db { padding:16px; display:flex; flex-direction:column; gap:13px; }
.dialog .df { display:flex; justify-content:flex-end; gap:9px; padding:13px 16px; border-top:1px solid var(--border); background:var(--surface-2); }

@media (prefers-reduced-motion: reduce) {
  a.card, .theme-toggle, .btn, button { transition:none; }
}
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/web/views/ -run KindIcon -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS. (No template changed, so existing web tests are unaffected; the CSS is served as-is.)

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/app.css internal/web/views/icons.go internal/web/views/icons_test.go
git commit -m "feat(web): token-based design-system css, components, and kind-icon map"
```

---

### Task 2: Theme shell — nav, no-flash bootstrap, toggle, active-nav

**Files:**
- Modify: `internal/web/views/layout.templ`
- Create: `internal/web/static/theme.js`
- Test: `internal/web/dashboard_test.go` (add a case)

**Interfaces:**
- Consumes: the `.theme-toggle`, `nav.top` styles (Task 1).
- Produces: the restyled shell; the `data-theme` bootstrap; `theme.js` (toggle + active-nav). Every page rendered through `Layout` gets the new nav and theme behavior.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/dashboard_test.go`:

```go
func TestLayoutHasThemeToggleAndBootstrap(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedGet(t, srv, st, "/")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	for _, want := range []string{
		`id="theme-toggle"`,          // the toggle control
		`data-theme`,                 // the no-flash bootstrap sets it
		`/static/theme.js`,           // toggle + active-nav script
		`class="brand"`,              // restyled nav
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard layout missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -run LayoutHasThemeToggle -count=1`
Expected: FAIL — markup not present yet.

- [ ] **Step 3: Rewrite `layout.templ`**

Replace `internal/web/views/layout.templ`:

```templ
package views

templ Layout(title string, username string) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ title } — netis</title>
			<script>
				// No-flash theme bootstrap: set data-theme before first paint.
				// Default dark unless a stored choice or an explicit OS light preference says otherwise.
				(function () {
					try {
						var t = localStorage.getItem('netis-theme');
						if (!t) { t = matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'; }
						document.documentElement.setAttribute('data-theme', t);
					} catch (e) {
						document.documentElement.setAttribute('data-theme', 'dark');
					}
				})();
			</script>
			<link rel="stylesheet" href="/static/app.css"/>
			<script src="/static/htmx.min.js"></script>
			<script src="/static/sse.js"></script>
		</head>
		<body hx-ext="sse" sse-connect="/events/stream">
			<nav class="top">
				<a href="/" class="brand"><span class="dot"></span> netis</a>
				<div class="links">
					<a href="/">Dashboard</a>
					<a href="/devices">Devices</a>
					<a href="/events">Events</a>
					<a href="/settings">Settings</a>
				</div>
				<div class="right">
					<span class="who">{ username }</span>
					<button type="button" id="theme-toggle" class="theme-toggle" aria-label="Toggle theme">
						<span class="tglabel">Theme</span>
					</button>
					<form method="post" action="/logout" class="logout">
						<button type="submit">Logout</button>
					</form>
				</div>
			</nav>
			<main>
				{ children... }
			</main>
			<script src="/static/theme.js"></script>
		</body>
	</html>
}
```

- [ ] **Step 4: Create `theme.js`**

`internal/web/static/theme.js`:

```js
(function () {
	function current() {
		return document.documentElement.getAttribute('data-theme') ||
			(matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
	}
	function apply(t) {
		document.documentElement.setAttribute('data-theme', t);
		try { localStorage.setItem('netis-theme', t); } catch (e) {}
		var l = document.querySelector('#theme-toggle .tglabel');
		if (l) { l.textContent = t === 'dark' ? 'Dark' : 'Light'; }
	}
	var btn = document.getElementById('theme-toggle');
	if (btn) {
		apply(current());
		btn.addEventListener('click', function () { apply(current() === 'dark' ? 'light' : 'dark'); });
	}
	// active-nav: longest-prefix match ("/" only exact) so no server change is needed.
	var path = location.pathname;
	document.querySelectorAll('nav.top .links a').forEach(function (a) {
		var href = a.getAttribute('href');
		var active = href === '/' ? path === '/' : path.indexOf(href) === 0;
		if (active) { a.classList.add('active'); }
	});
})();
```

(`theme.js` is picked up automatically by the existing `//go:embed static` in `server.go`; no Go change.)

- [ ] **Step 5: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/views/layout.templ internal/web/views/layout_templ.go internal/web/static/theme.js internal/web/dashboard_test.go
git commit -m "feat(web): themed nav shell with persisted dark/light toggle"
```

---

### Task 3: Redesigned dashboard subnet card

**Files:**
- Modify: `internal/web/dashboard.go`, `internal/web/views/dashboard.templ`, `internal/web/views/helpers.go`
- Test: `internal/web/dashboard_test.go` (add a case)

**Interfaces:**
- Consumes: `store.SubnetOccupancy` (`Occupant.Online`, `Occupant.EverSeen`), `scan.HostIPs`, the `.occ`/`.legend` CSS (Task 1).
- Produces: `views.DashRow` gains `Reserved int`, `Offline int`, `Hosts int`; `views.barPct(n, total int) string`; the occupancy-bar subnet card.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/dashboard_test.go`:

```go
func TestSubnetCardOccupancyCounts(t *testing.T) {
	srv, st := testServer(t)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	mk := func(name, ip string) int64 {
		devID, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		ifID, _ := st.AddIface(devID, nil, nil)
		st.AssignIP(ifID, snID, ip, "static")
		return ifID
	}
	onIf := mk("on", "10.0.0.1")
	st.MarkSeen(onIf, 1, time.Now())            // online
	offIf := mk("off", "10.0.0.2")
	st.MarkSeen(offIf, 1, time.Now())
	st.MarkMissed(offIf, 1)                     // seen then offline
	mk("res", "10.0.0.3")                       // assigned, never seen → reserved

	data, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	var row views.DashRow
	for _, r := range data.Rows {
		if r.Subnet.ID == snID {
			row = r
		}
	}
	if row.Online != 1 || row.Reserved != 1 || row.Offline != 1 {
		t.Fatalf("counts: online=%d reserved=%d offline=%d (want 1/1/1)", row.Online, row.Reserved, row.Offline)
	}
	// /29 has 6 host IPs; 3 used → 3 free; Hosts=6.
	if row.Hosts != 6 || row.Free != 3 || row.Used != 3 {
		t.Fatalf("hosts=%d used=%d free=%d (want 6/3/3)", row.Hosts, row.Used, row.Free)
	}
}

func TestBarPct(t *testing.T) {
	if got := views.BarPct(1, 4); got != "25%" {
		t.Errorf("BarPct(1,4)=%q want 25%%", got)
	}
	if got := views.BarPct(3, 0); got != "0%" {
		t.Errorf("BarPct(3,0)=%q want 0%%", got)
	}
}
```

(Add imports `"net/http/httptest"`, `"time"`, `"netis/internal/store"`, `"netis/internal/web/views"` to the test file if not already present.)

- [ ] **Step 2: Run test, verify failure**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -run 'SubnetCardOccupancy|BarPct' -count=1`
Expected: FAIL — `DashRow` has no `Reserved`/`Hosts`, `BarPct` undefined.

- [ ] **Step 3: Compute the counts in the handler**

In `internal/web/dashboard.go`, in `assembleDashboard`'s per-subnet block, replace the occupancy loop and the `DashRow` construction:

```go
		hosts, _ := scan.HostIPs(sn.CIDR)
		free := len(hosts) - len(occ)
		if free < 0 {
			free = 0
		}
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: free, Hosts: len(hosts)}
		seen := make(map[string]bool)
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
				data.Conflicts = append(data.Conflicts, views.AttentionConflict{
					IP: ip, SubnetID: sn.ID, SubnetName: sn.Name,
				})
			}
			if !seen[o.DeviceName] {
				seen[o.DeviceName] = true
				row.Occupants = append(row.Occupants, o.DeviceName)
			}
		}
		sort.Strings(row.Occupants)
		data.Rows = append(data.Rows, row)
```

- [ ] **Step 4: Add fields + `BarPct`**

In `internal/web/views/dashboard.templ`, extend `DashRow`:

```go
type DashRow struct {
	Subnet    store.Subnet
	Online    int
	Reserved  int
	Offline   int
	Used      int
	Free      int
	Hosts     int
	Occupants []string
}
```

In `internal/web/views/helpers.go`, add:

```go
// BarPct renders n as a whole-percent width of total (e.g. "25%"), 0% when
// total is zero. Used for the dashboard subnet occupancy bar.
func BarPct(n, total int) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", n*100/total)
}
```

(Ensure `helpers.go` imports `"fmt"` — `relTime` already uses it.)

- [ ] **Step 5: Render the occupancy bar**

In `internal/web/views/dashboard.templ`, replace the subnet-cards block (the `<h2>Subnets</h2>` … `</div>` section) with:

```templ
	<h2>Subnets</h2>
	<div class="cards">
		for _, r := range d.Rows {
			<a class="card" href={ templ.URL(fmt.Sprintf("/subnets/%d", r.Subnet.ID)) }>
				<div style="display:flex;align-items:baseline;justify-content:space-between;gap:10px">
					<h3>{ r.Subnet.Name }</h3>
					<span class="mono muted">{ r.Subnet.CIDR }</span>
				</div>
				<div class="occ">
					<i class="on" style={ "width:" + BarPct(r.Online, r.Hosts) }></i>
					<i class="res" style={ "width:" + BarPct(r.Reserved, r.Hosts) }></i>
					<i class="off" style={ "width:" + BarPct(r.Offline, r.Hosts) }></i>
				</div>
				<div class="legend">
					<span><span class="sw" style="background:var(--ok)"></span><b>{ fmt.Sprint(r.Online) }</b> online</span>
					<span><span class="sw" style="background:var(--warn)"></span><b>{ fmt.Sprint(r.Reserved) }</b> reserved</span>
					<span><span class="sw" style="background:var(--border-strong)"></span><b>{ fmt.Sprint(r.Free) }</b> free</span>
				</div>
			</a>
		}
	</div>
```

(`strings` may become unused in `dashboard.templ` if the old `.occupants` join was its only use. If `templ generate`/`go build` reports `strings` unused, drop it from the import block.)

- [ ] **Step 6: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web/dashboard.go internal/web/views/dashboard.templ internal/web/views/dashboard_templ.go internal/web/views/helpers.go internal/web/dashboard_test.go
git commit -m "feat(web): redesigned subnet card with occupancy bar"
```

---

## Final verification (after Task 3)

- [ ] `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && CGO_ENABLED=0 go test ./... -count=1` — all green.
- [ ] `CGO_ENABLED=0 go build -o netis ./cmd/netis` — builds.
- [ ] **Manual visual smoke (required — this is a visual change):** run the binary on a fresh DB, complete `/setup`+login+wizard, then load Dashboard, Devices, Events, Settings (all tabs). Confirm: default is dark; the top-right toggle flips to light and persists across reloads; the settings/onboarding/dashboard card borders are aligned and consistent; the subnet card shows the occupancy bar; no light-flash on load. Compare against the style tile `<scratchpad>/netis-styletile.html`.
- [ ] Use superpowers:finishing-a-development-branch.
