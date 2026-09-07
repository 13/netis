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

// eachDialect runs fn against every backend available in this environment:
// SQLite always, and Postgres when NETIS_TEST_PG_DSN points at a server. The
// store's queries are written once for both, so every behaviour worth trusting
// on SQLite is worth re-running on Postgres.
//
// Each Postgres subtest gets its own schema, created before the store opens so
// migrations land inside it and dropped afterwards; that keeps subtests
// isolated without needing a database per test.
func eachDialect(t *testing.T, fn func(t *testing.T, s *Store)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		s, err := Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		fn(t, s)
	})

	dsn := os.Getenv("NETIS_TEST_PG_DSN")
	if dsn == "" {
		t.Log("NETIS_TEST_PG_DSN not set; skipping postgres")
		return
	}
	t.Run("postgres", func(t *testing.T) {
		s := openPGSchema(t, dsn)
		fn(t, s)
	})
}

// openPGSchema creates a scratch schema on the server named by dsn and returns
// a store whose search_path points at it.
func openPGSchema(t *testing.T, dsn string) *Store {
	t.Helper()
	schema := fmt.Sprintf("netis_test_%d", time.Now().UnixNano())

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	s, err := Open(dsn + sep + "search_path=" + schema)
	if err != nil {
		t.Fatalf("open postgres schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		s.Close()
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer db.Close()
		db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	})
	return s
}

func TestRebind(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`SELECT 1`, `SELECT 1`},
		{`SELECT * FROM t WHERE a=? AND b=?`, `SELECT * FROM t WHERE a=$1 AND b=$2`},
		// A ? inside a string literal is data, not a placeholder.
		{`SELECT '?' , ? FROM t`, `SELECT '?' , $1 FROM t`},
		// A doubled quote closes and reopens the literal, so the ? after it is
		// still inside one.
		{`SELECT 'it''s ?' FROM t WHERE a=?`, `SELECT 'it''s ?' FROM t WHERE a=$1`},
	}
	for _, c := range cases {
		if got := Postgres.rebind(c.in); got != c.want {
			t.Errorf("Postgres.rebind(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := SQLite.rebind(c.in); got != c.in {
			t.Errorf("SQLite.rebind(%q) = %q, want it unchanged", c.in, got)
		}
	}
}

func TestIsPostgresDSN(t *testing.T) {
	for _, dsn := range []string{"postgres://u@h/db", "postgresql://u@h/db"} {
		if !IsPostgresDSN(dsn) {
			t.Errorf("IsPostgresDSN(%q) = false, want true", dsn)
		}
	}
	for _, dsn := range []string{":memory:", "netis.db", "/data/netis.db", ""} {
		if IsPostgresDSN(dsn) {
			t.Errorf("IsPostgresDSN(%q) = true, want false", dsn)
		}
	}
}

