# netis UI redesign (U-series): design brief

Date: 2026-09-29. Source: visual/UX audit of 141 screenshots of v0.3.0
(dashboard, devices, device detail, grid, events, settings, onboarding, empty
states, viewer role, desktop + mobile, light + dark).

## Subject, audience, job

netis is a home-network inventory and monitor. Audience: a homelab owner who
knows what a subnet and a MAC are, checks in a few times a week, and wants to
answer "what is on my network, what is new, what is down, what IP is free"
fast. Primary job: scan the state of the network at a glance, then act on the
one thing that needs attention.

## Direction: patch panel and link lights

The visual vocabulary comes from the rack: steel panels, numbered ports,
patch cables, link/activity LEDs. Calm, dense, precise. Status colour is
reserved for status (it means what an LED means) and never decorates.

The one bold element is the **subnet grid drawn as a patch panel**: rows of
16 ports with octet numbering along the edge, each port a small recessed
socket with an LED dot (link green = online, dim = offline, amber = reserved,
red = conflict, empty socket = free). Everything around it stays quiet.

## Tokens

Colours (defined once per theme in `app.css`; components use tokens only):

| Token | Light | Dark | Role |
|---|---|---|---|
| `--bg` | `#ECEEEC` | `#161A18` | page (brushed steel) |
| `--surface` | `#FFFFFF` | `#1D2220` | panels |
| `--surface-2` | `#F5F6F4` | `#242A27` | recessed / hover |
| `--border` | `#D3D7D3` | `#323A36` | hairlines |
| `--fg` | `#1A1F1C` | `#E4E8E5` | text |
| `--muted` | `#5B645F` | `#97A19B` | secondary text |
| `--accent` | `#1F5FD1` | `#6E9BFF` | actions, links (patch-cable blue) |
| `--link` (status) | `#1E9E46` | `#3CCB6A` | online |
| `--activity` | `#B97800` | `#F0B429` | new / reserved / attention |
| `--fault` | `#D1343B` | `#FF6B6B` | offline-with-alert, conflict, error, danger |

Each status colour has a `-soft` background variant. All text/background
pairs must meet WCAG AA in both themes (check with a contrast script).

Type: **IBM Plex Sans** (UI text) and **IBM Plex Mono** (IP addresses, MACs,
CIDRs, ports only — never for labels or dates). Self-hosted woff2 (latin
subset, 400/500/600), embedded with go:embed, OFL licence file included. No
CDN (offline homelab, CSP). Scale (rem): 0.75, 0.8125, 0.875 (body), 1, 1.25,
1.625. Line height 1.5 body, 1.2 headings. Tabular figures for numbers.

Spacing 4px base (4, 8, 12, 16, 24, 32, 48). Radius: 4px controls, 8px panels
— hierarchy, not one radius everywhere. Shadows: none on panels (hairline
border instead); a single shadow for overlays (dialogs, menus, toasts).

Icons: Lucide SVG (ISC licence) in one embedded sprite, 16/20px, stroke 1.75,
`currentColor`. One icon per device kind; unknown kind = neutral
"circle-help"-style icon in muted colour, never red.

## Principles

1. Status colour means status. Buttons, headings and decoration never use
   green/amber/red. Danger actions use `--fault` text/outline.
2. Words for people, not the schema: "New device", "Went offline", "Sync
   failed" — never `device_new`, `scan_error`, HTTP codes or action slugs.
   Sentence case everywhere. Buttons say what happens ("Save changes",
   "Scan now"); the toast repeats the verb ("Scan started").
3. Every timestamp is `<time datetime=…>` with relative text ("4 min ago") and
   the absolute local time on hover; never raw ISO in the UI.
4. Every empty screen explains itself and offers the one next action.
5. Dense but calm: tables are the primary surface; no card-per-item grids
   except tiles view; no decorative gradients; no all-caps labels; no
   eyebrow labels; no numbered markers unless it is a real sequence.
6. Motion only in answer to an action (dialog open, row update flash on SSE);
   `prefers-reduced-motion` disables it.
7. Works without JavaScript for core reading and forms; htmx enhances.
8. Mobile is first-class: at < 640px tables become stacked rows that keep
   name + status + IP; bottom tab bar; 44px targets.

## Information architecture

- Desktop: left sidebar (netis mark, Dashboard, Devices, Subnets, Events;
  bottom: Settings, account menu with theme + sign out). Mobile: bottom tab
  bar (Dashboard, Devices, Subnets, Events, More).
- Command palette (Ctrl/⌘-K, `/` focuses search): jump to device by name/IP/
  MAC, subnet, page, or action (new device, scan all).
- Settings split: **Account** (everyone: profile, password, sessions, API
  tokens, SSO link) and **Admin** (Network = subnets + scanning, Integrations,
  Notifications, Users, Audit, System = retention, backups, about). Subnets
  are managed in one place only.
- Integrations: one panel each with status, last run, Run now, Configure
  (form in a dialog/disclosure), not a wall of open forms.

## Series

- U1 visible bugs; U2 design system + style tile; U3 navigation/IA + command
  palette; U4 pages (dashboard, devices, device detail, grid, events, device
  form); U5 empty states + onboarding; U6 mobile; U7 best practice (time,
  contrast, favicon/manifest, loading states, visual regression in CI); U8
  copy pass. U5–U8 are applied inside each page's work and finished with a
  sweep.
