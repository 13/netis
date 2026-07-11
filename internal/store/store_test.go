package store

import (
	"database/sql"
	"testing"
)

// TestMigration0002AgainstPopulatedTable is the real data-safety guard: it
// applies ONLY 0001, inserts a parent/child device pair and a child iface
// while foreign keys are enforced, then lets Open() run 0002's device table
// rebuild against that populated data. If the migrate loop failed to disable
// foreign keys, DROP TABLE device would cascade-delete the iface (and the
// child via SET NULL churn) — this test would then see missing rows.
func TestMigration0002AgainstPopulatedTable(t *testing.T) {
	path := t.TempDir() + "/populated.db"

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		t.Fatal(err)
	}
	// Apply only the 0001 schema and record it, so a later Open() runs 0002
	// against the rows we insert below.
	sql0001, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(sql0001)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO schema_migrations (version) VALUES ('0001_init.sql')`); err != nil {
		t.Fatal(err)
	}
	// Parent + child (self-FK) + a child iface (cascade FK), FK enforcement on.
	if _, err := db.Exec(
		`INSERT INTO device (id,name,kind,source) VALUES (1,'parent','server','manual')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO device (id,name,kind,source,parent_device_id) VALUES (2,'child','vm','proxmox',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO iface (device_id,mac) VALUES (2,'aa:bb:cc:dd:ee:02')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Open runs migrate(): 0001 already recorded, so only 0002 runs — the
	// device table rebuild — against the populated tables.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open (runs 0002 against data): %v", err)
	}
	defer s.Close()

	var devices, ifaces int
	if err := s.DB.QueryRow(`SELECT count(*) FROM device`).Scan(&devices); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT count(*) FROM iface`).Scan(&ifaces); err != nil {
		t.Fatal(err)
	}
	if devices != 2 {
		t.Fatalf("device rows after 0002 rebuild = %d, want 2 (rebuild lost data)", devices)
	}
	if ifaces != 1 {
		t.Fatalf("iface rows after 0002 rebuild = %d, want 1 (DROP cascade-deleted the child iface)", ifaces)
	}
	// self-FK survived the rebuild
	child, err := s.GetDevice(2)
	if err != nil || child.ParentDeviceID == nil || *child.ParentDeviceID != 1 {
		t.Fatalf("parent link lost after rebuild: %+v err=%v", child, err)
	}
	// source='pihole' now accepted post-0002
	if _, err := s.DB.Exec(
		`INSERT INTO device (name,kind,source) VALUES ('ph','other','pihole')`); err != nil {
		t.Fatalf("source='pihole' should be accepted after 0002: %v", err)
	}
}

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
