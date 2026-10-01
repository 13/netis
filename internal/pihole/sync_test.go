package pihole

import (
	"context"
	"errors"
	"testing"

	"netis/internal/events"
	"netis/internal/leases"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

type fakeFetcher struct {
	leases  []Lease
	res     []Reservation
	dns     []DNSRecord
	pools   []leases.Range
	poolErr error
}

func (f *fakeFetcher) Leases(context.Context) ([]Lease, error)             { return f.leases, nil }
func (f *fakeFetcher) Reservations(context.Context) ([]Reservation, error) { return f.res, nil }
func (f *fakeFetcher) DNSRecords(context.Context) ([]DNSRecord, error)     { return f.dns, nil }
func (f *fakeFetcher) DHCPPool(context.Context) ([]leases.Range, error)    { return f.pools, f.poolErr }

func testSync(t *testing.T) (*store.Store, *fakeFetcher, *Sync) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	f := &fakeFetcher{}
	return st, f, NewSync(st, f, events.NewService(st, events.NewBroker()))
}

func deviceByName(t *testing.T, st *store.Store, name string) store.DeviceRow {
	t.Helper()
	rows, _ := st.ListDevices(t.Context())
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
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := deviceByName(t, st, "printer")
	if d.Source != "pihole" || len(d.IPs) != 1 || d.IPs[0].IP != "10.0.0.20" {
		t.Fatalf("device=%+v", d)
	}
	// reservation → static
	ifaces, _ := st.ListIfaces(t.Context(), d.ID)
	ips, _ := st.ListIPs(t.Context(), ifaces[0].ID)
	if ips[0].Kind != "static" {
		t.Fatalf("want static, got %q", ips[0].Kind)
	}
	evs, _ := st.ListEvents(t.Context(), 5)
	if len(evs) != 1 || evs[0].Type != "device_new" {
		t.Fatalf("events=%+v", evs)
	}
}

func TestSyncEnrichesExistingByMAC(t *testing.T) {
	st, f, sync := testSync(t)
	// Pre-existing device from another source with the same MAC.
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "known", Kind: "computer", Source: "scan"})
	st.AddIface(t.Context(), devID, strpP("aa:bb:cc:00:00:10"), nil)
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "laptop"}}
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListDevices(t.Context())
	if len(rows) != 1 {
		t.Fatalf("must not create a second device: %+v", rows)
	}
	d := deviceByName(t, st, "known")
	if len(d.IPs) != 1 || d.IPs[0].IP != "10.0.0.10" {
		t.Fatalf("IP not attached: %+v", d.IPs)
	}
	ifaces, _ := st.ListIfaces(t.Context(), d.ID)
	if ifaces[0].Hostname == nil || *ifaces[0].Hostname != "laptop" {
		t.Fatalf("hostname not set: %+v", ifaces[0])
	}
}

func TestReservationBeatsLease(t *testing.T) {
	st, f, sync := testSync(t)
	f.res = []Reservation{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}}
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := deviceByName(t, st, "printer")
	ifaces, _ := st.ListIfaces(t.Context(), d.ID)
	ips, _ := st.ListIPs(t.Context(), ifaces[0].ID)
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
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListDevices(t.Context())
	if len(rows) != 1 {
		t.Fatalf("DNS must not create a device: %+v", rows)
	}
	d := rows[0]
	fields, _ := st.ListCustomFields(t.Context(), d.ID)
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
		if _, err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	d := deviceByName(t, st, "printer")
	ifaces, _ := st.ListIfaces(t.Context(), d.ID)
	ips, _ := st.ListIPs(t.Context(), ifaces[0].ID)
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

