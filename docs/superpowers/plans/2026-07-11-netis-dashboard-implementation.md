# Dashboard Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the netis dashboard an integration-status widget, a summary stats row, a needs-attention panel, and live SSE refresh, backed by a new `integration_status` table the background loops write to.

**Architecture:** A new `integration_status` table (migration `0003`) records each background loop's last run. The three syncs' `RunOnce` return a small `Stats` value; their `Start` loops write status and publish a `dashboard` SSE topic. The scan scheduler records `scan` status per sweep. The dashboard body becomes one SSE-refreshed fragment assembled from the store.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, templ + HTMX + SSE.

**Spec:** `docs/superpowers/specs/2026-07-11-netis-dashboard-design.md` — read before starting.

## Global Constraints

- Go module `netis`, `go1.26.5`, `CGO_ENABLED=0`. SQLite driver `modernc.org/sqlite`.
- Timestamps UTC RFC3339. Integration names are exactly `proxmox`, `wireguard`, `pihole`, `scan`.
- `errors.Is(err, sql.ErrNoRows)` for not-found lookups (project convention).
- Build `CGO_ENABLED=0 go build ./...`; test `go test ./... -count=1`.
- **templ:** CLI at `/home/ben/go/bin/templ` (not on PATH — run `export PATH="$PATH:$(go env GOPATH)/bin"` first). After editing any `.templ`, run `templ generate` then build; commit BOTH the `.templ` and generated `*_templ.go`.
- `events.Service` exposes `Broker() *events.Broker`; `events.Broker` has `Publish(topic, data string)`. The SSE endpoint fans out topics as `event: <topic>` frames; the layout already runs `hx-ext="sse" sse-connect="/events/stream"`.
- Commit after each task; conventional-commit, body ending with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Shell prints harmless zsh-rc noise on stderr (`command not found: z`); ignore — exit codes are correct.

---

### Task 1: `integration_status` table + store methods

**Files:**
- Create: `internal/store/migrations/0003_integration_status.sql`, `internal/store/status_integration.go`, `internal/store/status_integration_test.go`

**Interfaces:**
- Consumes: `store.Open`, `openTest(t)`.
- Produces:
  - `type IntegrationStatus struct { Name, LastRun, Detail string; OK bool; ItemCount int }`
  - `func (s *Store) SetIntegrationStatus(st IntegrationStatus) error` — upsert on `name`.
  - `func (s *Store) ListIntegrationStatus() ([]IntegrationStatus, error)` — ordered by `name`.

- [ ] **Step 1: Write the failing tests**

`internal/store/status_integration_test.go`:

```go
package store

import "testing"

func TestIntegrationStatusUpsert(t *testing.T) {
	s := openTest(t)
	if err := s.SetIntegrationStatus(IntegrationStatus{
		Name: "pihole", LastRun: "2026-07-11T10:00:00Z", OK: true, Detail: "48 leases", ItemCount: 48,
	}); err != nil {
		t.Fatal(err)
	}
	// Upsert same name: update in place, no duplicate row.
	if err := s.SetIntegrationStatus(IntegrationStatus{
		Name: "pihole", LastRun: "2026-07-11T10:05:00Z", OK: false, Detail: "auth failed", ItemCount: 0,
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListIntegrationStatus()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	got := list[0]
	if got.Name != "pihole" || got.OK != false || got.Detail != "auth failed" ||
		got.LastRun != "2026-07-11T10:05:00Z" || got.ItemCount != 0 {
		t.Fatalf("row not updated: %+v", got)
	}
}

func TestIntegrationStatusOrderedByName(t *testing.T) {
	s := openTest(t)
	for _, n := range []string{"scan", "proxmox", "wireguard"} {
		if err := s.SetIntegrationStatus(IntegrationStatus{Name: n, LastRun: "2026-07-11T10:00:00Z", OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.ListIntegrationStatus()
	if len(list) != 3 || list[0].Name != "proxmox" || list[1].Name != "scan" || list[2].Name != "wireguard" {
		t.Fatalf("order wrong: %+v", list)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/store/ -run IntegrationStatus -count=1`
Expected: FAIL — undefined `SetIntegrationStatus`/`ListIntegrationStatus`.

- [ ] **Step 3: Write the migration + store methods**

`internal/store/migrations/0003_integration_status.sql`:

```sql
CREATE TABLE integration_status (
  name TEXT PRIMARY KEY,
  last_run TEXT NOT NULL,
  ok INTEGER NOT NULL DEFAULT 0,
  detail TEXT NOT NULL DEFAULT '',
  item_count INTEGER NOT NULL DEFAULT 0
);
```

`internal/store/status_integration.go`:

```go
package store

type IntegrationStatus struct {
	Name      string
	LastRun   string
	Detail    string
	OK        bool
	ItemCount int
}

func (s *Store) SetIntegrationStatus(st IntegrationStatus) error {
	_, err := s.DB.Exec(`INSERT INTO integration_status (name,last_run,ok,detail,item_count)
		VALUES (?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET
			last_run=excluded.last_run, ok=excluded.ok,
			detail=excluded.detail, item_count=excluded.item_count`,
		st.Name, st.LastRun, st.OK, st.Detail, st.ItemCount)
	return err
}

func (s *Store) ListIntegrationStatus() ([]IntegrationStatus, error) {
	rows, err := s.DB.Query(`SELECT name,last_run,ok,detail,item_count
		FROM integration_status ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IntegrationStatus
	for rows.Next() {
		var st IntegrationStatus
		if err := rows.Scan(&st.Name, &st.LastRun, &st.OK, &st.Detail, &st.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests + full suite**

Run: `go test ./internal/store/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS. (The migrate loop applies `0003` on top of `0001`/`0002`; existing migration tests still pass.)

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): integration_status table and upsert/list methods"
```

---

### Task 2: Sync status recording (proxmox, wireguard, pihole)

**Files:**
- Modify: `internal/proxmox/sync.go`, `internal/wireguard/sync.go`, `internal/pihole/sync.go`
- Test: `internal/proxmox/sync_test.go`, `internal/wireguard/sync_test.go`, `internal/pihole/sync_test.go`

**Interfaces:**
- Consumes: `store.SetIntegrationStatus`/`IntegrationStatus` (Task 1), `events.Service.Broker().Publish`.
- Produces (signature changes — each `RunOnce` now returns a stats value):
  - proxmox: `type Stats struct { Guests, Nodes int }`; `func (s *Sync) RunOnce(ctx) (Stats, error)`.
  - wireguard: `type Stats struct { Peers int }`; `func (s *Sync) RunOnce(ctx) (Stats, error)`.
  - pihole: `type Stats struct { Leases, Reservations, DNSRecords, Created int }`; `func (s *Sync) RunOnce(ctx) (Stats, error)`; `upsertByMAC` now returns `(created bool, err error)`.
  - Each `Start` records an `integration_status` row after every run and publishes the `dashboard` topic. `RunOnce` is only called by `Start` and tests, so the signature change is contained to each package.

- [ ] **Step 1: Update the tests first (they encode the new signatures)**

In `internal/proxmox/sync_test.go`, every `sync.RunOnce(...)` call becomes `_, err := sync.RunOnce(...)` (or capture stats where asserted). Add:

```go
func TestProxmoxRunOnceStats(t *testing.T) {
	srv := fixtureServer(t)
	st, _ := store.Open(":memory:")
	defer st.Close()
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	sync := NewSync(st, c, events.NewService(st, events.NewBroker()))
	stats, err := sync.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Guests != 2 || stats.Nodes != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}
```

In `internal/wireguard/sync_test.go`, update `RunOnce` call sites to `_, err := ...` / capture, and add:

```go
func TestWireguardRunOnceStats(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	st.CreateSubnet(store.Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})
	fresh := time.Now().Unix()
	dump := "priv\tpub\t51820\toff\n" +
		fmt.Sprintf("peerA=\t(none)\t1.2.3.4:51820\t10.6.0.2/32\t%d\t1\t1\toff\n", fresh) +
		"peerB=\t(none)\t(none)\t10.6.0.3/32\t0\t0\t0\toff\n"
	sync := NewSync(st, &fakeRunner{out: []byte(dump)}, events.NewService(st, events.NewBroker()), "wg0")
	stats, err := sync.RunOnce(context.Background())
	if err != nil || stats.Peers != 2 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}
```

In `internal/pihole/sync_test.go`, update every `sync.RunOnce(...)` to `_, err := ...` / capture, and add:

```go
func TestPiholeRunOnceStats(t *testing.T) {
	st, f, sync := testSync(t)
	f.res = []Reservation{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "laptop"}}
	f.dns = []DNSRecord{{IP: "10.0.0.20", Name: "printer.lan"}}
	stats, err := sync.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Reservations != 1 || stats.Leases != 1 || stats.DNSRecords != 1 || stats.Created != 2 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestPiholeStartRecordsStatus(t *testing.T) {
	st, f, sync := testSync(t)
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "laptop"}}
	// One iteration: record status via the same path Start uses.
	sync.recordStatus(sync.runAndCount(context.Background()))
	list, _ := st.ListIntegrationStatus()
	if len(list) != 1 || list[0].Name != "pihole" || !list[0].OK || list[0].ItemCount != 1 {
		t.Fatalf("status=%+v", list)
	}
}
```

(The `recordStatus`/`runAndCount` helpers are defined in Step 3 so `Start` and the test share one code path.)

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/proxmox/ ./internal/wireguard/ ./internal/pihole/ -count=1`
Expected: FAIL — `RunOnce` returns one value / undefined `Stats`, `recordStatus`, `runAndCount`.

- [ ] **Step 3: Implement — proxmox**

`internal/proxmox/sync.go` — change `RunOnce` and `Start`, add `Stats`, `recordStatus`, `runAndCount`:

```go
type Stats struct {
	Guests int
	Nodes  int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	guests, err := s.client.ListGuests(ctx)
	if err != nil {
		return Stats{}, err
	}
	nodeIDs := make(map[string]int64)
	for _, g := range guests {
		if _, ok := nodeIDs[g.Node]; ok {
			continue
		}
		id, err := s.upsertNode(g.Node)
		if err != nil {
			return Stats{}, err
		}
		nodeIDs[g.Node] = id
	}
	for _, g := range guests {
		if err := s.upsertGuest(ctx, g, nodeIDs[g.Node]); err != nil {
			log.Printf("proxmox guest %d: %v", g.VMID, err)
		}
	}
	return Stats{Guests: len(guests), Nodes: len(nodeIDs)}, nil
}

// runAndCount runs one cycle and returns the stats and error, so Start and
// tests share exactly one code path.
func (s *Sync) runAndCount(ctx context.Context) (Stats, error) {
	return s.RunOnce(ctx)
}

// recordStatus writes the integration_status row and manages the
// once-per-outage scan_error event.
func (s *Sync) recordStatus(stats Stats, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st := store.IntegrationStatus{Name: "proxmox", LastRun: now}
	if err != nil {
		if !s.failing {
			s.failing = true
			s.events.Emit("scan_error", nil, "proxmox sync failing: "+err.Error())
		}
		st.OK = false
		st.Detail = err.Error()
	} else {
		s.failing = false
		st.OK = true
		st.ItemCount = stats.Guests
		st.Detail = fmt.Sprintf("%d guests, %d nodes", stats.Guests, stats.Nodes)
	}
	if serr := s.store.SetIntegrationStatus(st); serr != nil {
		log.Printf("proxmox status write: %v", serr)
	}
	s.events.Broker().Publish("dashboard", "refresh")
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		s.recordStatus(s.runAndCount(ctx))
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
```

Add `"netis/internal/store"` to imports if not present (it already is). Ensure `fmt` is imported (it is).

- [ ] **Step 4: Implement — wireguard**

`internal/wireguard/sync.go`:

```go
type Stats struct {
	Peers int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	out, err := s.runner.Run(ctx, "wg show "+s.iface+" dump")
	if err != nil {
		return Stats{}, err
	}
	peers, err := ParseDump(out)
	if err != nil {
		return Stats{}, err
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return Stats{}, err
	}
	now := time.Now().UTC()
	for _, p := range peers {
		if err := s.upsertPeer(p, subnets, now); err != nil {
			return Stats{}, err
		}
	}
	return Stats{Peers: len(peers)}, nil
}

func (s *Sync) runAndCount(ctx context.Context) (Stats, error) {
	return s.RunOnce(ctx)
}

func (s *Sync) recordStatus(stats Stats, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st := store.IntegrationStatus{Name: "wireguard", LastRun: now}
	if err != nil {
		if !s.failing {
			s.failing = true
			s.events.Emit("scan_error", nil, "wireguard sync failing: "+err.Error())
		}
		st.OK = false
		st.Detail = err.Error()
	} else {
		s.failing = false
		st.OK = true
		st.ItemCount = stats.Peers
		st.Detail = fmt.Sprintf("%d peers", stats.Peers)
	}
	if serr := s.store.SetIntegrationStatus(st); serr != nil {
		log.Printf("wireguard status write: %v", serr)
	}
	s.events.Broker().Publish("dashboard", "refresh")
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		s.recordStatus(s.runAndCount(ctx))
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
```

Ensure imports include `fmt`, `log`, `time`, `netis/internal/store` (add any missing).

- [ ] **Step 5: Implement — pihole**

In `internal/pihole/sync.go`: change `RunOnce` to return `(Stats, error)` and tally counts; change `upsertByMAC` to return `(bool, error)` (created flag); add `Stats`, `runAndCount`, `recordStatus`; rewrite `Start`.

```go
type Stats struct {
	Leases        int
	Reservations  int
	DNSRecords    int
	Created       int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	reservations, err := s.client.Reservations(ctx)
	if err != nil {
		return Stats{}, err
	}
	leases, err := s.client.Leases(ctx)
	if err != nil {
		return Stats{}, err
	}
	dns, err := s.client.DNSRecords(ctx)
	if err != nil {
		return Stats{}, err
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Reservations: len(reservations), Leases: len(leases), DNSRecords: len(dns)}

	claimed := make(map[string]bool)
	for _, r := range reservations {
		snID, ok := subnetForIP(subnets, r.IP)
		if !ok {
			continue
		}
		created, err := s.upsertByMAC(r.MAC, r.IP, r.Hostname, snID, "static")
		if err != nil {
			return Stats{}, err
		}
		if created {
			stats.Created++
		}
		claimed[fmt.Sprintf("%d|%s", snID, r.IP)] = true
	}
	for _, l := range leases {
		snID, ok := subnetForIP(subnets, l.IP)
		if !ok {
			continue
		}
		if claimed[fmt.Sprintf("%d|%s", snID, l.IP)] {
			if l.Hostname != "" {
				iface, found, err := s.store.FindIfaceByMAC(l.MAC)
				if err != nil {
					return Stats{}, err
				}
				if found {
					if err := s.store.SetIfaceHostnameIfEmpty(iface.ID, l.Hostname); err != nil {
						return Stats{}, err
					}
				}
			}
			continue
		}
		created, err := s.upsertByMAC(l.MAC, l.IP, l.Hostname, snID, "dhcp")
		if err != nil {
			return Stats{}, err
		}
		if created {
			stats.Created++
		}
	}
	for _, rec := range dns {
		snID, ok := subnetForIP(subnets, rec.IP)
		if !ok {
			continue
		}
		iface, found, err := s.store.FindIfaceByIP(snID, rec.IP)
		if err != nil {
			return Stats{}, err
		}
		if !found {
			continue
		}
		if err := s.store.SetIfaceHostnameIfEmpty(iface.ID, rec.Name); err != nil {
			return Stats{}, err
		}
		if err := s.store.SetCustomField(iface.DeviceID, "pihole_dns", rec.Name); err != nil {
			return Stats{}, err
		}
	}
	return stats, nil
}
```

Change `upsertByMAC` to return the created flag:

```go
func (s *Sync) upsertByMAC(mac, ip, hostname string, subnetID int64, kind string) (bool, error) {
	iface, found, err := s.store.FindIfaceByMAC(mac)
	if err != nil {
		return false, err
	}
	if found {
		if err := s.store.UpsertIPAssignment(iface.ID, subnetID, ip, kind); err != nil {
			return false, err
		}
		if hostname != "" {
			return false, s.store.SetIfaceHostnameIfEmpty(iface.ID, hostname)
		}
		return false, nil
	}
	name := hostname
	if name == "" {
		name = "pihole-" + mac
	}
	devID, err := s.store.CreateDevice(store.Device{Name: name, Kind: "other", Source: "pihole"})
	if err != nil {
		return false, err
	}
	m := mac
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	ifID, err := s.store.AddIface(devID, &m, hp)
	if err != nil {
		return false, err
	}
	if err := s.store.UpsertIPAssignment(ifID, subnetID, ip, kind); err != nil {
		return false, err
	}
	s.events.Emit("device_new", &devID, fmt.Sprintf("pihole device %s at %s", name, ip))
	return true, nil
}
```

Add the shared cycle + status + new Start:

```go
func (s *Sync) runAndCount(ctx context.Context) (Stats, error) {
	return s.RunOnce(ctx)
}

func (s *Sync) recordStatus(stats Stats, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st := store.IntegrationStatus{Name: "pihole", LastRun: now}
	if err != nil {
		if !s.failing {
			s.failing = true
			s.events.Emit("scan_error", nil, "pihole sync failing: "+err.Error())
		}
		st.OK = false
		st.Detail = err.Error()
	} else {
		s.failing = false
		st.OK = true
		st.ItemCount = stats.Leases
		st.Detail = fmt.Sprintf("%d leases, %d new", stats.Leases, stats.Created)
	}
	if serr := s.store.SetIntegrationStatus(st); serr != nil {
		log.Printf("pihole status write: %v", serr)
	}
	s.events.Broker().Publish("dashboard", "refresh")
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		s.recordStatus(s.runAndCount(ctx))
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
```

Add `"log"` and `"netis/internal/store"` to the pihole sync imports if not already present (`store` is already imported; add `log`). Remove the now-unreachable old `Start`.

- [ ] **Step 6: Run all three package tests + suite**

Run: `go test ./internal/proxmox/ ./internal/wireguard/ ./internal/pihole/ -count=1 && go test -race ./internal/pihole/ ./internal/wireguard/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/proxmox/ internal/wireguard/ internal/pihole/
git commit -m "feat(sync): record integration_status and publish dashboard topic"
```

---

### Task 3: Scan scheduler records `scan` status

**Files:**
- Modify: `internal/scan/scheduler.go`
- Test: `internal/scan/scheduler_test.go` (create if absent; otherwise add to the existing scan test file)

**Interfaces:**
- Consumes: `store.SetIntegrationStatus`, `events.Broker.Publish`, the `Engine`'s `Store` and `Broker` fields.
- Produces: after each `run(ctx, sn)` sweep, a `scan` `integration_status` row (latest-wins) plus a `dashboard` publish.

- [ ] **Step 1: Write the failing test**

Add to `internal/scan/engine_test.go` (it already builds `Engine`/store/broker via `testEngine`):

```go
func TestSchedulerRecordsScanStatus(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}
	sn, _ := st.GetSubnet(snID)
	sched := NewScheduler(e, st)
	sched.run(context.Background(), sn) // one sweep
	list, _ := st.ListIntegrationStatus()
	if len(list) != 1 || list[0].Name != "scan" || !list[0].OK {
		t.Fatalf("scan status=%+v", list)
	}
	if !strings.Contains(list[0].Detail, sn.CIDR) {
		t.Fatalf("detail should mention the subnet: %q", list[0].Detail)
	}
}
```

Add `"strings"` to the test imports if missing.

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/scan/ -run SchedulerRecordsScanStatus -count=1`
Expected: FAIL — no `scan` status row written.

- [ ] **Step 3: Implement**

In `internal/scan/scheduler.go`, update `run` to record status after the sweep:

```go
func (s *Scheduler) run(ctx context.Context, sn store.Subnet) {
	// Guard here (not just at the call sites) so every entry point that
	// reaches run — periodic tick or manual Trigger — is protected.
	if !sn.ScanEnabled || sn.Kind == "wireguard" {
		return
	}
	s.mu.Lock()
	s.lastRun[sn.ID] = time.Now()
	s.mu.Unlock()
	err := s.engine.RunSubnet(ctx, sn)
	if err != nil {
		log.Printf("scan %s: %v", sn.CIDR, err)
	}
	st := store.IntegrationStatus{
		Name:    "scan",
		LastRun: time.Now().UTC().Format(time.RFC3339),
		OK:      err == nil,
		Detail:  "scanned " + sn.CIDR,
	}
	if err != nil {
		st.Detail = sn.CIDR + ": " + err.Error()
	}
	if serr := s.store.SetIntegrationStatus(st); serr != nil {
		log.Printf("scan status write: %v", serr)
	}
	s.engine.Broker.Publish("dashboard", "refresh")
}
```

(`Scheduler` already holds `s.store`; `Engine` already has an exported `Broker *events.Broker` field it uses to publish `grid:<id>`. Confirm `time`, `log`, `store` are imported in `scheduler.go` — they are.)

- [ ] **Step 4: Run test + suite**

Run: `go test ./internal/scan/ -count=1 && go test -race ./internal/scan/ -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scan/
git commit -m "feat(scan): record scan integration_status per sweep"
```

---

### Task 4: Dashboard widgets + SSE fragment

**Files:**
- Modify: `internal/web/dashboard.go`, `internal/web/views/dashboard.templ`, `internal/web/server.go`
- Create: `internal/web/views/helpers.go`
- Test: `internal/web/dashboard_test.go`

**Interfaces:**
- Consumes: `store.ListDevices` (`DeviceRow` has `ID, Name, Source, Online`), `store.ListSubnets`, `store.SubnetOccupancy` (`Occupant` has `Count`), `store.ListIntegrationStatus`, `store.ListEvents`, `scan.HostIPs`.
- Produces:
  - `views.relTime(rfc3339 string) string` in `internal/web/views/helpers.go`.
  - templ types (in `dashboard.templ`): `DashStats{Total, Online, Offline, Unknown, Subnets int}`, `AttentionUnknown{ID int64; Name string}`, `AttentionConflict{IP string; SubnetID int64; SubnetName string}`, `DashboardData{Username string; Stats DashStats; Integrations []store.IntegrationStatus; Unknowns []AttentionUnknown; Conflicts []AttentionConflict; Rows []DashRow; Events []store.Event}`.
  - templ `Dashboard(d DashboardData)` (full page) and `DashboardBody(d DashboardData)` (fragment).
  - Route `GET /dashboard/widgets` → `handleDashboardWidgets`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/dashboard_test.go`:

```go
func TestDashboardWidgetsRendersStatusAndAttention(t *testing.T) {
	srv, st := testServer(t)
	// a subnet + an online device + an unknown scan device + a conflict
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	on, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	onIf, _ := st.AddIface(on, nil, nil)
	st.AssignIP(onIf, snID, "10.0.0.1", "static")
	st.MarkSeen(onIf, 1, time.Now())
	unk, _ := st.CreateDevice(store.Device{Name: "unknown-aa:bb:cc:00:00:09", Kind: "other", Source: "scan"})
	unkIf, _ := st.AddIface(unk, nil, nil)
	st.AssignIP(unkIf, snID, "10.0.0.2", "dhcp")
	// conflict: a second device claims .1
	ghost, _ := st.CreateDevice(store.Device{Name: "ghost", Kind: "other", Source: "manual"})
	ghostIf, _ := st.AddIface(ghost, nil, nil)
	st.AssignIP(ghostIf, snID, "10.0.0.1", "static")
	// an integration status row
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "pihole", LastRun: time.Now().UTC().Format(time.RFC3339), OK: true, Detail: "48 leases, 2 new", ItemCount: 48})

	rec := authedGet(t, srv, st, "/dashboard/widgets")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"pihole", "48 leases, 2 new", "unknown-aa:bb:cc:00:00:09", "10.0.0.1"} {
		if !strings.Contains(body, want) {
			t.Errorf("widgets missing %q", want)
		}
	}
}

func TestDashboardPageHasFragmentContainer(t *testing.T) {
	srv, st := testServer(t)
	rec := authedGet(t, srv, st, "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `hx-get="/dashboard/widgets"`) {
		t.Fatalf("dashboard page missing fragment container (code=%d)", rec.Code)
	}
}
```

Add imports `"time"` to the web test file if missing (`store` and `strings` are already used by existing web tests). `relTime` is unexported and lives in the `views` package, so its unit test goes in `internal/web/views/helpers_test.go` (Step 4), not here.

- [ ] **Step 2: Run tests, verify failure**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -run Dashboard -count=1`
Expected: FAIL — `/dashboard/widgets` 404s / fragment container absent.

- [ ] **Step 3: Implement the handler + assembly**

Replace `internal/web/dashboard.go`:

```go
package web

import (
	"net/http"
	"sort"
	"strings"

	"netis/internal/scan"
	"netis/internal/web/views"
)

func (s *Server) assembleDashboard(r *http.Request) (views.DashboardData, error) {
	u, _ := userFrom(r)
	data := views.DashboardData{Username: u.Username}

	devices, err := s.store.ListDevices()
	if err != nil {
		return data, err
	}
	data.Stats.Total = len(devices)
	for _, d := range devices {
		if d.Online {
			data.Stats.Online++
		}
		if d.Source == "scan" && strings.HasPrefix(d.Name, "unknown-") {
			data.Stats.Unknown++
			data.Unknowns = append(data.Unknowns, views.AttentionUnknown{ID: d.ID, Name: d.Name})
		}
	}
	data.Stats.Offline = data.Stats.Total - data.Stats.Online

	subnets, err := s.store.ListSubnets()
	if err != nil {
		return data, err
	}
	data.Stats.Subnets = len(subnets)
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(sn.ID)
		if err != nil {
			return data, err
		}
		hosts, _ := scan.HostIPs(sn.CIDR)
		free := len(hosts) - len(occ)
		if free < 0 {
			free = 0
		}
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: free}
		seen := make(map[string]bool)
		for ip, o := range occ {
			if o.Online {
				row.Online++
			}
			if o.Count > 1 {
				data.Conflicts = append(data.Conflicts, views.AttentionConflict{
					IP: ip, SubnetID: sn.ID, SubnetName: sn.Name,
				})
			}
			if !seen[o.DeviceName] {
				seen[o.DeviceName] = true
				row.Occupants = append(row.Occupants, o.DeviceName)
			}
		}
		sort.Strings(row.Occupants)
		data.Rows = append(data.Rows, row)
	}
	sort.Slice(data.Conflicts, func(i, j int) bool { return data.Conflicts[i].IP < data.Conflicts[j].IP })

	statuses, err := s.store.ListIntegrationStatus()
	if err != nil {
		return data, err
	}
	data.Integrations = statuses

	evs, err := s.store.ListEvents(15)
	if err != nil {
		return data, err
	}
	data.Events = evs
	return data, nil
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.assembleDashboard(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.Dashboard(data).Render(r.Context(), w)
}

func (s *Server) handleDashboardWidgets(w http.ResponseWriter, r *http.Request) {
	data, err := s.assembleDashboard(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.DashboardBody(data).Render(r.Context(), w)
}
```

- [ ] **Step 4: Implement the relTime helper + its test**

`internal/web/views/helpers.go`:

```go
package views

import (
	"fmt"
	"time"
)

// relTime renders an RFC3339 timestamp as a short relative string. On a parse
// failure it returns the input unchanged.
func relTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
```

`internal/web/views/helpers_test.go`:

```go
package views

import (
	"testing"
	"time"
)

func TestRelTime(t *testing.T) {
	now := time.Now().UTC()
	cases := map[string]string{
		now.Add(-30 * time.Second).Format(time.RFC3339):    "just now",
		now.Add(-5 * time.Minute).Format(time.RFC3339):     "5m ago",
		now.Add(-2 * time.Hour).Format(time.RFC3339):       "2h ago",
		now.Add(-3 * 24 * time.Hour).Format(time.RFC3339):  "3d ago",
		"not-a-time": "not-a-time",
	}
	for in, want := range cases {
		if got := relTime(in); got != want {
			t.Errorf("relTime(%q)=%q want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 5: Implement the templates**

Replace `internal/web/views/dashboard.templ` (keep the existing `DashRow` type, add the new types and split the body into a fragment):

```templ
package views

import (
	"fmt"
	"strings"

	"netis/internal/store"
)

type DashRow struct {
	Subnet    store.Subnet
	Online    int
	Used      int
	Free      int
	Occupants []string
}

type DashStats struct {
	Total   int
	Online  int
	Offline int
	Unknown int
	Subnets int
}

type AttentionUnknown struct {
	ID   int64
	Name string
}

type AttentionConflict struct {
	IP         string
	SubnetID   int64
	SubnetName string
}

type DashboardData struct {
	Username     string
	Stats        DashStats
	Integrations []store.IntegrationStatus
	Unknowns     []AttentionUnknown
	Conflicts    []AttentionConflict
	Rows         []DashRow
	Events       []store.Event
}

templ Dashboard(d DashboardData) {
	@Layout("Dashboard", d.Username) {
		<h1>Dashboard</h1>
		<div id="dash" hx-get="/dashboard/widgets" hx-trigger="sse:dashboard, sse:events">
			@DashboardBody(d)
		</div>
	}
}

templ DashboardBody(d DashboardData) {
	<div class="stats">
		<div class="stat"><span class="num">{ fmt.Sprint(d.Stats.Total) }</span> devices</div>
		<div class="stat"><span class="num ok">{ fmt.Sprint(d.Stats.Online) }</span> online</div>
		<div class="stat"><span class="num">{ fmt.Sprint(d.Stats.Offline) }</span> offline</div>
		<div class="stat"><span class="num warn">{ fmt.Sprint(d.Stats.Unknown) }</span> unknown</div>
		<div class="stat"><span class="num">{ fmt.Sprint(d.Stats.Subnets) }</span> subnets</div>
	</div>
	<div class="band">
		<section class="panel">
			<h2>Integrations</h2>
			if len(d.Integrations) == 0 {
				<p class="muted">no integrations have run yet</p>
			} else {
				<table>
					for _, it := range d.Integrations {
						<tr>
							<td>{ it.Name }</td>
							<td>
								if it.OK {
									<span class="badge online">ok</span>
								} else {
									<span class="badge offline">failing</span>
								}
							</td>
							<td class="muted">{ relTime(it.LastRun) }</td>
							<td>{ it.Detail }</td>
						</tr>
					}
				</table>
			}
		</section>
		<section class="panel">
			<h2>Needs attention</h2>
			if len(d.Unknowns) == 0 && len(d.Conflicts) == 0 {
				<p class="muted">nothing needs attention</p>
			} else {
				<ul>
					for _, u := range d.Unknowns {
						<li>
							<a href={ templ.URL(fmt.Sprintf("/devices/%d", u.ID)) }>
								unknown device { u.Name }
							</a>
						</li>
					}
					for _, c := range d.Conflicts {
						<li>
							<a href={ templ.URL(fmt.Sprintf("/subnets/%d", c.SubnetID)) }>
								IP conflict { c.IP } in { c.SubnetName }
							</a>
						</li>
					}
				</ul>
			}
		</section>
	</div>
	<h2>Subnets</h2>
	<div class="cards">
		for _, r := range d.Rows {
			<a class="card" href={ templ.URL(fmt.Sprintf("/subnets/%d", r.Subnet.ID)) }>
				<h2>{ r.Subnet.Name }</h2>
				<p class="mono">{ r.Subnet.CIDR }</p>
				<p>
					<span class="ok">{ fmt.Sprint(r.Online) } online</span> ·
					{ fmt.Sprint(r.Used) } used · { fmt.Sprint(r.Free) } free
				</p>
				if len(r.Occupants) > 0 {
					<p class="mono occupants">{ strings.Join(r.Occupants, ", ") }</p>
				}
			</a>
		}
	</div>
	<h2>Recent events</h2>
	<table>
		for _, e := range d.Events {
			<tr>
				<td class="mono">{ e.TS }</td>
				<td><span class={ "badge", e.Type }>{ e.Type }</span></td>
				<td>{ e.Details }</td>
			</tr>
		}
	</table>
}
```

- [ ] **Step 6: Add the route and CSS**

In `internal/web/server.go`, add alongside the existing `GET /{$}` dashboard route:

```go
	s.mux.HandleFunc("GET /dashboard/widgets", s.handleDashboardWidgets)
```

Append to `internal/web/static/app.css`:

```css
.stats { display:flex; gap:1rem; flex-wrap:wrap; margin-bottom:1rem; }
.stat { background:var(--card); border-radius:8px; padding:.6rem 1rem; }
.stat .num { font-size:1.4rem; font-weight:700; display:block; }
.stat .num.ok { color:var(--ok); } .stat .num.warn { color:var(--warn); }
.band { display:flex; gap:1rem; flex-wrap:wrap; margin-bottom:1rem; }
.panel { background:var(--card); border-radius:8px; padding:1rem; flex:1; min-width:280px; }
.panel ul { margin:0; padding-left:1.1rem; }
```

- [ ] **Step 7: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ ./internal/web/views/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web/
git commit -m "feat(web): dashboard stats, integration status, attention panel, live fragment"
```

---

## Final verification (after Task 4)

- [ ] `CGO_ENABLED=0 go test ./... -count=1` — all green.
- [ ] `go test -race ./internal/pihole/ ./internal/wireguard/ ./internal/scan/ ./internal/events/ -count=1` — clean.
- [ ] `CGO_ENABLED=0 go build -o netis ./cmd/netis` — builds.
- [ ] Manual smoke: fresh DB, `/setup`→login, dashboard shows the stats row + "no integrations have run yet"; after a scan the `scan` integration row appears and the fragment container is present.
- [ ] Use superpowers:finishing-a-development-branch.
