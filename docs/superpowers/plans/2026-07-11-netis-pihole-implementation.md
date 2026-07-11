# Pi-hole Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Pi-hole v6 integration to netis that pulls DHCP leases, static reservations, and local DNS A records and merges them into the device inventory read-only.

**Architecture:** New package `internal/pihole` (a `Client` over the Pi-hole v6 REST API + a `Sync` with `RunOnce`/`Start`), mirroring `internal/proxmox` and `internal/wireguard` exactly. Two new idempotency-safe store methods, a `0002` migration widening the `device.source` CHECK via a table rebuild, and a transactional migrate loop (with foreign keys disabled around the pass) so the rebuild is safe. Settings-keyed config, conditional wiring in `main.go`.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, stdlib `net/http`/`crypto/tls`/`encoding/json`, templ.

**Spec:** `docs/superpowers/specs/2026-07-11-netis-pihole-design.md` — read before starting.

## Global Constraints

- Go module `netis`, `go1.26.5`, `CGO_ENABLED=0` everywhere. SQLite driver `modernc.org/sqlite` (driver name `"sqlite"`).
- Build `CGO_ENABLED=0 go build ./...`; test `go test ./... -count=1`.
- MACs normalized lowercase colon-separated (`aa:bb:cc:dd:ee:ff`); IPs canonical via `netip.Addr.String()`.
- Timestamps UTC RFC3339. Device `source` enum after this work: `manual`, `scan`, `proxmox`, `wireguard`, `pihole`.
- Not-found DB lookups use `errors.Is(err, sql.ErrNoRows)` (project convention) — never string-compare error text.
- No live network in tests: the Pi-hole HTTP client is tested against `httptest`; the sync is tested with a fake client. No real Pi-hole.
- After editing any `.templ` file: `export PATH="$PATH:$(go env GOPATH)/bin"` then `templ generate` before building; commit both the `.templ` source and the generated `*_templ.go`. templ CLI is at `/home/ben/go/bin/templ` (v0.3.887).
- Commit after every task with a conventional-commit message ending with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- The interactive shell prints harmless zsh-rc noise on stderr (`command not found: z`, `bad substitution`); ignore it — exit codes are correct.

---

### Task 1: Transactional migrate loop + `0002` migration (widen `device.source`)

**Files:**
- Create: `internal/store/migrations/0002_pihole_source.sql`
- Modify: `internal/store/store.go` (`Open` + `migrate`, add `applyMigration`)
- Test: `internal/store/store_test.go` (add cases)

**Interfaces:**
- Consumes: existing `store.Open(path)`, `openTest(t)` helper.
- Produces: no new exported API. After this task, inserting a `device` row with `source='pihole'` succeeds, child-table rows survive the rebuild, and each migration is applied atomically with foreign keys enforced at runtime.

**Background (why the FK dance):** child tables (`iface`, `device_tag`, `custom_field`, `device_link`, `event`) reference `device(id)` with `ON DELETE CASCADE`/`SET NULL`. `DROP TABLE device` with foreign keys ON performs an implicit delete that cascades and wipes those child rows. `PRAGMA foreign_keys` cannot be changed inside a transaction, so it is toggled around the whole migration pass; `PRAGMA foreign_key_check` inside each migration's transaction catches a rebuild that left a dangling reference.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/store_test.go`:

```go
func TestPiholeSourceAllowed(t *testing.T) {
	s := openTest(t)
	_, err := s.DB.Exec(`INSERT INTO device (name,kind,source) VALUES ('ph','other','pihole')`)
	if err != nil {
		t.Fatalf("inserting source='pihole' should succeed after 0002: %v", err)
	}
}

func TestMigrationPreservesChildRows(t *testing.T) {
	// A device with an iface and a self-referential parent must survive the
	// 0002 table rebuild with its children and self-FK intact.
	path := t.TempDir() + "/m.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	parentID, _ := s.CreateDevice(Device{Name: "parent", Kind: "server", Source: "manual"})
	childID, _ := s.CreateDevice(Device{Name: "child", Kind: "vm", Source: "proxmox", ParentDeviceID: &parentID})
	ifID, _ := s.AddIface(childID, strp("aa:bb:cc:dd:ee:01"), nil)
	_ = ifID
	s.Close()

	// Reopen (migrations already applied — exercises idempotency too).
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after migrations: %v", err)
	}
	defer s2.Close()
	rows, err := s2.ListDevices()
	if err != nil || len(rows) != 2 {
		t.Fatalf("devices after rebuild: %+v err=%v", rows, err)
	}
	// self-FK preserved
	child, _ := s2.GetDevice(childID)
	if child.ParentDeviceID == nil || *child.ParentDeviceID != parentID {
		t.Fatalf("parent link lost: %+v", child)
	}
	// child iface preserved
	ifaces, _ := s2.ListIfaces(childID)
	if len(ifaces) != 1 || ifaces[0].MAC == nil || *ifaces[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("iface lost: %+v", ifaces)
	}
	// foreign keys enforced at runtime after migration
	var fk int
	s2.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatalf("foreign_keys should be ON at runtime, got %d", fk)
	}
}
```

- [ ] **Step 2: Run the tests, verify they fail**

