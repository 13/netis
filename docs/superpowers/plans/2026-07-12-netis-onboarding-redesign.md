# Netis Onboarding Redesign (Sub-project E3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the setup + welcome flow into a focused, modern onboarding experience — a branded shell with a 3-step stepper on a soft accent backdrop — without changing any endpoint or field.

**Architecture:** A shared `onboardShell` templ (brand hero + `onboardStepper` + card) replaces the plain `.auth-card`/`@Layout` wrappers for Setup and both Welcome steps; login gains the same brand hero. All view-function signatures, POST endpoints, and field names stay identical, so handlers and existing tests are untouched.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), HTMX, `go test ./...`.

## Global Constraints

- No new dependencies; no store/handler/schema change; presentation only.
- Every POST endpoint and form field `name` unchanged; `integrationsFields` keeps its `<fieldset><legend>` blocks (the D1 welcome test asserts them).
- Theme-aware, token-driven CSS; no external assets (CSP-safe, all inline); respect `prefers-reduced-motion`.
- Regenerate templ after editing `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` with source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing — do not stage them.

---

### Task 1: Onboarding shell + stepper + brand; Setup + Login

**Files:**
- Modify: `internal/web/views/auth.templ` (`onboardBrand`, `onboardStep`, `onboardStepper`, `onboardShell`; `SetupPage` uses the shell; `LoginPage` gains the brand)
- Modify: `internal/web/static/app.css` (onboarding styles)
- Test: `internal/web/auth_test.go` (add `TestSetupOnboardingChrome`, `TestLoginBrand`)

**Interfaces:**
- Produces: `templ onboardShell(title string, step int)`, `onboardStepper(step int)`, `onboardBrand()` — reused by Task 2.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/auth_test.go` (it already imports `net/http/httptest`, `strings`, `testing`):

```go
func TestSetupOnboardingChrome(t *testing.T) {
	srv, _ := testServer(t) // no users → /setup renders
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/setup", nil))
	if rec.Code != 200 {
		t.Fatalf("setup code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="username"`, `name="password"`, `class="stepper"`, "onboard-brand", "Integrations"} {
		if !strings.Contains(body, want) {
			t.Errorf("setup page missing %q", want)
		}
	}
}

func TestLoginBrand(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st) // users exist → /login renders
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	body := rec.Body.String()
	for _, want := range []string{`name="username"`, "onboard-brand"} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'TestSetupOnboardingChrome|TestLoginBrand' -v`
Expected: FAIL — no `stepper`/`onboard-brand` in the current pages.

- [ ] **Step 3: Add the shell/stepper/brand templs and convert Setup + Login**

In `internal/web/views/auth.templ`, add the import and the new templs, and change
`SetupPage`/`LoginPage`. The file currently starts `package views` then
`templ authShell(...)`. Make the top of the file:

```
package views

import "fmt"

templ onboardBrand() {
	<div class="onboard-brand">
		<span class="mark"><span class="dot"></span></span>
		<div class="wm">netis</div>
		<div class="tag">Your home network, organized.</div>
	</div>
}

templ onboardStep(n, cur int, label string) {
	if n < cur {
		<div class="step done"><span class="num">✓</span><span class="lbl">{ label }</span></div>
	} else if n == cur {
		<div class="step active"><span class="num">{ fmt.Sprint(n) }</span><span class="lbl">{ label }</span></div>
	} else {
		<div class="step"><span class="num">{ fmt.Sprint(n) }</span><span class="lbl">{ label }</span></div>
	}
}

templ onboardStepper(step int) {
	<div class="stepper">
		@onboardStep(1, step, "Account")
		<i class="conn"></i>
		@onboardStep(2, step, "Subnets")
		<i class="conn"></i>
		@onboardStep(3, step, "Integrations")
	</div>
}

templ onboardShell(title string, step int) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ title } — netis</title>
			<script>
				(function () {
					try {
						document.documentElement.setAttribute('data-theme', localStorage.getItem('netis-theme') || 'dark');
					} catch (e) {
						document.documentElement.setAttribute('data-theme', 'dark');
					}
				})();
			</script>
			<link rel="stylesheet" href="/static/app.css"/>
		</head>
		<body class="onboard">
			@onboardBrand()
			@onboardStepper(step)
			<div class="onboard-card">
				{ children... }
			</div>
			<script src="/static/theme.js"></script>
		</body>
	</html>
}
```

Keep the existing `authShell` templ as-is. Replace `SetupPage` with:

```
templ SetupPage(errMsg string) {
	@onboardShell("Setup", 1) {
		<h1>Create your admin account</h1>
		<p class="muted">This is the login you'll use to manage netis.</p>
		if errMsg != "" {
			<p class="error">{ errMsg }</p>
		}
		<form method="post" action="/setup">
			<input name="username" placeholder="username" autofocus required/>
			<input name="password" type="password" placeholder="password (min 8 chars)" required minlength="8"/>
			<button type="submit" class="primary">Create account</button>
		</form>
	}
}
```

Replace `LoginPage` with (brand hero + primary button; `authShell` kept):

```
templ LoginPage(errMsg string) {
	@authShell("Login") {
		@onboardBrand()
		<form method="post" action="/login" class="auth-card">
			if errMsg != "" {
				<p class="error">{ errMsg }</p>
			}
			<input name="username" placeholder="username" autofocus required/>
			<input name="password" type="password" placeholder="password" required/>
			<button type="submit" class="primary">Sign in</button>
		</form>
	}
}
```

- [ ] **Step 4: Add the onboarding CSS**

Append to `internal/web/static/app.css`:

```css
/* ---- onboarding (E3) ---- */
.onboard { min-height:100vh; display:flex; flex-direction:column; align-items:center; gap:22px;
  padding:48px 20px; background:radial-gradient(1100px 520px at 50% -8%, var(--accent-soft), transparent 70%), var(--bg); }
