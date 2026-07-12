# Netis — Design System Foundation & Theme Toggle (Sub-project A): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

This is sub-project **A** of a three-part UI/UX modernization:
- **A (this doc):** design-system foundation, light/dark theme toggle, shell
  restyle, redesigned subnet card, consistent card/border fixes.
- **B (later):** device-list overhaul — sortable, list/grid views, static/dhcp
  chips, reviewed/edited markers, default IP sort.
- **C (later):** device create/edit modal dialog, icon picker with per-kind
  defaults, searchable parent device, tags/category fields.

A ships the reusable components (card, pill, chip, button, segmented control,
icon tile, dialog shell) that B and C consume, so those become markup-only.

## Purpose

Replace netis's ad-hoc dark-only stylesheet with a token-based design system
that supports light and dark themes, a persisted top-right theme toggle
(default dark), and a consistent, modern component set — turning the app into
a polished home-network operations console. The approved visual direction is
the reviewed style tile (cool blue-slate neutrals, a signal-cyan accent kept
separate from the semantic green/red/amber status set, monospace + tabular
figures for addresses, one hairline-bordered card).

## Architecture

netis has no frontend build step: CSS is a single file embedded via `go:embed`
and served at `/static/app.css`; pages are templ server-rendered under a shared
`Layout`. A keeps that model:

- `app.css` is rewritten as a token system: CSS custom properties define the
  palette; components style through the tokens only.
- Theme is chosen at the root element via `data-theme`, bootstrapped before
  first paint by a tiny inline script, toggled by a nav control, and persisted
  in `localStorage`.
- The only new JavaScript is the inline no-flash bootstrap plus a small
  embedded `theme.js` (toggle handler + active-nav highlighting). No Go
  handler signatures change except the dashboard view-model gaining two
  computed counts for the subnet card.

## Token system (`internal/web/static/app.css`)

`:root` holds the **light** palette (the default). The **dark** palette is
redefined in three places that must stay identical in the token values they
set: `@media (prefers-color-scheme: dark)`, `:root[data-theme="dark"]`, and
(light re-asserted) `:root[data-theme="light"]`. Components reference tokens
only — never redefined inside a media query — so both themes stay in sync and
the explicit `data-theme` always wins over the OS preference in both
directions.

Token set (values from the approved style tile):

| Token | Light | Dark |
|---|---|---|
| `--bg` | `#eef1f5` | `#0b0f14` |
| `--surface` | `#ffffff` | `#131a22` |
| `--surface-2` | `#f3f6f9` | `#1a232d` |
| `--border` | `#d5dce4` | `#26313d` |
| `--border-strong` | `#c2ccd6` | `#34414f` |
| `--fg` | `#141a21` | `#e6edf3` |
| `--muted` | `#5a6674` | `#8a97a6` |
| `--accent` | `#0e8ba8` | `#38bdf8` |
| `--accent-fg` | `#ffffff` | `#04141d` |
| `--accent-soft` | `#d5f0f7` | `#0e2b3a` |
| `--ok` / `--ok-soft` | `#1a7f37` / `#d6f2dd` | `#3fb950` / `#10261a` |
| `--bad` / `--bad-soft` | `#c8202f` / `#fbe0e2` | `#f85149` / `#2c1416` |
| `--warn` / `--warn-soft` | `#8a5a00` / `#f7ead0` | `#d8a12b` / `#2b2411` |
| `--shadow` | light shadow | dark shadow |
| `--radius` / `--radius-sm` | `10px` / `6px` | same |

Base: `box-sizing:border-box`; body uses `system-ui` sans; `.mono` uses
`ui-monospace` with `font-variant-numeric: tabular-nums`. Transitions on
theme-affected properties are wrapped so `prefers-reduced-motion: reduce`
disables them.

## Theme toggle

- **Bootstrap (no flash):** an inline `<script>` in `layout.templ`'s `<head>`,
  before the stylesheet's visible content, sets
  `document.documentElement.dataset.theme` to `localStorage["netis-theme"]`,
  falling back to `matchMedia('(prefers-color-scheme: dark)')`. Because it runs
  before first paint, there is no light-flash for a dark-preferring user.
  Default is dark: when nothing is stored and the OS has no preference, dark is
  used.
- **Control:** a button at the right of the nav (sun/moon glyph + label). Its
  handler (in `theme.js`) flips `data-theme`, writes `localStorage`, and
  updates the glyph/label.
- **Persistence rationale:** theme is a personal, per-browser preference;
  `localStorage` avoids a server round-trip and is correct for netis's
  multi-user model (each admin/viewer keeps their own choice). No store or
  settings change.

## Components (`app.css`)

One definition each, all token-driven; these replace today's overlapping
`.card`/`.panel`/`.auth-card`/`.stat`/`.badge` treatments:

- **`.card`** — `--surface`, `1px var(--border)`, `--radius`, `--shadow`. The
  single card used by dashboard cards, stat tiles, settings/onboarding
  `auth-card`s, and panels. Fixing them to one definition is what removes the
  misaligned borders reported on settings and onboarding.
- **`.stat`** — a `.card` variant with an uppercase micro-label and a large
  tabular value (`.v.ok/.bad/.warn` for semantic coloring).
