# Netis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Self-hosted home-network organizer: device inventory with CRUD, background scanning with online/last-seen tracking, per-subnet IP grid, Proxmox and WireGuard sync.

**Architecture:** Single Go monolith. HTTP server renders templ templates driven by HTMX with SSE live updates. Background goroutines run per-subnet scan loops and Proxmox/WireGuard pollers. All state in one SQLite file via a store package; scanning, integrations, and web layers only talk to the store and the event service.

**Tech Stack:** Go 1.24+, templ, HTMX + SSE extension, `modernc.org/sqlite` (pure Go), `github.com/prometheus-community/pro-bing` (ICMP, unprivileged fallback), `golang.org/x/crypto` (bcrypt + ssh).

**Spec:** `docs/superpowers/specs/2026-07-11-netis-design.md` — read it before starting any task.

## Global Constraints

- Go 1.24+, CGO disabled everywhere (`CGO_ENABLED=0`); SQLite driver is `modernc.org/sqlite` (import as `_ "modernc.org/sqlite"`, driver name `"sqlite"`).
- Module path is `netis` (`go mod init netis`).
- No live network calls in tests. Scanner, SSH, and Proxmox HTTP are behind interfaces; tests use fakes/fixtures. Store tests use `:memory:` SQLite.
- After editing any `.templ` file run `templ generate` before `go build`. Install once: `go install github.com/a-h/templ/cmd/templ@v0.3.887`.
- Run tests with `go test ./... -count=1`. Build with `CGO_ENABLED=0 go build ./...`.
- Timestamps stored as UTC RFC3339 strings (`time.Now().UTC().Format(time.RFC3339)`).
- MACs normalized lowercase colon-separated (`aa:bb:cc:dd:ee:ff`); IPs canonical via `netip.Addr.String()`.
- Enum values (device kind, source, subnet kind, event type, roles) exactly as in the spec's data-model section.
- Commit after every task with a conventional-commit message.

---

### Task 1: Project scaffold, config, HTTP server with /healthz

**Files:**
- Create: `go.mod`, `cmd/netis/main.go`, `internal/config/config.go`, `internal/web/server.go`
- Test: `internal/web/server_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `config.Load() Config` (fields `Addr string`, `DBPath string`, from env `NETIS_ADDR` default `:8080`, `NETIS_DB` default `netis.db`); `web.NewServer(...) *Server` with `func (s *Server) Handler() http.Handler`. Later tasks add constructor params — at this stage `web.NewServer()` takes no args.

- [ ] **Step 1: Init module and write failing tests**

```bash
go mod init netis
```

`internal/config/config_test.go`:

```go
package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("NETIS_ADDR", "")
	t.Setenv("NETIS_DB", "")
	c := Load()
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DBPath != "netis.db" {
		t.Errorf("DBPath = %q, want netis.db", c.DBPath)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("NETIS_ADDR", ":9999")
	t.Setenv("NETIS_DB", "/data/n.db")
	c := Load()
	if c.Addr != ":9999" || c.DBPath != "/data/n.db" {
		t.Errorf("got %+v", c)
	}
}
```

`internal/web/server_test.go`:

```go
package web

