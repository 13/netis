package scan

import (
	"context"
	"strings"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type fakeSweeper struct {
	results []Result
	calls   int
}

func (f *fakeSweeper) Sweep(ctx context.Context, cidr string) ([]Result, error) {
	f.calls++
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
		Store:   st,
		Events:  events.NewService(st, b),
		Broker:  b,
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
		if ip.IP == "10.0.0.42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new IP missing: %+v", rows[0].IPs)
	}
}

// TestAvailabilityStableAcrossIPChange guards against the closing loop in
// RunSubnet mis-marking a MAC-matched iface missed on the same sweep it
// changed IP: the iface's old IP is no longer alive, but the iface itself
// was seen (on its new IP), so it must not be recorded as down. It also
// verifies the stale old IP is retired from the subnet's occupancy.
func TestAvailabilityStableAcrossIPChange(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(snID)
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}

	iface, ok, err := st.FindIfaceByMAC("bc:24:11:00:00:01")
	if err != nil || !ok {
		t.Fatalf("iface not found: ok=%v err=%v", ok, err)
	}
	ifID := iface.ID

	// same MAC shows up at a new IP in the very next sweep
	e.ARP = func() (map[string]string, error) {
		return map[string]string{"10.0.0.42": "bc:24:11:00:00:01"}, nil
	}
	fs.results = []Result{{IP: "10.0.0.42", Alive: true, RTTms: 1.0}}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}

	pct, err := st.AvailabilityPct(ifID, "1970-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if pct != 100.0 {
		t.Fatalf("availability degraded across IP change: got %v, want 100", pct)
	}

	known, err := st.ListSubnetIfaceIPs(snID)
	if err != nil {
		t.Fatal(err)
	}
	var haveOld, haveNew bool
	for _, k := range known {
		if k.IP == "10.0.0.9" {
			haveOld = true
		}
		if k.IP == "10.0.0.42" {
			haveNew = true
		}
	}
	if haveOld {
		t.Fatalf("stale old IP 10.0.0.9 still present in subnet occupancy: %+v", known)
	}
	if !haveNew {
		t.Fatalf("new IP 10.0.0.42 missing from subnet occupancy: %+v", known)
	}
}

// TestTriggerSkipsDisabledSubnet ensures the manual Trigger path is subject
// to the same scan_enabled/wireguard guard as the periodic tick path: a
// triggered scan on a disabled subnet must not sweep or create side effects.
func TestTriggerSkipsDisabledSubnet(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(snID)
	sn.ScanEnabled = false
	if err := st.UpdateSubnet(sn); err != nil {
		t.Fatal(err)
	}
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}

	sched := NewScheduler(e, st)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		sched.Start(ctx)
		close(done)
	}()

	sched.Trigger(snID)
	<-done // wait for Start to return; establishes happens-before for fs.calls

	if fs.calls != 0 {
		t.Fatalf("sweeper called %d times for a disabled subnet", fs.calls)
	}
	rows, err := st.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("devices created for a disabled subnet: %+v", rows)
	}
}

func TestSchedulerRecordsScanStatus(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}
	sn, _ := st.GetSubnet(snID)
	sched := NewScheduler(e, st)
	sched.run(context.Background(), sn) // one sweep
	list, _ := st.ListIntegrationStatus()
	if len(list) != 1 || list[0].Name != "scan" || !list[0].OK {
		t.Fatalf("scan status=%+v", list)
	}
	if !strings.Contains(list[0].Detail, sn.CIDR) {
		t.Fatalf("detail should mention the subnet: %q", list[0].Detail)
	}
}