Run: `go test ./internal/store/ -run 'Pihole|MigrationPreserves' -count=1`
Expected: FAIL — `TestPiholeSourceAllowed` fails on the CHECK constraint (source='pihole' rejected).

- [ ] **Step 3: Write the migration**

`internal/store/migrations/0002_pihole_source.sql` (column definitions identical to the 0001 `device` table except the widened source CHECK):

```sql
CREATE TABLE device_new (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'other' CHECK (kind IN
    ('computer','switch','phone','server','printer','iot','vm','lxc','wg-peer','other')),
  notes TEXT NOT NULL DEFAULT '',
  vendor TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','scan','proxmox','wireguard','pihole')),
  parent_device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  proxmox_vmid INTEGER,
  wg_pubkey TEXT,
  icon TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
INSERT INTO device_new SELECT * FROM device;
DROP TABLE device;
ALTER TABLE device_new RENAME TO device;
```

Note: `parent_device_id` references `device(id)` (the final name). Modern SQLite (`legacy_alter_table` OFF by default) rewrites the self-reference correctly during `RENAME`. `TestMigrationPreservesChildRows` verifies this.

- [ ] **Step 4: Rewrite the migrate loop**

Replace `Open` and `migrate` in `internal/store/store.go`, and add `applyMigration`:

