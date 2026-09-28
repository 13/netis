# Netis — Roles and Audit Log (F5): Design

Date: 2026-09-28
Status: Approved (standing authorization), pre-implementation

## Purpose

Two gaps in multi-user installs:

1. A user's role is fixed at creation. Promoting a viewer or demoting an admin
   means deleting and recreating the account.
2. Nothing records who changed what. With more than one admin there is no way
   to answer "who deleted that subnet" or "was that failed login me".

## 1. Changing a role

- Route `POST /settings/users/{id}/role`, field `role` = `admin` | `viewer`,
  wrapped in `requireAdmin` (the route-walk test enforces this).
- Store `SetUserRoleGuarded(ctx, id, role) (changed bool, err error)`: a single
  `UPDATE ... WHERE id=? AND (?<>'viewer' OR role<>'admin' OR (SELECT COUNT(*)
  ... role='admin') > 1)` so demoting the last admin is refused atomically,
  exactly like `DeleteUserGuarded`. Setting the role a user already has is a
  no-op success.
- Handler: unknown id → 404; bad role → 400 inline on the Users tab; refused
  demotion → 400 "cannot demote the last admin"; success → 303 to the tab.
- Sessions are not revoked: the role is read from the database on every
  request (`GetSession` joins `user`), so a demotion takes effect on the
  demoted user's very next request. An admin may demote themselves while
  another admin exists; the next page they load is a viewer's.
- UI: each row of the Users table gets a role `<select>` + "set role" button.

## 2. Audit log

### Table (migration 0013, both dialects)

`audit_log(id, at, user_id NULL REFERENCES "user"(id) ON DELETE SET NULL,
username, action, target, detail, ip, status)`, with indexes on `at` (retention
sweep) and `(username, id)` / `(action, id)` for the filters. `username` is a
snapshot so an entry still names its actor after the account is deleted.

`status` (the HTTP status the request was answered with) is an addition to the
brief: without it a refused request (a viewer's 403, a wrong current password,
a demotion of the last admin) is indistinguishable from one that took effect.

### What gets recorded: route-pattern middleware

`Server.audit` wraps the mux, inside `requireAuth` (so the session user is on
the context) and inside `limitBody`. For every request whose method is not
GET/HEAD/OPTIONS and that matched a registered pattern (`r.Pattern`, set in
place by `ServeMux` since Go 1.22), it records one row after the handler has
run:

- `action`: a stable dotted name looked up from the pattern
  (`device.update`, `user.role`, `login`, ...). A pattern with no entry in the
  table records the pattern itself (`POST /api/devices`), so a mutating route
  added later — the write API, notifications — is covered automatically, just
  with a less pretty name.
- `target`: by default `<noun> <value>` from the pattern's first wildcard
  (`device 12`, `integration proxmox`); handlers may replace it with something
  readable (`user alice`, `subnet 10.0.0.0/24`).
- `detail`: empty unless a handler adds one (the new role, "wrong username or
  password", "rate limited"). Never form values in general, never passwords or
  secrets: detail is only ever what a handler explicitly writes.
- `ip`: `clientIP` (honours trusted proxies).
- `status`: captured from the response writer.
- actor: the session user; `/login` and `/setup` have none yet, so their
  handlers name one via the note.

Handlers enrich an entry through `auditNote(r)`, which returns a pointer held
on the request context by the middleware (nil-safe outside it).

Unmatched requests (404/405 from the mux) and requests stopped before the mux
(no session → redirect to login, cross-origin rejections, oversized bodies) are
not recorded: they never reached a handler and would be noise.

Failed logins record the account only when it exists. An unknown username is
recorded blank with detail "unknown user": people type passwords into the
username box, and the log must not become a list of them.

Writing the row uses `context.WithoutCancel` (a client hanging up must not
lose the entry) and a failure is logged, never surfaced to the client.

### UI: Settings → Audit (admins only)

- Tab link shown only when `IsAdmin`; `settingsData` maps `tab=audit` to
  `subnets` for viewers and never loads entries for them.
- Filters: user and action `<select>`s populated from the distinct values in
  the log; GET form, so filters live in the URL.
- Keyset pagination: 50 rows per page, newest first, "Older" link carries
  `before=<last id>`; "Newest" resets.
- Columns: time, user, action, target, detail, status, IP.

### Retention

New setting `audit_retention_days`, default 180, 0 = keep forever, edited on
the General tab next to the other retention windows. `runRetention` calls
`Store.PruneAudit(cutoff)` after the existing `Prune`, logging the count with
the rest. `migrate-db` copies `audit_log` (and resets its identity).

## Tests

- Store (both dialects): add/list/filter/keyset, distinct users/actions,
  prune, FK set-null on user delete, guarded role change incl. last admin.
- Web: role change success, last-admin refusal, 404, bad role, viewer 403
  (route walk); audit rows for login success/failure/unknown user, a device
  create, a viewer's refused post; no password in any row; audit tab admin-only
  and filtered; retention setting saved.
