package store

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
)

// AdGuard Home and OPNsense devices are accepted as sources after 0015.
func TestConformanceDHCPSourcesAllowed(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		for _, src := range []string{"pihole", "adguard", "opnsense"} {
			if _, err := s.CreateDevice(context.Background(), Device{Name: src, Kind: "other", Source: src}); err != nil {
				t.Errorf("source %q refused: %v", src, err)
			}
		}
		if _, err := s.CreateDevice(context.Background(), Device{Name: "x", Kind: "other", Source: "bogus"}); err == nil {
			t.Error("an unknown source must still be refused")
		}
	})
}

// The 0015 rebuild of the SQLite device table keeps every row, every column's
// value, and the rows that reference a device.
func TestMigration0015KeepsDevices(t *testing.T) {
	path := t.TempDir() + "/mig.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	applyMigrationsBefore(t, db, "sqlite", "0015")
	if _, err := db.Exec(`
		INSERT INTO device (id,name,kind,notes,vendor,source,proxmox_vmid,icon,reviewed,model,function,
			created_at,alert_offline,upstream_missing_since)
			VALUES (1,'pve','server','n','v','proxmox',100,'i',1,'m','f','2026-01-01T00:00:00Z',1,'2026-02-01T00:00:00Z');
		INSERT INTO device (id,name,kind,source,parent_device_id,wg_pubkey) VALUES (2,'peer','wg-peer','wireguard',1,'k=');
		INSERT INTO iface (id,device_id,mac) VALUES (1,2,'aa:bb:cc:00:00:01');
		INSERT INTO tag (id,name) VALUES (1,'t');
		INSERT INTO device_tag (device_id,tag_id) VALUES (2,1);
		INSERT INTO custom_field (device_id,key,value) VALUES (1,'k','v');`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open (runs 0015): %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	var alert int
	var missing sql.NullString
	var created string
	if err := s.DB.QueryRow(`SELECT alert_offline, upstream_missing_since, created_at FROM device WHERE id=1`).
		Scan(&alert, &missing, &created); err != nil {
		t.Fatal(err)
	}
	if alert != 1 || missing.String != "2026-02-01T00:00:00Z" || created != "2026-01-01T00:00:00Z" {
		t.Fatalf("columns lost: alert=%d missing=%v created=%s", alert, missing, created)
	}
	d, err := s.GetDevice(ctx, 1)
	if err != nil || d.Notes != "n" || d.Vendor != "v" || d.Model != "m" || d.Function != "f" || !d.Reviewed ||
		d.ProxmoxVMID == nil || *d.ProxmoxVMID != 100 {
		t.Fatalf("device 1 = %+v err=%v", d, err)
	}
	peer, err := s.GetDevice(ctx, 2)
	if err != nil || peer.ParentDeviceID == nil || *peer.ParentDeviceID != 1 {
		t.Fatalf("parent lost: %+v err=%v", peer, err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM iface WHERE device_id=2`:        1,
		`SELECT count(*) FROM device_tag WHERE device_id=2`:   1,
		`SELECT count(*) FROM custom_field WHERE device_id=1`: 1,
	} {
		var n int
		if err := s.DB.QueryRow(q).Scan(&n); err != nil || n != want {
			t.Errorf("%s = %d (err %v), want %d", q, n, err, want)
		}
	}
	// The 0009 partial unique indexes came back with the table.
	pk, vmid := "k=", int64(100)
	if _, err := s.CreateDevice(ctx, Device{Name: "dup", Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk}); !IsUniqueViolation(err) {
		t.Errorf("duplicate wg_pubkey after 0015: err=%v", err)
	}
	if _, err := s.CreateDevice(ctx, Device{Name: "dup", Kind: "vm", Source: "proxmox", ProxmoxVMID: &vmid}); !IsUniqueViolation(err) {
		t.Errorf("duplicate proxmox_vmid after 0015: err=%v", err)
	}
	// Foreign keys are enforced again: deleting the parent nulls the child's
	// link, and deleting a device cascades to its iface.
	if err := s.DeleteDevice(ctx, 2); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow(`SELECT count(*) FROM iface`).Scan(&n)
	if n != 0 {
		t.Fatalf("iface not cascaded after rebuild: %d", n)
	}
}

// A SQLite migration that rebuilds device lists its columns by hand, so one
// added by ALTER TABLE in an earlier migration — possibly merged in from
// another branch — would be silently dropped. Every column any migration adds
// to device must exist once all migrations have run.
func TestDeviceRebuildKeepsAddedColumns(t *testing.T) {
	s := openTest(t)
	have := map[string]bool{}
	rows, err := s.DB.Query(`SELECT name FROM pragma_table_info('device')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		have[name] = true
	}
	rows.Close()

	added := regexp.MustCompile(`(?i)ALTER\s+TABLE\s+device\s+ADD\s+COLUMN\s+(\w+)`)
	entries, err := migrationsFS.ReadDir("migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		b, err := migrationsFS.ReadFile("migrations/sqlite/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range added.FindAllStringSubmatch(string(b), -1) {
			checked++
			if !have[m[1]] {
				t.Errorf("column device.%s added by %s is gone after all migrations", m[1], e.Name())
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no ALTER TABLE device ADD COLUMN to check; the pattern is stale")
	}
}

// SetIfaceMACIfEmpty fills a missing MAC only, and never one another
// interface already has.
func TestConformanceSetIfaceMACIfEmpty(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		devID, _ := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "scan"})
		bare, _ := s.AddIface(ctx, devID, nil, nil)
		mac := "aa:bb:cc:00:00:01"
		withMAC, _ := s.AddIface(ctx, devID, &mac, nil)

		if ok, err := s.SetIfaceMACIfEmpty(ctx, bare, mac); err != nil || ok {
			t.Fatalf("filled a MAC another iface holds: ok=%v err=%v", ok, err)
		}
		if ok, err := s.SetIfaceMACIfEmpty(ctx, withMAC, "aa:bb:cc:00:00:02"); err != nil || ok {
			t.Fatalf("changed a set MAC: ok=%v err=%v", ok, err)
		}
		if ok, err := s.SetIfaceMACIfEmpty(ctx, bare, "aa:bb:cc:00:00:03"); err != nil || !ok {
			t.Fatalf("did not fill an empty MAC: ok=%v err=%v", ok, err)
		}
		iface, found, err := s.FindIfaceByMAC(ctx, "aa:bb:cc:00:00:03")
		if err != nil || !found || iface.ID != bare {
			t.Fatalf("iface by filled MAC = %+v found=%v err=%v", iface, found, err)
		}
	})
}
