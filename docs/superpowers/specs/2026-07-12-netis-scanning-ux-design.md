# Netis — Scanning UX (Sub-project C2): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Second of three sub-projects in the current batch (C1 device model done →
**C2 scanning UX** → C3 subnet occupancy grid v2). C2 reuses the existing
scan `Scheduler.Trigger`, the `POST /subnets/{id}/scan` route, the grid page's
Scan-now button, and the SSE refresh channels.

## Purpose

Make scanning controllable and visible: let a subnet keep periodic auto-scan
**off** while still being scannable on demand ("manual-only mode"), put a
**Scan now** control in the places the user actually looks (dashboard subnet
cards, the subnet page, the device list, settings), and give each scan a small
**toast** so a click has visible feedback.

## Decisions (locked)

- **2-state scan model, no migration.** `subnet.scan_enabled` now means
  *auto-scan* (periodic). A manual "Scan now" always works for any
  non-WireGuard subnet regardless of `scan_enabled`. "Manual-only mode" = leave
  auto off and click Scan now.
- **Toast feedback.** A Scan-now click pops a small dismissable toast
  ("Scanning 192.168.22.0/24…"); scan completion still flows through the
  existing SSE (`dashboard` refresh / `grid:{id}`) which updates counts and the
  grid live. The toast is "started" feedback, not completion tracking.
- WireGuard subnets are never ARP-scannable; their Scan-now controls are hidden
  and a manual trigger is a no-op (informational toast).

## Component 1: Manual scan bypasses auto-disable (`internal/scan/scheduler.go`)

Today `Scheduler.run` guards `if !sn.ScanEnabled || sn.Kind == "wireguard" { return }`,
so a manual `Trigger` on an auto-disabled subnet silently does nothing. Split
the guard by entry point using a pure helper:

```go
// shouldRunScan reports whether a scan should execute for sn. Manual scans
// (user-triggered) run regardless of the auto-scan flag; periodic scans respect
// it. WireGuard subnets are never ARP-scannable and are always skipped.
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

- `run(ctx context.Context, sn store.Subnet, manual bool)` replaces its inline
  guard with `if !shouldRunScan(sn, manual) { return }`.
- In `Start`, the `case id := <-s.trigger` path calls `run(ctx, sn, true)`
  (every `Trigger` is a manual, user-initiated scan); the `case <-tick.C` path
  calls `run(ctx, sn, false)` (periodic). The tick loop keeps its cheap
  pre-filter (`if !sn.ScanEnabled || sn.Kind == "wireguard" { continue }`) so it
  does not compute `due` for skipped subnets; `shouldRunScan` in `run` is the
  authoritative guard.
- The `Trigger(subnetID int64)` signature and the `web.ScanTrigger` interface
  are unchanged — the manual/auto distinction lives entirely in the scheduler,
  keyed off which `select` case fired, so the trigger channel carries no flag.

## Component 2: Toast component

- `Layout` gains `<div id="toasts" class="toasts"></div>` beside the existing
  `#modal`, and loads `toasts.js` once (like `dialog.js`).
- `internal/web/static/toasts.js`: for each `.toast` added under `#toasts`,
  auto-dismiss after ~4s and dismiss on click. Event-delegated / MutationObserver
  so it works for fragments HTMX injects after load. Guards a missing container.
- `app.css` gains `.toasts` (fixed top-right stack, `z-index` above content) and
  `.toast` (token-driven surface card) styles. Additive.
- A templ helper renders the fragment:
  ```go
  templ ScanToast(msg string) {
      <div class="toast">{ msg }</div>
  }
  ```

## Component 3: Scan handlers & routes (`internal/web/grid.go`, `server.go`)

- `handleScanNow` (`POST /subnets/{id}/scan`, `requireAdmin`) changes from
  `204` to returning the `ScanToast` fragment (HTTP 200, HTML):
  - Parse id → 404 on bad; `GetSubnet` → 404 on `errors.Is(sql.ErrNoRows)`.
  - If `sn.Kind == "wireguard"`: render `ScanToast(sn.CIDR + " is WireGuard — not scannable")` and return without triggering.
  - Else `s.trigger.Trigger(sn.ID)` (now bypasses auto-disable via Component 1)
    and render `ScanToast("Scanning " + sn.CIDR + "…")`.
- **New** `handleScanAll` (`POST /scan`, `requireAdmin`): `ListSubnets()`, call
  `Trigger` for every subnet with `Kind != "wireguard"`, render
  `ScanToast("Scanning all subnets…")`. Registered as `s.mux.HandleFunc("POST /scan", s.requireAdmin(s.handleScanAll))`.
- `s.trigger` may be nil in some test setups; both handlers guard `if s.trigger != nil` before triggering (as `handleScanNow` already does).

## Component 4: Buttons & settings wording

Every scan button posts with `hx-post="…" hx-target="#toasts" hx-swap="beforeend"`;
the handler's fragment is appended into the toast stack.