// TestConformanceDeviceLifecycle covers the id-returning inserts, the boolean
// columns, and the tag/IP/port upserts — the parts of the store the Postgres
// port actually changes.
func TestConformanceDeviceLifecycle(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		snID, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.0.0.0/24", Name: "lan",
			Kind: "lan", ScanEnabled: true, ScanIntervalSec: 60})
		if err != nil {
			t.Fatal(err)
		}
		if snID == 0 {
			t.Fatal("CreateSubnet returned id 0")
		}
		sn, err := s.GetSubnet(ctx, snID)
		if err != nil {
			t.Fatal(err)
		}
		if !sn.ScanEnabled || sn.CIDR != "10.0.0.0/24" {
			t.Fatalf("GetSubnet = %+v", sn)
		}

		// A scan-sourced device starts unreviewed; a manual one does not.
		scanned, err := s.CreateDevice(ctx, Device{Name: "found", Kind: "other", Source: "scan"})
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.GetDevice(ctx, scanned)
		if err != nil {
			t.Fatal(err)
		}
		if d.Reviewed {
			t.Error("scan-sourced device should start unreviewed")
		}
		manual, err := s.CreateDevice(ctx, Device{Name: "nas", Kind: "server",
			Source: "manual", Model: "DS220+", Function: "storage"})
		if err != nil {
			t.Fatal(err)
		}
		if manual == scanned {
			t.Fatal("CreateDevice returned the same id twice")
		}
		d, err = s.GetDevice(ctx, manual)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Reviewed || d.Model != "DS220+" || d.Function != "storage" {
			t.Fatalf("GetDevice = %+v", d)
		}

		// Editing counts as reviewing.
		if err := s.SetDeviceReviewed(ctx, scanned, false); err != nil {
			t.Fatal(err)
		}
		d, _ = s.GetDevice(ctx, scanned)
		if d.Reviewed {
			t.Error("SetDeviceReviewed(false) did not take")
		}
		d.Name, d.Kind, d.Source = "found-edited", "other", "scan"
		if err := s.UpdateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
		d, _ = s.GetDevice(ctx, scanned)
		if !d.Reviewed || d.Name != "found-edited" {
			t.Fatalf("after UpdateDevice: %+v", d)
		}

		mac := "aa:bb:cc:dd:ee:ff"
		ifID, err := s.AddIface(ctx, manual, &mac, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AssignIP(ctx, ifID, snID, "10.0.0.5", "dhcp"); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.FindIfaceByMAC(ctx, mac); err != nil || !ok {
			t.Fatalf("FindIfaceByMAC ok=%v err=%v", ok, err)
		}
		if _, ok, err := s.FindIfaceByIP(ctx, snID, "10.0.0.5"); err != nil || !ok {
			t.Fatalf("FindIfaceByIP ok=%v err=%v", ok, err)
		}

		// A static assignment must survive a later dhcp observation.
		if err := s.UpsertIPAssignment(ctx, ifID, snID, "10.0.0.5", "static"); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertIPAssignment(ctx, ifID, snID, "10.0.0.5", "dhcp"); err != nil {
			t.Fatal(err)
		}
		ips, err := s.ListIPs(ctx, ifID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ips) != 1 || ips[0].Kind != "static" {
			t.Fatalf("ListIPs = %+v, want one static row", ips)
		}

		// Tags: create, attach twice (idempotent), then sync to a new set.
		tagID, err := s.CreateTag(ctx, "prod", "#ff0000")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.TagDevice(ctx, manual, tagID); err != nil {
			t.Fatal(err)
		}
		if err := s.TagDevice(ctx, manual, tagID); err != nil {
			t.Fatalf("re-tagging must be a no-op, got %v", err)
		}
		if err := s.SetDeviceTags(ctx, manual, []string{"prod", "storage"}); err != nil {
			t.Fatal(err)
		}
		tags, err := s.DeviceTags(ctx, manual)
		if err != nil {
			t.Fatal(err)
		}
		if len(tags) != 2 {
			t.Fatalf("DeviceTags = %+v, want 2", tags)
		}

		if err := s.SetCustomField(ctx, manual, "rack", "A1"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetCustomField(ctx, manual, "rack", "B2"); err != nil {
			t.Fatal(err)
		}
		cfs, err := s.ListCustomFields(ctx, manual)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfs) != 1 || cfs[0].Value != "B2" {
			t.Fatalf("ListCustomFields = %+v, want one rack=B2", cfs)
		}

		linkID, err := s.AddLink(ctx, manual, "ui", "http://nas/")
		if err != nil {
			t.Fatal(err)
		}
		links, err := s.ListLinks(ctx, manual)
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != 1 || links[0].ID != linkID {
			t.Fatalf("ListLinks = %+v", links)
		}

		seen := time.Now().UTC().Format(time.RFC3339)
		if err := s.UpsertOpenPort(ctx, ifID, 443, "tcp", "https", seen); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertOpenPort(ctx, ifID, 443, "tcp", "https-alt", seen); err != nil {
			t.Fatal(err)
		}
		ports, err := s.ListOpenPorts(ctx, ifID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ports) != 1 || ports[0].ServiceGuess != "https-alt" {
			t.Fatalf("ListOpenPorts = %+v", ports)
		}

		rows, err := s.ListDevices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("ListDevices returned %d rows, want 2", len(rows))
		}
	})
}

