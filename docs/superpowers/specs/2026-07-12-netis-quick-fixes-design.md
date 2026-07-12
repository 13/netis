# Netis — Quick Fixes: Nav order + Pi-hole Run-now (Sub-project F1): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

First of three sub-projects in this batch (F1 quick fixes → F2 device-detail
overhaul → F3 device-list parent/child + sortable subnet list). F1 fixes two
small issues: the nav order and the misleading Pi-hole "run now" behavior.

## Purpose

1. Put **Devices** second in the top nav (Dashboard · Devices · Subnets · Events
   · Settings).
2. Fix **integration "Run now"**: today `main.go` registers an integration for
   run-now only if it was configured **at boot**, so an integration added later
   (e.g. Pi-hole configured after startup) is absent from the registry and
   "Run now" toasts "not configured" even though `pihole_url` is saved. Make
   run-now build the integration client from the **current** settings on each
   run, so it works immediately after saving — no restart.

## Root cause (verified)

Reproduced: after saving `pihole_url=https://pi.hole` via the settings form, the
status pill correctly reads "configured · not run yet", but `Run now` toasts
`Pi-hole: not configured`. `cmd/netis/main.go` builds each integration's client
once at startup (inside `if url != "" {...}`) and captures it in the `runNow`
map; an integration configured after boot has no `runNow` entry, so
`integrationRunner.Run` returns "not configured". The periodic poller has the
same boot-only limitation, but run-now is the user-facing "apply now" affordance
and must reflect current settings.

## Component 1: Nav order

In `internal/web/views/layout.templ`, reorder the `nav.top .links` so `Devices`
precedes `Subnets`:

```
<a href="/">Dashboard</a>
<a href="/devices">Devices</a>
<a href="/subnets">Subnets</a>
<a href="/events">Events</a>
<a href="/settings">Settings</a>
```

The existing `theme.js` longest-prefix active-nav logic is unaffected.

## Component 2: Run-now builds from current settings

Extract the runner construction in `cmd/netis/main.go` into a testable function
and register **all three** integrations unconditionally with closures that read
the current settings on each call:

```go
var errNotConfigured = errors.New("not configured")

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

`main` calls `runNow := newIntegrationRunner(st, evs)` and passes it to
`NewServer`. The existing boot-time `if url != "" { … go X.Start(ctx, time.Minute) }`
blocks stay for the **periodic** poller (each still builds its own Sync for
`Start`); only the `runNow[...] = …` registrations move out of those blocks into
`newIntegrationRunner`.

Effect on the toast (`handleIntegrationRun`, unchanged): a configured integration
now runs `RunOnce`, which records a `connected`/`failing` status → the toast (and
the status pill on reload) reflect the real result. A truly-unconfigured
integration's closure returns `errNotConfigured` and writes no status, so the
handler still shows "not configured".

## Error handling

- Run-now on an unconfigured integration → `errNotConfigured`, no status written
  → "not configured" toast (correct).
- Run-now on a configured integration that can't reach its host → `RunOnce`
  records a failing status → "failing · <detail>" toast (helpful).
- WireGuard SSH setup failure → the error surfaces from the closure; `RunOnce`
  isn't reached, no status written → the handler falls through to its `ran`/`not
  configured` branch (still non-fatal).

## Testing

- **main** (`cmd/netis/main_test.go`, new — `package main`):
  - `newIntegrationRunner(st, evs).Run(ctx, "pihole")` with **no** `pihole_url`
    set returns `errNotConfigured` (`errors.Is`).
  - After `st.SetSetting("pihole_url", "http://127.0.0.1:9")` (a closed port),
    `Run(ctx, "pihole")` returns a **non-nil** error that is **not**
    `errNotConfigured` — i.e. it read the current setting and attempted the run
    (connection refused), proving run-now no longer reports "not configured" for
    a configured integration. Uses a real in-memory store (`store.Open(":memory:")`)
    and `events.NewService`.
  - `integrationRunner.Run(ctx, "bogus")` returns a non-nil error (unknown name).
- **web** (`internal/web/`): the existing `TestIntegrationRun*` tests (fake
  runner) are unaffected. Add a layout-order assertion (`layout` renders the
  `/devices` link before the `/subnets` link) — e.g. in an existing rendered page
  test: `strings.Index(body, "\">Devices<") < strings.Index(body, "\">Subnets<")`.

## Project layout (files added / modified)

- Modify: `cmd/netis/main.go` (`errNotConfigured`, `newIntegrationRunner`; wire
  it; keep the boot `Start` blocks).
- Create: `cmd/netis/main_test.go`.
- Modify: `internal/web/views/layout.templ` (nav order) + regenerated
  `layout_templ.go`.
- Test: an order assertion in `internal/web/` (e.g. add to `dashboard_test.go`
  or `devices_test.go`).

## Out of scope (F1)

- Hot-reloading the **periodic** poller (still boot-time; the "restart netis to
  apply" note stays for automatic polling — run-now is the immediate path).
- The device-detail overhaul (F2) and device-list changes (F3).