- **Dashboard subnet cards (`dashboard.templ`)** — restructure each card from a
  full-card `<a class="card">` (a `<button>` cannot be nested in an anchor) to
  `<div class="card">`: the subnet name becomes the `<a>` link to
  `/subnets/{id}`, and a small **Scan** button sits in the header flex row,
  rendered only when `r.Subnet.Kind != "wireguard"`. A **Scan all** button is
  added to the `<h2>Subnets</h2>` heading (`POST /scan`).
- **Subnet / grid page (`grid.templ`)** — the existing Scan-now button gains
  `hx-target="#toasts" hx-swap="beforeend"` (drops `hx-swap="none"`) and is
  rendered only when `sn.Kind != "wireguard"`.
- **Device list toolbar (`devices.templ`)** — a **Scan all** button
  (`POST /scan`) next to the New device button (the list is not subnet-scoped,
  so "scan everything" is the fitting control).
- **Settings → Subnets table (`settings.templ`)** — a per-row **scan** button
  (`POST /subnets/{id}/scan`, shown for non-WireGuard rows); relabel for the new
  semantics: column header `Scan enabled → Auto-scan`, checkbox label
  `Scan enabled → Auto-scan (periodic)`. Interval unchanged.

## Error handling

- Manual scan of a WireGuard subnet: informational toast, no trigger, not an
  error.
- Scan-now with a non-numeric or nonexistent id → 404.
- Scan-all with zero subnets → still returns the "Scanning all subnets…" toast
  (harmless no-op).
- The trigger channel is buffered (len 8) and drops on a full buffer, so rapid
  repeated clicks are absorbed silently — no new rate-limit UI.
- `toasts.js` no-ops when `#toasts` is absent.

## Testing

- **scan** (`internal/scan/engine_test.go`):
  - New `TestShouldRunScan` table test:
    `{enabled,manual=false,lan}→true`, `{disabled,manual=false,lan}→false`,
    `{disabled,manual=true,lan}→true`, `{enabled,manual=true,wireguard}→false`,
    `{enabled,manual=false,wireguard}→false`.
  - **Rewrite** the existing `TestTriggerSkipsDisabledSubnet` into
    `TestManualTriggerRunsDisabledSubnet`: with `ScanEnabled=false`, `Kind=lan`,
    and a fake sweeper result, `sched.Trigger(snID)` now causes the sweeper to
    run and a device to be created (the opposite of the old assertion — this is
    the intended behavior change from Component 1). Reuse the existing
    `testEngine`/fake-sweeper harness.
- **web** (`internal/web/*_test.go`): using a recording fake `ScanTrigger` that
  captures triggered subnet IDs:
  - `POST /subnets/{id}/scan` on a LAN subnet returns a body containing
    `class="toast"` and the subnet CIDR, and records exactly that subnet id.
  - `POST /subnets/{id}/scan` on a WireGuard subnet returns a "not scannable"
    toast and records **no** trigger.
  - `POST /subnets/{id}/scan` with a nonexistent id → 404.
  - `POST /scan` records a trigger for each non-WireGuard subnet (seed one LAN +
    one WireGuard; assert only the LAN id recorded) and returns a toast.
  - A viewer session → 403 on both routes.
  - Render checks: the dashboard contains a `Scan all` control and a per-card
    scan post; the device list toolbar contains `Scan all`; the settings subnet
    row contains a scan post and the `Auto-scan` label.

## Project layout (files added / modified)

- Create: `internal/web/static/toasts.js`.
- Modify:
  - `internal/scan/scheduler.go` (`shouldRunScan`, `run(…, manual bool)`,
    per-case manual flag) + `internal/scan/engine_test.go` (new + rewritten
    tests).
  - `internal/web/grid.go` (`handleScanNow` → toast + WireGuard guard;
    `handleScanAll`) + `internal/web/*_test.go`.
  - `internal/web/server.go` (`POST /scan` route).
  - `internal/web/views/layout.templ` (`#toasts` container + `toasts.js`).
  - `internal/web/views/grid.templ` (`ScanToast` helper; Scan-now retarget +
    WireGuard guard).
  - `internal/web/views/dashboard.templ` (card restructure + Scan / Scan all).
  - `internal/web/views/devices.templ` (toolbar Scan all).
  - `internal/web/views/settings.templ` (per-row scan + Auto-scan relabel).
  - `internal/web/static/app.css` (`.toasts` / `.toast`).
  - Regenerated `*_templ.go` for every edited `.templ`.

## Out of scope (C2)

- Scan progress bars or completion tracking (the existing SSE refresh already
  updates counts/grid when a scan finishes).
- Scan history / per-subnet last-scanned display.
- Rate-limit UI for rapid Scan-now clicks (buffered channel absorbs them).
- The subnet occupancy grid v2 and static/dhcp toggle (C3).