import (
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	srv := NewServer()
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests, verify they fail to compile**

Run: `go test ./... -count=1`
Expected: FAIL — `undefined: Load`, `undefined: NewServer`.

- [ ] **Step 3: Implement**

`internal/config/config.go`:

```go
package config

import "os"

type Config struct {
	Addr   string
	DBPath string
}

func Load() Config {
	c := Config{Addr: ":8080", DBPath: "netis.db"}
	if v := os.Getenv("NETIS_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("NETIS_DB"); v != "" {
		c.DBPath = v
	}
	return c
}
```

`internal/web/server.go`:

```go
package web

import "net/http"

type Server struct {
	mux *http.ServeMux
}

func NewServer() *Server {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }
```

`cmd/netis/main.go`:

```go
package main

import (
	"log"
	"net/http"

	"netis/internal/config"
	"netis/internal/web"
)

func main() {
	cfg := config.Load()
	srv := web.NewServer()
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
```

- [ ] **Step 4: Run tests and build**

Run: `go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: all PASS, build clean.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: project scaffold with config and healthz"
```

---

### Task 2: SQLite schema, migrations, store.Open

**Files:**
- Create: `internal/store/store.go`, `internal/store/migrations/0001_init.sql`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `store.Open(path string) (*Store, error)` — opens SQLite (path or `:memory:`), enables foreign keys, applies embedded migrations idempotently (tracked in `schema_migrations(version TEXT PRIMARY KEY)`). `type Store struct { DB *sql.DB }`, `func (s *Store) Close() error`. All later store tasks add methods to `*Store`. Test helper pattern: `st, _ := store.Open(":memory:")`.

- [ ] **Step 1: Write failing test**

`internal/store/store_test.go`:

```go
package store

import "testing"

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesSchema(t *testing.T) {
	s := openTest(t)
	for _, table := range []string{
		"subnet", "device", "iface", "ip_assignment", "iface_status",
		"availability_history", "open_port", "tag", "device_tag",
		"custom_field", "device_link", "event", "user", "session", "setting",
	} {
		var n int
		err := s.DB.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s missing (n=%d err=%v)", table, n, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := t.TempDir() + "/x.db"
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/store/ -count=1`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 3: Write migration and store**

`internal/store/migrations/0001_init.sql`:

```sql
CREATE TABLE subnet (
  id INTEGER PRIMARY KEY,
  cidr TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  vlan_id INTEGER,
  kind TEXT NOT NULL DEFAULT 'lan' CHECK (kind IN ('lan','wireguard','proxmox-bridge')),
  scan_enabled INTEGER NOT NULL DEFAULT 1,
  scan_interval_sec INTEGER NOT NULL DEFAULT 120
);

CREATE TABLE device (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'other' CHECK (kind IN
    ('computer','switch','phone','server','printer','iot','vm','lxc','wg-peer','other')),
  notes TEXT NOT NULL DEFAULT '',
  vendor TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','scan','proxmox','wireguard')),
  parent_device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  proxmox_vmid INTEGER,
  wg_pubkey TEXT,
  icon TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE iface (
  id INTEGER PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  mac TEXT,
  hostname TEXT
);
CREATE UNIQUE INDEX idx_iface_mac ON iface(mac) WHERE mac IS NOT NULL;

CREATE TABLE ip_assignment (
  id INTEGER PRIMARY KEY,
  iface_id INTEGER NOT NULL REFERENCES iface(id) ON DELETE CASCADE,
  subnet_id INTEGER NOT NULL REFERENCES subnet(id) ON DELETE CASCADE,
  ip TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'dhcp' CHECK (kind IN ('static','dhcp'))
);
CREATE INDEX idx_ip_subnet ON ip_assignment(subnet_id, ip);

CREATE TABLE iface_status (
  iface_id INTEGER PRIMARY KEY REFERENCES iface(id) ON DELETE CASCADE,
  online INTEGER NOT NULL DEFAULT 0,
  first_seen TEXT,
  last_seen TEXT,
  last_rtt_ms REAL,
  missed_sweeps INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE availability_history (
  id INTEGER PRIMARY KEY,
  iface_id INTEGER NOT NULL REFERENCES iface(id) ON DELETE CASCADE,
  bucket_start TEXT NOT NULL,
  up_count INTEGER NOT NULL DEFAULT 0,
  total_count INTEGER NOT NULL DEFAULT 0,
  UNIQUE (iface_id, bucket_start)
);

CREATE TABLE open_port (
  id INTEGER PRIMARY KEY,
  iface_id INTEGER NOT NULL REFERENCES iface(id) ON DELETE CASCADE,
  port INTEGER NOT NULL,
  proto TEXT NOT NULL DEFAULT 'tcp' CHECK (proto IN ('tcp','udp')),
  service_guess TEXT NOT NULL DEFAULT '',
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  UNIQUE (iface_id, port, proto)
);

CREATE TABLE tag (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  color TEXT NOT NULL DEFAULT '#888888'
);

CREATE TABLE device_tag (
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
  PRIMARY KEY (device_id, tag_id)
);

CREATE TABLE custom_field (
  id INTEGER PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  UNIQUE (device_id, key)
);

CREATE TABLE device_link (
  id INTEGER PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  url TEXT NOT NULL
);

CREATE TABLE event (
  id INTEGER PRIMARY KEY,
  ts TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  type TEXT NOT NULL CHECK (type IN ('device_new','online','offline','ip_changed','scan_error')),
  device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  details TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_event_ts ON event(ts DESC);

CREATE TABLE user (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin','viewer'))
);

CREATE TABLE session (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);

CREATE TABLE setting (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

`internal/store/store.go`:

```go
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: single writer, avoids SQLITE_BUSY
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

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
		if _, err := s.DB.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := s.DB.Exec(
			`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
			return err
		}
	}
	return nil
}
```

Run: `go get modernc.org/sqlite`

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: sqlite store with embedded migrations and full schema"
```

---

### Task 3: Subnet, device, interface, IP repos

**Files:**
- Create: `internal/store/subnet.go`, `internal/store/device.go`
- Test: `internal/store/subnet_test.go`, `internal/store/device_test.go`

**Interfaces:**
- Consumes: `store.Open`, `openTest` helper (Task 2).
- Produces (all methods on `*Store`):
  - `type Subnet struct { ID int64; CIDR, Name string; VLANID *int64; Kind string; ScanEnabled bool; ScanIntervalSec int }`
  - `CreateSubnet(sn Subnet) (int64, error)` / `GetSubnet(id int64) (Subnet, error)` / `ListSubnets() ([]Subnet, error)` / `UpdateSubnet(sn Subnet) error` / `DeleteSubnet(id int64) error`
  - `type Device struct { ID int64; Name, Kind, Notes, Vendor, Source string; ParentDeviceID *int64; ProxmoxVMID *int64; WGPubKey *string; Icon string }`
  - `CreateDevice(d Device) (int64, error)` / `GetDevice(id int64) (Device, error)` / `UpdateDevice(d Device) error` / `DeleteDevice(id int64) error`
  - `type Iface struct { ID, DeviceID int64; MAC, Hostname *string }`
  - `AddIface(deviceID int64, mac, hostname *string) (int64, error)` / `FindIfaceByMAC(mac string) (Iface, bool, error)` / `ListIfaces(deviceID int64) ([]Iface, error)`
  - `AssignIP(ifaceID, subnetID int64, ip, kind string) (int64, error)` / `FindIfaceByIP(subnetID int64, ip string) (Iface, bool, error)` / `type IPRow struct { ID int64; IP string; SubnetID int64; Kind string }` / `ListIPs(ifaceID int64) ([]IPRow, error)`
  - `type DeviceRow struct { Device; IPs []string; MACs []string; Online bool; LastSeen *string; TagNames []string }`
  - `ListDevices() ([]DeviceRow, error)` — joined listing for the device table (status join arrives Task 4; until then Online=false, LastSeen=nil).

- [ ] **Step 1: Write failing tests**

`internal/store/subnet_test.go`:

```go
package store

import "testing"

func TestSubnetCRUD(t *testing.T) {
	s := openTest(t)
	id, err := s.CreateSubnet(Subnet{CIDR: "192.168.1.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	sn, err := s.GetSubnet(id)
	if err != nil || sn.CIDR != "192.168.1.0/24" || !sn.ScanEnabled {
		t.Fatalf("get: %+v err=%v", sn, err)
	}
	sn.Name = "main"
	if err := s.UpdateSubnet(sn); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSubnets()
	if err != nil || len(list) != 1 || list[0].Name != "main" {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	if err := s.DeleteSubnet(id); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.ListSubnets(); len(list) != 0 {
		t.Fatalf("want empty, got %+v", list)
	}
}
```

`internal/store/device_test.go`:

```go
package store

import "testing"

func strp(s string) *string { return &s }

func TestDeviceIfaceIP(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, err := s.CreateDevice(Device{Name: "nas", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	ifID, err := s.AddIface(devID, strp("aa:bb:cc:dd:ee:ff"), strp("nas.local"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignIP(ifID, snID, "10.0.0.5", "static"); err != nil {
		t.Fatal(err)
	}

	iface, ok, err := s.FindIfaceByMAC("aa:bb:cc:dd:ee:ff")
	if err != nil || !ok || iface.DeviceID != devID {
		t.Fatalf("byMAC: %+v ok=%v err=%v", iface, ok, err)
	}
	iface, ok, err = s.FindIfaceByIP(snID, "10.0.0.5")
	if err != nil || !ok || iface.ID != ifID {
		t.Fatalf("byIP: %+v ok=%v err=%v", iface, ok, err)
	}
	if _, ok, _ = s.FindIfaceByIP(snID, "10.0.0.99"); ok {
		t.Fatal("unexpected hit")
	}

	rows, err := s.ListDevices()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %+v err=%v", rows, err)
	}
	if rows[0].Name != "nas" || len(rows[0].IPs) != 1 || rows[0].IPs[0] != "10.0.0.5" {
		t.Fatalf("row: %+v", rows[0])
	}

	if err := s.DeleteDevice(devID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FindIfaceByMAC("aa:bb:cc:dd:ee:ff"); ok {
		t.Fatal("iface should cascade-delete")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/store/ -count=1`
Expected: FAIL — undefined types/methods.

- [ ] **Step 3: Implement**

`internal/store/subnet.go`:

```go
package store

type Subnet struct {
	ID              int64
	CIDR            string
	Name            string
	VLANID          *int64
	Kind            string
	ScanEnabled     bool
	ScanIntervalSec int
}

func (s *Store) CreateSubnet(sn Subnet) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO subnet (cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec)
		VALUES (?,?,?,?,?,?)`,
		sn.CIDR, sn.Name, sn.VLANID, sn.Kind, sn.ScanEnabled, sn.ScanIntervalSec)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetSubnet(id int64) (Subnet, error) {
	var sn Subnet
	err := s.DB.QueryRow(`SELECT id,cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec
		FROM subnet WHERE id=?`, id).
		Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.VLANID, &sn.Kind, &sn.ScanEnabled, &sn.ScanIntervalSec)
	return sn, err
}

func (s *Store) ListSubnets() ([]Subnet, error) {
	rows, err := s.DB.Query(`SELECT id,cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec
		FROM subnet ORDER BY cidr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subnet
	for rows.Next() {
		var sn Subnet
		if err := rows.Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.VLANID, &sn.Kind,
			&sn.ScanEnabled, &sn.ScanIntervalSec); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSubnet(sn Subnet) error {
	_, err := s.DB.Exec(`UPDATE subnet SET cidr=?,name=?,vlan_id=?,kind=?,scan_enabled=?,scan_interval_sec=?
		WHERE id=?`,
		sn.CIDR, sn.Name, sn.VLANID, sn.Kind, sn.ScanEnabled, sn.ScanIntervalSec, sn.ID)
	return err
}

func (s *Store) DeleteSubnet(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM subnet WHERE id=?`, id)
	return err
}
```

`internal/store/device.go`:

```go
package store

type Device struct {
	ID             int64
	Name           string
	Kind           string
	Notes          string
	Vendor         string
	Source         string
	ParentDeviceID *int64
	ProxmoxVMID    *int64
	WGPubKey       *string
	Icon           string
}

type Iface struct {
	ID       int64
	DeviceID int64
	MAC      *string
	Hostname *string
}

type IPRow struct {
	ID       int64
	IP       string
	SubnetID int64
	Kind     string
}

type DeviceRow struct {
	Device
	IPs      []string
	MACs     []string
	Online   bool
	LastSeen *string
	TagNames []string
}

const deviceCols = `id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Notes, &d.Vendor, &d.Source,
		&d.ParentDeviceID, &d.ProxmoxVMID, &d.WGPubKey, &d.Icon)
	return d, err
}

func (s *Store) CreateDevice(d Device) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO device (name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source, d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetDevice(id int64) (Device, error) {
	return scanDevice(s.DB.QueryRow(`SELECT `+deviceCols+` FROM device WHERE id=?`, id))
}

func (s *Store) UpdateDevice(d Device) error {
	_, err := s.DB.Exec(`UPDATE device SET name=?,kind=?,notes=?,vendor=?,source=?,
		parent_device_id=?,proxmox_vmid=?,wg_pubkey=?,icon=? WHERE id=?`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source,
		d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon, d.ID)
	return err
}

func (s *Store) DeleteDevice(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM device WHERE id=?`, id)
	return err
}

func (s *Store) AddIface(deviceID int64, mac, hostname *string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO iface (device_id,mac,hostname) VALUES (?,?,?)`,
		deviceID, mac, hostname)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListIfaces(deviceID int64) ([]Iface, error) {
	rows, err := s.DB.Query(`SELECT id,device_id,mac,hostname FROM iface WHERE device_id=?`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Iface
	for rows.Next() {
		var i Iface
		if err := rows.Scan(&i.ID, &i.DeviceID, &i.MAC, &i.Hostname); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) FindIfaceByMAC(mac string) (Iface, bool, error) {
	var i Iface
	err := s.DB.QueryRow(`SELECT id,device_id,mac,hostname FROM iface WHERE mac=?`, mac).
		Scan(&i.ID, &i.DeviceID, &i.MAC, &i.Hostname)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return i, false, nil
		}
		return i, false, err
	}
	return i, true, nil
}

func (s *Store) FindIfaceByIP(subnetID int64, ip string) (Iface, bool, error) {
	var i Iface
	err := s.DB.QueryRow(`SELECT f.id,f.device_id,f.mac,f.hostname FROM iface f
		JOIN ip_assignment a ON a.iface_id=f.id WHERE a.subnet_id=? AND a.ip=?`, subnetID, ip).
		Scan(&i.ID, &i.DeviceID, &i.MAC, &i.Hostname)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return i, false, nil
		}
		return i, false, err
	}
	return i, true, nil
}

func (s *Store) AssignIP(ifaceID, subnetID int64, ip, kind string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO ip_assignment (iface_id,subnet_id,ip,kind) VALUES (?,?,?,?)`,
		ifaceID, subnetID, ip, kind)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListIPs(ifaceID int64) ([]IPRow, error) {
	rows, err := s.DB.Query(`SELECT id,ip,subnet_id,kind FROM ip_assignment WHERE iface_id=?`, ifaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPRow
	for rows.Next() {
		var r IPRow
		if err := rows.Scan(&r.ID, &r.IP, &r.SubnetID, &r.Kind); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListDevices() ([]DeviceRow, error) {
	devRows, err := s.DB.Query(`SELECT ` + deviceCols + ` FROM device ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer devRows.Close()
	var out []DeviceRow
	for devRows.Next() {
		d, err := scanDevice(devRows)
		if err != nil {
			return nil, err
		}
		out = append(out, DeviceRow{Device: d})
	}
	if err := devRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ifaces, err := s.ListIfaces(out[i].ID)
		if err != nil {
			return nil, err
		}
		for _, f := range ifaces {
			if f.MAC != nil {
				out[i].MACs = append(out[i].MACs, *f.MAC)
			}
			ips, err := s.ListIPs(f.ID)
			if err != nil {
				return nil, err
			}
			for _, p := range ips {
				out[i].IPs = append(out[i].IPs, p.IP)
			}
			online, lastSeen, err := s.ifaceOnline(f.ID)
			if err != nil {
				return nil, err
			}
			if online {
				out[i].Online = true
			}
			if lastSeen != nil && (out[i].LastSeen == nil || *lastSeen > *out[i].LastSeen) {
				out[i].LastSeen = lastSeen
			}
		}
		tags, err := s.deviceTagNames(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].TagNames = tags
	}
	return out, nil
}

// ifaceOnline and deviceTagNames get real implementations in Task 4;
// these stubs keep Task 3 self-contained.
func (s *Store) ifaceOnline(ifaceID int64) (bool, *string, error) {
	var online bool
	var lastSeen *string
	err := s.DB.QueryRow(`SELECT online,last_seen FROM iface_status WHERE iface_id=?`, ifaceID).
		Scan(&online, &lastSeen)
	if err != nil {
		return false, nil, nil // no status row yet
	}
	return online, lastSeen, nil
}

func (s *Store) deviceTagNames(deviceID int64) ([]string, error) {
	rows, err := s.DB.Query(`SELECT t.name FROM tag t
		JOIN device_tag dt ON dt.tag_id=t.id WHERE dt.device_id=? ORDER BY t.name`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: subnet, device, interface and IP repositories"
```

---

### Task 4: Status, events, tags, custom fields, links, ports, users, settings repos

**Files:**
- Create: `internal/store/status.go`, `internal/store/event.go`, `internal/store/meta.go`, `internal/store/user.go`
- Test: `internal/store/status_test.go`, `internal/store/meta_test.go`

**Interfaces:**
- Consumes: Task 2/3 store types.
- Produces (methods on `*Store`):
  - `MarkSeen(ifaceID int64, rttMS float64, at time.Time) (wasOffline bool, err error)` — upsert status: online=1, missed_sweeps=0, sets first_seen if null, last_seen=at. Returns true if row was previously offline or absent.
  - `MarkMissed(ifaceID int64, offlineAfter int) (wentOffline bool, err error)` — increments missed_sweeps; when count reaches offlineAfter and online was 1, sets online=0 and returns true.
  - `RecordAvailability(ifaceID int64, up bool, bucketStart string) error` — upsert hourly bucket, increments total_count and up_count if up.
  - `AvailabilityPct(ifaceID int64, sinceBucket string) (float64, error)` — up/total since bucket; 100 when no data.
  - `AddEvent(typ string, deviceID *int64, details string) (int64, error)`; `type Event struct { ID int64; TS, Type string; DeviceID *int64; Details string }`; `ListEvents(limit int) ([]Event, error)` (newest first).
  - Tags: `CreateTag(name, color string) (int64, error)`, `ListTags() ([]Tag, error)` with `type Tag struct { ID int64; Name, Color string }`, `TagDevice(deviceID, tagID int64) error`, `UntagDevice(deviceID, tagID int64) error`.
  - Custom fields: `SetCustomField(deviceID int64, key, value string) error` (upsert), `DeleteCustomField(deviceID int64, key string) error`, `ListCustomFields(deviceID int64) ([]CustomField, error)` with `type CustomField struct { Key, Value string }`.
  - Links: `AddLink(deviceID int64, label, url string) (int64, error)`, `DeleteLink(id int64) error`, `ListLinks(deviceID int64) ([]Link, error)` with `type Link struct { ID int64; Label, URL string }`.
  - Ports: `UpsertOpenPort(ifaceID int64, port int, proto, guess, seenAt string) error`, `ListOpenPorts(ifaceID int64) ([]OpenPort, error)` with `type OpenPort struct { Port int; Proto, ServiceGuess, FirstSeen, LastSeen string }`.
  - Users: `CreateUser(username, passwordHash, role string) (int64, error)`, `GetUserByName(username string) (User, bool, error)` with `type User struct { ID int64; Username, PasswordHash, Role string }`, `CountUsers() (int, error)`, `ListUsers() ([]User, error)`, `DeleteUser(id int64) error`.
  - Sessions: `CreateSession(token string, userID int64, expiresAt string) error`, `GetSession(token string) (User, bool, error)` (joins user, checks expiry against now), `DeleteSession(token string) error`.
  - Settings: `GetSetting(key string) (string, error)` (empty string when missing), `SetSetting(key, value string) error` (upsert).

- [ ] **Step 1: Write failing tests**

`internal/store/status_test.go`:

```go
package store

import (
	"testing"
	"time"
)

func testIface(t *testing.T, s *Store) int64 {
	t.Helper()
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "manual"})
	ifID, _ := s.AddIface(devID, strp("11:22:33:44:55:66"), nil)
	return ifID
}

func TestMarkSeenAndMissed(t *testing.T) {
	s := openTest(t)
	ifID := testIface(t, s)
	now := time.Now().UTC()

	wasOffline, err := s.MarkSeen(ifID, 1.5, now)
	if err != nil || !wasOffline {
		t.Fatalf("first MarkSeen: wasOffline=%v err=%v", wasOffline, err)
	}
	wasOffline, _ = s.MarkSeen(ifID, 2.0, now)
	if wasOffline {
		t.Fatal("second MarkSeen should report already-online")
	}

	// 3 misses with offlineAfter=3: flips on the third
	for i := 1; i <= 2; i++ {
		if went, _ := s.MarkMissed(ifID, 3); went {
			t.Fatalf("miss %d should not flip", i)
		}
	}
	if went, _ := s.MarkMissed(ifID, 3); !went {
		t.Fatal("third miss should flip offline")
	}
	if went, _ := s.MarkMissed(ifID, 3); went {
		t.Fatal("already offline, no second flip")
	}
}

func TestAvailability(t *testing.T) {
	s := openTest(t)
	ifID := testIface(t, s)
	b := "2026-07-11T10:00:00Z"
	s.RecordAvailability(ifID, true, b)
	s.RecordAvailability(ifID, true, b)
	s.RecordAvailability(ifID, false, b)
	pct, err := s.AvailabilityPct(ifID, "2026-07-01T00:00:00Z")
	if err != nil || pct < 66.0 || pct > 67.0 {
		t.Fatalf("pct=%v err=%v", pct, err)
	}
}

func TestEvents(t *testing.T) {
	s := openTest(t)
	if _, err := s.AddEvent("scan_error", nil, "boom"); err != nil {
		t.Fatal(err)
	}
	evs, err := s.ListEvents(10)
	if err != nil || len(evs) != 1 || evs[0].Type != "scan_error" {
		t.Fatalf("evs=%+v err=%v", evs, err)
	}
}
```

`internal/store/meta_test.go`:

```go
package store

import "testing"

func TestTagsFieldsLinksPortsUsersSettings(t *testing.T) {
	s := openTest(t)
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "manual"})
	ifID, _ := s.AddIface(devID, nil, nil)

	tagID, err := s.CreateTag("critical", "#ff0000")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TagDevice(devID, tagID); err != nil {
		t.Fatal(err)
	}
	names, _ := s.deviceTagNames(devID)
	if len(names) != 1 || names[0] != "critical" {
		t.Fatalf("tags=%v", names)
	}

	s.SetCustomField(devID, "rack", "u4")
	s.SetCustomField(devID, "rack", "u5") // upsert
	cfs, _ := s.ListCustomFields(devID)
	if len(cfs) != 1 || cfs[0].Value != "u5" {
		t.Fatalf("cfs=%+v", cfs)
	}

	s.AddLink(devID, "web ui", "http://10.0.0.5")
	links, _ := s.ListLinks(devID)
	if len(links) != 1 || links[0].URL != "http://10.0.0.5" {
		t.Fatalf("links=%+v", links)
	}

	s.UpsertOpenPort(ifID, 22, "tcp", "ssh", "2026-07-11T10:00:00Z")
	s.UpsertOpenPort(ifID, 22, "tcp", "ssh", "2026-07-11T11:00:00Z")
	ports, _ := s.ListOpenPorts(ifID)
	if len(ports) != 1 || ports[0].LastSeen != "2026-07-11T11:00:00Z" ||
		ports[0].FirstSeen != "2026-07-11T10:00:00Z" {
		t.Fatalf("ports=%+v", ports)
	}

	if n, _ := s.CountUsers(); n != 0 {
		t.Fatal("expected 0 users")
	}
	s.CreateUser("ben", "hash", "admin")
	u, ok, _ := s.GetUserByName("ben")
	if !ok || u.Role != "admin" {
		t.Fatalf("user=%+v ok=%v", u, ok)
	}
	s.CreateSession("tok1", u.ID, "2099-01-01T00:00:00Z")
	su, ok, _ := s.GetSession("tok1")
	if !ok || su.Username != "ben" {
		t.Fatalf("session user=%+v ok=%v", su, ok)
	}
	s.CreateSession("tok2", u.ID, "2000-01-01T00:00:00Z")
	if _, ok, _ := s.GetSession("tok2"); ok {
		t.Fatal("expired session should not resolve")
	}

	if v, _ := s.GetSetting("nope"); v != "" {
		t.Fatal("missing setting should be empty")
	}
	s.SetSetting("offline_after", "3")
	s.SetSetting("offline_after", "4")
	if v, _ := s.GetSetting("offline_after"); v != "4" {
		t.Fatalf("setting=%q", v)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/store/ -count=1`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Implement**

`internal/store/status.go`:

```go
package store

import "time"

func (s *Store) MarkSeen(ifaceID int64, rttMS float64, at time.Time) (bool, error) {
	ts := at.UTC().Format(time.RFC3339)
	var online bool
	err := s.DB.QueryRow(`SELECT online FROM iface_status WHERE iface_id=?`, ifaceID).Scan(&online)
	wasOffline := err != nil || !online
	_, err = s.DB.Exec(`INSERT INTO iface_status (iface_id,online,first_seen,last_seen,last_rtt_ms,missed_sweeps)
		VALUES (?,1,?,?,?,0)
		ON CONFLICT(iface_id) DO UPDATE SET
			online=1, last_seen=excluded.last_seen, last_rtt_ms=excluded.last_rtt_ms,
			missed_sweeps=0, first_seen=COALESCE(iface_status.first_seen, excluded.first_seen)`,
		ifaceID, ts, ts, rttMS)
	return wasOffline, err
}

func (s *Store) MarkMissed(ifaceID int64, offlineAfter int) (bool, error) {
	var online bool
	var missed int
	err := s.DB.QueryRow(`SELECT online,missed_sweeps FROM iface_status WHERE iface_id=?`, ifaceID).
		Scan(&online, &missed)
	if err != nil {
		return false, nil // never seen: nothing to mark
	}
	missed++
	wentOffline := online && missed >= offlineAfter
	_, err = s.DB.Exec(`UPDATE iface_status SET missed_sweeps=?, online=CASE WHEN ?>=? THEN 0 ELSE online END
		WHERE iface_id=?`, missed, missed, offlineAfter, ifaceID)
	return wentOffline, err
}

func (s *Store) RecordAvailability(ifaceID int64, up bool, bucketStart string) error {
	upN := 0
	if up {
		upN = 1
	}
	_, err := s.DB.Exec(`INSERT INTO availability_history (iface_id,bucket_start,up_count,total_count)
		VALUES (?,?,?,1)
		ON CONFLICT(iface_id,bucket_start) DO UPDATE SET
			up_count=up_count+excluded.up_count, total_count=total_count+1`,
		ifaceID, bucketStart, upN)
	return err
}

func (s *Store) AvailabilityPct(ifaceID int64, sinceBucket string) (float64, error) {
	var up, total int
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(up_count),0), COALESCE(SUM(total_count),0)
		FROM availability_history WHERE iface_id=? AND bucket_start>=?`, ifaceID, sinceBucket).
		Scan(&up, &total)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 100, nil
	}
	return 100 * float64(up) / float64(total), nil
}
```

`internal/store/event.go`:

```go
package store

type Event struct {
	ID       int64
	TS       string
	Type     string
	DeviceID *int64
	Details  string
}

func (s *Store) AddEvent(typ string, deviceID *int64, details string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO event (type,device_id,details) VALUES (?,?,?)`,
		typ, deviceID, details)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListEvents(limit int) ([]Event, error) {
	rows, err := s.DB.Query(`SELECT id,ts,type,device_id,details FROM event
		ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &e.DeviceID, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

`internal/store/meta.go`:

```go
package store

type Tag struct {
	ID    int64
	Name  string
	Color string
}

type CustomField struct{ Key, Value string }

type Link struct {
	ID    int64
	Label string
	URL   string
}

type OpenPort struct {
	Port                                    int
	Proto, ServiceGuess, FirstSeen, LastSeen string
}

func (s *Store) CreateTag(name, color string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO tag (name,color) VALUES (?,?)`, name, color)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListTags() ([]Tag, error) {
	rows, err := s.DB.Query(`SELECT id,name,color FROM tag ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) TagDevice(deviceID, tagID int64) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO device_tag (device_id,tag_id) VALUES (?,?)`,
		deviceID, tagID)
	return err
}

func (s *Store) UntagDevice(deviceID, tagID int64) error {
	_, err := s.DB.Exec(`DELETE FROM device_tag WHERE device_id=? AND tag_id=?`, deviceID, tagID)
	return err
}

func (s *Store) SetCustomField(deviceID int64, key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO custom_field (device_id,key,value) VALUES (?,?,?)
		ON CONFLICT(device_id,key) DO UPDATE SET value=excluded.value`, deviceID, key, value)
	return err
}

func (s *Store) DeleteCustomField(deviceID int64, key string) error {
	_, err := s.DB.Exec(`DELETE FROM custom_field WHERE device_id=? AND key=?`, deviceID, key)
	return err
}

func (s *Store) ListCustomFields(deviceID int64) ([]CustomField, error) {
	rows, err := s.DB.Query(`SELECT key,value FROM custom_field WHERE device_id=? ORDER BY key`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomField
	for rows.Next() {
		var c CustomField
		if err := rows.Scan(&c.Key, &c.Value); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddLink(deviceID int64, label, url string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO device_link (device_id,label,url) VALUES (?,?,?)`,
		deviceID, label, url)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteLink(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM device_link WHERE id=?`, id)
	return err
}

func (s *Store) ListLinks(deviceID int64) ([]Link, error) {
	rows, err := s.DB.Query(`SELECT id,label,url FROM device_link WHERE device_id=?`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.Label, &l.URL); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) UpsertOpenPort(ifaceID int64, port int, proto, guess, seenAt string) error {
	_, err := s.DB.Exec(`INSERT INTO open_port (iface_id,port,proto,service_guess,first_seen,last_seen)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(iface_id,port,proto) DO UPDATE SET
			last_seen=excluded.last_seen, service_guess=excluded.service_guess`,
		ifaceID, port, proto, guess, seenAt, seenAt)
	return err
}

func (s *Store) ListOpenPorts(ifaceID int64) ([]OpenPort, error) {
	rows, err := s.DB.Query(`SELECT port,proto,service_guess,first_seen,last_seen
		FROM open_port WHERE iface_id=? ORDER BY port`, ifaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenPort
	for rows.Next() {
		var p OpenPort
		if err := rows.Scan(&p.Port, &p.Proto, &p.ServiceGuess, &p.FirstSeen, &p.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
```

`internal/store/user.go`:

```go
package store

import "time"

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
}

func (s *Store) CreateUser(username, passwordHash, role string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO user (username,password_hash,role) VALUES (?,?,?)`,
		username, passwordHash, role)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByName(username string) (User, bool, error) {
	var u User
	err := s.DB.QueryRow(`SELECT id,username,password_hash,role FROM user WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT count(*) FROM user`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.DB.Query(`SELECT id,username,password_hash,role FROM user ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM user WHERE id=?`, id)
	return err
}

func (s *Store) CreateSession(token string, userID int64, expiresAt string) error {
	_, err := s.DB.Exec(`INSERT INTO session (token,user_id,expires_at) VALUES (?,?,?)`,
		token, userID, expiresAt)
	return err
}

func (s *Store) GetSession(token string) (User, bool, error) {
	var u User
	now := time.Now().UTC().Format(time.RFC3339)
	err := s.DB.QueryRow(`SELECT u.id,u.username,u.password_hash,u.role FROM session s
		JOIN user u ON u.id=s.user_id WHERE s.token=? AND s.expires_at>?`, token, now).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM session WHERE token=?`, token)
	return err
}

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM setting WHERE key=?`, key).Scan(&v)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO setting (key,value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: status, event, tag, field, link, port, user, setting repos"
```

---

### Task 5: Event service with SSE broker

**Files:**
- Create: `internal/events/broker.go`, `internal/events/service.go`
- Test: `internal/events/broker_test.go`, `internal/events/service_test.go`

**Interfaces:**
- Consumes: `store.Store.AddEvent` (Task 4).
- Produces:
  - `type Msg struct { Topic string; Data string }`
  - `events.NewBroker() *Broker`; `(b *Broker) Subscribe() (<-chan Msg, func())` (cancel func unsubscribes); `(b *Broker) Publish(topic, data string)` — non-blocking, drops to slow subscribers (buffered chan 16).
  - `events.NewService(st *store.Store, b *Broker) *Service`; `(s *Service) Emit(typ string, deviceID *int64, details string)` — writes event row, publishes `Msg{Topic: "events", Data: details}` and, for grid refresh, callers publish topic `grid:<subnetID>` themselves via `Broker.Publish`.

- [ ] **Step 1: Write failing tests**

`internal/events/broker_test.go`:

```go
package events

import (
	"testing"
	"time"
)

func TestPubSub(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	defer cancel()
	b.Publish("events", "hello")
	select {
	case m := <-ch:
		if m.Topic != "events" || m.Data != "hello" {
			t.Fatalf("got %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	cancel()
	b.Publish("events", "x") // must not panic on closed set
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received after cancel")
		}
	case <-time.After(100 * time.Millisecond):
	}
}
```

`internal/events/service_test.go`:

```go
package events

import (
	"testing"
	"time"

	"netis/internal/store"
)

func TestEmitWritesAndBroadcasts(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := NewBroker()
	svc := NewService(st, b)
	ch, cancel := b.Subscribe()
	defer cancel()

	svc.Emit("scan_error", nil, "subnet unreachable")

	evs, _ := st.ListEvents(5)
	if len(evs) != 1 || evs[0].Type != "scan_error" {
		t.Fatalf("db events: %+v", evs)
	}
	select {
	case m := <-ch:
		if m.Topic != "events" {
			t.Fatalf("topic=%q", m.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/events/ -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/events/broker.go`:

```go
package events

import "sync"

type Msg struct {
	Topic string
	Data  string
}

type Broker struct {
	mu   sync.Mutex
	subs map[chan Msg]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[chan Msg]struct{})}
}

func (b *Broker) Subscribe() (<-chan Msg, func()) {
	ch := make(chan Msg, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

func (b *Broker) Publish(topic, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- Msg{Topic: topic, Data: data}:
		default: // slow subscriber: drop
		}
	}
}
```

`internal/events/service.go`:

```go
package events

import (
	"log"

	"netis/internal/store"
)

type Service struct {
	store  *store.Store
	broker *Broker
}

func NewService(st *store.Store, b *Broker) *Service {
	return &Service{store: st, broker: b}
}

func (s *Service) Emit(typ string, deviceID *int64, details string) {
	if _, err := s.store.AddEvent(typ, deviceID, details); err != nil {
		log.Printf("event write failed: %v", err)
		return
	}
	s.broker.Publish("events", details)
}

func (s *Service) Broker() *Broker { return s.broker }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/events/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: event service with SSE broker"
```

---

### Task 6: Auth — sessions, login/logout, first-run setup, middleware, rate limit

**Files:**
- Create: `internal/web/auth.go`, `internal/web/views/auth.templ`
- Modify: `internal/web/server.go` (NewServer signature), `cmd/netis/main.go`
- Test: `internal/web/auth_test.go`

**Interfaces:**
- Consumes: store user/session/settings methods (Task 4).
- Produces:
  - `web.NewServer(st *store.Store) *Server` — **breaking change**; update `main.go` to `web.NewServer(mustOpenStore(cfg))`.
  - Routes: `GET /login`, `POST /login` (form `username`,`password`), `POST /logout`, `GET /setup`, `POST /setup` (form `username`,`password`; only while `CountUsers()==0`, creates admin).
  - Middleware `s.requireAuth(next http.Handler) http.Handler`: no valid `netis_session` cookie → redirect 303 to `/login` (or `/setup` when zero users). Skips `/healthz`, `/login`, `/setup`, `/static/`.
  - `s.requireAdmin(next http.HandlerFunc) http.HandlerFunc` — 403 for viewer role. Current user in request context: `userFrom(r) (store.User, bool)`.
  - Login rate limit: per-IP sliding window, max 5 failed attempts per minute → 429.
  - Cookie: `netis_session`, HttpOnly, SameSite=Lax, Path=/, 30-day expiry; token = 32 random bytes hex.
- templ views compiled from `auth.templ`: `views.LoginPage(errMsg string)`, `views.SetupPage(errMsg string)`.

- [ ] **Step 1: Write failing tests**

`internal/web/auth_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/store"
)

func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewServer(st), st
}

func addAdmin(t *testing.T, st *store.Store) {
	t.Helper()
	h, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if _, err := st.CreateUser("ben", string(h), "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestRedirectToSetupWhenNoUsers(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 303 || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSetupCreatesAdminOnce(t *testing.T) {
	srv, st := testServer(t)
	form := url.Values{"username": {"ben"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 303 {
		t.Fatalf("code=%d", rec.Code)
	}
	u, ok, _ := st.GetUserByName("ben")
	if !ok || u.Role != "admin" {
		t.Fatalf("user=%+v", u)
	}
	// second setup attempt rejected
	req2 := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != 403 {
		t.Fatalf("second setup code=%d", rec2.Code)
	}
}

func TestLoginSetsSessionCookie(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 303 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "netis_session" {
			sess = c
		}
	}
	if sess == nil || sess.Value == "" || !sess.HttpOnly {
		t.Fatalf("cookie=%+v", sess)
	}
	// authed request passes middleware
	req2 := httptest.NewRequest("GET", "/healthz", nil)
	req2.AddCookie(sess)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("authed healthz=%d", rec2.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"wrong"}}
	var last int
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "10.9.9.9:1234"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != 429 {
		t.Fatalf("6th attempt code=%d, want 429", last)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/web/ -count=1`
Expected: FAIL — `NewServer` takes no args yet, handlers missing.

- [ ] **Step 3: Implement**

`internal/web/views/auth.templ`:

```templ
package views

templ authShell(title string) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ title } — netis</title>
			<link rel="stylesheet" href="/static/app.css"/>
		</head>
		<body class="auth">
			{ children... }
		</body>
	</html>
}

templ LoginPage(errMsg string) {
	@authShell("Login") {
		<form method="post" action="/login" class="auth-card">
			<h1>netis</h1>
			if errMsg != "" {
				<p class="error">{ errMsg }</p>
			}
			<input name="username" placeholder="username" autofocus required/>
			<input name="password" type="password" placeholder="password" required/>
			<button type="submit">Sign in</button>
		</form>
	}
}

templ SetupPage(errMsg string) {
	@authShell("Setup") {
		<form method="post" action="/setup" class="auth-card">
			<h1>Create admin account</h1>
			if errMsg != "" {
				<p class="error">{ errMsg }</p>
			}
			<input name="username" placeholder="username" autofocus required/>
			<input name="password" type="password" placeholder="password" required minlength="8"/>
			<button type="submit">Create</button>
		</form>
	}
}
```

Run `templ generate` (creates `auth_templ.go`). Add deps:

```bash
go get github.com/a-h/templ golang.org/x/crypto
```

`internal/web/auth.go`:

```go
package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/store"
	"netis/internal/web/views"
)

type ctxKey int

const userKey ctxKey = 0

func userFrom(r *http.Request) (store.User, bool) {
	u, ok := r.Context().Value(userKey).(store.User)
	return u, ok
}

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{attempts: make(map[string][]time.Time)}
}

// allow reports whether ip may attempt a login (max 5 failures/minute).
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := rl.attempts[ip][:0]
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	rl.attempts[ip] = kept
	return len(kept) < 5
}

func (rl *rateLimiter) fail(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/login" || r.URL.Path == "/setup" ||
			strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie("netis_session"); err == nil {
			if u, ok, _ := s.store.GetSession(c.Value); ok {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
		if n, _ := s.store.CountUsers(); n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r)
		if !ok || u.Role != "admin" {
			http.Error(w, "admin only", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	views.LoginPage("").Render(r.Context(), w)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		http.Error(w, "too many attempts, wait a minute", http.StatusTooManyRequests)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	u, ok, err := s.store.GetUserByName(username)
	if err == nil && ok &&
		bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil {
		token := newToken()
		expires := time.Now().UTC().Add(30 * 24 * time.Hour)
		s.store.CreateSession(token, u.ID, expires.Format(time.RFC3339))
		http.SetCookie(w, &http.Cookie{
			Name: "netis_session", Value: token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.limiter.fail(ip)
	w.WriteHeader(http.StatusUnauthorized)
	views.LoginPage("wrong username or password").Render(r.Context(), w)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("netis_session"); err == nil {
		s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "netis_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	views.SetupPage("").Render(r.Context(), w)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	if username == "" || len(password) < 6 {
		w.WriteHeader(http.StatusBadRequest)
		views.SetupPage("username required, password min 6 chars").Render(r.Context(), w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateUser(username, string(hash), "admin"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
```

`internal/web/server.go` (full replacement):

```go
package web

import (
	"net/http"

	"netis/internal/store"
)

type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	limiter *rateLimiter
}

func NewServer(st *store.Store) *Server {
	s := &Server{mux: http.NewServeMux(), store: st, limiter: newRateLimiter()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("POST /setup", s.handleSetup)
	return s
}

func (s *Server) Handler() http.Handler { return s.requireAuth(s.mux) }
```

Update `cmd/netis/main.go`:

```go
package main

import (
	"log"
	"net/http"

	"netis/internal/config"
	"netis/internal/store"
	"netis/internal/web"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	srv := web.NewServer(st)
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
```

`/healthz` stays public (Docker healthchecks) — it is in the skip list above. Update Task 1's `TestHealthz` construction to the new signature:

```go
func TestHealthz(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 4: Run tests**

Run: `templ generate && go test ./... -count=1`
Expected: PASS (all packages).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: auth with sessions, first-run setup and login rate limit"
```

---

### Task 7: Scan primitives — sweeper interface, ARP table parse, OUI vendor lookup, name resolution

**Files:**
- Create: `internal/scan/sweep.go`, `internal/scan/arp.go`, `internal/scan/oui.go`, `internal/scan/oui_data.txt`, `internal/scan/resolve.go`
- Test: `internal/scan/arp_test.go`, `internal/scan/oui_test.go`, `internal/scan/sweep_test.go`

**Interfaces:**
- Consumes: nothing internal.
- Produces:
  - `type Result struct { IP string; Alive bool; RTTms float64 }`
  - `type Sweeper interface { Sweep(ctx context.Context, cidr string) ([]Result, error) }`
  - `scan.NewICMPSweeper(concurrency int) *ICMPSweeper` — pro-bing based, 1 probe per IP, 1s timeout, `SetPrivileged(false)` by default with env `NETIS_PRIVILEGED_ICMP=1` switching to raw sockets. Iterates host addresses of the CIDR (skip network/broadcast for IPv4 prefixes < /31).
  - `scan.HostIPs(cidr string) ([]string, error)` — pure helper, testable.
  - `scan.ParseARPTable(r io.Reader) map[string]string` — `/proc/net/arp` format, IP → normalized MAC, skips incomplete entries (`00:00:00:00:00:00` or flags `0x0`).
  - `scan.ReadARPTable() (map[string]string, error)` — opens `/proc/net/arp`, delegates to ParseARPTable.
  - `scan.Vendor(mac string) string` — first-3-octet OUI lookup against embedded `oui_data.txt` (format per line: `AABBCC<TAB>Vendor Name`), empty string when unknown.
  - `scan.ResolveName(ctx context.Context, ip string) string` — reverse DNS with 500ms timeout, strips trailing dot, empty on failure. (mDNS deferred; PTR covers most home setups via router DNS.)

- [ ] **Step 1: Write failing tests**

`internal/scan/arp_test.go`:

```go
package scan

import (
	"strings"
	"testing"
)

const arpFixture = `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         AA:BB:CC:11:22:33     *        eth0
192.168.1.50     0x1         0x0         00:00:00:00:00:00     *        eth0
192.168.1.7      0x1         0x2         de:ad:be:ef:00:07     *        eth0
`

func TestParseARPTable(t *testing.T) {
	m := ParseARPTable(strings.NewReader(arpFixture))
	if len(m) != 2 {
		t.Fatalf("len=%d m=%v", len(m), m)
	}
	if m["192.168.1.1"] != "aa:bb:cc:11:22:33" {
		t.Errorf("gw mac=%q", m["192.168.1.1"])
	}
	if m["192.168.1.7"] != "de:ad:be:ef:00:07" {
		t.Errorf("mac=%q", m["192.168.1.7"])
	}
	if _, ok := m["192.168.1.50"]; ok {
		t.Error("incomplete entry must be skipped")
	}
}
```

`internal/scan/oui_test.go`:

```go
package scan

import "testing"

func TestVendorLookup(t *testing.T) {
	// BC:24:11 is in oui_data.txt as Proxmox Server Solutions GmbH
	if v := Vendor("bc:24:11:aa:bb:cc"); v != "Proxmox Server Solutions GmbH" {
		t.Errorf("got %q", v)
	}
	if v := Vendor("ff:ff:ff:00:00:00"); v != "" {
		t.Errorf("unknown OUI should be empty, got %q", v)
	}
}
```

`internal/scan/sweep_test.go`:

```go
package scan

import "testing"

func TestHostIPs(t *testing.T) {
	ips, err := HostIPs("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.1", "192.168.1.2"} // network+broadcast skipped
	if len(ips) != 2 || ips[0] != want[0] || ips[1] != want[1] {
		t.Fatalf("got %v", ips)
	}
	if _, err := HostIPs("garbage"); err == nil {
		t.Fatal("want error for bad cidr")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/scan/ -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/scan/oui_data.txt` — seed with common home-lab OUIs (extend anytime; one per line `OUIHEX<TAB>Vendor`):

```text
BC2411	Proxmox Server Solutions GmbH
B827EB	Raspberry Pi Foundation
DCA632	Raspberry Pi Trading Ltd
E45F01	Raspberry Pi Trading Ltd
001132	Synology Incorporated
9009D0	Synology Incorporated
245EBE	QNAP Systems Inc.
04D9F5	ASUSTek COMPUTER INC.
FCECDA	Ubiquiti Inc
245A4C	Ubiquiti Inc
784558	Apple, Inc.
3C0754	Apple, Inc.
F0D1A9	Apple, Inc.
5CCF7F	Espressif Inc.
240AC4	Espressif Inc.
A4CF12	Espressif Inc.
EC086B	TP-LINK TECHNOLOGIES CO.,LTD.
50C7BF	TP-LINK TECHNOLOGIES CO.,LTD.
001A11	Google, Inc.
F4F5D8	Google, Inc.
FCFC48	Samsung Electronics Co.,Ltd
8CDE52	ieGeek/Shenzhen
00155D	Microsoft Corporation (Hyper-V)
525400	QEMU/KVM virtual NIC
```

`internal/scan/oui.go`:

```go
package scan

import (
	"bufio"
	"bytes"
	_ "embed"
	"strings"
	"sync"
)

//go:embed oui_data.txt
var ouiRaw []byte

var (
	ouiOnce sync.Once
	ouiMap  map[string]string
)

func Vendor(mac string) string {
	ouiOnce.Do(func() {
		ouiMap = make(map[string]string)
		sc := bufio.NewScanner(bytes.NewReader(ouiRaw))
		for sc.Scan() {
			parts := strings.SplitN(sc.Text(), "\t", 2)
			if len(parts) == 2 {
				ouiMap[strings.ToLower(parts[0])] = parts[1]
			}
		}
	})
	clean := strings.ToLower(strings.NewReplacer(":", "", "-", "").Replace(mac))
	if len(clean) < 6 {
		return ""
	}
	return ouiMap[clean[:6]]
}
```

`internal/scan/arp.go`:

```go
package scan

import (
	"bufio"
	"io"
	"os"
	"strings"
)

func ParseARPTable(r io.Reader) map[string]string {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first { // header line
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		ip, flags, mac := f[0], f[2], strings.ToLower(f[3])
		if flags == "0x0" || mac == "00:00:00:00:00:00" {
			continue
		}
		out[ip] = mac
	}
	return out
}

func ReadARPTable() (map[string]string, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseARPTable(f), nil
}
```

`internal/scan/sweep.go`:

```go
package scan

import (
	"context"
	"net/netip"
	"os"
	"sync"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

type Result struct {
	IP    string
	Alive bool
	RTTms float64
}

type Sweeper interface {
	Sweep(ctx context.Context, cidr string) ([]Result, error)
}

func HostIPs(cidr string) ([]string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	var out []string
	skipEdges := prefix.Addr().Is4() && prefix.Bits() < 31
	first := prefix.Addr()
	for addr := first; prefix.Contains(addr); addr = addr.Next() {
		out = append(out, addr.String())
	}
	if skipEdges && len(out) >= 2 {
		out = out[1 : len(out)-1] // drop network + broadcast
	}
	return out, nil
}

type ICMPSweeper struct {
	Concurrency int
	Timeout     time.Duration
}

func NewICMPSweeper(concurrency int) *ICMPSweeper {
	return &ICMPSweeper{Concurrency: concurrency, Timeout: time.Second}
}

func (s *ICMPSweeper) Sweep(ctx context.Context, cidr string) ([]Result, error) {
	ips, err := HostIPs(cidr)
	if err != nil {
		return nil, err
	}
	results := make([]Result, len(ips))
	sem := make(chan struct{}, s.Concurrency)
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = s.ping(ctx, ip)
		}(i, ip)
	}
	wg.Wait()
	return results, ctx.Err()
}

func (s *ICMPSweeper) ping(ctx context.Context, ip string) Result {
	p, err := probing.NewPinger(ip)
	if err != nil {
		return Result{IP: ip}
	}
	p.Count = 1
	p.Timeout = s.Timeout
	p.SetPrivileged(os.Getenv("NETIS_PRIVILEGED_ICMP") == "1")
	if err := p.RunWithContext(ctx); err != nil {
		return Result{IP: ip}
	}
	stats := p.Statistics()
	if stats.PacketsRecv == 0 {
		return Result{IP: ip}
	}
	return Result{IP: ip, Alive: true, RTTms: float64(stats.AvgRtt.Microseconds()) / 1000}
}
```

`internal/scan/resolve.go`:

```go
package scan

import (
	"context"
	"net"
	"strings"
	"time"
)

func ResolveName(ctx context.Context, ip string) string {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}
```

Run: `go get github.com/prometheus-community/pro-bing`

- [ ] **Step 4: Run tests**

Run: `go test ./internal/scan/ -count=1`
Expected: PASS (no live network touched — ICMPSweeper itself is exercised via engine tests with a fake).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: scan primitives - sweep, arp parse, oui vendor, rdns"
```

---

### Task 8: Scan engine — diff logic, scheduler, manual trigger

**Files:**
- Create: `internal/scan/engine.go`, `internal/scan/scheduler.go`
- Modify: `internal/store/device.go` (add `ListSubnetIfaceIPs`), `cmd/netis/main.go` (start scheduler)
- Test: `internal/scan/engine_test.go`

**Interfaces:**
- Consumes: store (Tasks 3–4), `events.Service`/`Broker` (Task 5), `scan.Sweeper`, `Vendor`, `ResolveName` (Task 7).
- Produces:
  - Store addition: `type SubnetIfaceIP struct { IfaceID, DeviceID int64; IP string; MAC *string }`; `(s *Store) ListSubnetIfaceIPs(subnetID int64) ([]SubnetIfaceIP, error)`.
  - `type Engine struct { Store *store.Store; Events *events.Service; Broker *events.Broker; Sweeper Sweeper; ARP func() (map[string]string, error); Resolve func(context.Context, string) string; OfflineAfter int }`
  - `(e *Engine) RunSubnet(ctx context.Context, sn store.Subnet) error` — full pipeline + DB diff (below).
  - `scan.NewScheduler(e *Engine, st *store.Store) *Scheduler`; `(s *Scheduler) Start(ctx context.Context)` (blocking loop, call in goroutine); `(s *Scheduler) Trigger(subnetID int64)` — immediate scan, used by the web "scan now" button.
- Diff rules (implement exactly):
  1. Alive IP with existing `ip_assignment` in subnet → `MarkSeen`; if `wasOffline` → emit `online` event with device name.
  2. Alive IP unknown, ARP has MAC and `FindIfaceByMAC` hits → device changed IP: `AssignIP` the new IP to that iface, emit `ip_changed`.
  3. Alive IP unknown, no MAC match → auto-create: device name = resolved hostname, else `unknown-<mac>`, else `unknown-<ip>`; kind `other`, source `scan`, vendor from OUI; iface with MAC (nil if not in ARP); `AssignIP` kind `dhcp`; `MarkSeen`; emit `device_new`.
  4. Every previously known iface-IP in the subnet not alive this sweep → `MarkMissed(OfflineAfter)`; if flipped → emit `offline`.
  5. Record availability (hourly bucket `at.Truncate(time.Hour)`) up/down for every known iface touched.
  6. After the sweep publish `Broker.Publish("grid:<subnetID>", "refresh")`.
  7. Sweep error → `Events.Emit("scan_error", nil, err.Error())`, return err.

- [ ] **Step 1: Write failing test**

`internal/scan/engine_test.go`:

```go
package scan

import (
	"context"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type fakeSweeper struct{ results []Result }

func (f *fakeSweeper) Sweep(ctx context.Context, cidr string) ([]Result, error) {
	return f.results, nil
}

func testEngine(t *testing.T) (*Engine, *store.Store, *fakeSweeper, int64) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := events.NewBroker()
	fs := &fakeSweeper{}
	e := &Engine{
		Store:  st,
		Events: events.NewService(st, b),
		Broker: b,
		Sweeper: fs,
		ARP: func() (map[string]string, error) {
			return map[string]string{"10.0.0.9": "bc:24:11:00:00:01"}, nil
		},
		Resolve:      func(ctx context.Context, ip string) string { return "" },
		OfflineAfter: 3,
	}
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
	return e, st, fs, snID
}

func TestAutoCreatesUnknownDevice(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.2}}
	sn, _ := st.GetSubnet(snID)
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListDevices()
	if len(rows) != 1 {
		t.Fatalf("devices=%+v", rows)
	}
	d := rows[0]
	if d.Name != "unknown-bc:24:11:00:00:01" || d.Source != "scan" ||
		d.Vendor != "Proxmox Server Solutions GmbH" || !d.Online {
		t.Fatalf("device=%+v", d)
	}
	evs, _ := st.ListEvents(5)
	if len(evs) != 1 || evs[0].Type != "device_new" {
		t.Fatalf("events=%+v", evs)
	}
}

func TestOfflineAfterThreeMisses(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(snID)
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}
	e.RunSubnet(context.Background(), sn) // creates + online
	fs.results = nil                      // device disappears
	for i := 0; i < 3; i++ {
		e.RunSubnet(context.Background(), sn)
	}
	rows, _ := st.ListDevices()
	if rows[0].Online {
		t.Fatal("should be offline after 3 misses")
	}
	evs, _ := st.ListEvents(10)
	var hasOffline bool
	for _, ev := range evs {
		if ev.Type == "offline" {
			hasOffline = true
		}
	}
	if !hasOffline {
		t.Fatalf("no offline event: %+v", evs)
	}
}

func TestIPChangeDetectedByMAC(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(snID)
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}
	e.RunSubnet(context.Background(), sn)
	// same MAC shows up on a new IP
	e.ARP = func() (map[string]string, error) {
		return map[string]string{"10.0.0.42": "bc:24:11:00:00:01"}, nil
	}
	fs.results = []Result{{IP: "10.0.0.42", Alive: true, RTTms: 1.0}}
	e.RunSubnet(context.Background(), sn)
	rows, _ := st.ListDevices()
	if len(rows) != 1 {
		t.Fatalf("must not duplicate device: %+v", rows)
	}
	found := false
	for _, ip := range rows[0].IPs {
		if ip == "10.0.0.42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new IP missing: %+v", rows[0].IPs)
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/scan/ -count=1`
Expected: FAIL — undefined `Engine`.

- [ ] **Step 3: Implement**

Add to `internal/store/device.go`:

```go
type SubnetIfaceIP struct {
	IfaceID  int64
	DeviceID int64
	IP       string
	MAC      *string
}

func (s *Store) ListSubnetIfaceIPs(subnetID int64) ([]SubnetIfaceIP, error) {
	rows, err := s.DB.Query(`SELECT f.id, f.device_id, a.ip, f.mac
		FROM ip_assignment a JOIN iface f ON f.id=a.iface_id
		WHERE a.subnet_id=?`, subnetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubnetIfaceIP
	for rows.Next() {
		var r SubnetIfaceIP
		if err := rows.Scan(&r.IfaceID, &r.DeviceID, &r.IP, &r.MAC); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

`internal/scan/engine.go`:

```go
package scan

import (
	"context"
	"fmt"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Engine struct {
	Store        *store.Store
	Events       *events.Service
	Broker       *events.Broker
	Sweeper      Sweeper
	ARP          func() (map[string]string, error)
	Resolve      func(context.Context, string) string
	OfflineAfter int
}

func (e *Engine) RunSubnet(ctx context.Context, sn store.Subnet) error {
	results, err := e.Sweeper.Sweep(ctx, sn.CIDR)
	if err != nil {
		e.Events.Emit("scan_error", nil, fmt.Sprintf("subnet %s: %v", sn.CIDR, err))
		return err
	}
	arp, err := e.ARP()
	if err != nil {
		arp = map[string]string{}
	}
	now := time.Now().UTC()
	bucket := now.Truncate(time.Hour).Format(time.RFC3339)

	known, err := e.Store.ListSubnetIfaceIPs(sn.ID)
	if err != nil {
		return err
	}
	knownByIP := make(map[string]store.SubnetIfaceIP, len(known))
	for _, k := range known {
		knownByIP[k.IP] = k
	}

	aliveIPs := make(map[string]bool)
	for _, r := range results {
		if !r.Alive {
			continue
		}
		aliveIPs[r.IP] = true
		if k, ok := knownByIP[r.IP]; ok {
			e.markSeen(k.IfaceID, k.DeviceID, r.RTTms, now, bucket)
			continue
		}
		mac := arp[r.IP]
		if mac != "" {
			if iface, ok, _ := e.Store.FindIfaceByMAC(mac); ok {
				// known device moved to a new IP
				e.Store.AssignIP(iface.ID, sn.ID, r.IP, "dhcp")
				e.Events.Emit("ip_changed", &iface.DeviceID,
					fmt.Sprintf("MAC %s now at %s", mac, r.IP))
				e.markSeen(iface.ID, iface.DeviceID, r.RTTms, now, bucket)
				continue
			}
		}
		e.createUnknown(ctx, sn, r, mac, now, bucket)
	}

	for _, k := range known {
		if aliveIPs[k.IP] {
			continue
		}
		went, _ := e.Store.MarkMissed(k.IfaceID, e.OfflineAfter)
		e.Store.RecordAvailability(k.IfaceID, false, bucket)
		if went {
			d, _ := e.Store.GetDevice(k.DeviceID)
			e.Events.Emit("offline", &k.DeviceID, fmt.Sprintf("%s (%s) went offline", d.Name, k.IP))
		}
	}

	e.Broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	return nil
}

func (e *Engine) markSeen(ifaceID, deviceID int64, rtt float64, now time.Time, bucket string) {
	wasOffline, _ := e.Store.MarkSeen(ifaceID, rtt, now)
	e.Store.RecordAvailability(ifaceID, true, bucket)
	if wasOffline {
		d, _ := e.Store.GetDevice(deviceID)
		e.Events.Emit("online", &deviceID, fmt.Sprintf("%s is online", d.Name))
	}
}

func (e *Engine) createUnknown(ctx context.Context, sn store.Subnet, r Result, mac string, now time.Time, bucket string) {
	resolved := e.Resolve(ctx, r.IP)
	name := resolved
	if name == "" && mac != "" {
		name = "unknown-" + mac
	}
	if name == "" {
		name = "unknown-" + r.IP
	}
	d := store.Device{Name: name, Kind: "other", Source: "scan", Vendor: Vendor(mac)}
	devID, err := e.Store.CreateDevice(d)
	if err != nil {
		return
	}
	var macP, hostP *string
	if mac != "" {
		macP = &mac
	}
	if resolved != "" {
		hostP = &resolved
	}
	ifID, err := e.Store.AddIface(devID, macP, hostP)
	if err != nil {
		return
	}
	e.Store.AssignIP(ifID, sn.ID, r.IP, "dhcp")
	e.Store.MarkSeen(ifID, r.RTTms, now)
	e.Store.RecordAvailability(ifID, true, bucket)
	e.Events.Emit("device_new", &devID, fmt.Sprintf("new device %s at %s", name, r.IP))
}
```

`internal/scan/scheduler.go`:

```go
package scan

import (
	"context"
	"log"
	"sync"
	"time"

	"netis/internal/store"
)

type Scheduler struct {
	engine  *Engine
	store   *store.Store
	mu      sync.Mutex
	lastRun map[int64]time.Time
	trigger chan int64
}

func NewScheduler(e *Engine, st *store.Store) *Scheduler {
	return &Scheduler{engine: e, store: st, lastRun: make(map[int64]time.Time), trigger: make(chan int64, 8)}
}

func (s *Scheduler) Trigger(subnetID int64) {
	select {
	case s.trigger <- subnetID:
	default:
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.trigger:
			if sn, err := s.store.GetSubnet(id); err == nil {
				s.run(ctx, sn)
			}
		case <-tick.C:
			subnets, err := s.store.ListSubnets()
			if err != nil {
				continue
			}
			for _, sn := range subnets {
				if !sn.ScanEnabled || sn.Kind == "wireguard" {
					continue
				}
				s.mu.Lock()
				due := time.Since(s.lastRun[sn.ID]) >= time.Duration(sn.ScanIntervalSec)*time.Second
				s.mu.Unlock()
				if due {
					s.run(ctx, sn)
				}
			}
		}
	}
}

func (s *Scheduler) run(ctx context.Context, sn store.Subnet) {
	s.mu.Lock()
	s.lastRun[sn.ID] = time.Now()
	s.mu.Unlock()
	if err := s.engine.RunSubnet(ctx, sn); err != nil {
		log.Printf("scan %s: %v", sn.CIDR, err)
	}
}
```

Wire into `cmd/netis/main.go` (full replacement):

```go
package main

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web"
)

func main() {
	cfg := config.Load()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	broker := events.NewBroker()
	evs := events.NewService(st, broker)

	offlineAfter := 3
	if v, _ := st.GetSetting("offline_after"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offlineAfter = n
		}
	}
	engine := &scan.Engine{
		Store: st, Events: evs, Broker: broker,
		Sweeper:      scan.NewICMPSweeper(64),
		ARP:          scan.ReadARPTable,
		Resolve:      scan.ResolveName,
		OfflineAfter: offlineAfter,
	}
	sched := scan.NewScheduler(engine, st)
	ctx := context.Background()
	go sched.Start(ctx)

	srv := web.NewServer(st)
	log.Printf("netis listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Handler()))
}
```

(`web.NewServer` gains broker/scheduler params in Task 11 — leave as `NewServer(st)` here.)

- [ ] **Step 4: Run tests**

Run: `go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS, build clean.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: scan engine with diff logic and scheduler"
```

---

### Task 9: Proxmox client and sync

**Files:**
- Create: `internal/proxmox/client.go`, `internal/proxmox/sync.go`
- Test: `internal/proxmox/client_test.go`, `internal/proxmox/sync_test.go`

**Interfaces:**
- Consumes: store (Tasks 3–4), `events.Service` (Task 5).
- Produces:
  - `type Guest struct { VMID int64; Name, Node, Type, Status string }` (Type `"qemu"` or `"lxc"`).
  - `proxmox.NewClient(baseURL, tokenID, secret string, insecure bool) *Client` — sets header `Authorization: PVEAPIToken=<tokenID>=<secret>`; insecure skips TLS verify.
  - `(c *Client) ListGuests(ctx) ([]Guest, error)` — `GET {base}/api2/json/cluster/resources?type=vm`.
  - `(c *Client) GuestMACs(ctx, node string, vmid int64, typ string) ([]string, error)` — `GET {base}/api2/json/nodes/{node}/{qemu|lxc}/{vmid}/config`, regex-extract MACs from any `netN` value, normalized lowercase.
  - `proxmox.NewSync(st *store.Store, c *Client, ev *events.Service) *Sync`; `(s *Sync) RunOnce(ctx) error`:
    - Upsert one device per node: name=node, kind `server`, source `proxmox` (match by name+source).
    - Upsert guests matched by `proxmox_vmid`: name, kind `vm`/`lxc`, parent=node device, source `proxmox`; store status via `SetCustomField(devID, "proxmox_status", status)`; create iface per MAC not yet present (`FindIfaceByMAC` dedupe).
    - New guest → `device_new` event. Never deletes devices.
  - `(s *Sync) Start(ctx, interval time.Duration)` — loop `RunOnce` every interval; on error emit `scan_error` **once per outage** (bool flag reset on success).
  - Settings keys used (read in main): `proxmox_url`, `proxmox_token_id`, `proxmox_secret`, `proxmox_insecure` (`"1"` = true). Empty URL → sync not started.

- [ ] **Step 1: Write failing tests**

`internal/proxmox/client_test.go`:

```go
package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const resourcesJSON = `{"data":[
 {"vmid":100,"name":"nas-vm","node":"pve1","status":"running","type":"qemu"},
 {"vmid":101,"name":"pihole","node":"pve1","status":"stopped","type":"lxc"}
]}`

const qemuConfigJSON = `{"data":{"net0":"virtio=BC:24:11:AA:00:01,bridge=vmbr0","cores":4}}`
const lxcConfigJSON = `{"data":{"net0":"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:AA:00:02,ip=dhcp"}}`

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=root@pam!netis=s3cret" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(resourcesJSON))
	})
	mux.HandleFunc("/api2/json/nodes/pve1/qemu/100/config", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(qemuConfigJSON))
	})
	mux.HandleFunc("/api2/json/nodes/pve1/lxc/101/config", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(lxcConfigJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestListGuests(t *testing.T) {
	srv := fixtureServer(t)
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	guests, err := c.ListGuests(context.Background())
	if err != nil || len(guests) != 2 {
		t.Fatalf("guests=%+v err=%v", guests, err)
	}
	if guests[0].VMID != 100 || guests[0].Type != "qemu" || guests[0].Status != "running" {
		t.Fatalf("guest0=%+v", guests[0])
	}
}

func TestGuestMACs(t *testing.T) {
	srv := fixtureServer(t)
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	macs, err := c.GuestMACs(context.Background(), "pve1", 100, "qemu")
	if err != nil || len(macs) != 1 || macs[0] != "bc:24:11:aa:00:01" {
		t.Fatalf("macs=%v err=%v", macs, err)
	}
	macs, err = c.GuestMACs(context.Background(), "pve1", 101, "lxc")
	if err != nil || len(macs) != 1 || macs[0] != "bc:24:11:aa:00:02" {
		t.Fatalf("lxc macs=%v err=%v", macs, err)
	}
}
```

`internal/proxmox/sync_test.go`:

```go
package proxmox

import (
	"context"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

func TestSyncUpsertsGuestsIdempotently(t *testing.T) {
	srv := fixtureServer(t)
	st, _ := store.Open(":memory:")
	defer st.Close()
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	sync := NewSync(st, c, events.NewService(st, events.NewBroker()))

	for i := 0; i < 2; i++ { // idempotent
		if err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.ListDevices()
	// pve1 node + 2 guests
	if len(rows) != 3 {
		t.Fatalf("want 3 devices, got %+v", rows)
	}
	var node, vm store.DeviceRow
	for _, r := range rows {
		switch r.Name {
		case "pve1":
			node = r
		case "nas-vm":
			vm = r
		}
	}
	if node.Kind != "server" || node.Source != "proxmox" {
		t.Fatalf("node=%+v", node)
	}
	if vm.Kind != "vm" || vm.ParentDeviceID == nil || *vm.ParentDeviceID != node.ID ||
		vm.ProxmoxVMID == nil || *vm.ProxmoxVMID != 100 {
		t.Fatalf("vm=%+v", vm)
	}
	if len(vm.MACs) != 1 || vm.MACs[0] != "bc:24:11:aa:00:01" {
		t.Fatalf("vm macs=%v", vm.MACs)
	}
	cfs, _ := st.ListCustomFields(vm.ID)
	if len(cfs) != 1 || cfs[0].Key != "proxmox_status" || cfs[0].Value != "running" {
		t.Fatalf("cfs=%+v", cfs)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/proxmox/ -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/proxmox/client.go`:

```go
package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Guest struct {
	VMID   int64
	Name   string
	Node   string
	Type   string
	Status string
}

type Client struct {
	base   string
	auth   string
	client *http.Client
}

func NewClient(baseURL, tokenID, secret string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:   strings.TrimSuffix(baseURL, "/"),
		auth:   fmt.Sprintf("PVEAPIToken=%s=%s", tokenID, secret),
		client: &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("proxmox %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) ListGuests(ctx context.Context) ([]Guest, error) {
	var body struct {
		Data []struct {
			VMID   int64  `json:"vmid"`
			Name   string `json:"name"`
			Node   string `json:"node"`
			Status string `json:"status"`
			Type   string `json:"type"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/api2/json/cluster/resources?type=vm", &body); err != nil {
		return nil, err
	}
	out := make([]Guest, 0, len(body.Data))
	for _, d := range body.Data {
		out = append(out, Guest{VMID: d.VMID, Name: d.Name, Node: d.Node,
			Type: d.Type, Status: d.Status})
	}
	return out, nil
}

var macRe = regexp.MustCompile(`(?i)\b([0-9a-f]{2}(?::[0-9a-f]{2}){5})\b`)

func (c *Client) GuestMACs(ctx context.Context, node string, vmid int64, typ string) ([]string, error) {
	var body struct {
		Data map[string]any `json:"data"`
	}
	path := fmt.Sprintf("/api2/json/nodes/%s/%s/%d/config", node, typ, vmid)
	if err := c.get(ctx, path, &body); err != nil {
		return nil, err
	}
	var macs []string
	for key, val := range body.Data {
		if !strings.HasPrefix(key, "net") {
			continue
		}
		sval, ok := val.(string)
		if !ok {
			continue
		}
		for _, m := range macRe.FindAllString(sval, -1) {
			macs = append(macs, strings.ToLower(m))
		}
	}
	return macs, nil
}
```

`internal/proxmox/sync.go`:

```go
package proxmox

import (
	"context"
	"fmt"
	"log"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Sync struct {
	store    *store.Store
	client   *Client
	events   *events.Service
	failing  bool
}

func NewSync(st *store.Store, c *Client, ev *events.Service) *Sync {
	return &Sync{store: st, client: c, events: ev}
}

func (s *Sync) RunOnce(ctx context.Context) error {
	guests, err := s.client.ListGuests(ctx)
	if err != nil {
		return err
	}
	nodeIDs := make(map[string]int64)
	for _, g := range guests {
		if _, ok := nodeIDs[g.Node]; ok {
			continue
		}
		id, err := s.upsertNode(g.Node)
		if err != nil {
			return err
		}
		nodeIDs[g.Node] = id
	}
	for _, g := range guests {
		if err := s.upsertGuest(ctx, g, nodeIDs[g.Node]); err != nil {
			log.Printf("proxmox guest %d: %v", g.VMID, err)
		}
	}
	return nil
}

func (s *Sync) upsertNode(node string) (int64, error) {
	var id int64
	err := s.store.DB.QueryRow(
		`SELECT id FROM device WHERE name=? AND source='proxmox' AND proxmox_vmid IS NULL`, node).
		Scan(&id)
	if err == nil {
		return id, nil
	}
	return s.store.CreateDevice(store.Device{Name: node, Kind: "server", Source: "proxmox"})
}

func (s *Sync) upsertGuest(ctx context.Context, g Guest, nodeID int64) error {
	kind := "vm"
	if g.Type == "lxc" {
		kind = "lxc"
	}
	var devID int64
	err := s.store.DB.QueryRow(
		`SELECT id FROM device WHERE proxmox_vmid=? AND source='proxmox'`, g.VMID).Scan(&devID)
	if err != nil { // new guest
		vmid := g.VMID
		devID, err = s.store.CreateDevice(store.Device{
			Name: g.Name, Kind: kind, Source: "proxmox",
			ParentDeviceID: &nodeID, ProxmoxVMID: &vmid,
		})
		if err != nil {
			return err
		}
		s.events.Emit("device_new", &devID, fmt.Sprintf("proxmox guest %s (%d)", g.Name, g.VMID))
	} else {
		d, err := s.store.GetDevice(devID)
		if err != nil {
			return err
		}
		d.Name, d.Kind, d.ParentDeviceID = g.Name, kind, &nodeID
		if err := s.store.UpdateDevice(d); err != nil {
			return err
		}
	}
	if err := s.store.SetCustomField(devID, "proxmox_status", g.Status); err != nil {
		return err
	}
	macs, err := s.client.GuestMACs(ctx, g.Node, g.VMID, g.Type)
	if err != nil {
		return err
	}
	for _, mac := range macs {
		if _, ok, _ := s.store.FindIfaceByMAC(mac); !ok {
			m := mac
			s.store.AddIface(devID, &m, nil)
		}
	}
	return nil
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			if !s.failing {
				s.failing = true
				s.events.Emit("scan_error", nil, "proxmox sync failing: "+err.Error())
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

Wire into `main.go` after scheduler start:

```go
	if pxURL, _ := st.GetSetting("proxmox_url"); pxURL != "" {
		tokenID, _ := st.GetSetting("proxmox_token_id")
		secret, _ := st.GetSetting("proxmox_secret")
		insecure, _ := st.GetSetting("proxmox_insecure")
		px := proxmox.NewSync(st, proxmox.NewClient(pxURL, tokenID, secret, insecure == "1"), evs)
		go px.Start(ctx, time.Minute)
	}
```

(add imports `netis/internal/proxmox`, `time`)

- [ ] **Step 4: Run tests**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: proxmox api client and device sync"
```

---

### Task 10: WireGuard SSH sync

**Files:**
- Create: `internal/wireguard/dump.go`, `internal/wireguard/ssh.go`, `internal/wireguard/sync.go`
- Test: `internal/wireguard/dump_test.go`, `internal/wireguard/sync_test.go`

**Interfaces:**
- Consumes: store, events.
- Produces:
  - `type Peer struct { PubKey, Endpoint string; AllowedIPs []string; LastHandshake time.Time }`
  - `wireguard.ParseDump(b []byte) ([]Peer, error)` — `wg show <iface> dump` output: first line is the interface (skip), peer lines are tab-separated `pubkey  psk  endpoint  allowed-ips  latest-handshake  rx  tx  keepalive`; handshake is unix seconds (`0` = never → zero time).
  - `type Runner interface { Run(ctx context.Context, cmd string) ([]byte, error) }`
  - `wireguard.NewSSHRunner(addr, user, keyPath string) (*SSHRunner, error)` — x/crypto/ssh, `InsecureIgnoreHostKey` (home-lab tradeoff, documented), dials per call, 10s timeout.
  - `wireguard.NewSync(st *store.Store, r Runner, ev *events.Service, iface string) *Sync`; `(s *Sync) RunOnce(ctx) error`:
    - Runs `wg show <iface> dump`, parses peers.
    - Upsert device by `wg_pubkey`: kind `wg-peer`, source `wireguard`, name = first allowed-IP (or pubkey first 8 chars); one iface per peer device (no MAC); AssignIP each allowed-IP that fits a subnet with kind `wireguard` (match via `netip.ParsePrefix(subnet.CIDR).Contains(ip)`); skip IPs with no matching subnet.
    - Handshake < 3 min → `MarkSeen(ifaceID, 0, now)` (+ `online` event if flipped); else `MarkMissed(ifaceID, 1)` (+ `offline` event if flipped).
    - New peer → `device_new` event.
  - `(s *Sync) Start(ctx, interval time.Duration)` — same once-per-outage error pattern as Proxmox sync.
  - Settings keys: `wg_ssh_addr` (host:port), `wg_ssh_user`, `wg_ssh_key_path`, `wg_iface` (default `wg0`). Empty addr → sync not started.

- [ ] **Step 1: Write failing tests**

`internal/wireguard/dump_test.go`:

```go
package wireguard

import (
	"testing"
	"time"
)

const dumpFixture = "privkeyhidden\tpubSERVER\t51820\toff\n" +
	"pubPEER1=\t(none)\t203.0.113.9:51820\t10.6.0.2/32\t1752220000\t1024\t2048\toff\n" +
	"pubPEER2=\t(none)\t(none)\t10.6.0.3/32,fd00::3/128\t0\t0\t0\toff\n"

func TestParseDump(t *testing.T) {
	peers, err := ParseDump([]byte(dumpFixture))
	if err != nil || len(peers) != 2 {
		t.Fatalf("peers=%+v err=%v", peers, err)
	}
	p1 := peers[0]
	if p1.PubKey != "pubPEER1=" || p1.Endpoint != "203.0.113.9:51820" {
		t.Fatalf("p1=%+v", p1)
	}
	if len(p1.AllowedIPs) != 1 || p1.AllowedIPs[0] != "10.6.0.2/32" {
		t.Fatalf("p1 ips=%v", p1.AllowedIPs)
	}
	if p1.LastHandshake != time.Unix(1752220000, 0) {
		t.Fatalf("p1 hs=%v", p1.LastHandshake)
	}
	if !peers[1].LastHandshake.IsZero() {
		t.Fatalf("p2 should have zero handshake: %v", peers[1].LastHandshake)
	}
	if len(peers[1].AllowedIPs) != 2 {
		t.Fatalf("p2 ips=%v", peers[1].AllowedIPs)
	}
}
```

`internal/wireguard/sync_test.go`:

```go
package wireguard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type fakeRunner struct{ out []byte }

func (f *fakeRunner) Run(ctx context.Context, cmd string) ([]byte, error) { return f.out, nil }

func TestSyncCreatesPeersAndStatus(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	st.CreateSubnet(store.Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})

	fresh := time.Now().Unix()
	dump := "priv\tpub\t51820\toff\n" +
		fmt.Sprintf("peerA=\t(none)\t1.2.3.4:51820\t10.6.0.2/32\t%d\t1\t1\toff\n", fresh) +
		"peerB=\t(none)\t(none)\t10.6.0.3/32\t0\t0\t0\toff\n"

	sync := NewSync(st, &fakeRunner{out: []byte(dump)}, events.NewService(st, events.NewBroker()), "wg0")
	for i := 0; i < 2; i++ { // idempotent
		if err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.ListDevices()
	if len(rows) != 2 {
		t.Fatalf("devices=%+v", rows)
	}
	byName := map[string]store.DeviceRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	a := byName["10.6.0.2"]
	if a.Kind != "wg-peer" || a.Source != "wireguard" || !a.Online {
		t.Fatalf("peerA=%+v", a)
	}
	if len(a.IPs) != 1 || a.IPs[0] != "10.6.0.2" {
		t.Fatalf("peerA ips=%v", a.IPs)
	}
	if byName["10.6.0.3"].Online {
		t.Fatal("peerB (no handshake) must be offline")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/wireguard/ -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/wireguard/dump.go`:

```go
package wireguard

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"time"
)

type Peer struct {
	PubKey        string
	Endpoint      string
	AllowedIPs    []string
	LastHandshake time.Time
}

func ParseDump(b []byte) ([]Peer, error) {
	var peers []Peer
	sc := bufio.NewScanner(bytes.NewReader(b))
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first { // interface line
			first = false
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		p := Peer{PubKey: f[0]}
		if f[2] != "(none)" {
			p.Endpoint = f[2]
		}
		if f[3] != "(none)" && f[3] != "" {
			p.AllowedIPs = strings.Split(f[3], ",")
		}
		if secs, err := strconv.ParseInt(f[4], 10, 64); err == nil && secs > 0 {
			p.LastHandshake = time.Unix(secs, 0)
		}
		peers = append(peers, p)
	}
	return peers, sc.Err()
}
```

`internal/wireguard/ssh.go`:

```go
package wireguard

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

type Runner interface {
	Run(ctx context.Context, cmd string) ([]byte, error)
}

type SSHRunner struct {
	addr   string
	config *ssh.ClientConfig
}

func NewSSHRunner(addr, user, keyPath string) (*SSHRunner, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}
	return &SSHRunner{
		addr: addr,
		config: &ssh.ClientConfig{
			User:            user,
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), // home-lab: pin later if needed
			Timeout:         10 * time.Second,
		},
	}, nil
}

func (r *SSHRunner) Run(ctx context.Context, cmd string) ([]byte, error) {
	client, err := ssh.Dial("tcp", r.addr, r.config)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return sess.Output(cmd)
}
```

`internal/wireguard/sync.go`:

```go
package wireguard

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

const onlineWindow = 3 * time.Minute

type Sync struct {
	store   *store.Store
	runner  Runner
	events  *events.Service
	iface   string
	failing bool
}

func NewSync(st *store.Store, r Runner, ev *events.Service, iface string) *Sync {
	return &Sync{store: st, runner: r, events: ev, iface: iface}
}

func (s *Sync) RunOnce(ctx context.Context) error {
	out, err := s.runner.Run(ctx, "wg show "+s.iface+" dump")
	if err != nil {
		return err
	}
	peers, err := ParseDump(out)
	if err != nil {
		return err
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, p := range peers {
		if err := s.upsertPeer(p, subnets, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sync) upsertPeer(p Peer, subnets []store.Subnet, now time.Time) error {
	var devID, ifaceID int64
	err := s.store.DB.QueryRow(`SELECT id FROM device WHERE wg_pubkey=?`, p.PubKey).Scan(&devID)
	if err != nil { // new peer
		name := p.PubKey
		if len(name) > 8 {
			name = name[:8]
		}
		if len(p.AllowedIPs) > 0 {
			name = strings.SplitN(p.AllowedIPs[0], "/", 2)[0]
		}
		pk := p.PubKey
		devID, err = s.store.CreateDevice(store.Device{
			Name: name, Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk,
		})
		if err != nil {
			return err
		}
		ifaceID, err = s.store.AddIface(devID, nil, nil)
		if err != nil {
			return err
		}
		s.assignAllowedIPs(ifaceID, p, subnets)
		s.events.Emit("device_new", &devID, fmt.Sprintf("wireguard peer %s", name))
	} else {
		ifaces, err := s.store.ListIfaces(devID)
		if err != nil || len(ifaces) == 0 {
			return fmt.Errorf("peer %s has no iface: %v", p.PubKey, err)
		}
		ifaceID = ifaces[0].ID
	}

	if !p.LastHandshake.IsZero() && now.Sub(p.LastHandshake) < onlineWindow {
		wasOffline, _ := s.store.MarkSeen(ifaceID, 0, now)
		if wasOffline {
			d, _ := s.store.GetDevice(devID)
			s.events.Emit("online", &devID, fmt.Sprintf("wg peer %s connected", d.Name))
		}
	} else {
		went, _ := s.store.MarkMissed(ifaceID, 1)
		if went {
			d, _ := s.store.GetDevice(devID)
			s.events.Emit("offline", &devID, fmt.Sprintf("wg peer %s disconnected", d.Name))
		}
	}
	return nil
}

func (s *Sync) assignAllowedIPs(ifaceID int64, p Peer, subnets []store.Subnet) {
	for _, cidr := range p.AllowedIPs {
		ipStr := strings.SplitN(cidr, "/", 2)[0]
		addr, err := netip.ParseAddr(ipStr)
		if err != nil {
			continue
		}
		for _, sn := range subnets {
			if sn.Kind != "wireguard" {
				continue
			}
			prefix, err := netip.ParsePrefix(sn.CIDR)
			if err != nil || !prefix.Contains(addr) {
				continue
			}
			s.store.AssignIP(ifaceID, sn.ID, addr.String(), "static")
		}
	}
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			if !s.failing {
				s.failing = true
				s.events.Emit("scan_error", nil, "wireguard sync failing: "+err.Error())
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

Wire into `main.go` after the Proxmox block:

```go
	if wgAddr, _ := st.GetSetting("wg_ssh_addr"); wgAddr != "" {
		wgUser, _ := st.GetSetting("wg_ssh_user")
		wgKey, _ := st.GetSetting("wg_ssh_key_path")
		wgIface, _ := st.GetSetting("wg_iface")
		if wgIface == "" {
			wgIface = "wg0"
		}
		if runner, err := wireguard.NewSSHRunner(wgAddr, wgUser, wgKey); err != nil {
			log.Printf("wireguard ssh setup: %v", err)
		} else {
			go wireguard.NewSync(st, runner, evs, wgIface).Start(ctx, time.Minute)
		}
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: wireguard peer sync over ssh"
```

---

### Task 11: Web shell — layout, static assets, SSE endpoint, dashboard

**Files:**
- Create: `internal/web/views/layout.templ`, `internal/web/views/dashboard.templ`, `internal/web/static/app.css`, `internal/web/static/htmx.min.js`, `internal/web/static/sse.js`, `internal/web/sse.go`, `internal/web/dashboard.go`
- Modify: `internal/web/server.go`, `internal/web/auth_test.go` (testServer helper), `cmd/netis/main.go`
- Test: `internal/web/dashboard_test.go`

**Interfaces:**
- Consumes: store, `events.Broker` (Task 5), grid occupancy (added here), `scan.HostIPs` (Task 7).
- Produces:
  - **Breaking change:** `web.NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger) *Server` with `type ScanTrigger interface { Trigger(subnetID int64) }` (nil allowed in tests). Update `main.go`: `web.NewServer(st, broker, sched)`.
  - Store addition (`internal/store/grid.go`): `type Occupant struct { DeviceID int64; DeviceName, MAC, LastSeen string; Online, EverSeen bool; Count int }`; `(s *Store) SubnetOccupancy(subnetID int64) (map[string]Occupant, error)` — one entry per assigned IP; `Count` = number of distinct ifaces claiming that IP (>1 ⇒ conflict); `EverSeen` = status row with non-null first_seen exists.
  - `GET /events/stream` — SSE endpoint: subscribes to broker, writes `event: <topic>\ndata: <data>\n\n` per message, flushes, exits on client disconnect.
  - `GET /` — dashboard: per subnet name/CIDR + online/used/free counts (from occupancy + `scan.HostIPs` length), 15 most recent events, links to grid pages.
  - templ: `views.Layout(title string, username string)` shell with nav (Dashboard, Devices, Events, Settings, logout form) loading `/static/htmx.min.js`, `/static/sse.js`, `/static/app.css`; `views.Dashboard(rows []DashRow, evs []store.Event)` with `type DashRow struct { Subnet store.Subnet; Online, Used, Free int }` (declare `DashRow` in package `views`, file `dashboard.templ`).
  - Static: `//go:embed static` served at `/static/`. Download HTMX 2.x once: `curl -Lo internal/web/static/htmx.min.js https://unpkg.com/htmx.org@2.0.6/dist/htmx.min.js` and `curl -Lo internal/web/static/sse.js https://unpkg.com/htmx-ext-sse@2.2.3/dist/sse.min.js` (committed, self-contained after that).

- [ ] **Step 1: Write failing test**

`internal/web/dashboard_test.go`:

```go
package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// authedGet performs a request with a valid admin session cookie.
func authedGet(t *testing.T, srv *Server, st *store.Store, path string) *httptest.ResponseRecorder {
	t.Helper()
	u, ok, _ := st.GetUserByName("ben")
	if !ok {
		addAdmin(t, st)
		u, _, _ = st.GetUserByName("ben")
	}
	st.CreateSession("testtok", u.ID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestDashboardShowsSubnetCounts(t *testing.T) {
	srv, st := testServer(t)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/30", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	st.MarkSeen(ifID, 1, time.Now())

	rec := authedGet(t, srv, st, "/")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"lab", "10.0.0.0/30", "gw"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}
```

(add `"net/http"` import; `testServer` updated below to the new constructor.)

- [ ] **Step 2: Run test, verify failure**

Run: `templ generate && go test ./internal/web/ -count=1`
Expected: FAIL — constructor mismatch / route missing.

- [ ] **Step 3: Implement**

`internal/store/grid.go`:

```go
package store

type Occupant struct {
	DeviceID   int64
	DeviceName string
	MAC        string
	LastSeen   string
	Online     bool
	EverSeen   bool
	Count      int
}

func (s *Store) SubnetOccupancy(subnetID int64) (map[string]Occupant, error) {
	rows, err := s.DB.Query(`SELECT a.ip, d.id, d.name,
			COALESCE(f.mac,''), COALESCE(st.last_seen,''),
			COALESCE(st.online,0), st.first_seen IS NOT NULL
		FROM ip_assignment a
		JOIN iface f ON f.id=a.iface_id
		JOIN device d ON d.id=f.device_id
		LEFT JOIN iface_status st ON st.iface_id=f.id
		WHERE a.subnet_id=?`, subnetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]Occupant)
	for rows.Next() {
		var o Occupant
		var ip string
		if err := rows.Scan(&ip, &o.DeviceID, &o.DeviceName, &o.MAC,
			&o.LastSeen, &o.Online, &o.EverSeen); err != nil {
			return nil, err
		}
		if prev, ok := out[ip]; ok {
			prev.Count++
			out[ip] = prev
			continue
		}
		o.Count = 1
		out[ip] = o
	}
	return out, rows.Err()
}
```

`internal/web/views/layout.templ`:

```templ
package views

templ Layout(title string, username string) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ title } — netis</title>
			<link rel="stylesheet" href="/static/app.css"/>
			<script src="/static/htmx.min.js"></script>
			<script src="/static/sse.js"></script>
		</head>
		<body hx-ext="sse" sse-connect="/events/stream">
			<nav>
				<a href="/" class="brand">netis</a>
				<a href="/">Dashboard</a>
				<a href="/devices">Devices</a>
				<a href="/events">Events</a>
				<a href="/settings">Settings</a>
				<form method="post" action="/logout" class="logout">
					<span>{ username }</span>
					<button type="submit">Logout</button>
				</form>
			</nav>
			<main>
				{ children... }
			</main>
		</body>
	</html>
}
```

`internal/web/views/dashboard.templ`:

```templ
package views

import (
	"fmt"

	"netis/internal/store"
)

type DashRow struct {
	Subnet store.Subnet
	Online int
	Used   int
	Free   int
}

templ Dashboard(username string, rows []DashRow, evs []store.Event) {
	@Layout("Dashboard", username) {
		<h1>Dashboard</h1>
		<div class="cards">
			for _, r := range rows {
				<a class="card" href={ templ.URL(fmt.Sprintf("/subnets/%d", r.Subnet.ID)) }>
					<h2>{ r.Subnet.Name }</h2>
					<p class="mono">{ r.Subnet.CIDR }</p>
					<p>
						<span class="ok">{ fmt.Sprint(r.Online) } online</span> ·
						{ fmt.Sprint(r.Used) } used · { fmt.Sprint(r.Free) } free
					</p>
				</a>
			}
		</div>
		<h2>Recent events</h2>
		<table>
			for _, e := range evs {
				<tr>
					<td class="mono">{ e.TS }</td>
					<td><span class={ "badge", e.Type }>{ e.Type }</span></td>
					<td>{ e.Details }</td>
				</tr>
			}
		</table>
	}
}
```

`internal/web/static/app.css` (starter — expand freely later):

```css
:root { --bg:#111418; --fg:#e6e6e6; --card:#1b2027; --ok:#3fb950; --bad:#f85149;
        --warn:#d29922; --muted:#8b949e; --free:#2d333b; }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--fg);
       font:15px/1.5 system-ui, sans-serif; }
nav { display:flex; gap:1rem; align-items:center; padding:.6rem 1rem;
      background:var(--card); }
nav a { color:var(--fg); text-decoration:none; }
nav .brand { font-weight:700; }
nav .logout { margin-left:auto; display:flex; gap:.5rem; align-items:center; }
main { padding:1rem; max-width:1100px; margin:0 auto; }
.mono { font-family:ui-monospace, monospace; }
.cards { display:flex; flex-wrap:wrap; gap:1rem; }
.card { background:var(--card); border-radius:8px; padding:1rem;
        color:var(--fg); text-decoration:none; min-width:220px; }
.ok { color:var(--ok); } .error { color:var(--bad); }
table { width:100%; border-collapse:collapse; }
td, th { padding:.35rem .5rem; border-bottom:1px solid var(--free); text-align:left; }
.badge { padding:0 .4rem; border-radius:4px; font-size:.85em; background:var(--free); }
.badge.online { color:var(--ok); } .badge.offline { color:var(--bad); }
.badge.device_new { color:var(--warn); } .badge.scan_error { color:var(--bad); }
.grid { display:grid; grid-template-columns:repeat(16, 28px); gap:3px; }
.sq { width:28px; height:28px; border-radius:4px; background:var(--free);
      display:block; position:relative; }
.sq.online { background:var(--ok); } .sq.offline { background:#57606a; }
.sq.reserved { background:var(--warn); } .sq.conflict { background:var(--bad); }
.auth { display:grid; place-items:center; min-height:100vh; }
.auth-card { background:var(--card); padding:2rem; border-radius:8px;
             display:flex; flex-direction:column; gap:.75rem; width:280px; }
input, select, button { padding:.5rem; border-radius:6px; border:1px solid var(--free);
                        background:var(--bg); color:var(--fg); }
button { cursor:pointer; background:var(--free); }
```

`internal/web/sse.go`:

```go
package web

import (
	"fmt"
	"net/http"
)

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.broker.Subscribe()
	defer cancel()
	for {
		select {
		case <-r.Context().Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Topic, m.Data)
			flusher.Flush()
		}
	}
}
```

`internal/web/dashboard.go`:

```go
package web

import (
	"net/http"

	"netis/internal/scan"
	"netis/internal/web/views"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	subnets, err := s.store.ListSubnets()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var rows []views.DashRow
	for _, sn := range subnets {
		occ, err := s.store.SubnetOccupancy(sn.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		hosts, _ := scan.HostIPs(sn.CIDR)
		row := views.DashRow{Subnet: sn, Used: len(occ), Free: len(hosts) - len(occ)}
		for _, o := range occ {
			if o.Online {
				row.Online++
			}
		}
		rows = append(rows, row)
	}
	evs, _ := s.store.ListEvents(15)
	views.Dashboard(u.Username, rows, evs).Render(r.Context(), w)
}
```

`internal/web/server.go` (full replacement):

```go
package web

import (
	"embed"
	"net/http"

	"netis/internal/events"
	"netis/internal/store"
)

//go:embed static
var staticFS embed.FS

type ScanTrigger interface {
	Trigger(subnetID int64)
}

type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	broker  *events.Broker
	trigger ScanTrigger
	limiter *rateLimiter
}

func NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger) *Server {
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, limiter: newRateLimiter(),
	}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	s.mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("POST /setup", s.handleSetup)
	s.mux.HandleFunc("GET /events/stream", s.handleSSE)
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	return s
}

func (s *Server) Handler() http.Handler { return s.requireAuth(s.mux) }
```

Update `testServer` in `auth_test.go`:

```go
func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewServer(st, events.NewBroker(), nil), st
}
```

Update `main.go`: `srv := web.NewServer(st, broker, sched)`.

- [ ] **Step 4: Run tests**

Run: `templ generate && go test ./... -count=1 && CGO_ENABLED=0 go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: web shell with layout, dashboard and SSE stream"
```

---

### Task 12: Subnet grid view with live updates and scan-now

**Files:**
- Create: `internal/web/views/grid.templ`, `internal/web/grid.go`
- Modify: `internal/web/server.go` (routes)
- Test: `internal/web/grid_test.go`

**Interfaces:**
- Consumes: `SubnetOccupancy` (Task 11), `scan.HostIPs` (Task 7), `ScanTrigger` (Task 11).
- Produces:
  - `type GridCell struct { IP string; State string; DeviceID int64; Title string }` (package `web`); state ∈ `online|offline|reserved|free|conflict`. Derivation: no occupant → `free`; `Count>1` → `conflict`; `!EverSeen` → `reserved`; `Online` → `online`; else `offline`.
  - Routes: `GET /subnets/{
  - templ: `views.GridPage(username string, sn store.Subnet, cells []GridCell)` and `views.GridFrag(sn store.Subnet, cells []GridCell)`; page wraps fragment in `<div id="grid" hx-get="/subnets/<id>/grid" hx-trigger="sse:grid:<id>">`, includes a "Scan now" button `hx-post=".../scan"`. Move `GridCell` to package `views` so templates can use it: declare it in `grid.templ`'s Go section; `internal/web/grid.go` references `views.GridCell`.

- [ ] **Step 1: Write failing test**

`internal/web/grid_test.go`:

```go
package web

import (
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestGridStates(t *testing.T) {
	srv, st := testServer(t)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})

	mk := func(name, ip string) int64 {
		devID, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		ifID, _ := st.AddIface(devID, nil, nil)
		st.AssignIP(ifID, snID, ip, "static")
		return ifID
	}
	onlineIf := mk("gw", "10.0.0.1")
	st.MarkSeen(onlineIf, 1, time.Now())
	offlineIf := mk("nas", "10.0.0.2")
	st.MarkSeen(offlineIf, 1, time.Now())
	st.MarkMissed(offlineIf, 1)
	mk("printer", "10.0.0.3") // reserved: assigned, never seen
	// conflict: second iface claims .1
	dup, _ := st.CreateDevice(store.Device{Name: "ghost", Kind: "other", Source: "manual"})
	dupIf, _ := st.AddIface(dup, nil, nil)
	st.AssignIP(dupIf, snID, "10.0.0.1", "static")

	rec := authedGet(t, srv, st, "/subnets/1/grid")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"sq conflict", "sq offline", "sq reserved", "sq free"} {
		if !strings.Contains(body, want) {
			t.Errorf("grid missing %q", want)
		}
	}
	// /29 → 6 host squares
	if n := strings.Count(body, `class="sq`); n != 6 {
		t.Errorf("squares=%d, want 6", n)
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `templ generate && go test ./internal/web/ -count=1`
Expected: FAIL — 404 (route missing).

- [ ] **Step 3: Implement**

`internal/web/views/grid.templ`:

```templ
package views

import (
	"fmt"

	"netis/internal/store"
)

type GridCell struct {
	IP       string
	State    string
	DeviceID int64
	Title    string
}

templ GridPage(username string, sn store.Subnet, cells []GridCell) {
	@Layout(sn.Name, username) {
		<h1>{ sn.Name } <span class="mono">{ sn.CIDR }</span></h1>
		<button hx-post={ fmt.Sprintf("/subnets/%d/scan", sn.ID) } hx-swap="none">Scan now</button>
		<div
			id="grid"
			hx-get={ fmt.Sprintf("/subnets/%d/grid", sn.ID) }
			hx-trigger={ fmt.Sprintf("sse:grid:%d", sn.ID) }
		>
			@GridFrag(sn, cells)
		</div>
		<p class="muted">
			<span class="sq online"></span> online
			<span class="sq offline"></span> offline
			<span class="sq reserved"></span> reserved
			<span class="sq"></span> free
			<span class="sq conflict"></span> conflict
		</p>
	}
}

templ GridFrag(sn store.Subnet, cells []GridCell) {
	<div class="grid">
		for _, c := range cells {
			if c.DeviceID > 0 {
				<a
					class={ "sq", c.State }
					href={ templ.URL(fmt.Sprintf("/devices/%d", c.DeviceID)) }
					title={ c.Title }
				></a>
			} else {
				<span class={ "sq", c.State } title={ c.Title }></span>
			}
		}
	</div>
}
```

`internal/web/grid.go`:

```go
package web

import (
	"fmt"
	"net/http"
	"strconv"

	"netis/internal/scan"
	"netis/internal/store"
	"netis/internal/web/views"
)

func (s *Server) gridCells(sn store.Subnet) ([]views.GridCell, error) {
	occ, err := s.store.SubnetOccupancy(sn.ID)
	if err != nil {
		return nil, err
	}
	ips, err := scan.HostIPs(sn.CIDR)
	if err != nil {
		return nil, err
	}
	cells := make([]views.GridCell, 0, len(ips))
	for _, ip := range ips {
		c := views.GridCell{IP: ip, State: "free", Title: ip}
		if o, ok := occ[ip]; ok {
			c.DeviceID = o.DeviceID
			c.Title = fmt.Sprintf("%s — %s %s last seen %s", ip, o.DeviceName, o.MAC, o.LastSeen)
			switch {
			case o.Count > 1:
				c.State = "conflict"
			case !o.EverSeen:
				c.State = "reserved"
			case o.Online:
				c.State = "online"
			default:
				c.State = "offline"
			}
		}
		cells = append(cells, c)
	}
	return cells, nil
}

func (s *Server) subnetFromPath(r *http.Request) (store.Subnet, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return store.Subnet{}, err
	}
	return s.store.GetSubnet(id)
}

func (s *Server) handleSubnetPage(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cells, err := s.gridCells(sn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	u, _ := userFrom(r)
	views.GridPage(u.Username, sn, cells).Render(r.Context(), w)
}

func (s *Server) handleGridFrag(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cells, err := s.gridCells(sn)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views.GridFrag(sn, cells).Render(r.Context(), w)
}

func (s *Server) handleScanNow(w http.ResponseWriter, r *http.Request) {
	sn, err := s.subnetFromPath(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if s.trigger != nil {
		s.trigger.Trigger(sn.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Routes in `NewServer`:

```go
	s.mux.HandleFunc("GET /subnets/{id}", s.handleSubnetPage)
	s.mux.HandleFunc("GET /subnets/{id}/grid", s.handleGridFrag)
	s.mux.HandleFunc("POST /subnets/{id}/scan", s.requireAdmin(s.handleScanNow))
```

- [ ] **Step 4: Run tests**

Run: `templ generate && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: subnet grid view with live SSE refresh and scan-now"
```

---

### Task 13: Device list, device page, CRUD

**Files:**
- Create: `internal/web/views/devices.templ`, `internal/web/devices.go`
- Modify: `internal/web/server.go` (routes)
- Test: `internal/web/devices_test.go`

**Interfaces:**
- Consumes: `ListDevices`, `GetDevice`, `CreateDevice`, `UpdateDevice`, `DeleteDevice`, iface/IP/tag/field/link/port/availability store methods.
- Produces:
  - Routes (list/read for all roles, mutations admin-only):
    - `GET /devices` — table (name, kind, IPs, MACs, vendor, tags, online, last seen) + client-side filter box (HTMX `GET /devices?q=` re-render, match on name/IP/MAC/tag substring).
    - `GET /devices/new`, `POST /devices` — create form: name, kind select, notes; optional first iface MAC + IP + subnet select.
    - `GET /devices/{id}` — detail page: fields, badges (`proxmox_status` custom field as status badge for vm/lxc), interfaces with IPs and open ports, links, tags (add/remove), custom fields (add/remove), availability % last 30 days per iface (`AvailabilityPct` with `sinceBucket = now-30d`), parent/children list, event history (`SELECT` events by device), edit form.
    - `POST /devices/{id}` — update name/kind/notes/icon/parent.
    - `POST /devices/{id}/delete` — delete, redirect to `/devices`.
    - `POST /devices/{id}/links` (label,url) / `POST /links/{id}/delete`
    - `POST /devices/{id}/tags` (name — creates tag if missing, color `#888888`) / `POST /devices/{id}/tags/{tagID}/delete`
    - `POST /devices/{id}/fields` (key,value) / `POST /devices/{id}/fields/delete` (key)
  - Store addition: `(s *Store) ListDeviceEvents(deviceID int64, limit int) ([]Event, error)` (same shape as ListEvents, filtered) and `(s *Store) ListChildren(deviceID int64) ([]Device, error)`.
  - templ: `views.DeviceList(username string, rows []store.DeviceRow, q string)`, `views.DeviceForm(username string, subnets []store.Subnet)`, `views.DevicePage(username string, d DeviceDetail)` where `type DeviceDetail struct { Device store.Device; Ifaces []IfaceDetail; Tags []store.Tag; AllTags []store.Tag; Fields []store.CustomField; Links []store.Link; Children []store.Device; Parent *store.Device; Events []store.Event }` and `type IfaceDetail struct { Iface store.Iface; IPs []store.IPRow; Ports []store.OpenPort; AvailabilityPct float64; Online bool; LastSeen string }` (both declared in `devices.templ`).

- [ ] **Step 1: Write failing tests**

`internal/web/devices_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

func authedPost(t *testing.T, srv *Server, st *store.Store, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec0 := authedGet(t, srv, st, "/") // ensures admin+session exist
	_ = rec0
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCreateAndShowDevice(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"office-switch"}, "kind": {"switch"}, "notes": {"rack top"},
	})
	if rec.Code != 303 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	rows, _ := st.ListDevices()
	if len(rows) != 1 || rows[0].Kind != "switch" {
		t.Fatalf("rows=%+v", rows)
	}
	page := authedGet(t, srv, st, rec.Header().Get("Location"))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "office-switch") {
		t.Fatalf("detail code=%d", page.Code)
	}
}

func TestDeviceListFilter(t *testing.T) {
	srv, st := testServer(t)
	st.CreateDevice(store.Device{Name: "alpha", Kind: "computer", Source: "manual"})
	st.CreateDevice(store.Device{Name: "beta", Kind: "phone", Source: "manual"})
	rec := authedGet(t, srv, st, "/devices?q=alp")
	body := rec.Body.String()
	if !strings.Contains(body, "alpha") || strings.Contains(body, "beta") {
		t.Fatalf("filter failed: %s", body)
	}
}

func TestDeleteDeviceRequiresAdmin(t *testing.T) {
	srv, st := testServer(t)
	devID, _ := st.CreateDevice(store.Device{Name: "x", Kind: "other", Source: "manual"})
	// viewer session
	uID, _ := st.CreateUser("eve", "hash", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/devices/1/delete", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("viewer delete code=%d", rec.Code)
	}
	if _, err := st.GetDevice(devID); err != nil {
		t.Fatal("device must still exist")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `templ generate && go test ./internal/web/ -count=1`
Expected: FAIL — 404s.

- [ ] **Step 3: Implement**

Store additions (`internal/store/device.go`):

```go
func (s *Store) ListDeviceEvents(deviceID int64, limit int) ([]Event, error) {
	rows, err := s.DB.Query(`SELECT id,ts,type,device_id,details FROM event
		WHERE device_id=? ORDER BY id DESC LIMIT ?`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &e.DeviceID, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ListChildren(deviceID int64) ([]Device, error) {
	rows, err := s.DB.Query(`SELECT `+deviceCols+` FROM device WHERE parent_device_id=? ORDER BY name`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
```

Handlers `internal/web/devices.go` — follow the pattern of Task 12 exactly (path value parsing, `requireAdmin` on mutations, 303 redirect back to `GET /devices/{id}` after each POST). Core create/update/delete:

```go
package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"netis/internal/store"
	"netis/internal/web/views"
)

var validKinds = map[string]bool{"computer": true, "switch": true, "phone": true,
	"server": true, "printer": true, "iot": true, "vm": true, "lxc": true,
	"wg-peer": true, "other": true}

func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	kind := r.FormValue("kind")
	if !validKinds[kind] {
		http.Error(w, "bad kind", 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", 400)
		return
	}
	devID, err := s.store.CreateDevice(store.Device{
		Name: name, Kind: kind, Notes: r.FormValue("notes"), Source: "manual",
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if mac := normMAC(r.FormValue("mac")); mac != "" || r.FormValue("ip") != "" {
		var macP *string
		if mac != "" {
			macP = &mac
		}
		ifID, err := s.store.AddIface(devID, macP, nil)
		if err == nil && r.FormValue("ip") != "" {
			if snID, err := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64); err == nil {
				s.store.AssignIP(ifID, snID, r.FormValue("ip"), "static")
			}
		}
	}
	http.Redirect(w, r, "/devices/"+strconv.FormatInt(devID, 10), http.StatusSeeOther)
}

func normMAC(in string) string {
	m := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(in, "-", ":")))
	if len(m) != 17 {
		return ""
	}
	return m
}

func (s *Server) handleDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	d, err := s.store.GetDevice(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if kind := r.FormValue("kind"); validKinds[kind] {
		d.Kind = kind
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		d.Name = name
	}
	d.Notes = r.FormValue("notes")
	d.Icon = r.FormValue("icon")
	if p := r.FormValue("parent_device_id"); p != "" {
		if pid, err := strconv.ParseInt(p, 10, 64); err == nil && pid != d.ID {
			d.ParentDeviceID = &pid
		}
	} else {
		d.ParentDeviceID = nil
	}
	if err := s.store.UpdateDevice(d); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteDevice(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/devices", http.StatusSeeOther)
}
```

List with filter:

```go
func (s *Server) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListDevices()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q != "" {
		filtered := rows[:0]
		for _, row := range rows {
			hay := strings.ToLower(row.Name + " " + strings.Join(row.IPs, " ") + " " +
				strings.Join(row.MACs, " ") + " " + strings.Join(row.TagNames, " "))
			if strings.Contains(hay, q) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	u, _ := userFrom(r)
	views.DeviceList(u.Username, rows, r.URL.Query().Get("q")).Render(r.Context(), w)
}
```

Detail assembly (`handleDevicePage`): load device, ifaces (each with `ListIPs`, `ListOpenPorts`, `ifaceOnline`, `AvailabilityPct(ifID, time.Now().UTC().Add(-30*24*time.Hour).Truncate(time.Hour).Format(time.RFC3339))`), tags via `deviceTagNames` + `ListTags` (expose `(s *Store) DeviceTags(deviceID int64) ([]Tag, error)` — add analogous to `deviceTagNames` but returning full `Tag` rows), `ListCustomFields`, `ListLinks`, `ListChildren`, parent via `GetDevice(*d.ParentDeviceID)`, `ListDeviceEvents(id, 20)`. Render `views.DevicePage`.

Link/tag/field mutation handlers are 5-line wrappers around the store calls from Task 4, each ending `http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)`.

`devices.templ` — `DeviceList` renders the filter input:

```templ
<input
	type="search" name="q" value={ q } placeholder="filter…"
	hx-get="/devices" hx-trigger="input changed delay:300ms"
	hx-target="body" hx-push-url="true"
/>
```

then a table of rows (name links to detail, online dot `<span class="ok">●</span>` when `row.Online`, tags as badges). `DeviceForm` is a plain form POSTing `/devices` with a kind `<select>` over the ten valid kinds and optional MAC/IP/subnet fields. `DevicePage` renders the sections listed above; keep it a long but plain template — no logic beyond `if`/`for`.

Routes in `NewServer`:

```go
	s.mux.HandleFunc("GET /devices", s.handleDeviceList)
	s.mux.HandleFunc("GET /devices/new", s.handleDeviceForm)
	s.mux.HandleFunc("POST /devices", s.requireAdmin(s.handleDeviceCreate))
	s.mux.HandleFunc("GET /devices/{id}", s.handleDevicePage)
	s.mux.HandleFunc("POST /devices/{id}", s.requireAdmin(s.handleDeviceUpdate))
	s.mux.HandleFunc("POST /devices/{id}/delete", s.requireAdmin(s.handleDeviceDelete))
	s.mux.HandleFunc("POST /devices/{id}/links", s.requireAdmin(s.handleLinkAdd))
	s.mux.HandleFunc("POST /links/{id}/delete", s.requireAdmin(s.handleLinkDelete))
	s.mux.HandleFunc("POST /devices/{id}/tags", s.requireAdmin(s.handleTagAdd))
	s.mux.HandleFunc("POST /devices/{id}/tags/{tagID}/delete", s.requireAdmin(s.handleTagRemove))
	s.mux.HandleFunc("POST /devices/{id}/fields", s.requireAdmin(s.handleFieldSet))
	s.mux.HandleFunc("POST /devices/{id}/fields/delete", s.requireAdmin(s.handleFieldDelete))
```

- [ ] **Step 4: Run tests**

Run: `templ generate && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: device list, detail page and full CRUD"
```

---

### Task 14: Events page and settings (subnets, integrations, users, general)

**Files:**
- Create: `internal/web/views/eventspage.templ`, `internal/web/views/settings.templ`, `internal/web/eventspage.go`, `internal/web/settings.go`
- Modify: `internal/web/server.go` (routes)
- Test: `internal/web/settings_test.go`

**Interfaces:**
- Consumes: store settings/user/subnet methods, `ListEvents`.
- Produces:
  - `GET /events` — last 200 events, filter by type via `?type=` (exact match on the five event types).
  - `GET /settings` — page with four sections (subnets, integrations, users, general); viewing requires login, every POST is admin-only.
  - Subnets: `POST /settings/subnets` (cidr, name, kind select, scan_interval_sec, scan_enabled checkbox; validate CIDR with `netip.ParsePrefix`), `POST /settings/subnets/{id}/delete`, `POST /settings/subnets/{id}` (update same fields).
  - Integrations: `POST /settings/integrations` — writes settings keys `proxmox_url`, `proxmox_token_id`, `proxmox_secret`, `proxmox_insecure`, `wg_ssh_addr`, `wg_ssh_user`, `wg_ssh_key_path`, `wg_iface`. Page note: "restart netis to apply integration changes" (pollers read settings at startup — YAGNI on hot reload).
  - Users: `POST /settings/users` (username, password ≥6, role select) — bcrypt like setup; `POST /settings/users/{id}/delete` — refuses deleting the last admin (count admins first, 400 if 1 and target is admin).
  - General: `POST /settings/general` — `offline_after` (int 1–10, stored via `SetSetting`).
  - templ: `views.EventsPage(username string, evs []store.Event, typeFilter string)`, `views.SettingsPage(username string, d SettingsData)` with `type SettingsData struct { Subnets []store.Subnet; Users []store.User; Values map[string]string }` (`Values` = current settings for form defaults; never echo `proxmox_secret` back — render empty password input).

- [ ] **Step 1: Write failing test**

`internal/web/settings_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCreateSubnetViaSettings(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/settings/subnets", url.Values{
		"cidr": {"192.168.1.0/24"}, "name": {"main"}, "kind": {"lan"},
		"scan_interval_sec": {"120"}, "scan_enabled": {"on"},
	})
	if rec.Code != 303 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	subnets, _ := st.ListSubnets()
	if len(subnets) != 1 || subnets[0].Name != "main" || !subnets[0].ScanEnabled {
		t.Fatalf("subnets=%+v", subnets)
	}
	// invalid CIDR rejected
	rec = authedPost(t, srv, st, "/settings/subnets", url.Values{
		"cidr": {"not-a-cidr"}, "name": {"x"}, "kind": {"lan"}, "scan_interval_sec": {"120"},
	})
	if rec.Code != 400 {
		t.Fatalf("bad cidr code=%d", rec.Code)
	}
}

func TestViewerCannotPostSettings(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/settings/general",
		nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestCannotDeleteLastAdmin(t *testing.T) {
	srv, st := testServer(t)
	rec := authedGet(t, srv, st, "/") // creates admin "ben" id=1
	_ = rec
	del := authedPost(t, srv, st, "/settings/users/1/delete", url.Values{})
	if del.Code != 400 {
		t.Fatalf("code=%d", del.Code)
	}
	if n, _ := st.CountUsers(); n != 1 {
		t.Fatal("admin must survive")
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `templ generate && go test ./internal/web/ -count=1`
Expected: FAIL — 404s.

- [ ] **Step 3: Implement**

`internal/web/settings.go` — handlers exactly per the Produces list. Subnet create core:

```go
func (s *Server) handleSubnetCreate(w http.ResponseWriter, r *http.Request) {
	cidr := strings.TrimSpace(r.FormValue("cidr"))
	if _, err := netip.ParsePrefix(cidr); err != nil {
		http.Error(w, "invalid CIDR", 400)
		return
	}
	kind := r.FormValue("kind")
	if kind != "lan" && kind != "wireguard" && kind != "proxmox-bridge" {
		http.Error(w, "bad kind", 400)
		return
	}
	interval, err := strconv.Atoi(r.FormValue("scan_interval_sec"))
	if err != nil || interval < 30 {
		interval = 120
	}
	_, err = s.store.CreateSubnet(store.Subnet{
		CIDR: cidr, Name: r.FormValue("name"), Kind: kind,
		ScanEnabled: r.FormValue("scan_enabled") == "on", ScanIntervalSec: interval,
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
```

Last-admin guard:

```go
func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	users, err := s.store.ListUsers()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	admins := 0
	var target *store.User
	for i, u := range users {
		if u.Role == "admin" {
			admins++
		}
		if u.ID == id {
			target = &users[i]
		}
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	if target.Role == "admin" && admins <= 1 {
		http.Error(w, "cannot delete the last admin", 400)
		return
	}
	s.store.DeleteUser(id)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
```

Integrations handler writes each posted key via `SetSetting`; skip empty `proxmox_secret` (keep old value) so the blank password field doesn't wipe it. Events page handler filters `?type=` before render. Settings templ: four plain form sections; user creation form mirrors setup validation; general section single number input `offline_after` (default from `Values["offline_after"]` or "3").

Routes:

```go
	s.mux.HandleFunc("GET /events", s.handleEventsPage)
	s.mux.HandleFunc("GET /settings", s.handleSettingsPage)
	s.mux.HandleFunc("POST /settings/subnets", s.requireAdmin(s.handleSubnetCreate))
	s.mux.HandleFunc("POST /settings/subnets/{id}", s.requireAdmin(s.handleSubnetUpdate))
	s.mux.HandleFunc("POST /settings/subnets/{id}/delete", s.requireAdmin(s.handleSubnetDelete))
	s.mux.HandleFunc("POST /settings/integrations", s.requireAdmin(s.handleIntegrationsSave))
	s.mux.HandleFunc("POST /settings/users", s.requireAdmin(s.handleUserCreate))
	s.mux.HandleFunc("POST /settings/users/{id}/delete", s.requireAdmin(s.handleUserDelete))
	s.mux.HandleFunc("POST /settings/general", s.requireAdmin(s.handleGeneralSave))
```

- [ ] **Step 4: Run tests**

Run: `templ generate && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: events page and settings for subnets, integrations, users"
```

---

### Task 15: Wake-on-LAN, port scan, deploy artifacts

**Files:**
- Create: `internal/wol/wol.go`, `internal/scan/ports.go`, `Dockerfile`, `deploy/netis.service`, `README.md`
- Modify: `internal/web/devices.go` + `server.go` (two routes), `internal/web/views/devices.templ` (WOL + port-scan buttons)
- Test: `internal/wol/wol_test.go`, `internal/scan/ports_test.go`

**Interfaces:**
- Consumes: device page (Task 13), `UpsertOpenPort` (Task 4).
- Produces:
  - `wol.BuildMagicPacket(mac string) ([]byte, error)` — 6×`0xFF` + 16×MAC (102 bytes); `wol.Send(mac string) error` — UDP broadcast `255.255.255.255:9`.
  - `scan.PortScan(ctx context.Context, ip string, ports []int, timeout time.Duration) []int` — TCP connect, 32 workers; `scan.CommonPorts` (`[]int`: 21,22,23,25,53,80,110,143,443,445,554,587,631,993,995,1883,3000,3306,3389,5000,5432,5900,6443,8000,8006,8080,8081,8123,8443,9000,9090,9100,32400) and `scan.ServiceGuess(port int) string` map (ssh, http, https, dns, smb, rdp, mqtt, proxmox…, empty when unknown).
  - Routes: `POST /devices/{id}/wol` (admin; first iface MAC, 400 if none) and `POST /devices/{id}/portscan` (admin; scans first IP of first iface, upserts open ports, redirects back).
  - `Dockerfile` (multi-stage: `golang:1.24-alpine` + `templ generate` + build, final `gcr.io/distroless/static`, `ENTRYPOINT ["/netis"]`, `EXPOSE 8080`, volume `/data`, env `NETIS_DB=/data/netis.db`).
  - `deploy/netis.service` — systemd unit: `ExecStart=/opt/netis/netis`, `Environment=NETIS_DB=/var/lib/netis/netis.db`, `AmbientCapabilities=CAP_NET_RAW` (optional privileged ICMP), `Restart=on-failure`.
  - `README.md` — what it is, build (`templ generate && CGO_ENABLED=0 go build -o netis ./cmd/netis`), run, Docker (`--network host` for ARP), LXC notes, settings keys reference, screenshots placeholder-free description of views.

- [ ] **Step 1: Write failing tests**

`internal/wol/wol_test.go`:

```go
package wol

import (
	"bytes"
	"testing"
)

func TestBuildMagicPacket(t *testing.T) {
	p, err := BuildMagicPacket("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 102 {
		t.Fatalf("len=%d", len(p))
	}
	if !bytes.Equal(p[:6], bytes.Repeat([]byte{0xFF}, 6)) {
		t.Fatal("missing FF header")
	}
	mac := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	for i := 0; i < 16; i++ {
		if !bytes.Equal(p[6+i*6:12+i*6], mac) {
			t.Fatalf("mac repeat %d wrong", i)
		}
	}
	if _, err := BuildMagicPacket("garbage"); err == nil {
		t.Fatal("want error")
	}
}
```

`internal/scan/ports_test.go`:

```go
package scan

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestPortScanFindsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	open := PortScan(context.Background(), "127.0.0.1", []int{port, port + 1}, 500*time.Millisecond)
	if len(open) != 1 || open[0] != port {
		t.Fatalf("open=%v want [%d]", open, port)
	}
}

func TestServiceGuess(t *testing.T) {
	if ServiceGuess(22) != "ssh" || ServiceGuess(8006) != "proxmox" {
		t.Fatal("guess broken")
	}
	if ServiceGuess(59999) != "" {
		t.Fatal("unknown port must be empty")
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/wol/ ./internal/scan/ -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/wol/wol.go`:

```go
package wol

import (
	"bytes"
	"fmt"
	"net"
)

func BuildMagicPacket(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return nil, fmt.Errorf("bad mac %q: %w", mac, err)
	}
	var b bytes.Buffer
	b.Write(bytes.Repeat([]byte{0xFF}, 6))
	for i := 0; i < 16; i++ {
		b.Write(hw)
	}
	return b.Bytes(), nil
}

func Send(mac string) error {
	pkt, err := BuildMagicPacket(mac)
	if err != nil {
		return err
	}
	conn, err := net.Dial("udp", "255.255.255.255:9")
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(pkt)
	return err
}
```

`internal/scan/ports.go`:

```go
package scan

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

var CommonPorts = []int{21, 22, 23, 25, 53, 80, 110, 143, 443, 445, 554, 587,
	631, 993, 995, 1883, 3000, 3306, 3389, 5000, 5432, 5900, 6443, 8000, 8006,
	8080, 8081, 8123, 8443, 9000, 9090, 9100, 32400}

var serviceNames = map[int]string{
	21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp", 53: "dns", 80: "http",
	110: "pop3", 143: "imap", 443: "https", 445: "smb", 554: "rtsp",
	587: "smtp-sub", 631: "ipp", 993: "imaps", 995: "pop3s", 1883: "mqtt",
	3000: "http-alt", 3306: "mysql", 3389: "rdp", 5000: "http-alt",
	5432: "postgres", 5900: "vnc", 6443: "kube-api", 8000: "http-alt",
	8006: "proxmox", 8080: "http-alt", 8081: "http-alt", 8123: "home-assistant",
	8443: "https-alt", 9000: "http-alt", 9090: "prometheus", 9100: "jetdirect",
	32400: "plex",
}

func ServiceGuess(port int) string { return serviceNames[port] }

func PortScan(ctx context.Context, ip string, ports []int, timeout time.Duration) []int {
	var (
		mu   sync.Mutex
		open []int
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 32)
	d := net.Dialer{Timeout: timeout}
	for _, port := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", ip, port))
			if err != nil {
				return
			}
			conn.Close()
			mu.Lock()
			open = append(open, port)
			mu.Unlock()
		}(port)
	}
	wg.Wait()
	sort.Ints(open)
	return open
}
```

Web handlers (`internal/web/devices.go`):

```go
func (s *Server) handleWOL(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ifaces, err := s.store.ListIfaces(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, f := range ifaces {
		if f.MAC != nil {
			if err := wol.Send(*f.MAC); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
			return
		}
	}
	http.Error(w, "device has no MAC", 400)
}

func (s *Server) handlePortScan(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ifaces, err := s.store.ListIfaces(id)
	if err != nil || len(ifaces) == 0 {
		http.Error(w, "device has no interface", 400)
		return
	}
	ips, err := s.store.ListIPs(ifaces[0].ID)
	if err != nil || len(ips) == 0 {
		http.Error(w, "device has no IP", 400)
		return
	}
	open := scan.PortScan(r.Context(), ips[0].IP, scan.CommonPorts, time.Second)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, p := range open {
		s.store.UpsertOpenPort(ifaces[0].ID, p, "tcp", scan.ServiceGuess(p), now)
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}
```

Routes: `POST /devices/{id}/wol` and `POST /devices/{id}/portscan`, both `requireAdmin`. Buttons in `DevicePage` next to the edit form.

`Dockerfile`:

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
RUN go install github.com/a-h/templ/cmd/templ@v0.3.887
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN templ generate && CGO_ENABLED=0 go build -ldflags="-s -w" -o /netis ./cmd/netis

FROM gcr.io/distroless/static
COPY --from=build /netis /netis
ENV NETIS_DB=/data/netis.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/netis"]
```

`deploy/netis.service`:

```ini
[Unit]
Description=netis home network organizer
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/netis/netis
Environment=NETIS_DB=/var/lib/netis/netis.db
Environment=NETIS_PRIVILEGED_ICMP=1
AmbientCapabilities=CAP_NET_RAW
StateDirectory=netis
Restart=on-failure
User=netis
Group=netis

[Install]
WantedBy=multi-user.target
```

`README.md` — sections: What is netis (one paragraph from the spec's Purpose); Quick start (build commands, first-run setup at `/setup`); Docker (`docker run --network host -v netis-data:/data ...` — host network required for ARP/MAC discovery); Proxmox LXC install (binary + systemd unit); Configuration (env vars table: `NETIS_ADDR`, `NETIS_DB`, `NETIS_PRIVILEGED_ICMP`; settings UI keys for Proxmox/WireGuard); Limitations (MAC only on local L2, WG status via SSH).

- [ ] **Step 4: Run all tests and build**

Run: `templ generate && go test ./... -count=1 && CGO_ENABLED=0 go build ./... && docker build -t netis . 2>/dev/null || echo "docker build skipped (no docker)"`
Expected: tests PASS, Go build clean; Docker build optional locally.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: wake-on-lan, port scan and deploy artifacts"
```

---

## Final verification (after Task 15)

- [ ] `templ generate && go test ./... -count=1` — all green.
- [ ] `CGO_ENABLED=0 go build -o netis ./cmd/netis` — static binary builds.
- [ ] Manual smoke: `NETIS_DB=/tmp/netis-smoke.db ./netis` → `/setup` creates admin → login → Settings → add real LAN subnet → Scan now → devices appear in grid and list.
- [ ] Use superpowers:finishing-a-development-branch.




