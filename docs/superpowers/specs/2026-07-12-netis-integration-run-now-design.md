# Netis — Integration "Run now" + Settings Button Alignment (Sub-project E1): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

First of three sub-projects in this batch (E1 settings run-now + alignment → E2
subnet-page device list + CRUD → E3 onboarding redesign). E1 adds an on-demand
"Run now" for each integration and fixes the misaligned settings buttons.

## Purpose

Let an admin trigger an integration poll on demand from the settings page and see
the result immediately, and fix the two button-alignment problems on the settings
page (checkbox labels stacking their box above the text; the subnet card's Delete
button wrapping onto its own line).

## Decisions (locked)

- "Run now" runs the integration's existing `RunOnce` against the **currently
  running (startup-loaded) config** — the same config the background poller uses
  and the status reflects. Integrations not configured at startup return a "not
  configured" toast.
- Run synchronously in the handler with a ~20s context timeout; toast the result.
- No async/progress UI, no in-place status-pill refresh (a page reload shows the
  updated pill), no per-integration concurrency guard (`RunOnce` writes are
  find-or-create/upsert — idempotent; an overlap with the once-a-minute poller is
  an accepted low risk).
- No store/schema change.

## Component 1: Runner registry & wiring

- **Interface** (`internal/web/server.go`):
  ```go
  type IntegrationRunner interface {
      Run(ctx context.Context, name string) error
  }
  ```
  `Server` gains a `runner IntegrationRunner` field; `NewServer` gains a trailing
  `runner IntegrationRunner` param. `s.runner` may be nil (tests / unconfigured
  builds).
- **Composition root** (`cmd/netis/main.go`): build a runner from the syncs that
  were configured at startup. Each integration Sync exposes
  `RunOnce(ctx) (Stats, error)`. Define, in `main`:
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
  Register an entry per configured integration, e.g. where the proxmox Sync `px`
  is built: `runner["proxmox"] = func(ctx context.Context) error { _, err := px.RunOnce(ctx); return err }`
  (and likewise `wireguard`, `pihole`). Pass `runner` to `NewServer`. The web
  package never imports proxmox/pihole/wireguard.
- `RunOnce` already records the `store.IntegrationStatus` (OK / Detail / LastRun),
  including on failure — so after `Run` returns, the recorded status reflects the
  attempt.

## Component 2: Run handler & route

- `handleIntegrationRun` (`POST /settings/integrations/{name}/run`, `requireAdmin`):
  - `name` must be one of `proxmox`/`wireguard`/`pihole` → else 400.
  - If `s.runner == nil` → render a `ScanToast("integration run not available")`
    and return (no 500).
  - Else run with a bounded context:
    ```go
    ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
    defer cancel()
    runErr := s.runner.Run(ctx, name)
    ```
  - Look up the just-recorded status: `ListIntegrationStatus()`, find the entry
    whose `.Name == name`.
  - Toast (`views.ScanToast`):
    - runner returned a non-nil error that is the "not configured" case (no status
      row and `runErr != nil`) → `"<Title>: not configured"`.
    - status found & `OK` → `"<Title>: connected · <Detail>"` (omit the `· <Detail>`
      when Detail is empty).
    - status found & `!OK` → `"<Title>: failing · <Detail>"`.
    - no status and no error → `"<Title>: ran"`.
    `<Title>` is the display name: `Proxmox` / `WireGuard` / `Pi-hole`.
- **Route** in `NewServer`, next to `POST /settings/integrations`:
  ```go
  s.mux.HandleFunc("POST /settings/integrations/{name}/run", s.requireAdmin(s.handleIntegrationRun))
  ```
- No new store method: the handler filters `ListIntegrationStatus()` for the name.

## Component 3: UI — Run-now button

In `integrationsTab` (`internal/web/views/settings.templ`), each integration
card header gains a **Run now** button beside the status pill:

```
<div class="sc-head">
  <h3>Pi-hole</h3>
  @integrationStatus("pihole", d.Values["pihole_url"] != "", d.Statuses)
  <button type="button" class="ghost" hx-post="/settings/integrations/pihole/run" hx-target="#toasts" hx-swap="beforeend">Run now</button>
</div>
```

