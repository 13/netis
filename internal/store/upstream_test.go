package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func changeIDs(chs []UpstreamChange) []int64 {
	var ids []int64
	for _, c := range chs {
		ids = append(ids, c.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// A device leaves its integration's list once (marked, reported), stays
// missing quietly, and is cleared and reported when it comes back. Devices
// outside the scope are never touched.
func TestConformanceReconcileUpstream(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		v1, v2 := int64(101), int64(102)
		g1, _ := s.CreateDevice(ctx, Device{Name: "g1", Kind: "vm", Source: "proxmox", ProxmoxVMID: &v1})
		g2, _ := s.CreateDevice(ctx, Device{Name: "g2", Kind: "vm", Source: "proxmox", ProxmoxVMID: &v2})
		node, _ := s.CreateDevice(ctx, Device{Name: "pve", Kind: "server", Source: "proxmox"})
		pk := "peer="
		peer, _ := s.CreateDevice(ctx, Device{Name: "p", Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk})
		manual, _ := s.CreateDevice(ctx, Device{Name: "m", Kind: "other", Source: "manual"})

		t0 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
		gone, back, err := s.ReconcileUpstream(ctx, ScopeProxmoxGuests, map[int64]bool{g1: true}, t0)
		if err != nil {
			t.Fatal(err)
		}
		if got := changeIDs(gone); len(got) != 1 || got[0] != g2 || len(back) != 0 {
			t.Fatalf("first reconcile gone=%v back=%v, want only g2 gone", got, changeIDs(back))
		}
		if gone[0].Name != "g2" {
			t.Errorf("gone name = %q", gone[0].Name)
		}
		d, _ := s.GetDevice(ctx, g2)
		if d.UpstreamMissingSince == nil || *d.UpstreamMissingSince != "2026-09-28T10:00:00Z" {
			t.Errorf("g2 missing since = %v", d.UpstreamMissingSince)
		}
		for _, id := range []int64{g1, node, peer, manual} {
			if d, _ := s.GetDevice(ctx, id); d.UpstreamMissingSince != nil {
				t.Errorf("device %d marked missing outside the scope or while listed", id)
			}
		}

		// Still missing an hour later: no second report, time unchanged.
		gone, back, err = s.ReconcileUpstream(ctx, ScopeProxmoxGuests, map[int64]bool{g1: true}, t0.Add(time.Hour))
		if err != nil || len(gone) != 0 || len(back) != 0 {
			t.Fatalf("second reconcile gone=%v back=%v err=%v, want nothing", gone, back, err)
		}
		if d, _ := s.GetDevice(ctx, g2); *d.UpstreamMissingSince != "2026-09-28T10:00:00Z" {
			t.Errorf("missing-since moved to %s", *d.UpstreamMissingSince)
		}

		// Back again.
		gone, back, err = s.ReconcileUpstream(ctx, ScopeProxmoxGuests, map[int64]bool{g1: true, g2: true}, t0)
		if err != nil {
			t.Fatal(err)
		}
		if got := changeIDs(back); len(gone) != 0 || len(got) != 1 || got[0] != g2 {
			t.Fatalf("return reconcile gone=%v back=%v, want only g2 back", changeIDs(gone), got)
		}
		if d, _ := s.GetDevice(ctx, g2); d.UpstreamMissingSince != nil {
			t.Error("g2 still marked after returning")
		}

		// The WireGuard scope covers peers only.
		gone, _, err = s.ReconcileUpstream(ctx, ScopeWireGuardPeers, map[int64]bool{}, t0)
		if err != nil {
			t.Fatal(err)
		}
		if got := changeIDs(gone); len(got) != 1 || got[0] != peer {
			t.Errorf("wireguard gone = %v, want only the peer", got)
		}

		// The edit form never clears the mark.
		d, _ = s.GetDevice(ctx, peer)
		d.Name = "renamed"
		if err := s.UpdateDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
		if d, _ := s.GetDevice(ctx, peer); d.UpstreamMissingSince == nil {
			t.Error("UpdateDevice cleared the missing mark")
		}
		rows, _ := s.ListDevices(ctx)
		for _, r := range rows {
			if r.ID == peer && r.UpstreamMissingSince == nil {
				t.Error("ListDevices does not carry the missing mark")
			}
		}
	})
}

func TestConformanceSyncIntegrationIPs(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		sn, _ := s.CreateSubnet(ctx, Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})
		dev, _ := s.CreateDevice(ctx, Device{Name: "p", Kind: "wg-peer", Source: "wireguard"})
		ifc, _ := s.AddIface(ctx, dev, nil, nil)
		// The user's own static address on the peer.
		if _, err := s.AssignIP(ctx, ifc, sn, "10.6.0.50", "static"); err != nil {
			t.Fatal(err)
		}

		ips := func() map[string]string {
			rows, err := s.ListIPs(ctx, ifc)
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]string{}
			for _, r := range rows {
				m[r.IP] = r.Kind + "/" + r.Source
			}
			return m
		}

		add, rm, err := s.SyncIntegrationIPs(ctx, ifc, "wireguard",
			[]SubnetIP{{sn, "10.6.0.2"}, {sn, "10.6.0.3"}, {sn, "10.6.0.2"}})
		if err != nil || add != 2 || rm != 0 {
			t.Fatalf("first sync added=%d removed=%d err=%v", add, rm, err)
		}
		want := map[string]string{"10.6.0.2": "static/wireguard", "10.6.0.3": "static/wireguard", "10.6.0.50": "static/"}
		if got := ips(); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("after first sync %v, want %v", got, want)
		}

		// .3 leaves AllowedIPs, .4 joins, and the user's .50 is listed too: it
		// must stay the user's, not be taken over.
		add, rm, err = s.SyncIntegrationIPs(ctx, ifc, "wireguard",
			[]SubnetIP{{sn, "10.6.0.2"}, {sn, "10.6.0.4"}, {sn, "10.6.0.50"}})
		if err != nil || add != 1 || rm != 1 {
			t.Fatalf("second sync added=%d removed=%d err=%v", add, rm, err)
		}
		want = map[string]string{"10.6.0.2": "static/wireguard", "10.6.0.4": "static/wireguard", "10.6.0.50": "static/"}
		if got := ips(); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("after second sync %v, want %v", got, want)
		}

		// Nothing wanted: only the sync's rows go.
		if _, _, err := s.SyncIntegrationIPs(ctx, ifc, "wireguard", nil); err != nil {
			t.Fatal(err)
		}
		want = map[string]string{"10.6.0.50": "static/"}
		if got := ips(); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("after empty sync %v, want %v", got, want)
		}
	})
}

