# Netis — Onboarding Redesign (Sub-project E3): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Third of three sub-projects in this batch (E1 done, E2 done → **E3 onboarding**).
E3 redesigns the setup + welcome flow into a focused, modern, eye-candy
onboarding experience.

## Purpose

Make first-run onboarding beautiful and guided: a centered, branded flow with a
3-step stepper (Account → Subnets → Integrations) on a soft accent backdrop,
replacing the plain `.auth-card`/`@Layout` forms — while keeping every endpoint
and form field unchanged so the handlers and existing tests still work.

## Decisions (locked)

- A shared **onboarding shell** (`onboardShell`) — its own focused page (no top
  nav), with a brand hero + a 3-step stepper + a polished card — used by Setup
  and both Welcome steps.
- Steps: **1 Account** (Setup / create admin) · **2 Subnets** (WelcomeSubnets) ·
  **3 Integrations** (WelcomeIntegrations).
- Keep all view-function signatures, POST endpoints, and form field names
  unchanged (handlers untouched). `integrationsFields` stays as the same
  `<fieldset><legend>` blocks (the D1 welcome test asserts them).
- Login keeps its own `authShell` but gains the same brand hero for a cohesive
  auth look.
- Theme-aware, token-driven CSS; no external assets (CSP-safe, all inline).
- No store/schema/handler change.

## Component 1: `onboardShell` + brand + stepper

- `templ onboardBrand()` — the netis dot mark + `netis` wordmark + a one-line
  tagline ("Your home network, organized."). Reused by `onboardShell` and
  `LoginPage`.
- `templ onboardStepper(step int)` — a horizontal 3-step indicator with labels
  `Account`, `Subnets`, `Integrations`; the step at index `step` is `.active`,
  lower indices `.done` (check mark), higher `.pending`, joined by connector
  lines.
- `templ onboardShell(title string, step int)` — a full HTML document like
  `authShell` (no-flash theme bootstrap defaulting dark, `app.css`, `theme.js`),
  `<body class="onboard">` with the soft-gradient backdrop, containing
  `@onboardBrand()`, `@onboardStepper(step)`, and a `.onboard-card` wrapping
  `{ children... }`.

## Component 2: Setup (step 1 — Account)

`SetupPage(errMsg string)` renders through `@onboardShell("Setup", 1)`:
- heading "Create your admin account", a short muted subline, the existing
  `username` + `password` (`minlength=8`) inputs, an error line when `errMsg`,
  and a primary **Create account** button. POST target `/setup` unchanged.

## Component 3: Welcome subnets (step 2)

`WelcomeSubnets(username string, detected []netdetect.Detected)` renders through
`@onboardShell("Welcome", 2)` (the `username` param is now unused — the shell has
no nav; kept to avoid a handler change):
- heading "Which subnets should netis scan?", the detected subnets as polished
  `.toggle-row` checkbox rows (`name="subnet"` value `CIDR|Iface`, checked), the
  `manual_cidr` add field, and a primary **Continue** button. POST
  `/welcome/subnets` unchanged; empty-detected message preserved.

## Component 4: Welcome integrations (step 3)

`WelcomeIntegrations(username string, values map[string]string)` renders through
`@onboardShell("Welcome", 3)`:
- heading "Connect an integration (optional)", `@integrationsFields(values)`
  unchanged (same fieldsets/legends/field names), a primary **Save & finish**
  button (POST `/welcome/integrations`), and a secondary **Skip for now** button
  (POST `/welcome/skip`).

## Component 5: Login brand hero

`LoginPage(errMsg string)` keeps `@authShell` but renders `@onboardBrand()` above
its card so login matches the onboarding look. Fields/endpoint unchanged.

## Component 6: CSS (`app.css`, additive)

- `.onboard` — full-viewport centered flex column on a soft backdrop:
  `background: radial-gradient(1200px 600px at 50% -10%, var(--accent-soft), transparent), var(--bg);`
  (theme-aware; the accent glow sits behind the card). Padding + `min-height:100vh`.
- `.onboard-brand` — centered brand hero: an enlarged dot (`var(--accent)` with an
  `--accent-soft` ring/glow), the `netis` wordmark, and a muted tagline.
- `.onboard-card` — surface card, `max-width:420px`, generous padding,
  `var(--shadow-lg)`, rounded; a column of labeled inputs + CTA.
- `.stepper` / `.step` / `.step.active` / `.step.done` / `.step .num` — the 3-step
  indicator: numbered/checked circles with labels and connector lines; active uses
  `--accent`/`--accent-soft`, done uses `--ok`, pending muted.
- `.toggle-row` — a bordered row for the subnet checkboxes (checkbox + mono CIDR +
  muted iface), hover highlight.
- Optional tasteful motion: a gentle fade/slide-in on `.onboard-card`
  (`@keyframes`), respecting `prefers-reduced-motion`.

All token-driven; both light and dark themes covered by the existing tokens.

## Error handling

- Setup validation errors still render via `errMsg` (unchanged handler behavior:
  400 + re-rendered SetupPage with the message).
- No new failure modes; the redesign is presentation-only.

## Testing

- **web** (`internal/web/auth_test.go` / `welcome_test.go`):
  - `GET /setup` renders `name="username"` + `name="password"` inputs and the
    onboarding chrome — contains the stepper (`class="stepper"`) and the `netis`
    brand wordmark and the step label `Integrations`.
  - `GET /welcome` renders a detected-subnet `name="subnet"` checkbox, the
    `Continue` button, and the stepper (step 2 active).
  - `GET /welcome/integrations` still renders `name="proxmox_url"` / `name="wg_ssh_addr"`
    / `name="pihole_url"` and `<legend>` (the existing D1 welcome test stays
    green), plus the stepper.
  - `GET /login` renders the `netis` brand and the sign-in fields.
  - The existing onboarding POST-flow tests (setup create, welcome subnets create,
    welcome integrations save, welcome skip, viewer-403 on welcome POSTs) stay
    green — endpoints and field names unchanged.

## Project layout (files added / modified)

- Modify: `internal/web/views/auth.templ` (`onboardBrand`, `onboardStepper`,
  `onboardShell`; `SetupPage` uses `onboardShell`; `LoginPage` gains
  `@onboardBrand`) + regenerated `auth_templ.go`.
- Modify: `internal/web/views/welcome.templ` (`WelcomeSubnets`,
  `WelcomeIntegrations` use `onboardShell`) + regenerated `welcome_templ.go`.
- Modify: `internal/web/static/app.css` (onboarding styles).
- Test: additions to `internal/web/auth_test.go` and `internal/web/welcome_test.go`.

## Out of scope (E3)

- Changing the onboarding step order, endpoints, or the fields collected.
- Reworking the post-onboarding dashboard/nav.
- Any store/handler change.