(analogously for Proxmox → `/settings/integrations/proxmox/run` and WireGuard →
`/settings/integrations/wireguard/run`). The button always shows; an unconfigured
integration returns the "not configured" toast.

## Component 4: Button alignment fixes

1. **Checkbox labels.** The base rule `label { display:flex; flex-direction:column; }`
   makes a checkbox label render the box above its text (integration "Skip TLS
   verification", welcome, the general-tab, etc.). Add one global rule to
   `app.css`:
   ```css
   label:has(> input[type="checkbox"]) { flex-direction:row; align-items:center; gap:7px; }
   ```
   This fixes every checkbox label in the app at once (`:has` is broadly
   supported). The existing `.field-grid label.chk` rule becomes redundant but is
   harmless; leave it.
2. **Subnet card actions.** Today Save+Scan are inside the edit `<form>`'s
   `.sc-actions` and the **Delete** `<form>` follows as a separate block, dropping
   onto its own line. Give the edit form an id and move Save out via the standard
   `form=` attribute so all three sit in one `.sc-actions` row:
   ```
   <form id={ fmt.Sprintf("sn-edit-%d", sn.ID) } method="post" action={ templ.URL(fmt.Sprintf("/settings/subnets/%d", sn.ID)) }>
       <div class="field-grid"> …fields… </div>
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
   `.sc-actions` already lays out its children in a flex row, so Save · Scan ·
   Delete align. All endpoints/field names unchanged; the fields stay inside the
   `sn-edit-{id}` form (submitted by the linked Save button).

## Error handling

- Unknown `{name}` → 400.
- `s.runner == nil` → informational toast, no error.
- Runner "not configured" (integration absent from the registry) → "not
  configured" toast.
- `RunOnce` network/timeout error → recorded as a failing status by `RunOnce`; the
  handler toasts `failing · <detail>` (useful feedback even on failure).

## Testing

- **web** (`internal/web/integrations_run_test.go`, new): a recording fake
  `IntegrationRunner` (captures the `name` passed to `Run`) + a `testServerRun(t)`
  helper mirroring `scan_test.go`'s `testServerTrig` (builds `NewServer` with the
  fake runner):
  - Seed `IntegrationStatus{Name:"pihole", OK:true, Detail:"48 leases, 2 new"}`;
    `POST /settings/integrations/pihole/run` → 200; the fake recorded `"pihole"`;
    the body contains `Pi-hole`, `connected`, and `48 leases, 2 new`.
  - `POST /settings/integrations/bogus/run` → 400.
  - A viewer session → 403 on the run route.
  - Default `testServer` (nil runner): `POST /settings/integrations/pihole/run`
    → 200 with the "not available" toast, no 500.
  - Render: `GET /settings?tab=integrations` contains
    `hx-post="/settings/integrations/pihole/run"`; a settings subnets tab render
    contains `form="sn-edit-1"` on the Save button.
- The existing settings/welcome/scan tests stay green after the `NewServer`
  signature change (all callers updated to pass a runner — `nil` for the plain
  helpers).

## Project layout (files added / modified)

- Modify: `internal/web/server.go` (`IntegrationRunner`, `runner` field,
  `NewServer` param, run route).
- Create: `internal/web/integrations_run.go` (`handleIntegrationRun`).
- Modify: `internal/web/views/settings.templ` (Run-now button; subnet-card
  `form=` restructure) + regenerated `settings_templ.go`.
- Modify: `internal/web/static/app.css` (checkbox-label `:has` rule).
- Modify: `cmd/netis/main.go` (`integrationRunner` map + wire into `NewServer`).
- Modify: `internal/web/auth_test.go` (`testServer` passes nil runner) and
  `internal/web/scan_test.go` (`testServerTrig` passes nil runner) + new
  `internal/web/integrations_run_test.go`.

## Out of scope (E1)

- Async run / progress UI; in-place status-pill refresh.
- Per-integration concurrency guard.
- The subnet-page device list + CRUD (E2) and onboarding redesign (E3).
