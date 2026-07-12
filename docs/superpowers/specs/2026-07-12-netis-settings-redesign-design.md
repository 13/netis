# Netis — Settings Redesign + Integration Status (Sub-project D1): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

First of two sub-projects (D1 settings redesign + integration status → D2
grid-v2 discoverability). D1 reworks the settings page presentation and surfaces
the per-integration connection status that the store already records. Reuses the
design-system tokens/components (`.panel`, `.badge`, `.pill`, `relTime`).

## Purpose

Make the settings page modern and user-friendly, and — the concrete driver —
show for each integration (Proxmox / WireGuard / Pi-hole) **whether the
connection last succeeded and when it last worked**, using the status the
background pollers already write (`store.IntegrationStatus`).

## Decisions (locked)

- **Display last poller result**, no live "Test connection" button. Integration
  config applies on restart, so the recorded status reflects the running config.
- Presentation-only rework: every existing POST endpoint and form field name is
  unchanged; secrets still render blank (never echoed).
- No store or schema change — `IntegrationStatus{Name,LastRun,Detail,OK,ItemCount}`
  and `ListIntegrationStatus()` already exist.

## Component 1: View-model & handler

- `views.SettingsData` gains `Statuses map[string]store.IntegrationStatus`
  (keyed by integration name: `proxmox`, `wireguard`, `pihole`).
- `handleSettingsPage` (`internal/web/settings.go`) builds `Statuses` from
  `s.store.ListIntegrationStatus()` (a slice) into a `map[string]…` keyed by
  `.Name`, and sets it on `SettingsData`. Build it when the active tab is
  `integrations` (the only consumer); a query error → HTTP 500. Nil/empty map is
  safe — lookups for a missing name yield the zero value.
- `SettingsPage` is split into four focused templs — `subnetsTab(d)`,
  `integrationsTab(d)`, `usersTab(d)`, `generalTab(d)` — called from
  `SettingsPage` by `d.ActiveTab`. Same file, smaller units.

## Component 2: Integrations tab + status

The integrations tab renders the "restart netis to apply integration changes"
note, then one save-`<form>` (`POST /settings/integrations`, unchanged) wrapping
three integration **cards** (`.setting-card`):

- **Card header:** the integration title (Proxmox / WireGuard / Pi-hole) + a
  status pill resolved by `integrationStatus`:

  ```
  templ integrationStatus(name string, configured bool, statuses map[string]store.IntegrationStatus)
  ```

  Resolution (given `st, ok := statuses[name]`):
  - `ok && st.OK` → green pill `connected` + muted `last worked {relTime(st.LastRun)}`
  - `ok && !st.OK` → red pill `failing` + muted `{relTime(st.LastRun)}`
  - `!ok && configured` → muted pill `configured · not run yet`
  - `!ok && !configured` → muted pill `not configured`
- **Detail line:** when `ok`, render `st.Detail` (e.g. `48 leases, 2 new`) muted
  under the header.
- **Card body:** the existing config inputs for that integration, unchanged
  (names, placeholders, password blanking).

**Preserving the welcome page.** `integrationsFields(values)` is also used by the
onboarding welcome page (`welcome.templ`), which must keep rendering the plain
fieldsets (no status, no cards). So split the field markup into three
per-integration field templs — `proxmoxFields(values)`, `wireguardFields(values)`,
`piholeFields(values)` — each emitting exactly the current `<label>`s for that
integration. `integrationsFields` (welcome) becomes a thin wrapper that renders
the three inside the existing `<fieldset><legend>…</legend>` blocks, so the
welcome page's output is unchanged. The settings integrations tab instead wraps
each field templ in a `.setting-card` with the `integrationStatus` header. This
keeps the field names byte-identical for both pages and the save handler
untouched.

`configured` per integration = the integration's primary field in `d.Values` is
non-empty: `proxmox` → `proxmox_url`, `wireguard` → `wg_ssh_addr`, `pihole` →
`pihole_url`.

Example: a Pi-hole that polled 3 minutes ago renders
**Pi-hole · `connected` · last worked 3m ago** with `48 leases, 2 new` beneath.

## Component 3: Subnets / Users / General tabs

