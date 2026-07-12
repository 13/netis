# Netis — Onboarding & Settings Clarity: Design

Date: 2026-07-12
Status: Approved design, pre-implementation

## Purpose

Make netis usable within seconds of first launch and make the settings page
readable:

- **Auto-detect subnets** from the host's network interfaces so the user
  doesn't have to know or type their LAN CIDR.
- **First-run quick-setup wizard** that runs right after admin creation: pick
  detected subnets to scan, optionally configure an integration, and land on
  a populated dashboard instead of an empty one.
- **Tabbed settings** so the long stacked page (Subnets / Integrations /
  Users / General) shows one section at a time.

## Architecture

Three loosely-coupled pieces sharing one primitive (`netdetect`):

- A pure `internal/netdetect` package that lists scannable IPv4 subnets from
  the host interfaces, with an injectable interface-lister seam for tests.
- A wizard in `internal/web` gated by a new `onboarded` setting, reachable
  only by the authenticated admin.
- A settings-template restructure into server-rendered tabs, plus reuse of
  `netdetect` on the Subnets tab.

No new tables. The only new persisted state is the `onboarded` setting
(`"1"` once setup is complete), stored via the existing `setting` table.

## Component 1: Subnet detection (`internal/netdetect`)

```go
type Detected struct {
	CIDR  string // masked network, e.g. "192.168.1.0/24"
	Iface string // interface name, e.g. "eth0"
}

// DetectSubnets returns scannable IPv4 subnets from the host interfaces.
func DetectSubnets() ([]Detected, error)
```

Internally `DetectSubnets` calls an unexported `detectFrom(lister)` where
`lister func() ([]ifaceInfo, error)` is the seam tests inject.
`ifaceInfo{ Name string; Up bool; Loopback bool; Addrs []netip.Prefix }`. The
production lister wraps `net.Interfaces()` / `iface.Addrs()`.

