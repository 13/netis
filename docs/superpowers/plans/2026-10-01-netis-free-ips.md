# Free static IPs (F) Implementation Plan

> Written after the merge (`f87c5ae`) as a record; every task is done.

**Goal:** Give a subnet an optional DHCP pool, keep the pool out of every "free" answer, and put each subnet's next free static address one click or keystroke away, with a hover card for grid ports.

**Architecture:** Two subnet columns and `Subnet.InDHCPPool`. One free-for-static rule shared by `freeSummary` (dashboard, palette) and the grid's `free`/`pool` cell states. Free ranges are computed from grid cells; the hover card reads per-port details from the grid's embedded JSON.

**Tech Stack:** Go, templ, htmx, plain JS, Playwright (e2e). Spec: `docs/superpowers/specs/2026-10-01-netis-free-ips-design.md`.

## Global Constraints

- Every migration exists under the same name in both dialect directories.
- Pool bounds are stored as canonical addresses; empty means no pool.
- "Free" never includes a pool address, in any view.
- `make generate` before `go test` when `.templ` changes. Commits: conventional prefix, ending with the session's attribution lines.

---

### Task 1: DHCP pool (`6fc9073`)

**Files:** Create `internal/store/migrations/{sqlite,postgres}/0018_dhcp_pool.sql`. Modify `internal/store/subnet.go`, `internal/store/subnet_test.go`, `internal/web/settings.go` (`parseDHCPPool`, `poolAddr`), `internal/web/views/settings_network.templ`, `internal/web/api.go`.

- [x] Migration adds `dhcp_start`, `dhcp_end`.
- [x] `Subnet.DHCPStart/DHCPEnd`, read and written by create, get, list, update; `InDHCPPool`.
- [x] Subnet forms take the pool (full address or last octet), with inline errors.
- [x] Subnets API returns the pool.
- [x] `TestSubnetDHCPPool`, `TestParseDHCPPool`, `TestSubnetFormDHCPPool`.

### Task 2: Free IPs on the dashboard and in the palette (`c350056`)

**Files:** Modify `internal/web/dashboard.go` (`freeSummary`), `internal/web/views/dashboard.templ`, `internal/web/search.go` (`freeQuery`, `searchFree`), `internal/web/static/palette.js`, `internal/web/views/palette.templ`, `internal/web/static/dialog.js` (`netisCopy`, `[data-copy-text]`).

- [x] Dashboard rows: next free IP link and copy button; free count leaves the pool out.
- [x] Palette "free [filter]" lists next free per subnet; Enter opens, Shift-Enter copies; `g f`.
- [x] `TestSearchFree`.

### Task 3: Subnet page and hover card (`c350056`)

**Files:** Modify `internal/web/grid.go` (`pool` state, `freeRanges`), `internal/web/views/grid_layout.go` (`FreeRange`, `shownRanges`, `rangeLabel`, `portCards`), `internal/web/views/grid.templ`, `internal/web/static/grid.js`, `internal/web/static/pages/subnets.css`, `internal/web/views/styleguide.templ`.

- [x] Pool ports drawn stippled; next free and free counts skip them.
- [x] Free ranges, largest six first; a click marks the run and opens its first address.
- [x] "Free only" filter, remembered per browser; copy button on Next free IP.
- [x] Hover card from `#grid-data`, keyboard-reachable; **C** copies.
- [x] `TestFreeRanges`, `TestGridDHCPPool`.

### Task 4: Docs and baselines (`c350056`)

- [x] `docs/usage.md` (dashboard, subnet grid, palette, `g f`), README feature line.
- [x] e2e seed gives LAN the pool `.100–.199`; screenshot baselines re-taken.