- **`.pill`** (`.online/.offline/.reserved`) — status pill with a leading dot,
  colored text on the matching `-soft` background. Replaces `.badge` for
  device/host status. Event-type badges (`device_new/online/offline/scan_error`
  on the dashboard and events pages) keep their existing color mapping,
  restyled through tokens.
- **`.chip`** (`.static/.dhcp`) — lease-type marker; `.static` uses the accent,
  `.dhcp` is muted. **`.tag`** — rounded tag.
- **`.btn`** / **`.btn.primary`** — default and accent buttons; `input`,
  `select` get consistent `--surface-2` fields and a visible focus ring
  (`--accent` border + `--accent-soft` glow) for keyboard accessibility.
- **`.seg`** — segmented control (B's list/grid toggle).
- **`.ic`** — rounded emoji icon tile.
- **`.dialog`** — modal shell (header / body / footer) with scrim styling
  (C's device modal).
- **table** — `thead` uses uppercase micro-labels on `--surface-2`, row hover,
  `tabular-nums`; **subnet grid squares** (`.sq.online/.offline/.reserved/
  .conflict`) and **tabs** (`.tabs/.tab.active`) restyled to tokens.

**Icons are emoji.** Zero embedded assets, theme-agnostic, universally
rendered; the picker in C is an emoji palette. A defines the `.ic` tile and a
default emoji for each of netis's ten device kinds (used by B/C):
`computer 💻, switch 🔀, phone 📱, server 🖥️, printer 🖨️, iot 💡, vm 🧊,
lxc 📦, wg-peer 🔒, other ❓`. (A only defines the mapping constant + tile;
B/C consume it.)

## Nav & shell (`layout.templ`, `theme.js`)

The nav becomes: brand (dot + wordmark) · section links (Dashboard / Devices /
Events / Settings) · right side (username + theme toggle). Active-section
highlighting is done in `theme.js` by matching `location.pathname` against each
link's `href` (longest-prefix match, `/` only exact), so no `Layout` signature
change and no edits to the ~7 call sites. `main` keeps the centered
max-width container.

`theme.js` is a new file under `internal/web/static/` (embedded by the existing
`//go:embed static`), loaded from `layout.templ` after the page. The no-flash
bootstrap stays inline in `<head>` (it must run before paint).

## Redesigned subnet card (`dashboard.templ`, `dashboard.go`)

The dashboard subnet cards (currently plain text) become the style-tile design:
title + monospace CIDR on one row, a thin **occupancy bar** (online / reserved
/ offline / free segments), and a legend with tabular counts.

The bar needs four proportions. `handleDashboard`/`assembleDashboard` already
iterates `SubnetOccupancy` per subnet; it will compute, per subnet:
- `Online` = occupants with `Online` true (already computed);
- `Reserved` = occupants with `EverSeen` false (assigned, never seen);
- `Offline` = occupants with `EverSeen` true and `Online` false;
- `Free` = `len(HostIPs) − Used` (already computed as `Free`).

`views.DashRow` gains `Reserved int` and `Offline int`. The template renders
segment widths as percentages of `len(HostIPs)` (clamped so rounding never
overflows 100%), with a `title`/legend showing the raw counts. Subnets with a
huge host space still render a sensible bar (segments are proportional).

## Error handling

- The theme bootstrap is defensive: a `try/catch` around `localStorage`
  access (private-mode / disabled storage) falls back to the media query, so
  the page never fails to render a theme.
- Occupancy math clamps segment percentages to a `[0,100]` sum; a subnet with
  zero hosts renders an empty bar rather than dividing by zero.

## Testing

- `templ generate` clean; full `go test ./... -count=1` green (this is
  primarily CSS/templ; the suite must not regress).
- `internal/web/dashboard_test.go`: seed a subnet with one online, one
  reserved (assigned, never seen), and one offline device; assert the
  resulting `DashRow` has `Online=1, Reserved=1, Offline=1` and the expected
  `Free`.
- `internal/web/`: assert the dashboard page HTML contains the theme-toggle
  control and the `data-theme` bootstrap script (guards the toggle wiring).
- Manual visual smoke against the running binary (both themes; confirm the
  settings/onboarding/dashboard card borders are aligned and consistent) is
  run before the sub-project is finished; automated visual regression is out
  of scope.

## Project layout (files added / modified)

- Create: `internal/web/static/theme.js`.
- Modify: `internal/web/static/app.css` (full rewrite to tokens + components),
  `internal/web/views/layout.templ` (nav, head bootstrap, toggle, theme.js
  include), `internal/web/views/dashboard.templ` (subnet card),
  `internal/web/dashboard.go` (compute Reserved/Offline),
  `internal/web/views/dashboard.templ`'s `DashRow` (add `Reserved`, `Offline`),
  `internal/web/dashboard_test.go`.
- No store, migration, or Go route changes.

## Out of scope (A)

- Device-list behavior: sorting, list/grid toggle, static/dhcp display,
  reviewed/edited flags (sub-project B).
- Device dialog, icon picker, searchable parent, tags/category fields
  (sub-project C).
- Embedding a webfont or an SVG icon set (system fonts + emoji by decision).
- Server-side theme persistence (client `localStorage` by decision).