.onboard-brand { display:flex; flex-direction:column; align-items:center; gap:6px; text-align:center; margin-top:6px; }
.onboard-brand .mark { width:44px; height:44px; border-radius:13px; display:grid; place-items:center;
  background:var(--surface); border:1px solid var(--border); box-shadow:var(--shadow); }
.onboard-brand .mark .dot { width:14px; height:14px; border-radius:50%; background:var(--accent); box-shadow:0 0 0 5px var(--accent-soft); }
.onboard-brand .wm { font-size:22px; font-weight:680; letter-spacing:-.01em; }
.onboard-brand .tag { color:var(--muted); font-size:13px; }
.onboard-card { width:100%; max-width:420px; background:var(--surface); border:1px solid var(--border-strong);
  border-radius:var(--radius); box-shadow:var(--shadow-lg); padding:22px; display:flex; flex-direction:column; gap:12px;
  animation:onboardin .22s ease-out; }
.onboard-card h1 { font-size:19px; margin:0; }
.onboard-card form { display:flex; flex-direction:column; gap:11px; margin:0; }
@keyframes onboardin { from { opacity:0; transform:translateY(8px); } to { opacity:1; transform:none; } }
.stepper { display:flex; align-items:center; gap:6px; }
.stepper .step { display:flex; align-items:center; gap:7px; color:var(--muted); font-size:12.5px; font-weight:540; }
.stepper .step .num { width:22px; height:22px; border-radius:50%; display:grid; place-items:center; font-size:11.5px;
  background:var(--surface-2); border:1px solid var(--border); color:var(--muted); }
.stepper .step.active { color:var(--fg); }
.stepper .step.active .num { background:var(--accent-soft); border-color:var(--accent); color:var(--accent); }
.stepper .step.done .num { background:var(--ok-soft); border-color:var(--ok); color:var(--ok); }
.stepper .conn { width:26px; height:1px; background:var(--border); display:inline-block; }
.toggle-row { flex-direction:row; align-items:center; gap:9px; padding:9px 11px; border:1px solid var(--border);
  border-radius:var(--radius-sm); background:var(--surface-2); }
