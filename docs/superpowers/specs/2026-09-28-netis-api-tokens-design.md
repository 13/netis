# netis F2: API tokens, write API, export and CSV import

Date: 2026-09-28. Status: implemented on branch `f2-api-tokens`.

## Goal

Make netis scriptable without borrowing a browser cookie: long-lived personal
API tokens, a small write API for devices, inventory export, and a CSV import
with a dry run.

## 1. API tokens

### Storage (migration 0011, both dialects)

```
api_token(
  id           identity primary key,
  user_id      -> "user"(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,   -- sha256 hex, like session.token_hash
  created_at   TEXT NOT NULL,          -- UTC RFC3339
  last_used_at TEXT NULL,
  expires_at   TEXT NULL               -- NULL = never expires
)
index on user_id
```

The token is hashed with the same `hashToken` (SHA-256, hex) used for sessions
since Q7: a copied database or backup carries no working credential, and a
plain digest is enough for a 256-bit random value.

The migration does not depend on 0010 (reserved for another branch); it only
creates a new table referencing `"user"`.

`netis migrate-db` copies the table (after `session`) and advances its identity.

### Format

`netis_` + base64url (no padding) of 32 random bytes — 49 characters. The
prefix makes a leaked token recognisable to secret scanners and to people.
The plaintext is shown once, on the page that created it, and never again.

### Who can do what

- Any signed-in user creates and revokes **their own** tokens on the new
  Settings > API tokens tab (label "API tokens" for every role).
- An admin sees every user's tokens on that tab (with the owner) and can revoke
  any of them.
- A token acts with its owner's current role: deleting the user deletes their
  tokens (cascade); demoting them demotes the token.
- Routes: `POST /settings/tokens` (create; `name`, optional `expires_days`,
  0/blank = never) and `POST /settings/tokens/{id}/delete`. Both are
  self-service (listed in the route-walk test's `selfService`), and the revoke
  handler scopes non-admins to their own tokens (someone else's id is a 404).

### Authentication

- `Authorization: Bearer <token>` is honoured on `/api/*` only. `/metrics`
  keeps its own `NETIS_METRICS_TOKEN` and is unchanged.
- When a Bearer header is present on `/api/*` it is the only credential
  considered: no fallback to the cookie. A missing/unknown/expired token is
  `401 {"error":"invalid or expired API token"}` with `WWW-Authenticate: Bearer`.
- Bearer requests skip the Origin check and `http.CrossOriginProtection`:
  they carry no ambient credential, so there is nothing to forge (a browser
  cannot attach an Authorization header cross-origin without a CORS preflight,
  which netis never grants).
- Rate limiting still applies: failed token lookups are counted per client
  address (20 per minute, same limiter type as login, IPv6 keyed by /64);
  over the limit is `429` JSON before the database is asked.
- `last_used_at` is written at most once a minute per token (conditional
  UPDATE), so a busy script does not turn every read into a write.
- Expired tokens are refused at lookup and deleted by the retention sweep
  (`RetentionResult.APITokens`).

## 2. Write API (admin role)

All wrapped in `requireAdmin`, which now answers `/api/*` with JSON
(`403 {"error":"admin only"}`). Errors are `{"error": "..."}` with 400
(validation, unknown parent/subnet), 404 (no device), 409 (MAC already
belongs to another device), 415 for a non-JSON body.

- `POST /api/devices` — body `{name, kind, notes, vendor, model, function,
  icon, parent_device_id, tags[], mac, ip, subnet_id}`. `name` and `kind`
  required. Validation is shared with the HTML create form (extracted into
  `checkNewDevice`), and the write is the Q9 `CreateDeviceWithIface`
  transaction. If `ip` is given without `subnet_id` the API picks the
  configured subnet containing it (the form still asks). Returns `201` with the
  device in the `GET /api/devices/{id}` shape and a `Location` header.
- `PATCH /api/devices/{id}` — any subset of `name, kind, notes, vendor, model,
  function, icon, parent_device_id, tags`. Absent fields are untouched;
  `"parent_device_id": null` clears the parent; `tags` replaces the set.
  Unlike the form, a bad kind or blank name is a 400 rather than ignored.
  Like the form, editing marks the device reviewed. Returns `200` + device.
- `DELETE /api/devices/{id}` — `204`, or `404`.

## 3. Export (any role)

- `GET /api/export/devices.json` — `{"devices":[{id,name,kind,source,vendor,
  model,function,notes,tags[],online,last_seen,ifaces:[{mac,hostname,
  ips:[{ip,kind,subnet}]}]}]}`.
- `GET /api/export/devices.csv` — one row per device:
  `id,name,kind,source,mac,ips,tags,notes,vendor,model,function,online,last_seen`.
  Multi-valued columns are joined with `;`. `Content-Disposition: attachment`.
  The CSV is directly re-importable (import matches on the first MAC).
- Cells starting with `= + - @` (tab/CR too) are prefixed with `'` so a
  device name cannot become a spreadsheet formula.
- Both reachable with a session or a token. Buttons on the device list.

## 4. CSV import (admin)

- Page `GET /devices/import` (viewers see an explanation, no form), linked
  from the device list. `POST /devices/import` (admin): multipart `file` or a
  `csv` text field; without `commit=1` it is a **dry run** that renders a
  preview table of creates / updates / unchanged / errors, carrying the CSV in
  a hidden field so "Import" re-posts the same data with `commit=1`.
- The body cap for this one route is 5 MB (`maxImportBytes`); everything else
  keeps the 1 MB cap from Q6.
- Columns by header name, case-insensitive, any order: `mac` (or `macs`,
  required; first of a `;`-list is the key), `name`, `kind`, `ip`/`ips`
  (first), `tags` (`;` or `,`), `notes`, `vendor`, `model`, `function`.
  Unknown columns (e.g. `id`, `source`, `last_seen` from an export) are ignored.
- Matching is by MAC only. No MAC, bad MAC, bad kind, duplicate MAC within the
  file → row error; error rows are skipped, the others still commit.
- **Create** (MAC unknown): needs `name`; kind defaults to `other`; source
  `manual`; IP attached as static when a configured subnet contains it,
  otherwise the row is an error.
- **Update** (MAC known) follows the project rule "enrich, don't clobber":
  - `notes`, `vendor`, `model`, `function` are filled only where the device has
    none. An existing value is kept.
  - `name` and `kind` are replaced only on devices netis guessed itself and no
    one has reviewed (`source='scan'`, `reviewed=false`). Integration-managed
    devices (Proxmox, WireGuard, Pi-hole) and anything reviewed keep their
    identity.
  - tags are added, never removed. IPs of existing devices are not touched.
  - The guards are in the SQL (`CASE WHEN col='' …`) as well as in the
    preview, so the commit cannot clobber a value that changed after preview.
  - An import update does not mark the device reviewed.

## Out of scope

Token scopes/read-only tokens, a JSON import endpoint, per-token rate limits
for valid tokens, editing IPs/interfaces through the API.