func TestPiholeRunOnceStats(t *testing.T) {
	_, f, sync := testSync(t)
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

func TestPiholeStatsStatus(t *testing.T) {
	count, detail := Stats{Leases: 48, Created: 2}.Status()
	if count != 48 || detail != "48 leases, 2 new" {
		t.Fatalf("status = %d %q", count, detail)
	}
}

func TestPiholeLeaseKeepsManualStatic(t *testing.T) {
	st, f, sync := testSync(t)
	subnets, _ := st.ListSubnets(t.Context())
	snID := subnets[0].ID
	// A device the user marked static, whose MAC pihole will report as a lease.
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(t.Context(), devID, strpP("aa:bb:cc:00:00:40"), nil)
	st.AssignIP(t.Context(), ifID, snID, "10.0.0.40", "static")

	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:40", IP: "10.0.0.40", Hostname: "gw"}}
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	ips, _ := st.ListIPs(t.Context(), ifID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("pihole lease must not downgrade a manual static, got %+v", ips)
	}
}

// A lease that moves to a new address retires the old DHCP address, and when
// that old address is later leased to a different MAC, only the new holder
// claims it. A static assignment on the moving iface is kept.
func TestLeaseMoveRetiresOldDHCPAddress(t *testing.T) {
	storetest.EachDialect(t, testLeaseMoveRetiresOldDHCPAddress)
}

func testLeaseMoveRetiresOldDHCPAddress(t *testing.T, st *store.Store) {
	ctx := t.Context()
	snID, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{}
	sync := NewSync(st, f, events.NewService(st, events.NewBroker()))
	run := func() {
		t.Helper()
		if _, err := sync.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	ipsOf := func(mac string) map[string]string {
		t.Helper()
		iface, ok, err := st.FindIfaceByMAC(ctx, mac)
		if err != nil || !ok {
			t.Fatalf("iface %s: ok=%v err=%v", mac, ok, err)
		}
		ips, err := st.ListIPs(ctx, iface.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, ip := range ips {
			out[ip.IP] = ip.Kind
		}
		return out
	}

	const a, b = "aa:bb:cc:00:00:01", "aa:bb:cc:00:00:02"
	f.leases = []Lease{{MAC: a, IP: "10.0.0.10", Hostname: "alpha"}}
	run()
	// The user pins a static address on the same iface; it must survive.
	ia, _, _ := st.FindIfaceByMAC(ctx, a)
	if _, err := st.AssignIP(ctx, ia.ID, snID, "10.0.0.200", "static"); err != nil {
		t.Fatal(err)
	}

	f.leases = []Lease{{MAC: a, IP: "10.0.0.11", Hostname: "alpha"}}
	run()
	if got := ipsOf(a); len(got) != 2 || got["10.0.0.11"] != "dhcp" || got["10.0.0.200"] != "static" {
		t.Fatalf("after move, alpha ips = %v, want 10.0.0.11 dhcp + 10.0.0.200 static", got)
	}

	// Another device's stale DHCP claim on an address is dropped when the
	// address is leased to someone else: b's lease lapses (b drops out of the
	// lease list) and its address goes to a.
	f.leases = []Lease{{MAC: a, IP: "10.0.0.11"}, {MAC: b, IP: "10.0.0.12"}}
	run()
	f.leases = []Lease{{MAC: a, IP: "10.0.0.12"}}
	run()
	iface, ok, err := st.FindIfaceByIP(ctx, snID, "10.0.0.12")
	if err != nil || !ok || iface.MAC == nil || *iface.MAC != a {
		t.Fatalf("10.0.0.12 owner = %+v ok=%v err=%v, want %s", iface, ok, err, a)
	}
	rows, err := st.ListSubnetIfaceIPs(ctx, snID)
	if err != nil {
		t.Fatal(err)
	}
	var claims int
	for _, row := range rows {
		if row.IP == "10.0.0.12" {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("10.0.0.12 claimed by %d ifaces, want 1", claims)
	}
}

// The sync gives the subnet Pi-hole's pool. A pool it cannot read does not
// fail the run: the leases are still worth having.
func TestSyncAppliesPool(t *testing.T) {
	st, f, sync := testSync(t)
	f.pools = []leases.Range{{Start: "10.0.0.100", End: "10.0.0.199"}}
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListSubnets(t.Context())
	if sn := list[0]; sn.DHCPStart != "10.0.0.100" || sn.DHCPEnd != "10.0.0.199" || sn.DHCPPoolSource != "pihole" {
		t.Fatalf("subnet = %+v", sn)
	}

	f.pools, f.poolErr = nil, errors.New("pihole /api/config/dhcp: HTTP 403")
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "laptop"}}
	if _, err := sync.RunOnce(context.Background()); err != nil {
		t.Fatalf("a pool read failure failed the run: %v", err)
	}
	deviceByName(t, st, "laptop")
}