func TestConformanceStatusAndEvents(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		devID, err := s.CreateDevice(ctx, Device{Name: "host", Kind: "server", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		ifID, err := s.AddIface(ctx, devID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}

		// First sighting counts as a transition from offline.
		wasOffline, err := s.MarkSeen(ctx, ifID, 1.5, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !wasOffline {
			t.Error("first MarkSeen should report a transition")
		}
		if wasOffline, err = s.MarkSeen(ctx, ifID, 1.5, time.Now()); err != nil || wasOffline {
			t.Errorf("second MarkSeen: wasOffline=%v err=%v", wasOffline, err)
		}
		online, lastSeen, err := s.IfaceOnline(ctx, ifID)
		if err != nil || !online || lastSeen == nil {
			t.Fatalf("IfaceOnline = %v %v %v", online, lastSeen, err)
		}

		// Two misses with a threshold of 2 takes it offline exactly once.
		if went, err := s.MarkMissed(ctx, ifID, 2); err != nil || went {
			t.Errorf("first MarkMissed: went=%v err=%v", went, err)
		}
		if went, err := s.MarkMissed(ctx, ifID, 2); err != nil || !went {
			t.Errorf("second MarkMissed: went=%v err=%v", went, err)
		}
		if online, _, _ = s.IfaceOnline(ctx, ifID); online {
			t.Error("iface should be offline after reaching the threshold")
		}

		bucket := "2026-09-07T17:00:00Z"
		if err := s.RecordAvailability(ctx, ifID, true, bucket); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordAvailability(ctx, ifID, false, bucket); err != nil {
			t.Fatal(err)
		}
		pct, err := s.AvailabilityPct(ctx, ifID, "2026-09-07T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if pct != 50 {
			t.Errorf("AvailabilityPct = %v, want 50", pct)
		}

		evID, err := s.AddEvent(ctx, "online", &devID, "up")
		if err != nil {
			t.Fatal(err)
		}
		if evID == 0 {
			t.Error("AddEvent returned id 0")
		}
		if _, err := s.AddEvent(ctx, "scan_error", nil, "timeout"); err != nil {
			t.Fatal(err)
		}
		evs, err := s.ListEvents(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != 2 {
			t.Fatalf("ListEvents = %d rows, want 2", len(evs))
		}
		devEvs, err := s.ListDeviceEvents(ctx, devID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(devEvs) != 1 {
			t.Fatalf("ListDeviceEvents = %d rows, want 1", len(devEvs))
		}

		if err := s.SetIntegrationStatus(ctx, IntegrationStatus{
			Name: "pihole", LastRun: bucket, OK: true, Detail: "ok", ItemCount: 7}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetIntegrationStatus(ctx, IntegrationStatus{
			Name: "pihole", LastRun: bucket, OK: false, Detail: "boom", ItemCount: 0}); err != nil {
			t.Fatal(err)
		}
		sts, err := s.ListIntegrationStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(sts) != 1 || sts[0].OK || sts[0].Detail != "boom" {
			t.Fatalf("ListIntegrationStatus = %+v", sts)
		}
	})
}

func TestConformanceUsersSettingsAndGrid(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		created, err := s.CreateFirstAdmin(ctx, "root", "hash")
		if err != nil {
			t.Fatal(err)
		}
		if !created {
			t.Fatal("CreateFirstAdmin should create on an empty table")
		}
		if created, err = s.CreateFirstAdmin(ctx, "root2", "hash"); err != nil || created {
			t.Errorf("second CreateFirstAdmin: created=%v err=%v", created, err)
		}
		u, ok, err := s.GetUserByName(ctx, "root")
		if err != nil || !ok || u.Role != "admin" {
			t.Fatalf("GetUserByName = %+v %v %v", u, ok, err)
		}
		if _, ok, err := s.GetUserByName(ctx, "nobody"); err != nil || ok {
			t.Errorf("missing user: ok=%v err=%v", ok, err)
		}

		// The last admin is protected; a viewer is not.
		if deleted, err := s.DeleteUserGuarded(ctx, u.ID); err != nil || deleted {
			t.Errorf("deleting the last admin: deleted=%v err=%v", deleted, err)
		}
		viewerID, err := s.CreateUser(ctx, "bob", "hash", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		if deleted, err := s.DeleteUserGuarded(ctx, viewerID); err != nil || !deleted {
			t.Errorf("deleting a viewer: deleted=%v err=%v", deleted, err)
		}
		if n, err := s.CountUsers(ctx); err != nil || n != 1 {
			t.Errorf("CountUsers = %d, %v; want 1", n, err)
		}
		if users, err := s.ListUsers(ctx); err != nil || len(users) != 1 {
			t.Errorf("ListUsers = %+v, %v", users, err)
		}

		future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		if err := s.CreateSession(ctx, "tok", u.ID, future); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := s.GetSession(ctx, "tok"); err != nil || !ok || got.ID != u.ID {
			t.Fatalf("GetSession = %+v %v %v", got, ok, err)
		}
		past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		if err := s.CreateSession(ctx, "old", u.ID, past); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.GetSession(ctx, "old"); err != nil || ok {
			t.Errorf("expired session: ok=%v err=%v", ok, err)
		}
		if err := s.DeleteSession(ctx, "tok"); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := s.GetSession(ctx, "tok"); ok {
			t.Error("deleted session still resolves")
		}

		if v, err := s.GetSetting(ctx, "absent"); err != nil || v != "" {
			t.Errorf("GetSetting(absent) = %q, %v", v, err)
		}
		if err := s.SetSetting(ctx, "scan_interval", "60"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSetting(ctx, "scan_interval", "90"); err != nil {
			t.Fatal(err)
		}
		if v, err := s.GetSetting(ctx, "scan_interval"); err != nil || v != "90" {
			t.Errorf("GetSetting = %q, %v; want 90", v, err)
		}

		// Grid: two ifaces claiming one IP is a conflict; only one is online.
		snID, err := s.CreateSubnet(ctx, Subnet{CIDR: "192.168.1.0/24", Kind: "lan",
			ScanEnabled: true, ScanIntervalSec: 60})
		if err != nil {
			t.Fatal(err)
		}
		devID, err := s.CreateDevice(ctx, Device{Name: "a", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		macA, macB := "00:11:22:33:44:55", "00:11:22:33:44:66"
		ifA, err := s.AddIface(ctx, devID, &macA, nil)
		if err != nil {
			t.Fatal(err)
		}
		ifB, err := s.AddIface(ctx, devID, &macB, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []int64{ifA, ifB} {
			if _, err := s.AssignIP(ctx, id, snID, "192.168.1.10", "dhcp"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.MarkSeen(ctx, ifA, 2, time.Now()); err != nil {
			t.Fatal(err)
		}
		occ, err := s.SubnetOccupancy(ctx, snID)
		if err != nil {
			t.Fatal(err)
		}
		o, ok := occ["192.168.1.10"]
		if !ok {
			t.Fatalf("SubnetOccupancy missing the IP: %+v", occ)
		}
		if o.Count != 2 {
			t.Errorf("Count = %d, want 2 (conflict)", o.Count)
		}
		if err := s.SetIPKind(ctx, snID, "192.168.1.10", "static"); err != nil {
			t.Fatal(err)
		}
		occ, _ = s.SubnetOccupancy(ctx, snID)
		if occ["192.168.1.10"].Kind != "static" {
			t.Errorf("Kind = %q, want static", occ["192.168.1.10"].Kind)
		}
	})
}
