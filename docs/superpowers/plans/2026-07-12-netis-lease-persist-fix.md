# Netis Lease-Persist Fix (Sub-project G1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A manually-set static lease no longer reverts to dhcp — `UpsertIPAssignment` (integration-driven) never downgrades an existing `static` to `dhcp`, so the periodic Pi-hole sync stops clobbering the user's toggle.

**Architecture:** One-line SQL guard in `store.UpsertIPAssignment`; the manual `SetIPKind` toggle stays unconditional. No schema change.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, `go test ./...`.

## Global Constraints

- No new dependencies; no schema change (behavioral SQL only).
- The web package must NOT import proxmox/pihole/wireguard.
- Commit trailer `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- If you build/run the app, delete stray `netis`/`netis.db*` before finishing.

---

### Task 1: `UpsertIPAssignment` never downgrades static → dhcp

**Files:**
- Modify: `internal/store/device.go` (`UpsertIPAssignment` update branch)
- Test: `internal/store/device_test.go` (store cases)
- Test: `internal/pihole/sync_test.go` (lease-keeps-static)

**Interfaces:**
- Unchanged signature: `func (s *Store) UpsertIPAssignment(ifaceID, subnetID int64, ip, kind string) error`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/device_test.go` (uses the existing `openTest`/`strp` helpers):

```go
func TestUpsertIPAssignmentNeverDowngradesStatic(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:30"), nil)
	// User manually set this IP static.
	if _, err := s.AssignIP(ifID, snID, "10.0.0.30", "static"); err != nil {
		t.Fatal(err)
	}
	// A pihole dhcp-lease upsert must NOT downgrade it.
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.30", "dhcp"); err != nil {
		t.Fatal(err)
	}
	ips, _ := s.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("static must survive a dhcp upsert, got %+v", ips)
	}
}

func TestUpsertIPAssignmentUpgradesDhcpToStatic(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:31"), nil)
	if _, err := s.AssignIP(ifID, snID, "10.0.0.31", "dhcp"); err != nil {
		t.Fatal(err)
	}
	// A reservation upgrade dhcp -> static must still work.
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.31", "static"); err != nil {
		t.Fatal(err)
	}
	ips, _ := s.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("dhcp should upgrade to static, got %+v", ips)
	}
}

func TestUpsertIPAssignmentInsertsWhenAbsent(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:32"), nil)
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.32", "dhcp"); err != nil {
		t.Fatal(err)
	}
	ips, _ := s.ListIPs(ifID)
	if len(ips) != 1 || ips[0].IP != "10.0.0.32" || ips[0].Kind != "dhcp" {
		t.Fatalf("absent IP should be inserted as dhcp, got %+v", ips)
	}
}
```

Add to `internal/pihole/sync_test.go` (uses the existing `testSync`/`strpP` helpers):

```go
func TestPiholeLeaseKeepsManualStatic(t *testing.T) {
	st, f, sync := testSync(t)
	subnets, _ := st.ListSubnets()
	snID := subnets[0].ID
	// A device the user marked static, whose MAC pihole will report as a lease.
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(devID, strpP("aa:bb:cc:00:00:40"), nil)
	st.AssignIP(ifID, snID, "10.0.0.40", "static")

	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:40", IP: "10.0.0.40", Hostname: "gw"}}
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	ips, _ := st.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("pihole lease must not downgrade a manual static, got %+v", ips)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/store/ ./internal/pihole/ -run 'UpsertIPAssignment|LeaseKeepsManualStatic' -v`
Expected: `TestUpsertIPAssignmentNeverDowngradesStatic` and `TestPiholeLeaseKeepsManualStatic` FAIL (current code downgrades to dhcp); the upgrade/insert tests pass.

- [ ] **Step 3: Guard the update branch**

In `internal/store/device.go`, in `UpsertIPAssignment`, change the update statement:

```go
	_, err = s.DB.Exec(`UPDATE ip_assignment SET kind=? WHERE id=?`, kind, id)
	return err
```

to:

```go
	// Never downgrade a static assignment to dhcp: a user (or a reservation)
	// marked this IP static, and a dhcp-lease observation must not clobber it.
	_, err = s.DB.Exec(
		`UPDATE ip_assignment SET kind=? WHERE id=? AND NOT (kind='static' AND ?='dhcp')`,
		kind, id, kind)
	return err
```

- [ ] **Step 4: Run tests + full build**

```bash
CGO_ENABLED=0 go build ./... && go test ./...
```
Expected: builds clean; the new tests pass; all existing store/pihole tests (including `TestReservationBeatsLease`, `TestSyncEnrichesExistingByMAC`) stay green.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/store/device.go internal/store/device_test.go internal/pihole/sync_test.go && go vet ./...
git add internal/store/device.go internal/store/device_test.go internal/pihole/sync_test.go
git commit -m "$(printf 'fix: do not let a dhcp-lease sync downgrade a manual static lease\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>')"
```

---

## Self-Review

**Spec coverage:**
- `UpsertIPAssignment` non-downgrade guard → Task 1. ✅
- Manual `SetIPKind` stays unconditional (untouched) → Task 1. ✅
- Tests: static survives dhcp upsert, dhcp upgrades to static, absent inserts, pihole lease keeps manual static → Task 1. ✅
- Out of scope (grid click, settings config, provenance column, SetIPKind/AssignIP) → untouched. ✅

**Placeholder scan:** none — every step shows complete code.

**Type consistency:** `UpsertIPAssignment(ifaceID, subnetID int64, ip, kind string) error` unchanged; the parameterized `?='dhcp'` binds `kind` a second time (three args: `kind, id, kind`). `AssignIP(ifID, snID, ip, kind) (int64, error)`, `ListIPs(ifID) ([]IPRow, error)`, `openTest`, `strp`, `testSync`, `strpP` are all existing helpers. `IPRow.Kind` is the asserted field.

**Ordering note:** single task; the guarded UPDATE keeps upgrade (dhcp→static) working while blocking downgrade (static→dhcp), so `TestReservationBeatsLease` (reservation processed first, static) is unaffected.