```go
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer, avoids SQLITE_BUSY
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	// Enforce foreign keys for all normal operation (migrations ran with them off).
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	// Foreign keys must be OFF during migrations: a table rebuild (drop+rename)
	// with enforcement on would cascade-delete child rows. PRAGMA foreign_keys
	// cannot be toggled inside a transaction, so toggle it around the whole
	// pass. Safe: migrations run once at startup on the single pooled
	// connection before the server serves any request. Open() turns it back on.
	if _, err := s.DB.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	for _, name := range names {
		var n int
		if err := s.DB.QueryRow(
			`SELECT count(*) FROM schema_migrations WHERE version=?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := s.applyMigration(name, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one migration file in a transaction: exec the script,
// verify no dangling foreign-key references, record the version, commit. Any
// error rolls back the whole file so a partial migration can't wedge the DB.
func (s *Store) applyMigration(name, script string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(script); err != nil {
		return fmt.Errorf("migration %s: %w", name, err)
	}
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("migration %s fk check: %w", name, err)
	}
	violations := 0
	for rows.Next() {
		violations++
	}
	rows.Close()
	if violations > 0 {
		return fmt.Errorf("migration %s: %d foreign key violation(s)", name, violations)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 5: Run the tests + full suite**

Run: `go test ./internal/store/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS. (The existing `TestOpenCreatesSchema`/`TestOpenIsIdempotent` still pass; the two new tests pass.)

- [ ] **Step 6: Commit**

```bash
git add internal/store/
git commit -m "feat(store): transactional migrations + 0002 widen device.source for pihole"
```

---

### Task 2: Idempotency-safe store methods

**Files:**
- Modify: `internal/store/device.go` (add two methods)
- Test: `internal/store/device_test.go` (add cases)

**Interfaces:**
- Consumes: `*Store`, existing `AssignIP`, `AddIface`, `FindIfaceByIP`, `openTest`, `strp` helpers.
- Produces:
  - `func (s *Store) UpsertIPAssignment(ifaceID, subnetID int64, ip, kind string) error` — insert the `(iface_id, subnet_id, ip)` assignment if absent, else update its `kind`. Idempotent; upgrades a `dhcp` row to `static`.
  - `func (s *Store) SetIfaceHostnameIfEmpty(ifaceID int64, hostname string) error` — set `iface.hostname` only when currently NULL or empty; never overwrites.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/device_test.go`:

```go
func TestUpsertIPAssignment(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "pihole"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:01"), nil)

	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.5", "dhcp"); err != nil {
		t.Fatal(err)
	}
	// Re-run: no duplicate row.
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.5", "dhcp"); err != nil {
		t.Fatal(err)
	}
	ips, _ := s.ListIPs(ifID)
	if len(ips) != 1 {
		t.Fatalf("want 1 assignment, got %d: %+v", len(ips), ips)
	}
	// Upgrade dhcp -> static.
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.5", "static"); err != nil {
		t.Fatal(err)
	}
	ips, _ = s.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("want single static assignment, got %+v", ips)
	}
}

func TestSetIfaceHostnameIfEmpty(t *testing.T) {
	s := openTest(t)
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "pihole"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:02"), nil)

	if err := s.SetIfaceHostnameIfEmpty(ifID, "nas.lan"); err != nil {
		t.Fatal(err)
	}
	ifaces, _ := s.ListIfaces(devID)
	if ifaces[0].Hostname == nil || *ifaces[0].Hostname != "nas.lan" {
		t.Fatalf("hostname not set: %+v", ifaces[0])
	}
	// Must not overwrite an existing hostname.
	if err := s.SetIfaceHostnameIfEmpty(ifID, "other.lan"); err != nil {
		t.Fatal(err)
	}
	ifaces, _ = s.ListIfaces(devID)
	if *ifaces[0].Hostname != "nas.lan" {
		t.Fatalf("hostname was overwritten: %q", *ifaces[0].Hostname)
	}
}
```

- [ ] **Step 2: Run the tests, verify they fail**

Run: `go test ./internal/store/ -run 'UpsertIPAssignment|SetIfaceHostnameIfEmpty' -count=1`
Expected: FAIL — `undefined: s.UpsertIPAssignment` / `s.SetIfaceHostnameIfEmpty`.

- [ ] **Step 3: Implement**

Add to `internal/store/device.go` (the file already imports `database/sql` and `errors`):

```go
// UpsertIPAssignment assigns ip to (ifaceID, subnetID) idempotently: it
// inserts the row if absent, otherwise updates its kind. This lets a Pi-hole
// reservation upgrade an existing dhcp assignment to static without creating a
// duplicate row.
func (s *Store) UpsertIPAssignment(ifaceID, subnetID int64, ip, kind string) error {
	var id int64
	err := s.DB.QueryRow(
		`SELECT id FROM ip_assignment WHERE iface_id=? AND subnet_id=? AND ip=?`,
		ifaceID, subnetID, ip).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = s.DB.Exec(
				`INSERT INTO ip_assignment (iface_id,subnet_id,ip,kind) VALUES (?,?,?,?)`,
				ifaceID, subnetID, ip, kind)
			return err
		}
		return err
	}
	_, err = s.DB.Exec(`UPDATE ip_assignment SET kind=? WHERE id=?`, kind, id)
	return err
}

// SetIfaceHostnameIfEmpty sets the interface hostname only when it is not
// already set, so an integration never clobbers a user- or scan-provided name.
func (s *Store) SetIfaceHostnameIfEmpty(ifaceID int64, hostname string) error {
	_, err := s.DB.Exec(
		`UPDATE iface SET hostname=? WHERE id=? AND (hostname IS NULL OR hostname='')`,
		hostname, ifaceID)
	return err
}
```

- [ ] **Step 4: Run the tests + full suite**

Run: `go test ./internal/store/ -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS, build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/store/device.go internal/store/device_test.go
git commit -m "feat(store): idempotent UpsertIPAssignment and SetIfaceHostnameIfEmpty"
```

---

### Task 3: Pi-hole v6 API client

**Files:**
- Create: `internal/pihole/client.go`, `internal/pihole/client_test.go`

**Interfaces:**
- Consumes: nothing internal.
- Produces:
  - `type Lease struct { IP, MAC, Hostname string; Expiry int64 }`
  - `type Reservation struct { MAC, IP, Hostname string }`
  - `type DNSRecord struct { IP, Name string }`
  - `func NewClient(baseURL, password string, insecure bool) *Client`
  - `func (c *Client) Leases(ctx context.Context) ([]Lease, error)`
  - `func (c *Client) Reservations(ctx context.Context) ([]Reservation, error)`
  - `func (c *Client) DNSRecords(ctx context.Context) ([]DNSRecord, error)`
  - Auth is internal: the client logs in via `POST /api/auth` (body `{"password":...}`), caches the returned `session.sid`, sends it as the `X-FTL-SID` header, and re-authenticates once on a 401 before retrying.

- [ ] **Step 1: Write the failing tests**

`internal/pihole/client_test.go`:

```go
package pihole

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const (
	leasesJSON = `{"leases":[
	  {"ip":"10.0.0.10","hwaddr":"AA:BB:CC:00:00:10","name":"laptop","expires":1893456000},
	  {"ip":"10.0.0.11","hwaddr":"AA:BB:CC:00:00:11","name":"","expires":0}
	]}`
	dhcpHostsJSON = `{"config":{"dhcp":{"hosts":["AA:BB:CC:00:00:20,10.0.0.20,printer","BADENTRY"]}}}`
	dnsHostsJSON  = `{"config":{"dns":{"hosts":["10.0.0.20 printer.lan","10.0.0.30 nas.lan"]}}}`
)

// fixtureServer serves /api/auth (returns a SID) and the three read endpoints.
// authHits counts /api/auth calls so tests can assert the re-auth behavior.
func fixtureServer(t *testing.T, authHits *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(authHits, 1)
		w.Write([]byte(`{"session":{"sid":"SID123","valid":true}}`))
	})
	guard := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-FTL-SID") != "SID123" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/dhcp/leases", guard(leasesJSON))
	mux.HandleFunc("/api/config/dhcp/hosts", guard(dhcpHostsJSON))
	mux.HandleFunc("/api/config/dns/hosts", guard(dnsHostsJSON))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLeases(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, &hits)
	c := NewClient(srv.URL, "pw", false)
	leases, err := c.Leases(context.Background())
	if err != nil || len(leases) != 2 {
		t.Fatalf("leases=%+v err=%v", leases, err)
	}
	if leases[0].IP != "10.0.0.10" || leases[0].MAC != "aa:bb:cc:00:00:10" ||
		leases[0].Hostname != "laptop" || leases[0].Expiry != 1893456000 {
		t.Fatalf("lease0=%+v", leases[0])
	}
	if hits != 1 {
		t.Fatalf("expected one auth, got %d", hits)
	}
}

func TestReservationsSkipsMalformed(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, &hits)
	c := NewClient(srv.URL, "pw", false)
	res, err := c.Reservations(context.Background())
	if err != nil || len(res) != 1 {
		t.Fatalf("reservations=%+v err=%v", res, err)
	}
	if res[0].MAC != "aa:bb:cc:00:00:20" || res[0].IP != "10.0.0.20" || res[0].Hostname != "printer" {
		t.Fatalf("res0=%+v", res[0])
	}
}

func TestDNSRecords(t *testing.T) {
	var hits int32
	srv := fixtureServer(t, &hits)
	c := NewClient(srv.URL, "pw", false)
	recs, err := c.DNSRecords(context.Background())
	if err != nil || len(recs) != 2 {
		t.Fatalf("records=%+v err=%v", recs, err)
	}
	if recs[0].IP != "10.0.0.20" || recs[0].Name != "printer.lan" {
		t.Fatalf("rec0=%+v", recs[0])
	}
}

func TestReauthOn401(t *testing.T) {
	// Server rejects the first SID once, forcing a single re-auth + retry.
	var hits int32
	var rejectNext atomic.Bool
	rejectNext.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"session":{"sid":"SID123","valid":true}}`))
	})
	mux.HandleFunc("/api/dhcp/leases", func(w http.ResponseWriter, r *http.Request) {
		if rejectNext.Swap(false) {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(leasesJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "pw", false)
	leases, err := c.Leases(context.Background())
	if err != nil || len(leases) != 2 {
		t.Fatalf("leases=%+v err=%v", leases, err)
	}
	if hits != 2 {
		t.Fatalf("expected re-auth (2 auth calls), got %d", hits)
	}
}
```

- [ ] **Step 2: Run the tests, verify they fail**

Run: `go test ./internal/pihole/ -count=1`
Expected: FAIL — package/types undefined.

- [ ] **Step 3: Implement**

`internal/pihole/client.go`:

```go
package pihole

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type Lease struct {
	IP       string
	MAC      string
	Hostname string
	Expiry   int64
}

type Reservation struct {
	MAC      string
	IP       string
	Hostname string
}

type DNSRecord struct {
	IP   string
	Name string
}

type Client struct {
	base     string
	password string
	http     *http.Client

	mu  sync.Mutex
	sid string
}

func NewClient(baseURL, password string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:     strings.TrimSuffix(baseURL, "/"),
		password: password,
		http:     &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

// login authenticates and caches the session id.
func (c *Client) login(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"password": c.password})
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/auth", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("pihole auth: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Session struct {
			SID   string `json:"sid"`
			Valid bool   `json:"valid"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Session.Valid || out.Session.SID == "" {
		return fmt.Errorf("pihole auth: invalid session")
	}
	c.mu.Lock()
	c.sid = out.Session.SID
	c.mu.Unlock()
	return nil
}

// get performs an authenticated GET, decoding JSON into out. It logs in when
// no SID is cached, and re-authenticates once on a 401 before retrying.
func (c *Client) get(ctx context.Context, path string, out any) error {
	c.mu.Lock()
	sid := c.sid
	c.mu.Unlock()
	if sid == "" {
		if err := c.login(ctx); err != nil {
			return err
		}
	}
	status, err := c.doGet(ctx, path, out)
	if err != nil {
		return err
	}
	if status == 401 {
		if err := c.login(ctx); err != nil {
			return err
		}
		status, err = c.doGet(ctx, path, out)
		if err != nil {
			return err
		}
	}
	if status != 200 {
		return fmt.Errorf("pihole %s: HTTP %d", path, status)
	}
	return nil
}

// doGet issues one GET with the cached SID and, on 200, decodes into out.
// It returns the HTTP status so get() can decide whether to re-auth.
func (c *Client) doGet(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	req.Header.Set("X-FTL-SID", c.sid)
	c.mu.Unlock()
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return 0, err
	}
	return 200, nil
}

func normMAC(s string) string {
	m := strings.ToLower(strings.TrimSpace(s))
	if _, err := netip.ParseAddr(m); err == nil { // guard against an IP slipping in
		return ""
	}
	if len(m) != 17 {
		return ""
	}
	return m
}

func normIP(s string) string {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return a.String()
}

func (c *Client) Leases(ctx context.Context) ([]Lease, error) {
	var body struct {
		Leases []struct {
			IP     string `json:"ip"`
			HWAddr string `json:"hwaddr"`
			Name   string `json:"name"`
			Expires int64 `json:"expires"`
		} `json:"leases"`
	}
	if err := c.get(ctx, "/api/dhcp/leases", &body); err != nil {
		return nil, err
	}
	out := make([]Lease, 0, len(body.Leases))
	for _, l := range body.Leases {
		ip, mac := normIP(l.IP), normMAC(l.HWAddr)
		if ip == "" || mac == "" {
			continue
		}
		out = append(out, Lease{IP: ip, MAC: mac, Hostname: strings.TrimSpace(l.Name), Expiry: l.Expires})
	}
	return out, nil
}

func (c *Client) Reservations(ctx context.Context) ([]Reservation, error) {
	var body struct {
		Config struct {
			DHCP struct {
				Hosts []string `json:"hosts"`
			} `json:"dhcp"`
		} `json:"config"`
	}
	if err := c.get(ctx, "/api/config/dhcp/hosts", &body); err != nil {
		return nil, err
	}
	var out []Reservation
	for _, h := range body.Config.DHCP.Hosts {
		parts := strings.Split(h, ",")
		if len(parts) < 2 {
			continue
		}
		mac, ip := normMAC(parts[0]), normIP(parts[1])
		if mac == "" || ip == "" {
			continue
		}
		name := ""
		if len(parts) >= 3 {
			name = strings.TrimSpace(parts[2])
		}
		out = append(out, Reservation{MAC: mac, IP: ip, Hostname: name})
	}
	return out, nil
}

func (c *Client) DNSRecords(ctx context.Context) ([]DNSRecord, error) {
	var body struct {
		Config struct {
			DNS struct {
				Hosts []string `json:"hosts"`
			} `json:"dns"`
		} `json:"config"`
	}
	if err := c.get(ctx, "/api/config/dns/hosts", &body); err != nil {
		return nil, err
	}
	var out []DNSRecord
	for _, h := range body.Config.DNS.Hosts {
		fields := strings.Fields(h)
		if len(fields) < 2 {
			continue
		}
		ip := normIP(fields[0])
		if ip == "" {
			continue
		}
		for _, name := range fields[1:] {
			out = append(out, DNSRecord{IP: ip, Name: name})
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests + full suite**

Run: `go test ./internal/pihole/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pihole/client.go internal/pihole/client_test.go
git commit -m "feat(pihole): v6 REST client for leases, reservations, dns records"
```

---

### Task 4: Pi-hole sync / reconciliation

**Files:**
- Create: `internal/pihole/sync.go`, `internal/pihole/sync_test.go`

**Interfaces:**
- Consumes: `store` (`ListSubnets`, `FindIfaceByMAC`, `FindIfaceByIP`, `CreateDevice`, `AddIface`, `UpsertIPAssignment`, `SetIfaceHostnameIfEmpty`, `SetCustomField`), `events.Service`, and a `Fetcher` interface the real `Client` satisfies.
- Produces:
  - `type Fetcher interface { Leases(context.Context) ([]Lease, error); Reservations(context.Context) ([]Reservation, error); DNSRecords(context.Context) ([]DNSRecord, error) }`
  - `func NewSync(st *store.Store, c Fetcher, ev *events.Service) *Sync`
  - `func (s *Sync) RunOnce(ctx context.Context) error`
  - `func (s *Sync) Start(ctx context.Context, interval time.Duration)` — loops `RunOnce`, emits one `scan_error` event per outage (reset on success), same as `proxmox.Sync.Start`.
- Reconcile rules (implement exactly):
  1. Load subnets once. `subnetForIP(ip)` returns the subnet whose CIDR `Contains` the IP; no match → item skipped.
  2. Process reservations first, then leases, via a shared `upsertByMAC(mac, ip, hostname, subnetID, kind)`: `FindIfaceByMAC(mac)` hit → `UpsertIPAssignment(ifaceID, subnetID, ip, kind)` + `SetIfaceHostnameIfEmpty` when a hostname is present; miss → `CreateDevice{Name: hostname or "pihole-<mac>", Kind:"other", Source:"pihole"}`, `AddIface(devID, &mac, hostnamePtr)`, `UpsertIPAssignment(...)`, emit `device_new`. Reservations pass `kind="static"`, leases pass `kind="dhcp"`.
  3. Static beats dhcp: `RunOnce` records every `(subnetID, ip)` a reservation claimed this run in a `claimed` set. A lease whose `(subnetID, ip)` is already claimed skips its assignment (so it can't downgrade `static`→`dhcp`) but still enriches the hostname if the reservation had none.
  4. DNS A records: `FindIfaceByIP(subnetID, ip)` hit → `SetIfaceHostnameIfEmpty(ifaceID, name)` + `SetCustomField(deviceID, "pihole_dns", name)`. Miss → skip (never create a device from a DNS record).
  5. Never write `iface_status`. Never delete. Idempotent (a second `RunOnce` produces no new rows).

- [ ] **Step 1: Write the failing test**

`internal/pihole/sync_test.go`:

```go
package pihole

import (
	"context"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type fakeFetcher struct {
	leases []Lease
	res    []Reservation
	dns    []DNSRecord
}

func (f *fakeFetcher) Leases(context.Context) ([]Lease, error)             { return f.leases, nil }
func (f *fakeFetcher) Reservations(context.Context) ([]Reservation, error) { return f.res, nil }
func (f *fakeFetcher) DNSRecords(context.Context) ([]DNSRecord, error)     { return f.dns, nil }

func testSync(t *testing.T) (*store.Store, *fakeFetcher, *Sync) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	f := &fakeFetcher{}
	return st, f, NewSync(st, f, events.NewService(st, events.NewBroker()))
}

func deviceByName(t *testing.T, st *store.Store, name string) store.DeviceRow {
	t.Helper()
	rows, _ := st.ListDevices()
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("device %q not found in %+v", name, rows)
	return store.DeviceRow{}
}

func TestSyncCreatesUnknownFromReservation(t *testing.T) {
	st, f, sync := testSync(t)
	f.res = []Reservation{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	if err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := deviceByName(t, st, "printer")
	if d.Source != "pihole" || len(d.IPs) != 1 || d.IPs[0] != "10.0.0.20" {
		t.Fatalf("device=%+v", d)
	}
	// reservation → static
	ifaces, _ := st.ListIfaces(d.ID)
	ips, _ := st.ListIPs(ifaces[0].ID)
	if ips[0].Kind != "static" {
		t.Fatalf("want static, got %q", ips[0].Kind)
	}
	evs, _ := st.ListEvents(5)
	if len(evs) != 1 || evs[0].Type != "device_new" {
		t.Fatalf("events=%+v", evs)
	}
}

func TestSyncEnrichesExistingByMAC(t *testing.T) {
	st, f, sync := testSync(t)
	// Pre-existing device from another source with the same MAC.
	devID, _ := st.CreateDevice(store.Device{Name: "known", Kind: "computer", Source: "scan"})
	st.AddIface(devID, strpP("aa:bb:cc:00:00:10"), nil)
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "laptop"}}
	if err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListDevices()
	if len(rows) != 1 {
		t.Fatalf("must not create a second device: %+v", rows)
	}
	d := deviceByName(t, st, "known")
	if len(d.IPs) != 1 || d.IPs[0] != "10.0.0.10" {
		t.Fatalf("IP not attached: %+v", d.IPs)
	}
	ifaces, _ := st.ListIfaces(d.ID)
	if ifaces[0].Hostname == nil || *ifaces[0].Hostname != "laptop" {
		t.Fatalf("hostname not set: %+v", ifaces[0])
	}
}

func TestReservationBeatsLease(t *testing.T) {
	st, f, sync := testSync(t)
	f.res = []Reservation{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	if err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := deviceByName(t, st, "printer")
	ifaces, _ := st.ListIfaces(d.ID)
	ips, _ := st.ListIPs(ifaces[0].ID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("reservation should win (static), got %+v", ips)
	}
}

func TestDNSRecordAttachesAndSkipsUnknown(t *testing.T) {
	st, f, sync := testSync(t)
	// device at 10.0.0.20 exists (from a reservation this run)
	f.res = []Reservation{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: ""}}
	f.dns = []DNSRecord{
		{IP: "10.0.0.20", Name: "printer.lan"},
		{IP: "10.0.0.99", Name: "ghost.lan"}, // no device at this IP → skipped
	}
	if err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListDevices()
	if len(rows) != 1 {
		t.Fatalf("DNS must not create a device: %+v", rows)
	}
	d := rows[0]
	fields, _ := st.ListCustomFields(d.ID)
	var hasDNS bool
	for _, cf := range fields {
		if cf.Key == "pihole_dns" && cf.Value == "printer.lan" {
			hasDNS = true
		}
	}
	if !hasDNS {
		t.Fatalf("pihole_dns custom field missing: %+v", fields)
	}
}

func TestSyncIdempotentAndNoStatus(t *testing.T) {
	st, f, sync := testSync(t)
	f.res = []Reservation{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	f.dns = []DNSRecord{{IP: "10.0.0.20", Name: "printer.lan"}}
	for i := 0; i < 2; i++ {
		if err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	d := deviceByName(t, st, "printer")
	ifaces, _ := st.ListIfaces(d.ID)
	ips, _ := st.ListIPs(ifaces[0].ID)
	if len(ips) != 1 {
		t.Fatalf("duplicate assignment on re-run: %+v", ips)
	}
	// sync must never write iface_status
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM iface_status`).Scan(&n)
	if n != 0 {
		t.Fatalf("pihole sync must not touch iface_status, found %d rows", n)
	}
}

func strpP(s string) *string { return &s }
```

- [ ] **Step 2: Run the tests, verify they fail**

Run: `go test ./internal/pihole/ -run Sync -count=1`
Expected: FAIL — `NewSync`/`Sync` undefined.

- [ ] **Step 3: Implement**

`internal/pihole/sync.go`:

```go
package pihole

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Fetcher interface {
	Leases(context.Context) ([]Lease, error)
	Reservations(context.Context) ([]Reservation, error)
	DNSRecords(context.Context) ([]DNSRecord, error)
}

type Sync struct {
	store   *store.Store
	client  Fetcher
	events  *events.Service
	failing bool
}

func NewSync(st *store.Store, c Fetcher, ev *events.Service) *Sync {
	return &Sync{store: st, client: c, events: ev}
}

func (s *Sync) RunOnce(ctx context.Context) error {
	reservations, err := s.client.Reservations(ctx)
	if err != nil {
		return err
	}
	leases, err := s.client.Leases(ctx)
	if err != nil {
		return err
	}
	dns, err := s.client.DNSRecords(ctx)
	if err != nil {
		return err
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return err
	}

	// (subnetID<<0, ip) pairs a reservation already claimed this run; leases
	// for these skip assignment so a reservation's static kind is never
	// downgraded to dhcp.
	claimed := make(map[string]bool)

	for _, r := range reservations {
		snID, ok := subnetForIP(subnets, r.IP)
		if !ok {
			continue
		}
		s.upsertByMAC(r.MAC, r.IP, r.Hostname, snID, "static")
		claimed[fmt.Sprintf("%d|%s", snID, r.IP)] = true
	}
	for _, l := range leases {
		snID, ok := subnetForIP(subnets, l.IP)
		if !ok {
			continue
		}
		if claimed[fmt.Sprintf("%d|%s", snID, l.IP)] {
			// a reservation already assigned this IP as static; still enrich
			// the hostname but do not touch the assignment kind
			if iface, found, _ := s.store.FindIfaceByMAC(l.MAC); found && l.Hostname != "" {
				s.store.SetIfaceHostnameIfEmpty(iface.ID, l.Hostname)
			}
			continue
		}
		s.upsertByMAC(l.MAC, l.IP, l.Hostname, snID, "dhcp")
	}
	for _, rec := range dns {
		snID, ok := subnetForIP(subnets, rec.IP)
		if !ok {
			continue
		}
		iface, found, err := s.store.FindIfaceByIP(snID, rec.IP)
		if err != nil {
			return err
		}
		if !found {
			continue // never create a device from a DNS record alone
		}
		s.store.SetIfaceHostnameIfEmpty(iface.ID, rec.Name)
		s.store.SetCustomField(iface.DeviceID, "pihole_dns", rec.Name)
	}
	return nil
}

// upsertByMAC enriches an existing device (matched by MAC) or creates a new
// pihole-sourced device, then assigns the IP with the given kind.
func (s *Sync) upsertByMAC(mac, ip, hostname string, subnetID int64, kind string) {
	iface, found, err := s.store.FindIfaceByMAC(mac)
	if err != nil {
		log.Printf("pihole: FindIfaceByMAC %s: %v", mac, err)
		return
	}
	if found {
		s.store.UpsertIPAssignment(iface.ID, subnetID, ip, kind)
		if hostname != "" {
			s.store.SetIfaceHostnameIfEmpty(iface.ID, hostname)
		}
		return
	}
	name := hostname
	if name == "" {
		name = "pihole-" + mac
	}
	devID, err := s.store.CreateDevice(store.Device{Name: name, Kind: "other", Source: "pihole"})
	if err != nil {
		log.Printf("pihole: CreateDevice: %v", err)
		return
	}
	m := mac
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	ifID, err := s.store.AddIface(devID, &m, hp)
	if err != nil {
		log.Printf("pihole: AddIface: %v", err)
		return
	}
	s.store.UpsertIPAssignment(ifID, subnetID, ip, kind)
	s.events.Emit("device_new", &devID, fmt.Sprintf("pihole device %s at %s", name, ip))
}

func subnetForIP(subnets []store.Subnet, ip string) (int64, bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return 0, false
	}
	for _, sn := range subnets {
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil {
			continue
		}
		if p.Contains(addr) {
			return sn.ID, true
		}
	}
	return 0, false
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			if !s.failing {
				s.failing = true
				s.events.Emit("scan_error", nil, "pihole sync failing: "+err.Error())
			}
		} else {
			s.failing = false
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
```

- [ ] **Step 4: Run the tests + full suite**

Run: `go test ./internal/pihole/ -count=1 && CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pihole/sync.go internal/pihole/sync_test.go
git commit -m "feat(pihole): reconcile leases, reservations and dns into devices"
```

---

### Task 5: Settings keys, secret handling, settings UI

**Files:**
- Modify: `internal/web/settings.go` (settings keys + secret/insecure handling), `internal/web/views/settings.templ` (Pi-hole subsection)
- Test: `internal/web/settings_test.go` (add a case)

**Interfaces:**
- Consumes: existing `settingsKeys`, `handleSettingsPage`, `handleIntegrationsSave`, `SettingsData.Values`.
- Produces: three new settings keys `pihole_url`, `pihole_password`, `pihole_insecure`, handled so the password is never echoed back and a blank submission keeps the stored value, and the insecure checkbox is normalized `"on"→"1"`. A Pi-hole fieldset in the integrations form.

- [ ] **Step 1: Write the failing test**

Add to `internal/web/settings_test.go`:

```go
func TestPiholeSecretNeverEchoedAndBlankKeeps(t *testing.T) {
	srv, st := testServer(t)
	// Seed a stored password, then load the settings page as admin.
	st.SetSetting("pihole_password", "topsecret")
	rec := authedGet(t, srv, st, "/settings")
	if rec.Code != 200 {
		t.Fatalf("settings page code=%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "topsecret") {
		t.Fatal("pihole_password must never be rendered into the settings form")
	}
	// Posting integrations with a blank pihole_password keeps the stored value.
	authedPost(t, srv, st, "/settings/integrations", url.Values{
		"pihole_url":      {"https://pi.hole"},
		"pihole_password": {""},
		"pihole_insecure": {"on"},
	})
	if v, _ := st.GetSetting("pihole_password"); v != "topsecret" {
		t.Fatalf("blank password should keep stored value, got %q", v)
	}
	if v, _ := st.GetSetting("pihole_insecure"); v != "1" {
		t.Fatalf("insecure checkbox should normalize to '1', got %q", v)
	}
	if v, _ := st.GetSetting("pihole_url"); v != "https://pi.hole" {
		t.Fatalf("url not saved: %q", v)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `templ generate && go test ./internal/web/ -run Pihole -count=1`
Expected: FAIL — `pihole_url` not in `settingsKeys`, so it's never saved (and the page has no Pi-hole fields yet). (Run `export PATH="$PATH:$(go env GOPATH)/bin"` first.)

- [ ] **Step 3: Implement**

In `internal/web/settings.go`, extend `settingsKeys`:

```go
var settingsKeys = []string{
	"proxmox_url", "proxmox_token_id", "proxmox_secret", "proxmox_insecure",
	"wg_ssh_addr", "wg_ssh_user", "wg_ssh_key_path", "wg_iface",
	"pihole_url", "pihole_password", "pihole_insecure",
}
```

In `handleSettingsPage`, skip echoing the Pi-hole password (change the existing secret-skip condition):

```go
	for _, k := range settingsKeys {
		if k == "proxmox_secret" || k == "pihole_password" {
			// Never echo secrets back into the form.
			continue
		}
		v, err := s.store.GetSetting(k)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		values[k] = v
	}
```

In `handleIntegrationsSave`, treat the Pi-hole password/insecure like the Proxmox ones:

```go
	for _, k := range settingsKeys {
		v := r.FormValue(k)
		if (k == "proxmox_secret" || k == "pihole_password") && v == "" {
			// Blank means "leave unchanged" — don't wipe the stored secret.
			continue
		}
		if k == "proxmox_insecure" || k == "pihole_insecure" {
			// Normalize the checkbox ("on"/"") to the "1"/"" that main.go reads.
			if v == "on" {
				v = "1"
			} else {
				v = ""
			}
		}
		if err := s.store.SetSetting(k, v); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
```

In `internal/web/views/settings.templ`, add a Pi-hole `<fieldset>` inside the integrations `<form>`, after the WireGuard fieldset and before the `Save integrations` button:

```templ
			<fieldset>
				<legend>Pi-hole (v6)</legend>
				<label>
					URL
					<input type="text" name="pihole_url" value={ d.Values["pihole_url"] } placeholder="https://pi.hole"/>
				</label>
				<label>
					App password
					<input type="password" name="pihole_password" placeholder="leave blank to keep current password"/>
				</label>
				<label>
					<input
						type="checkbox" name="pihole_insecure"
						if d.Values["pihole_insecure"] == "1" {
							checked
						}
					/>
					Skip TLS verification
				</label>
			</fieldset>
```

- [ ] **Step 4: Regenerate templ, run tests + build**

Run: `export PATH="$PATH:$(go env GOPATH)/bin" && templ generate && go test ./internal/web/ -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/settings.go internal/web/views/settings.templ internal/web/views/settings_templ.go internal/web/settings_test.go
git commit -m "feat(web): pihole settings keys, secret handling and settings fieldset"
```

---

### Task 6: Wire the poller into main + README

**Files:**
- Modify: `cmd/netis/main.go` (start the Pi-hole poller), `README.md` (settings keys + note)

**Interfaces:**
- Consumes: `pihole.NewClient`, `pihole.NewSync`, existing `st`, `evs`, `ctx`.
- Produces: on startup, when `pihole_url` is set, a background poller runs `pihole.Sync.Start(ctx, time.Minute)`.

- [ ] **Step 1: Add the wiring**

In `cmd/netis/main.go`, add the import `"netis/internal/pihole"` and, after the WireGuard block and before `srv := web.NewServer(...)`, add:

```go
	if phURL, _ := st.GetSetting("pihole_url"); phURL != "" {
		phPass, _ := st.GetSetting("pihole_password")
		phInsecure, _ := st.GetSetting("pihole_insecure")
		ph := pihole.NewSync(st, pihole.NewClient(phURL, phPass, phInsecure == "1"), evs)
		go ph.Start(ctx, time.Minute)
	}
```

- [ ] **Step 2: Verify it builds**

Run: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./...`
Expected: clean (no unused import, no error).

- [ ] **Step 3: Update the README**

In `README.md`, in the settings-keys / integrations section, add the Pi-hole keys alongside the Proxmox and WireGuard ones:

```markdown
### Pi-hole (v6)

Set in Settings → Integrations, or as `setting` rows:

- `pihole_url` — base URL of the Pi-hole admin, e.g. `https://pi.hole` (empty disables the integration)
- `pihole_password` — Pi-hole app password (never shown back in the UI)
- `pihole_insecure` — `1` to skip TLS verification (self-signed certs)

When configured, netis polls Pi-hole every minute and merges DHCP leases,
static reservations, and local DNS A records into the device inventory:
devices are matched by MAC (unknown MACs are created with source `pihole`),
reservations mark their IP `static`, and DNS names attach to the matching
device. Pi-hole data never changes a device's online/last-seen status — that
stays driven by the scanner. IPs are only attached when they fall inside a
subnet you've configured in netis.
```

- [ ] **Step 4: Full verification**

Run: `CGO_ENABLED=0 go test ./... -count=1 && CGO_ENABLED=0 go build -o /tmp/netis-pihole ./cmd/netis`
Expected: all tests pass, binary builds.

- [ ] **Step 5: Commit**

```bash
git add cmd/netis/main.go README.md
git commit -m "feat: wire pihole poller into main and document settings"
```

---

## Final verification (after Task 6)

- [ ] `CGO_ENABLED=0 go test ./... -count=1` — all green.
- [ ] `go test -race ./internal/pihole/ ./internal/store/ -count=1` — clean.
- [ ] `CGO_ENABLED=0 go build -o netis ./cmd/netis` — builds.
- [ ] Manual smoke: open a fresh DB, confirm `/setup`→login still works and the Settings page shows the Pi-hole fieldset (the migration widening `device.source` applied cleanly on the existing schema).
- [ ] Use superpowers:finishing-a-development-branch.