.toggle-row:hover { border-color:var(--border-strong); }
@media (prefers-reduced-motion: reduce) { .onboard-card { animation:none; } }
```

- [ ] **Step 5: Regenerate templ, run the tests**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestSetupOnboardingChrome|TestLoginBrand|TestSetupCreatesAdminOnce|TestRedirectToSetupWhenNoUsers' -v
```
Expected: PASS — the two new tests + the existing setup/login flow tests (endpoints/fields unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/web/views/auth.templ internal/web/views/auth_templ.go internal/web/static/app.css internal/web/auth_test.go
git commit -m "$(printf 'feat: branded onboarding shell + stepper; restyle setup and login\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Welcome steps on the onboarding shell

**Files:**
- Modify: `internal/web/views/welcome.templ` (`WelcomeSubnets` step 2, `WelcomeIntegrations` step 3 use `onboardShell`)
- Test: `internal/web/welcome_test.go` (add `TestWelcomeOnboardingChrome`)

**Interfaces:**
- Consumes: `onboardShell`, `onboardStepper` (Task 1); `integrationsFields` (existing, unchanged).

- [ ] **Step 1: Write the failing test**

Add to `internal/web/welcome_test.go` (it imports `net/http`, `net/url`, `testing`; add `strings` if missing):

```go
func TestWelcomeOnboardingChrome(t *testing.T) {
	srv, st := testServer(t)
	sub := authedGet(t, srv, st, "/welcome").Body.String()
	for _, want := range []string{`class="stepper"`, "Continue", "Which subnets"} {
		if !strings.Contains(sub, want) {
			t.Errorf("welcome subnets missing %q", want)
		}
	}
	integ := authedGet(t, srv, st, "/welcome/integrations").Body.String()
	if !strings.Contains(integ, `class="stepper"`) || !strings.Contains(integ, `name="pihole_url"`) {
		t.Errorf("welcome integrations missing stepper/fields: %s", integ)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestWelcomeOnboardingChrome -v`
Expected: FAIL — welcome pages still use `@Layout`, no `stepper`.

- [ ] **Step 3: Convert the welcome templs**

Replace the whole `internal/web/views/welcome.templ` body (keep `package views`
and the `import "netis/internal/netdetect"`) with:

```
templ WelcomeSubnets(username string, detected []netdetect.Detected) {
	@onboardShell("Welcome", 2) {
		<h1>Which subnets should netis scan?</h1>
		<p class="muted">Pick the networks to watch. You can change these later in Settings.</p>
		<form method="post" action="/welcome/subnets">
			if len(detected) == 0 {
				<p class="muted">No subnets auto-detected. Add one manually below.</p>
			} else {
				for _, d := range detected {
					<label class="toggle-row">
						<input type="checkbox" name="subnet" value={ d.CIDR + "|" + d.Iface } checked/>
						<span class="mono">{ d.CIDR }</span> <span class="muted">{ d.Iface }</span>
					</label>
				}
			}
			<label>Add another CIDR <input type="text" name="manual_cidr" placeholder="192.168.1.0/24"/></label>
			<button type="submit" class="primary">Continue</button>
		</form>
	}
}

templ WelcomeIntegrations(username string, values map[string]string) {
	@onboardShell("Welcome", 3) {
		<h1>Connect an integration</h1>
		<p class="muted">Optional — link Proxmox, WireGuard, or Pi-hole. You can do this later in Settings.</p>
		<form method="post" action="/welcome/integrations">
			@integrationsFields(values)
			<button type="submit" class="primary">Save &amp; finish</button>
		</form>
		<form method="post" action="/welcome/skip">
			<button type="submit" class="ghost">Skip for now</button>
		</form>
	}
}
```

(The `username` params are now unused — Go permits unused function parameters, and
keeping them avoids changing the two handlers that call these templs.)

- [ ] **Step 4: Regenerate templ, run the test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run 'TestWelcomeOnboardingChrome|TestWelcomeIntegrationsStillRenders|TestWelcomeShowsDetectedAndCreates|TestWelcomePostsRequireAdmin' -v
```
Expected: PASS — the new test + the existing welcome-flow and D1 field-render tests (fields/legends/endpoints unchanged).

- [ ] **Step 5: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass.

- [ ] **Step 6: Commit**

```bash
git add internal/web/views/welcome.templ internal/web/views/welcome_templ.go internal/web/welcome_test.go
git commit -m "$(printf 'feat: welcome subnets/integrations steps on the onboarding shell\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `onboardShell` + `onboardStepper` + `onboardBrand` → Task 1. ✅
- Setup on the shell (step 1) → Task 1. ✅
- Login brand hero → Task 1. ✅
- Welcome subnets (step 2) + integrations (step 3) on the shell → Task 2. ✅
- CSS (`.onboard`/`.onboard-brand`/`.onboard-card`/`.stepper`/`.toggle-row`, motion + reduced-motion) → Task 1. ✅
- Tests: setup chrome, login brand, welcome chrome; existing setup/welcome/D1 tests stay green → Tasks 1-2. ✅
- Out of scope (step order/endpoints/fields, dashboard nav, store/handler) → untouched. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `onboardShell(title string, step int)` (Task 1) is called by `SetupPage` (step 1), `WelcomeSubnets` (step 2), `WelcomeIntegrations` (step 3). `onboardStepper(step int)` and `onboardBrand()` are the shell's helpers. `integrationsFields(values)` is reused unchanged (same field names/legends the D1 test asserts). The view-function signatures `SetupPage(errMsg string)`, `WelcomeSubnets(username string, detected []netdetect.Detected)`, `WelcomeIntegrations(username string, values map[string]string)`, `LoginPage(errMsg string)` are all unchanged, so no handler edits. `fmt` is imported into `auth.templ` for `onboardStep`.

**Ordering note:** Task 1 defines the shell/stepper/brand used by Task 2's welcome templs; sequential execution ensures they exist. Both regenerate templ (Task 1 `auth_templ.go`, Task 2 `welcome_templ.go`) and touch `app.css` only in Task 1.
