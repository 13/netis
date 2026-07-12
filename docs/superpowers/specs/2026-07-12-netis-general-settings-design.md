# Netis — General Settings Expansion (Sub-project G3): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Add more to the Settings → **General** tab. Today it holds only "Offline after".
Add a **Defaults for new subnets** card (scan interval, subnet kind, auto-scan)
whose values prefill the Add-subnet form, so adding subnets is faster and
consistent.

## Purpose

The General tab is sparse (one field). Give it a coherent, genuinely-wired
expansion: configurable defaults for new subnets. These are self-contained
within the Settings page (consumed by the same `SettingsData` the tabs already
receive), so no invasive threading through the shared `Layout`.

Scope note (chosen default, per standing "always go"): rather than add many
loosely-wired knobs or a configurable instance name (which would require
threading a value through every `Layout` call site), G3 adds three practical,
fully-wired new-subnet defaults. Instance-name/branding and retention settings
are deliberately out of scope.

## Component 1: General tab — "Defaults for new subnets" card

Extend the existing General `<form action="/settings/general">` (one form, one
Save) with a second `.setting-card` holding three fields:

- **Default scan interval (s)** — `default_scan_interval_sec` (number, `min=30`,
  shown value = the setting or `120`).
- **Default subnet kind** — `default_subnet_kind` (`<select>` over `subnetKinds`
  = lan / wireguard / proxmox-bridge; selected = the setting or `lan`).
- **Default auto-scan** — `default_scan_enabled` (`<select>`: Enabled=`on` /
  Disabled=`off`; selected = the setting, default `on`). A `<select>` (not a
  checkbox) so the field is always submitted and never ambiguously absent.

The Availability card (offline_after) is unchanged; both cards live in the one
General form with the existing single Save button.

## Component 2: `handleGeneralSave` — persist the new keys

Keep `offline_after` **required** (1–10, existing behavior — existing tests post
only this and must stay green). Persist each new key only when its field is
present and valid, so a form that omits them (older callers/tests) is
unaffected:

```go
func (s *Server) handleGeneralSave(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.FormValue("offline_after"))
	if err != nil || n < 1 || n > 10 {
		http.Error(w, "offline_after must be an integer 1-10", 400)
		return
	}
	if err := s.store.SetSetting("offline_after", strconv.Itoa(n)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if v := r.FormValue("default_scan_interval_sec"); v != "" {
		iv, err := strconv.Atoi(v)
		if err != nil || iv < 30 {
			http.Error(w, "default scan interval must be an integer >= 30", 400)
			return
		}
		if err := s.store.SetSetting("default_scan_interval_sec", strconv.Itoa(iv)); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if v := r.FormValue("default_subnet_kind"); v != "" {
		if v != "lan" && v != "wireguard" && v != "proxmox-bridge" {
			http.Error(w, "bad subnet kind", 400)
			return
		}
		if err := s.store.SetSetting("default_subnet_kind", v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if v := r.FormValue("default_scan_enabled"); v != "" {
		if v != "on" && v != "off" {
			http.Error(w, "bad default scan enabled", 400)
			return
		}
		if err := s.store.SetSetting("default_scan_enabled", v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/settings?tab=general", http.StatusSeeOther)
}
```

The kind whitelist mirrors `handleSubnetCreate`'s existing check
(lan/wireguard/proxmox-bridge).

## Component 3: Add-subnet form consumes the defaults

In `subnetsTab`, the **Add subnet** form uses the saved defaults:

- Interval input `value = settingOr(d.Values, "default_scan_interval_sec", "120")`.
- Kind `<select>` marks the option equal to
  `settingOr(d.Values, "default_subnet_kind", "lan")` as `selected`.
- Auto-scan checkbox is `checked` when `defaultScanEnabled(d.Values)` (true
  unless the setting is `off`).

The **Adopt detected subnet** hidden form uses the interval default for its
`scan_interval_sec` hidden value; its `kind` stays `lan` and `scan_enabled`
stays `on` (detected subnets are LAN and normally want scanning) — unchanged
otherwise.

Two small helpers are added to `settings.templ`'s Go block:

```go
func settingOr(values map[string]string, key, def string) string {
	if v := values[key]; v != "" {
		return v
	}
	return def
}

func defaultScanEnabled(values map[string]string) bool {
	return values["default_scan_enabled"] != "off"
}
```

`handleSettingsPage` already loads all settings rows into `d.Values`, so no
handler change is needed to surface the new keys to the templ.

## Error handling

- `offline_after` invalid → 400 (unchanged).
- New fields present but invalid (interval < 30, bad kind, bad enabled) → 400.
- New fields absent → left unchanged (backward compatible).
- Add-subnet form falls back to `120`/`lan`/on when a default is unset.

## Testing

- **web** (`internal/web/settings_test.go`):
  - `TestGeneralSavesSubnetDefaults`: `POST /settings/general`
    `offline_after=3&default_scan_interval_sec=300&default_subnet_kind=proxmox-bridge&default_scan_enabled=off`
    → 303; `GetSetting` returns each persisted value.
  - `TestGeneralTabRendersDefaults`: `GET /settings?tab=general` body contains
    `name="default_scan_interval_sec"`, `name="default_subnet_kind"`,
    `name="default_scan_enabled"`.
  - `TestAddSubnetFormUsesDefaults`: `SetSetting("default_scan_interval_sec",
    "300")` and `SetSetting("default_subnet_kind", "proxmox-bridge")`; `GET
    /settings?tab=subnets` → the Add-subnet interval input has `value="300"` and
    the `proxmox-bridge` kind option is `selected`.
  - `TestGeneralSaveRejectsBadDefaults`: `default_scan_interval_sec=5` (< 30) →
    400.
  - Existing general-save test (posts only `offline_after`) stays green.

## Project layout (files added / modified)

- Modify: `internal/web/views/settings.templ` — `generalTab` defaults card;
  `subnetsTab` add-form prefill; `settingOr`/`defaultScanEnabled` helpers.
  Regenerate `settings_templ.go`.
- Modify: `internal/web/settings.go` — `handleGeneralSave` persists the new keys.
- Test: `internal/web/settings_test.go`.

## Out of scope (G3)

- Instance name / branding (would thread through every `Layout` call).
- Retention/purge of old devices or availability history (needs a background job).
- Applying the defaults retroactively to existing subnets.
- Any store/schema change — reuses the `setting` key/value table.

## Global constraints

- No new dependencies; no store/schema change.
- The web package must not import proxmox/pihole/wireguard.
- Regenerate templ after editing `.templ`
  (`export PATH="$(go env GOPATH)/bin:$PATH"` then `templ generate`); commit the
  regenerated `settings_templ.go` with source.
- Default-dark theme; reuse existing `.setting-card`/form styles (no new CSS).
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Delete stray `netis`/`netis.db*` before finishing.
