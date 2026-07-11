package scan

import (
	"context"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type fakeSweeper struct{ results []Result }

func (f *fakeSweeper) Sweep(ctx context.Context, cidr string) ([]Result, error) {
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
		if ip == "10.0.0.42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new IP missing: %+v", rows[0].IPs)
	}
}