// Changing an IP's lease kind in the grid makes the row the user's, so the
// integration that created it no longer removes it.
func TestConformanceSetIPKindClaimsIntegrationIP(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		sn, _ := s.CreateSubnet(ctx, Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})
		dev, _ := s.CreateDevice(ctx, Device{Name: "p", Kind: "wg-peer", Source: "wireguard"})
		ifc, _ := s.AddIface(ctx, dev, nil, nil)
		s.SyncIntegrationIPs(ctx, ifc, "wireguard", []SubnetIP{{sn, "10.6.0.2"}})
		if err := s.SetIPKind(ctx, sn, "10.6.0.2", "dhcp"); err != nil {
			t.Fatal(err)
		}
		if _, rm, _ := s.SyncIntegrationIPs(ctx, ifc, "wireguard", nil); rm != 0 {
			t.Error("the sync removed an IP the user edited")
		}
	})
}

// The new event types are accepted, and widening the constraint again kept
// the types the alerts migration added.
func TestConformanceUpstreamEventTypes(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		for _, typ := range []string{"device_missing", "device_returned", "ip_conflict", "sync_recovered", "offline"} {
			if _, err := s.AddEvent(ctx, typ, nil, "x"); err != nil {
				t.Errorf("event type %s refused: %v", typ, err)
			}
		}
		if _, err := s.AddEvent(ctx, "bogus", nil, "x"); err == nil {
			t.Error("an unknown event type was accepted")
		}
	})
}

// Migration 0012 hands the WireGuard sync the rows it has always created — the
// static IPs of WireGuard peers inside WireGuard subnets — and nothing else.
// Events survive the table rebuild.
func TestMigration0012BackfillsWireGuardIPs(t *testing.T) {
	const seed = `INSERT INTO subnet (id,cidr,kind) VALUES (1,'10.6.0.0/24','wireguard');
		INSERT INTO subnet (id,cidr,kind) VALUES (2,'10.0.0.0/24','lan');
		INSERT INTO device (id,name,kind,source,wg_pubkey) VALUES (1,'peer','wg-peer','wireguard','k=');
		INSERT INTO device (id,name,kind,source) VALUES (2,'pc','computer','manual');
		INSERT INTO iface (id,device_id) VALUES (1,1);
		INSERT INTO iface (id,device_id) VALUES (2,2);
		INSERT INTO ip_assignment (id,iface_id,subnet_id,ip,kind) VALUES (1,1,1,'10.6.0.2','static');
		INSERT INTO ip_assignment (id,iface_id,subnet_id,ip,kind) VALUES (2,1,2,'10.0.0.9','static');
		INSERT INTO ip_assignment (id,iface_id,subnet_id,ip,kind) VALUES (3,2,1,'10.6.0.9','static');
		INSERT INTO ip_assignment (id,iface_id,subnet_id,ip,kind) VALUES (4,1,1,'10.6.0.3','dhcp');
		INSERT INTO event (id,type,device_id,details) VALUES (7,'offline',1,'kept');`
	check := func(t *testing.T, s *Store) {
		t.Helper()
		ctx := context.Background()
		want := map[int64]string{1: "wireguard", 2: "", 3: "", 4: ""}
		for id, src := range want {
			var got string
			if err := s.queryRow(ctx, `SELECT source FROM ip_assignment WHERE id=?`, id).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != src {
				t.Errorf("assignment %d source = %q, want %q", id, got, src)
			}
		}
		evs, err := s.ListEvents(ctx, 10)
		if err != nil || len(evs) != 1 || evs[0].ID != 7 || evs[0].Details != "kept" {
			t.Errorf("events after migration = %+v err=%v", evs, err)
		}
		if d, _ := s.GetDevice(ctx, 1); d.UpstreamMissingSince != nil {
			t.Error("existing device starts out marked missing")
		}
	}

	t.Run("sqlite", func(t *testing.T) {
		path := t.TempDir() + "/mig.db"
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		applyMigrationsBefore(t, db, "sqlite", "0012")
		if _, err := db.Exec(seed); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open (runs 0012): %v", err)
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
		applyMigrationsBefore(t, db, "postgres", "0012")
		if _, err := db.Exec(seed); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(scoped)
		if err != nil {
			t.Fatalf("Open (runs 0012): %v", err)
		}
		defer s.Close()
		check(t, s)
	})
}
