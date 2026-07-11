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
	if _, err := sync.RunOnce(context.Background()); err != nil {
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
	if _, err := sync.RunOnce(context.Background()); err != nil {
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
	if _, err := sync.RunOnce(context.Background()); err != nil {
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
	if _, err := sync.RunOnce(context.Background()); err != nil {
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
		if _, err := sync.RunOnce(context.Background()); err != nil {
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

func TestPiholeStartRecordsStatus(t *testing.T) {
	st, f, sync := testSync(t)
	f.leases = []Lease{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "laptop"}}
	// One iteration: record status via the same path Start uses.
	sync.recordStatus(sync.runAndCount(context.Background()))
	list, _ := st.ListIntegrationStatus()
	if len(list) != 1 || list[0].Name != "pihole" || !list[0].OK || list[0].ItemCount != 1 {
		t.Fatalf("status=%+v", list)
	}
}
