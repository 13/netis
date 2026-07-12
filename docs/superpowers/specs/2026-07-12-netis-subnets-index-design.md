# Netis — Subnets Index Page (Sub-project D2): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Second of two sub-projects (D1 settings redesign done → **D2 grid-v2
discoverability**). D2 makes the subnet occupancy grid (grid v2) reachable from
a top-nav entry instead of only via dashboard subnet cards.

## Purpose

Add a **Subnets** page — a top-nav link → a page listing every subnet as an
occupancy card, each opening its grid v2 (`/subnets/{id}`). The dashboard cards
keep linking too; this just adds a discoverable, dedicated entry point.

## Decisions (locked)

- Top-nav **Subnets** link (after Dashboard) → `GET /subnets` index page.
- Reuse the dashboard's existing occupancy computation and card markup — no new
  occupancy logic; extract the shared pieces so both pages use one definition.
- Read-only page; auth-gated like the rest (viewers may view). No store/schema
  change.

## Component 1: Shared subnet-row helper

The dashboard's `assembleDashboard` (`internal/web/dashboard.go`) already loops
`ListSubnets()` → `SubnetOccupancy(sn.ID)` → builds `[]views.DashRow` (with
`Used`, `Free`, `Hosts`, `Online`, `Reserved`). Extract that loop into a method:

```go
func (s *Server) subnetRows() ([]views.DashRow, error)
```

`assembleDashboard` calls it (replacing the inline loop) and keeps its extra
work (the per-IP conflict "unknowns"/attention list stays in `assembleDashboard`
— only the `DashRow` construction moves). The new index handler calls the same
helper. This keeps the occupancy numbers identical between the two pages.

## Component 2: Shared subnet card templ

Extract the dashboard's current subnet card (the `<div class="card">` with the
name link to `/subnets/{id}`, the `.occ` bar, and the online/reserved/free
legend, plus the non-WireGuard Scan button) into a reusable templ:

```go
templ subnetCard(r DashRow)
```

`DashboardPage`'s `for _, r := range d.Rows` loop renders `@subnetCard(r)` (same
output as today); `SubnetsPage` renders the same card. One card definition, two
consumers.

## Component 3: Subnets index page & handler

- `handleSubnetsIndex` (`GET /subnets`, auth-gated — no `requireAdmin`): calls
  `subnetRows()`; on error → 500; renders `views.SubnetsPage(username, rows)`.
- `SubnetsPage(username string, rows []DashRow)` templ: an `<h1>Subnets</h1>`, a
  short muted intro line, then a `.cards` grid of `@subnetCard(r)`. Empty state
  (`len(rows) == 0`) → a muted "No subnets yet — add one in Settings." with a
  link to `/settings?tab=subnets`.
- Route `s.mux.HandleFunc("GET /subnets", s.handleSubnetsIndex)` — distinct from
  the existing `GET /subnets/{id}` (Go 1.22 ServeMux routes them separately).

## Component 4: Navigation

Add `<a href="/subnets">Subnets</a>` to the top-nav `.links` in `layout.templ`,
placed after **Dashboard** (so the order is Dashboard · Subnets · Devices ·
Events · Settings). The existing `theme.js` active-nav longest-prefix match will
mark it active on `/subnets` and `/subnets/{id}`.

## Error handling

- `subnetRows()` propagates a `SubnetOccupancy`/`ListSubnets` error; both callers
  return 500 on it (unchanged dashboard behavior).
- Empty subnet list → the index renders the empty state (not an error).
- `GET /subnets` vs `GET /subnets/{id}`: the numeric-id route still matches
  `/subnets/5`; `/subnets` matches the index. No collision.

## Testing

- **web** (`internal/web/dashboard_test.go` or a new `subnets_test.go`):
  - `GET /subnets` with a seeded subnet renders its name, its CIDR, and a link
    to `/subnets/{id}`; the page contains the nav `Subnets` link (`href="/subnets"`).
  - `GET /subnets` with no subnets renders the empty-state text and the
    `/settings?tab=subnets` link.
  - The dashboard still renders its subnet cards (regression: extracting
    `subnetRows`/`subnetCard` didn't change dashboard output — an existing
    dashboard test that asserts a subnet card, or a new assertion, stays green).
  - A viewer session can `GET /subnets` (200, read-only).

## Project layout (files added / modified)

- Modify: `internal/web/dashboard.go` (extract `subnetRows()`; `assembleDashboard`
  calls it; add `handleSubnetsIndex`).
- Modify: `internal/web/server.go` (`GET /subnets` route).
- Modify: `internal/web/views/dashboard.templ` (extract `subnetCard`; `SubnetsPage`;
  dashboard loop calls `@subnetCard`) + regenerated `dashboard_templ.go`.
- Modify: `internal/web/views/layout.templ` (nav `Subnets` link) + regenerated
  `layout_templ.go`.
- Test: `internal/web/subnets_test.go` (new) or additions to `dashboard_test.go`.

## Out of scope (D2)

- Any change to the grid v2 page itself (`/subnets/{id}`).
- Per-subnet actions beyond the existing Scan button on the card.
- A dedicated subnets store query (reuses `ListSubnets`/`SubnetOccupancy`).
