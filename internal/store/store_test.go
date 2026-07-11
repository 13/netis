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