- **Subnets tab (`subnetsTab`)** — replace the two-row-per-subnet table with one
  card per subnet: header = name + CIDR (mono) + kind badge + an auto-scan badge
  (`on`/`off`); body = the edit `<form>` (`POST /settings/subnets/{id}`) with
  labeled fields in a grid (CIDR, name, kind, interval, auto-scan toggle) + Save;
  actions = Scan (non-WireGuard, `POST /subnets/{id}/scan` → `#toasts`) and
  Delete (`POST /settings/subnets/{id}/delete`). Below: **Detected subnets** as
  small add-chips (`POST /settings/subnets`, hidden fields unchanged) and **Add
  subnet** as one card form (`POST /settings/subnets`).
- **Users tab (`usersTab`)** — a tidy table (Username · Role badge · Delete via
  `POST /settings/users/{id}/delete`) + an Add-user card
  (`POST /settings/users`, username/password/role).
- **General tab (`generalTab`)** — the "Offline after (missed scans)" field
  (`POST /settings/general`) in a card with a one-line description + Save.

All tabs share the same card/spacing language. Every endpoint and field name is
unchanged.

## Component 4: CSS

Additive to `app.css`:
- `.setting-card` — a card container (surface, border, radius, padding) matching
  `.panel`, used for subnet/integration/add cards.
- `.status-line` — a row holding the status pill + muted detail text.
- a field-grid helper (e.g. `.field-grid`) for the subnet/add/user forms so
  labeled inputs lay out in a responsive grid rather than stacking raw.
- restyle `.tabs`/`.tab` into a pill row (active pill uses `--accent-soft`/
  `--accent`, like the nav active state).

All token-driven; no new colors. Theme-aware via the existing tokens.

## Error handling

- `ListIntegrationStatus()` error in `handleSettingsPage` → HTTP 500.
- A missing status for an integration name → the `configured`/`not configured`
  branch (nil-safe map read).
- Secrets (`proxmox_secret`, `pihole_password`) never populated into `Values`
  (unchanged) — password inputs always render blank.

## Testing

- **web** (`internal/web/settings_test.go`):
  - Seed `IntegrationStatus{Name:"pihole", OK:true, LastRun:<now>, Detail:"48 leases, 2 new"}`
    and set `pihole_url`; `GET /settings?tab=integrations` body contains
    `Pi-hole`, `connected`, and `48 leases, 2 new`.
  - Seed `IntegrationStatus{Name:"proxmox", OK:false, LastRun:<now>}` and set
    `proxmox_url`; body contains `failing` in the Proxmox card.
  - With no WireGuard status and blank `wg_ssh_addr`; body contains
    `not configured` for WireGuard.
  - `GET /settings?tab=subnets` still renders a seeded subnet's CIDR and the
    `scan` control; `GET /settings?tab=users` renders the Add-user form;
    `GET /settings?tab=general` renders the offline-after field.
  - The existing settings POST-handler tests (subnet create/update/delete,
    integrations save, user create/delete, general save) remain unchanged and
    pass — endpoints and field names are untouched.
  - The onboarding welcome integrations step still renders the fields: the
    existing welcome test(s) that assert the integration inputs on
    `GET /welcome/integrations` remain green (the field templs are unchanged for
    that page).

## Project layout (files added / modified)

- Modify: `internal/web/views/settings.templ` (`SettingsData.Statuses`; split
  into `subnetsTab`/`integrationsTab`/`usersTab`/`generalTab`; per-integration
  field templs `proxmoxFields`/`wireguardFields`/`piholeFields`; `integrationsFields`
  becomes a thin wrapper; `integrationStatus` helper; card redesign) + regenerated
  `internal/web/views/settings_templ.go`. If `integrationsFields` lives in or is
  shared via a file the welcome page imports, keep it callable unchanged from
  `welcome.templ`.
- Modify: `internal/web/settings.go` (`handleSettingsPage` builds `Statuses`).
- Modify: `internal/web/static/app.css` (`.setting-card`, `.status-line`,
  `.field-grid`, `.tabs`/`.tab` pill restyle).
- Modify: `internal/web/settings_test.go` (integration-status render tests).

## Out of scope (D1)

- Live "Test connection" button / run-once endpoints.
- Changing the restart-to-apply integration model.
- The dashboard integrations widget (unchanged).
- The grid-v2 discoverability / Subnets nav page (that is D2).
