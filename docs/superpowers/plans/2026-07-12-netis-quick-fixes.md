# Netis Quick Fixes: Nav order + Run-now hot-config (Sub-project F1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make integration "Run now" use current settings (works without a restart) and put Devices second in the top nav.

**Architecture:** Extract `main.go`'s run-now registry into a `newIntegrationRunner` that registers all three integrations with closures reading the store on each run; keep the boot-time periodic pollers. Reorder the nav links. No store/schema change.

**Tech Stack:** Go 1.26, templ (CLI at `/home/ben/go/bin/templ`), `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; no store/schema change.
- The web package must NOT import proxmox/pihole/wireguard (integration wiring stays in `cmd/netis/main.go`).
- Run-now on an unconfigured integration returns `errNotConfigured` (no status written → "not configured" toast); a configured one runs `RunOnce` (records connected/failing).
- Regenerate templ after editing `.templ`: `export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`; commit the regenerated `*_templ.go` with source.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` from the repo root before finishing (note: `.gitignore` now covers these).

---

### Task 1: Run-now builds the integration client from current settings

**Files:**
- Modify: `cmd/netis/main.go` (`errNotConfigured`, `newIntegrationRunner`; wire it; keep boot `Start` blocks)
- Test: `cmd/netis/main_test.go` (new)

**Interfaces:**
- Produces: `func newIntegrationRunner(st *store.Store, evs *events.Service) integrationRunner`; `var errNotConfigured error`. (`integrationRunner` already exists in `main`.)

- [ ] **Step 1: Write the failing test**

Create `cmd/netis/main_test.go`:

```go
package main

import (
	"context"
	"errors"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

func TestIntegrationRunnerReadsCurrentSettings(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runner := newIntegrationRunner(st, events.NewService(st, events.NewBroker()))

	// Unconfigured → the not-configured sentinel.
	if err := runner.Run(context.Background(), "pihole"); !errors.Is(err, errNotConfigured) {
		t.Fatalf("unconfigured pihole: got %v, want errNotConfigured", err)
	}

	// Configured but unreachable → a real error that is NOT the sentinel, proving
	// the runner read the current setting and attempted the run (run-now no longer
	// reports "not configured" for a configured integration).
	if err := st.SetSetting("pihole_url", "http://127.0.0.1:9"); err != nil {
		t.Fatal(err)
	}
	err = runner.Run(context.Background(), "pihole")
	if err == nil || errors.Is(err, errNotConfigured) {
		t.Fatalf("configured pihole: got %v, want a non-nil non-sentinel error", err)
	}

	// Unknown integration name → error.
	if err := runner.Run(context.Background(), "bogus"); err == nil {
		t.Fatal("bogus integration should error")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/netis/ -run TestIntegrationRunnerReadsCurrentSettings -v`
Expected: FAIL to compile — `newIntegrationRunner`/`errNotConfigured` undefined.

- [ ] **Step 3: Add `errNotConfigured` + `newIntegrationRunner`**

In `cmd/netis/main.go`, add `"errors"` to the imports if not present. Add at
top level (near the existing `integrationRunner` type):

```go
var errNotConfigured = errors.New("not configured")

// newIntegrationRunner builds a run-now registry whose closures read the current
// settings on each call, so "Run now" reflects settings saved after startup.
func newIntegrationRunner(st *store.Store, evs *events.Service) integrationRunner {
	return integrationRunner{
		"proxmox": func(ctx context.Context) error {
			url, _ := st.GetSetting("proxmox_url")
			if url == "" {
				return errNotConfigured
			}
			tokenID, _ := st.GetSetting("proxmox_token_id")
			secret, _ := st.GetSetting("proxmox_secret")
			insecure, _ := st.GetSetting("proxmox_insecure")
			_, err := proxmox.NewSync(st, proxmox.NewClient(url, tokenID, secret, insecure == "1"), evs).RunOnce(ctx)
			return err
		},
		"pihole": func(ctx context.Context) error {
			url, _ := st.GetSetting("pihole_url")
			if url == "" {
				return errNotConfigured
			}
			pass, _ := st.GetSetting("pihole_password")
			insecure, _ := st.GetSetting("pihole_insecure")
			_, err := pihole.NewSync(st, pihole.NewClient(url, pass, insecure == "1"), evs).RunOnce(ctx)
			return err
		},
		"wireguard": func(ctx context.Context) error {
			addr, _ := st.GetSetting("wg_ssh_addr")
			if addr == "" {
				return errNotConfigured
			}
			user, _ := st.GetSetting("wg_ssh_user")
			key, _ := st.GetSetting("wg_ssh_key_path")
			iface, _ := st.GetSetting("wg_iface")
			if iface == "" {
				iface = "wg0"
			}
			sshRunner, err := wireguard.NewSSHRunner(addr, user, key)
			if err != nil {
				return err
			}
			_, err = wireguard.NewSync(st, sshRunner, evs, iface).RunOnce(ctx)
			return err
		},
	}
}
```

- [ ] **Step 4: Wire it in `main` and keep the boot pollers**

In `cmd/netis/main.go`'s `main`, change `runNow := integrationRunner{}` to:

```go
	runNow := newIntegrationRunner(st, evs)
```

and remove the `runNow[...] = func(ctx …) …` line from each of the three
`if …url != "" { … }` blocks (the boot blocks now only start the periodic
poller). The three blocks become:

