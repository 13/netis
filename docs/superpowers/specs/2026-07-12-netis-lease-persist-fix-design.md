# Netis — Lease-Persist Fix (Sub-project G1): Design

Date: 2026-07-12
Status: Approved design, pre-implementation

Bug fix. A manually-set **static** lease reverts to **dhcp** on reload because the
Pi-hole sync's DHCP-lease path overwrites it every run. F4 (periodic sync every
minute) made the revert near-immediate.

## Root cause (verified)

`internal/pihole/sync.go` processes Pi-hole **reservations** first
(`upsertByMAC(..., "static")`, recorded in a `claimed` set) then **leases**
(`upsertByMAC(..., "dhcp")` for any IP not `claimed`). `upsertByMAC` calls
`store.UpsertIPAssignment(ifaceID, subnetID, ip, kind)`, whose update branch is:

```go
_, err = s.DB.Exec(`UPDATE ip_assignment SET kind=? WHERE id=?`, kind, id)
```

This is **unconditional**: an IP that Pi-hole reports as a DHCP lease is written
back as `kind='dhcp'` on every sync. The `claimed` guard only shields Pi-hole's
*own* reservations — a user's **manual** static (set via the device-detail lease
toggle / grid cell toggle, `store.SetIPKind`) on an IP that Pi-hole sees only as
a lease is silently downgraded to `dhcp` on the next sync. So: toggle → static,
sync runs (≤1 min, or at boot), UpsertIPAssignment overwrites → reload shows
`dhcp`.

## Fix: `UpsertIPAssignment` never downgrades static → dhcp

Integration-driven assignment is not authoritative over a user's explicit
static choice. Change `UpsertIPAssignment`'s update branch so it never
downgrades an existing `static` to `dhcp`:

```go
// Never downgrade a static assignment to dhcp: a user (or a reservation)
// marked this IP static, and a dhcp-lease observation must not clobber it.
_, err = s.DB.Exec(
	`UPDATE ip_assignment SET kind=? WHERE id=? AND NOT (kind='static' AND ?='dhcp')`,
	kind, id, kind)
return err
```

Effect:
- Existing `static` + incoming `dhcp` → no-op on kind (static preserved). **Fixes the bug.**
- Existing `dhcp` + incoming `static` → upgraded to static (Pi-hole reservation upgrade path still works).
- Existing `static` + incoming `static`, existing `dhcp` + incoming `dhcp` → unchanged.
- Absent row → inserted with the given kind (insert branch unchanged).

The insert branch and the hostname-enrich logic are untouched.

## Why the manual toggle stays authoritative

The device-detail and grid lease toggles call `store.SetIPKind` (a direct
`UPDATE … SET kind=… WHERE subnet_id=? AND ip=?`), which remains
**unconditional** — the user can still toggle static⇄dhcp in either direction.
Only `UpsertIPAssignment` (integration-driven) becomes non-downgrading, so the
periodic Pi-hole sync stops clobbering the user's choice.

## Error handling

- Unchanged elsewhere. The guarded UPDATE affecting 0 rows (a no-op downgrade) is
  not an error, matching the existing tolerance.

## Testing

- **store** (`internal/store/device_test.go` or `grid_test.go` — wherever the
  ip-assignment store tests live; create the file if none):
  - `TestUpsertIPAssignmentNeverDowngradesStatic`: `AssignIP(iface, subnet, ip,
    "static")`; `UpsertIPAssignment(iface, subnet, ip, "dhcp")`; assert
    `ListIPs(iface)` shows the IP still `static`.
  - `TestUpsertIPAssignmentUpgradesDhcpToStatic`: assignment starts `dhcp`;
    `UpsertIPAssignment(..., "static")` → becomes `static` (upgrade preserved).
  - `TestUpsertIPAssignmentInsertsWhenAbsent`: no row → `UpsertIPAssignment(...,
    "dhcp")` inserts a `dhcp` row.
- **pihole** (existing tests in `internal/pihole/`): a lease for an
  already-static IP must leave it static. Add/confirm a case:
  `TestPiholeLeaseKeepsManualStatic` — seed an iface with a static IP, run the
  sync with that IP present only as a **lease** (no reservation), assert the IP
  stays `static` after `RunOnce`. If the existing pihole test harness makes this
  awkward, the store-level tests above are the primary guard and this one is
  best-effort.

## Project layout (files added / modified)

- Modify: `internal/store/device.go` — `UpsertIPAssignment` update branch.
- Test: `internal/store/*_test.go` — the three store cases.
- Test (best-effort): `internal/pihole/*_test.go` — lease-keeps-static.

## Out of scope (G1)

- The grid-square click behavior and settings-config additions (separate
  sub-projects).
- Provenance tracking (a `source`/`manual` flag on assignments) — the
  non-downgrade rule is sufficient for the reported bug without new columns.
- Changing `SetIPKind` (the manual toggle stays unconditional) or `AssignIP`.

## Global constraints

- No new dependencies; no schema change (behavioral SQL change only, no
  migration).
- The web package must not import proxmox/pihole/wireguard.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Delete stray `netis`/`netis.db*` before finishing.
