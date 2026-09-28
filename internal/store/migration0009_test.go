package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// A second device with a WireGuard key or Proxmox VMID already in use is
// refused, so two overlapping sync runs cannot both create the same peer or
// guest.
func TestConformanceDeviceIntegrationKeysAreUnique(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		pk, vmid := "peerkey=", int64(100)
		if _, err := s.CreateDevice(ctx, Device{Name: "a", Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk}); err != nil {
			t.Fatal(err)
		}
		_, err := s.CreateDevice(ctx, Device{Name: "b", Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk})
		if !IsUniqueViolation(err) {
			t.Errorf("duplicate wg_pubkey: err=%v, want a unique violation", err)
		}
		if _, err := s.CreateDevice(ctx, Device{Name: "g", Kind: "vm", Source: "proxmox", ProxmoxVMID: &vmid}); err != nil {
			t.Fatal(err)
		}
		_, err = s.CreateDevice(ctx, Device{Name: "h", Kind: "vm", Source: "proxmox", ProxmoxVMID: &vmid})
		if !IsUniqueViolation(err) {
			t.Errorf("duplicate proxmox_vmid: err=%v, want a unique violation", err)
		}
		// Devices without either key are unaffected by the partial indexes.
		for i := 0; i < 2; i++ {
			if _, err := s.CreateDevice(ctx, Device{Name: "plain", Kind: "other", Source: "manual"}); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// Migration 0009 must not fail on a database that already holds duplicates:
// the lowest id keeps the key and the later copies lose it, and no row is
// deleted.
func TestMigration0009ResolvesExistingDuplicates(t *testing.T) {
	check := func(t *testing.T, s *Store) {
		t.Helper()
		ctx := context.Background()
		var n int
		if err := s.queryRow(ctx, `SELECT count(*) FROM device`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 4 {
			t.Fatalf("device rows = %d, want 4 (the migration must not delete)", n)
		}
		if id, ok, err := s.FindDeviceByWGPubKey(ctx, "k="); err != nil || !ok || id != 1 {
			t.Errorf("wg key owner = %d ok=%v err=%v, want device 1", id, ok, err)
		}
		if id, ok, err := s.FindProxmoxGuest(ctx, 100); err != nil || !ok || id != 3 {
			t.Errorf("vmid owner = %d ok=%v err=%v, want device 3", id, ok, err)
		}
		dup, err := s.GetDevice(ctx, 4)
		if err != nil {
			t.Fatal(err)
		}
		if dup.ProxmoxVMID != nil || dup.Source != "manual" {
			t.Errorf("duplicate guest = %+v, want no VMID and source manual", dup)
		}
	}
	const dups = `INSERT INTO device (id,name,kind,source,wg_pubkey) VALUES (1,'p1','wg-peer','wireguard','k=');
		INSERT INTO device (id,name,kind,source,wg_pubkey) VALUES (2,'p2','wg-peer','wireguard','k=');
		INSERT INTO device (id,name,kind,source,proxmox_vmid) VALUES (3,'g1','vm','proxmox',100);
		INSERT INTO device (id,name,kind,source,proxmox_vmid) VALUES (4,'g2','vm','proxmox',100);`

	t.Run("sqlite", func(t *testing.T) {
		path := t.TempDir() + "/mig.db"
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		applyMigrationsBefore(t, db, "sqlite", "0009")
		if _, err := db.Exec(dups); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open (runs 0009): %v", err)
		}
		defer s.Close()
		check(t, s)
	})

	dsn := os.Getenv("NETIS_TEST_PG_DSN")
	if dsn == "" {
		t.Log("NETIS_TEST_PG_DSN not set; skipping postgres")
		return
	}
	t.Run("postgres", func(t *testing.T) {
		schema := fmt.Sprintf("netis_test_%d", time.Now().UnixNano())
		admin, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		scoped := dsn + sep + "search_path=" + schema
		db, err := sql.Open("pgx", scoped)
		if err != nil {
			t.Fatal(err)
		}
		applyMigrationsBefore(t, db, "postgres", "0009")
		if _, err := db.Exec(dups); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(scoped)
		if err != nil {
			t.Fatalf("Open (runs 0009): %v", err)
		}
		defer s.Close()
		check(t, s)
	})
}

// applyMigrationsBefore applies and records every migration of a dialect whose
// name sorts before the given version, so a later Open runs only the rest.
func applyMigrationsBefore(t *testing.T, db *sql.DB, dialect, version string) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations/" + dialect)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries { // ReadDir returns them sorted by name
		if e.Name() >= version {
			break
		}
		b, err := migrationsFS.ReadFile("migrations/" + dialect + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply %s: %v", e.Name(), err)
		}
		if _, err := db.Exec(Dialect(dialect).rebind(`INSERT INTO schema_migrations (version) VALUES (?)`), e.Name()); err != nil {
			t.Fatal(err)
		}
	}
}