```go
	if pxURL, _ := st.GetSetting("proxmox_url"); pxURL != "" {
		tokenID, _ := st.GetSetting("proxmox_token_id")
		secret, _ := st.GetSetting("proxmox_secret")
		insecure, _ := st.GetSetting("proxmox_insecure")
		go proxmox.NewSync(st, proxmox.NewClient(pxURL, tokenID, secret, insecure == "1"), evs).Start(ctx, time.Minute)
	}

	if wgAddr, _ := st.GetSetting("wg_ssh_addr"); wgAddr != "" {
		wgUser, _ := st.GetSetting("wg_ssh_user")
		wgKey, _ := st.GetSetting("wg_ssh_key_path")
		wgIface, _ := st.GetSetting("wg_iface")
		if wgIface == "" {
			wgIface = "wg0"
		}
		if sshRunner, err := wireguard.NewSSHRunner(wgAddr, wgUser, wgKey); err != nil {
			log.Printf("wireguard ssh setup: %v", err)
		} else {
			go wireguard.NewSync(st, sshRunner, evs, wgIface).Start(ctx, time.Minute)
		}
	}

	if phURL, _ := st.GetSetting("pihole_url"); phURL != "" {
		phPass, _ := st.GetSetting("pihole_password")
		phInsecure, _ := st.GetSetting("pihole_insecure")
		go pihole.NewSync(st, pihole.NewClient(phURL, phPass, phInsecure == "1"), evs).Start(ctx, time.Minute)
	}
```

Keep the `runNow := newIntegrationRunner(st, evs)` line before these blocks (or
anywhere before `web.NewServer(st, broker, sched, runNow)`), and the existing
`srv := web.NewServer(st, broker, sched, runNow)` call unchanged.

- [ ] **Step 5: Run the test + build**

Run:
```bash
CGO_ENABLED=0 go build ./...
go test ./cmd/netis/ -run TestIntegrationRunnerReadsCurrentSettings -v
```
Expected: builds clean; the test PASSes.

- [ ] **Step 6: Commit**

```bash
git add cmd/netis/main.go cmd/netis/main_test.go
git commit -m "$(printf 'fix: run-now builds the integration client from current settings\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

### Task 2: Nav order — Devices second

**Files:**
- Modify: `internal/web/views/layout.templ` (nav link order)
- Test: `internal/web/devices_test.go` (add `TestNavDevicesBeforeSubnets`)

- [ ] **Step 1: Write the failing test**

Add to `internal/web/devices_test.go`:

```go
func TestNavDevicesBeforeSubnets(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices").Body.String()
	di := strings.Index(body, `href="/devices"`)
	si := strings.Index(body, `href="/subnets"`)
	if di < 0 || si < 0 {
		t.Fatalf("nav links missing: devices@%d subnets@%d", di, si)
	}
	if di > si {
		t.Fatalf("Devices nav link should come before Subnets: devices@%d subnets@%d", di, si)
	}
}
```

(The nav renders near the top of `<body>`, before page content; `href="/devices"`
and `href="/subnets"` — each with its closing quote — match the nav links, not the
`/devices/{id}` / `/subnets/{id}` links used in page content.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestNavDevicesBeforeSubnets -v`
Expected: FAIL — Subnets currently precedes Devices in the nav.

- [ ] **Step 3: Reorder the nav links**

In `internal/web/views/layout.templ`, change the `nav.top .links` block so
`Devices` precedes `Subnets`:

```
					<a href="/">Dashboard</a>
					<a href="/devices">Devices</a>
					<a href="/subnets">Subnets</a>
					<a href="/events">Events</a>
					<a href="/settings">Settings</a>
```

- [ ] **Step 4: Regenerate templ, run the test**

Run:
```bash
export PATH="$(go env GOPATH)/bin:$PATH"
templ generate
go test ./internal/web/ -run TestNavDevicesBeforeSubnets -v
```
Expected: PASS.

- [ ] **Step 5: Full build + suite**

Run:
```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean, all packages pass.

- [ ] **Step 6: Commit**

```bash
git add internal/web/views/layout.templ internal/web/views/layout_templ.go internal/web/devices_test.go
git commit -m "$(printf 'feat: put Devices second in the top nav\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `newIntegrationRunner` reading current settings + `errNotConfigured`; boot pollers kept → Task 1. ✅
- `main` wires it; the `runNow[...] =` registrations removed from boot blocks → Task 1. ✅
- Nav reorder (Devices 2nd) → Task 2. ✅
- Tests: unconfigured→sentinel, configured→non-sentinel error, bogus→error; nav order → Tasks 1-2. ✅
- Out of scope (periodic-poller hot reload; F2/F3) → untouched. ✅

**Placeholder scan:** none — every code step shows complete code.

**Type consistency:** `newIntegrationRunner(st *store.Store, evs *events.Service) integrationRunner` matches the `main` call `newIntegrationRunner(st, evs)` (both exist: `st` from `store.Open`, `evs` from `events.NewService`). `integrationRunner.Run` (unchanged, existing) is exercised by the test and used by `web.handleIntegrationRun` via the `web.IntegrationRunner` interface. The closures use the real constructors (`proxmox.NewClient`/`NewSync`, `pihole.NewClient`/`NewSync`, `wireguard.NewSSHRunner`/`NewSync`) with the exact signatures in the codebase. `errNotConfigured` is compared with `errors.Is` in both the test and (indirectly) surfaces to the web handler's existing "not configured" toast branch.

**Ordering note:** the two tasks are independent (main vs templ) and each self-tests; either order works. Task 1's `main_test.go` runs `RunOnce` against a closed port (127.0.0.1:9) → deterministic connection-refused error (not the sentinel).
