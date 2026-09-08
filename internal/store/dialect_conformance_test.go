package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
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
func openPGSchema(t testing.TB, dsn string) *Store {
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

// A failing database must not be reported as "this device is offline". Before
// this, ifaceOnline turned every error into (false, nil, nil), so an
// unreachable database rendered the whole fleet offline with nothing logged.
func TestIfaceOnlineDistinguishesNoRowsFromFailure(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		devID, err := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		ifID, err := s.AddIface(ctx, devID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}

		// Never scanned: legitimately offline, and not an error.
		online, _, err := s.IfaceOnline(ctx, ifID)
		if err != nil || online {
			t.Fatalf("unscanned iface: online=%v err=%v", online, err)
		}

		// Database gone: an error, not a confident "offline".
		s.Close()
		if _, _, err := s.IfaceOnline(ctx, ifID); err == nil {
			t.Error("a closed database must produce an error, not a false offline")
		}
	})
}

// ListDevices assembles its rows from bulk queries rather than per-device ones,
// so the aggregation it does in Go — MAC and IP order, "online if any interface
// is", latest last-seen, sorted tag names — is worth pinning down.
func TestConformanceListDevicesAggregation(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		snID, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.2.0.0/24", Kind: "lan",
			ScanEnabled: true, ScanIntervalSec: 60})
		if err != nil {
			t.Fatal(err)
		}

		// "alpha" sorts before "beta"; devices come back ordered by name.
		beta, err := s.CreateDevice(ctx, Device{Name: "beta", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		alpha, err := s.CreateDevice(ctx, Device{Name: "alpha", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		// A device with no interfaces at all must still appear.
		if _, err := s.CreateDevice(ctx, Device{Name: "gamma", Kind: "other", Source: "manual"}); err != nil {
			t.Fatal(err)
		}

		// alpha has two interfaces: the first offline, the second online, so
		// the device is online and takes the later last-seen.
		mac1, mac2 := "aa:00:00:00:00:01", "aa:00:00:00:00:02"
		if1, err := s.AddIface(ctx, alpha, &mac1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if2, err := s.AddIface(ctx, alpha, &mac2, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, ip := range []string{"10.2.0.11", "10.2.0.12"} {
			if _, err := s.AssignIP(ctx, if1, snID, ip, "dhcp"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.AssignIP(ctx, if2, snID, "10.2.0.13", "static"); err != nil {
			t.Fatal(err)
		}
		early := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
		late := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
		if _, err := s.MarkSeen(ctx, if1, 1, early); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ { // take if1 offline again
			if _, err := s.MarkMissed(ctx, if1, 1); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.MarkSeen(ctx, if2, 1, late); err != nil {
			t.Fatal(err)
		}
		if err := s.SetDeviceTags(ctx, alpha, []string{"zeta", "alpha-tag"}); err != nil {
			t.Fatal(err)
		}

		// beta has one interface, never scanned: offline, no last-seen.
		mac3 := "bb:00:00:00:00:01"
		if _, err := s.AddIface(ctx, beta, &mac3, nil); err != nil {
			t.Fatal(err)
		}

		rows, err := s.ListDevices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 {
			t.Fatalf("got %d devices, want 3", len(rows))
		}
		if rows[0].Name != "alpha" || rows[1].Name != "beta" || rows[2].Name != "gamma" {
			t.Fatalf("order = %q,%q,%q; want alpha,beta,gamma",
				rows[0].Name, rows[1].Name, rows[2].Name)
		}

		a := rows[0]
		if !a.Online {
			t.Error("alpha should be online: one of its interfaces is")
		}
		if a.LastSeen == nil || *a.LastSeen != late.Format(time.RFC3339) {
			t.Errorf("alpha LastSeen = %v, want the later of the two", a.LastSeen)
		}
		if !slices.Equal(a.MACs, []string{mac1, mac2}) {
			t.Errorf("alpha MACs = %v, want them in interface order", a.MACs)
		}
		wantIPs := []IPInfo{
			{IP: "10.2.0.11", Kind: "dhcp"},
			{IP: "10.2.0.12", Kind: "dhcp"},
			{IP: "10.2.0.13", Kind: "static"},
		}
		if !slices.Equal(a.IPs, wantIPs) {
			t.Errorf("alpha IPs = %+v, want %+v", a.IPs, wantIPs)
		}
		if !slices.Equal(a.TagNames, []string{"alpha-tag", "zeta"}) {
			t.Errorf("alpha TagNames = %v, want them sorted by name", a.TagNames)
		}

		b := rows[1]
		if b.Online || b.LastSeen != nil {
			t.Errorf("beta was never scanned: online=%v lastSeen=%v", b.Online, b.LastSeen)
		}
		if !slices.Equal(b.MACs, []string{mac3}) {
			t.Errorf("beta MACs = %v", b.MACs)
		}

		g := rows[2]
		if len(g.MACs) != 0 || len(g.IPs) != 0 || g.Online {
			t.Errorf("gamma has no interfaces: %+v", g)
		}
	})
}