Filter rules (an interface's address is kept only if all hold):
- interface is `Up` and not `Loopback`;
- the address is IPv4 (`Addr().Is4()`);
- not link-local (`!Addr().IsLinkLocalUnicast()`, i.e. skips `169.254.0.0/16`);
- prefix length ≤ 30 (skip `/31` and `/32` host routes);
- the interface name does not start with a virtual prefix: `docker`, `veth`,
  `br-`, `tap`, `cni`, `virbr`, `lo`.

The kept CIDR is `prefix.Masked().String()`. Results are deduped by CIDR
(first interface wins for the `Iface` label) and returned sorted by CIDR for
determinism. Dedupe-against-already-configured subnets is the caller's job
(wizard and settings), keeping this package pure and independently testable.

## Component 2: Quick-setup wizard (`internal/web/welcome.go`, `views/welcome.templ`)

### Gating

A new setting `onboarded`. It is `""` (falsy) on a fresh install and `"1"`
once the wizard is completed or skipped.

- `handleSetup` (admin creation) redirects to `/welcome` instead of `/`.
- A redirect check in the auth middleware (`requireAuth`): once a request is
  authenticated, if `onboarded != "1"` and the path is not already under the
  onboarding/allowed set (`/welcome`, `/welcome/…`, `/logout`, `/login`,
  `/setup`, `/static/`), redirect (303) to `/welcome`. This survives a
  restart mid-wizard and prevents landing on an empty dashboard.

### Step 1 — Subnets

- `GET /welcome` renders the detected subnets (`DetectSubnets()` minus those
  already in `ListSubnets()`) as pre-checked checkboxes, each labelled with
  its interface, plus one free-text "add another CIDR" input. Each checkbox is
  `<input type="checkbox" name="subnet" value="<cidr>|<iface>">` so the POST
  carries both the CIDR and the interface label.
- `POST /welcome/subnets`: for each checked `subnet` value, split on `|` into
  CIDR and iface; for the optional manual CIDR the iface is empty. Validate
  the CIDR with `netip.ParsePrefix`, store `.Masked().String()`, and create a
  subnet with `kind=lan`, `scan_enabled=true`, `scan_interval_sec=120`, and
  `name` = the interface label (or the CIDR when the label is empty). Invalid
  CIDRs are skipped. Redirect to `/welcome/integrations`.

### Step 2 — Integrations (optional)

- `GET /welcome/integrations` renders the same Proxmox / WireGuard / Pi-hole
  fieldsets used on the settings page (identical field names), with a "Skip"
  action and a "Save & finish" action.
- `POST /welcome/integrations` calls the shared `saveIntegrationSettings(r)`
  (Component 3), sets `onboarded=1`, redirects to `/`.
- `POST /welcome/skip` sets `onboarded=1`, redirects to `/`. (Skipping writes
  no integration settings.)

All four routes require auth (the logged-in admin). Because the middleware
allows `/welcome*`, the wizard is reachable while `onboarded` is unset.

## Component 3: Settings tabs + detect button (`internal/web/settings.go`, `views/settings.templ`)

### Tabs

`GET /settings?tab=subnets|integrations|users|general`, default `subnets`.
The handler validates the tab against that fixed set (unknown → `subnets`)
and passes it to the template as `ActiveTab`. The template renders a tab nav
(four links, the active one marked) and only the active section's content.
All existing POST handlers and form actions are unchanged; each redirects to
its own tab on success:
- subnet create/update/delete → `/settings?tab=subnets`
- integrations save → `/settings?tab=integrations`
- user create/delete → `/settings?tab=users`
- general save → `/settings?tab=general`

### Detect button on the Subnets tab

The Subnets tab also renders `DetectSubnets()` minus already-configured
subnets as one-click "add" rows: each is a tiny form prefilling the existing
`POST /settings/subnets` create handler (`cidr`, `name`=iface, `kind=lan`,
`scan_interval_sec=120`, `scan_enabled=on`). No new handler. If nothing new is
detected, the section shows "no new subnets detected".

### Shared integration save

The per-key loop currently inside `handleIntegrationsSave` (blank-keeps
`proxmox_secret`/`pihole_password`, normalizes `*_insecure` `on→1`, writes the
rest) is extracted into `func (s *Server) saveIntegrationSettings(r *http.Request) error`
and called by both `handleIntegrationsSave` and `POST /welcome/integrations`.
No behavioral change; removes duplication.

## Error handling

- `DetectSubnets` returning an error (rare — `net.Interfaces()` failure) is
  handled gracefully by callers: the wizard/settings render with an empty
  detected list and a note, never a 500.
- Invalid CIDRs submitted in the wizard or settings are skipped silently
  (settings subnet create already returns 400 on an invalid CIDR for the
  manual form; the wizard skips invalid entries so one typo doesn't block the
  whole batch).
- The onboarding redirect fails safe: a `GetSetting("onboarded")` error is
  treated as not-onboarded (redirect to `/welcome`), never a crash.

## Testing

- **netdetect** (`detect_test.go`): a fixture lister yields interfaces
  covering each rule — a normal `eth0` (kept), `lo` (skipped), a down
  interface (skipped), a `docker0`/`veth…` (skipped by prefix), a link-local
  `169.254.x` address (skipped), an IPv6 address (skipped), and two
  interfaces on the same CIDR (deduped). Assert the kept set, the masked
  CIDRs, and sorted order.
- **welcome** (`welcome_test.go`): `GET /welcome` (authed) renders a detected
  CIDR as a checkbox; `POST /welcome/subnets` with a checked CIDR creates the
  subnet with the right defaults; `POST /welcome/skip` sets `onboarded=1`;
  after onboarding, a normal authed request to `/` is NOT redirected, while
  before onboarding it IS redirected to `/welcome`. Detection is injected via
  a test seam so no real host networking is used.
- **settings** (`settings_test.go`): `GET /settings?tab=users` renders the
  users section and not the subnet create form; an unknown `?tab=` falls back
  to subnets; a detected-but-unconfigured subnet appears as an add row on the
  subnets tab; `saveIntegrationSettings` still keeps a blank secret and
  normalizes the insecure checkbox (the existing secret test continues to
  pass through the refactor).

## Project layout (files added / modified)

- Create: `internal/netdetect/detect.go`, `internal/netdetect/detect_test.go`,
  `internal/web/welcome.go`, `internal/web/views/welcome.templ`,
  `internal/web/welcome_test.go`.
- Modify: `internal/web/auth.go` (setup redirect to `/welcome`; onboarding
  redirect in `requireAuth`), `internal/web/settings.go` (extract
  `saveIntegrationSettings`, active-tab handling, detected list, tab-aware
  redirects), `internal/web/views/settings.templ` (tab nav + per-tab
  sections + detect rows), `internal/web/server.go` (welcome routes),
  `internal/web/static/app.css` (tab + wizard styling),
  `internal/web/settings_test.go`.
- To inject detection into the web layer for tests, the `Server` gains a
  `detect func() ([]netdetect.Detected, error)` field defaulting to
  `netdetect.DetectSubnets`; tests set it to a fixture. `cmd/netis/main.go` is
  unchanged (the default is wired in `NewServer`).

## Out of scope

- Re-running the full wizard after onboarding (the settings detect button
  covers ongoing subnet discovery).
- Detecting/scanning IPv6 subnets (the scanner is IPv4-only).
- Auto-starting a scan from the wizard (the scheduler picks up new
  scan-enabled subnets on its next tick).
- Persisting per-interface metadata beyond the created subnet.
