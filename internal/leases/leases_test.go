package leases

import (
	"testing"

	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

// fixture is a store with one 10.0.0.0/24 subnet and an apply helper that
// records leases as source "adguard" (any accepted source would do).
type fixture struct {
	t    *testing.T
	st   *store.Store
	ev   *events.Service
	snID int64
}

func newFixture(t *testing.T, st *store.Store) *fixture {
	t.Helper()
	snID, err := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, st: st, ev: events.NewService(st, events.NewBroker()), snID: snID}
}

func (f *fixture) apply(source string, static, dynamic []Entry) int {
	f.t.Helper()
	subnets, err := f.st.ListSubnets(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	n, err := Apply(f.t.Context(), f.st, f.ev, source, subnets, static, dynamic)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// ipsOf maps each IP on mac's iface to its kind.
func (f *fixture) ipsOf(mac string) map[string]string {
	f.t.Helper()
	iface, ok, err := f.st.FindIfaceByMAC(f.t.Context(), mac)
	if err != nil || !ok {
		f.t.Fatalf("iface %s: ok=%v err=%v", mac, ok, err)
	}
	ips, err := f.st.ListIPs(f.t.Context(), iface.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]string{}
	for _, ip := range ips {
		out[ip.IP] = ip.Kind
	}
	return out
}

func strp(s string) *string { return &s }

func TestApplyCreatesDevicesWithSource(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		f := newFixture(t, st)
		n := f.apply("opnsense",
			[]Entry{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}},
			[]Entry{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10"}})
		if n != 2 {
			t.Fatalf("created %d, want 2", n)
		}
		rows, err := st.ListDevices(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]string{}
		for _, r := range rows {
			names[r.Name] = r.Source
		}
		// A lease without a hostname is named after its source and MAC.
		if names["printer"] != "opnsense" || names["opnsense-aa:bb:cc:00:00:10"] != "opnsense" {
			t.Fatalf("devices = %v", names)
		}
		if got := f.ipsOf("aa:bb:cc:00:00:20"); got["10.0.0.20"] != "static" {
			t.Fatalf("reservation ips = %v, want static", got)
		}
		if got := f.ipsOf("aa:bb:cc:00:00:10"); got["10.0.0.10"] != "dhcp" {
			t.Fatalf("lease ips = %v, want dhcp", got)
		}
		// The rows the source created carry its label.
		iface, _, _ := st.FindIfaceByMAC(t.Context(), "aa:bb:cc:00:00:10")
		if ips, _ := st.ListIPs(t.Context(), iface.ID); len(ips) != 1 || ips[0].Source != "opnsense" {
			t.Fatalf("ip rows = %+v, want source opnsense", ips)
		}
		evs, _ := st.ListEvents(t.Context(), 5)
		if len(evs) != 2 || evs[0].Type != "device_new" {
			t.Fatalf("events = %+v", evs)
		}
		// A second identical run creates nothing and duplicates nothing.
		if n := f.apply("opnsense",
			[]Entry{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}},
			[]Entry{{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10"}}); n != 0 {
			t.Fatalf("re-run created %d", n)
		}
		if got := f.ipsOf("aa:bb:cc:00:00:20"); len(got) != 1 {
			t.Fatalf("re-run duplicated assignment: %v", got)
		}
	})
}

// A known MAC is enriched, never renamed or re-sourced, and its hostname is
// only filled when empty.
func TestApplyEnrichesWithoutClobbering(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		f := newFixture(t, st)
		ctx := t.Context()
		devID, _ := st.CreateDevice(ctx, store.Device{Name: "known", Kind: "computer", Source: "manual"})
		if _, err := st.AddIface(ctx, devID, strp("aa:bb:cc:00:00:10"), strp("my-name")); err != nil {
			t.Fatal(err)
		}
		devID2, _ := st.CreateDevice(ctx, store.Device{Name: "bare", Kind: "computer", Source: "scan"})
		if _, err := st.AddIface(ctx, devID2, strp("aa:bb:cc:00:00:11"), nil); err != nil {
			t.Fatal(err)
		}
		if n := f.apply("adguard", nil, []Entry{
			{MAC: "aa:bb:cc:00:00:10", IP: "10.0.0.10", Hostname: "dhcp-name"},
			{MAC: "aa:bb:cc:00:00:11", IP: "10.0.0.11", Hostname: "filled"},
		}); n != 0 {
			t.Fatalf("created %d, want 0", n)
		}
		d, _ := st.GetDevice(ctx, devID)
		if d.Name != "known" || d.Source != "manual" {
			t.Fatalf("device clobbered: %+v", d)
		}
		ifs, _ := st.ListIfaces(ctx, devID)
		if *ifs[0].Hostname != "my-name" {
			t.Fatalf("hostname clobbered: %q", *ifs[0].Hostname)
		}
		ifs2, _ := st.ListIfaces(ctx, devID2)
		if ifs2[0].Hostname == nil || *ifs2[0].Hostname != "filled" {
			t.Fatalf("empty hostname not filled: %+v", ifs2[0])
		}
		if got := f.ipsOf("aa:bb:cc:00:00:10"); got["10.0.0.10"] != "dhcp" {
			t.Fatalf("ip not attached: %v", got)
		}
	})
}

// A reservation and a lease for the same address leave it static, and a
// lease never downgrades a static address the user set.
func TestApplyStaticNeverDowngraded(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		f := newFixture(t, st)
		ctx := t.Context()
		f.apply("adguard",
			[]Entry{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20"}},
			[]Entry{{MAC: "aa:bb:cc:00:00:20", IP: "10.0.0.20", Hostname: "printer"}})
		if got := f.ipsOf("aa:bb:cc:00:00:20"); len(got) != 1 || got["10.0.0.20"] != "static" {
			t.Fatalf("reservation should win, got %v", got)
		}
		// The lease still filled the hostname the reservation lacked.
		iface, _, _ := st.FindIfaceByMAC(ctx, "aa:bb:cc:00:00:20")
		if iface.Hostname == nil || *iface.Hostname != "printer" {
			t.Fatalf("hostname = %v, want printer", iface.Hostname)
		}

		devID, _ := st.CreateDevice(ctx, store.Device{Name: "gw", Kind: "router", Source: "manual"})
		ifID, _ := st.AddIface(ctx, devID, strp("aa:bb:cc:00:00:40"), nil)
		if _, err := st.AssignIP(ctx, ifID, f.snID, "10.0.0.40", "static"); err != nil {
			t.Fatal(err)
		}
		f.apply("opnsense", nil, []Entry{{MAC: "aa:bb:cc:00:00:40", IP: "10.0.0.40"}})
		if got := f.ipsOf("aa:bb:cc:00:00:40"); got["10.0.0.40"] != "static" {
			t.Fatalf("lease downgraded a manual static: %v", got)
		}
		// ...and the user's row stays the user's.
		if ips, _ := st.ListIPs(ctx, ifID); ips[0].Source != "" {
			t.Fatalf("user row relabelled %q", ips[0].Source)
		}
	})
}

// A moved lease retires the old DHCP address and takes the new one off its
// previous DHCP holder; static addresses survive.
func TestApplyLeaseMoveRetiresOldAddress(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		f := newFixture(t, st)
		ctx := t.Context()
		const a, b = "aa:bb:cc:00:00:01", "aa:bb:cc:00:00:02"
		f.apply("adguard", nil, []Entry{{MAC: a, IP: "10.0.0.10"}, {MAC: b, IP: "10.0.0.12"}})
		ia, _, _ := st.FindIfaceByMAC(ctx, a)
		if _, err := st.AssignIP(ctx, ia.ID, f.snID, "10.0.0.200", "static"); err != nil {
			t.Fatal(err)
		}
		f.apply("adguard", nil, []Entry{{MAC: a, IP: "10.0.0.12"}})
		if got := f.ipsOf(a); len(got) != 2 || got["10.0.0.12"] != "dhcp" || got["10.0.0.200"] != "static" {
			t.Fatalf("a ips = %v", got)
		}
		if got := f.ipsOf(b); len(got) != 0 {
			t.Fatalf("b kept its stale claim: %v", got)
		}
	})
}

func TestApplySkipsOutsideSubnets(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		f := newFixture(t, st)
		if n := f.apply("adguard",
			[]Entry{{MAC: "aa:bb:cc:00:00:01", IP: "192.168.9.1"}},
			[]Entry{{MAC: "aa:bb:cc:00:00:02", IP: "192.168.9.2"}}); n != 0 {
			t.Fatalf("created %d outside every subnet", n)
		}
		rows, _ := st.ListDevices(t.Context())
		if len(rows) != 0 {
			t.Fatalf("devices = %+v", rows)
		}
	})
}

func TestNorm(t *testing.T) {
	for in, want := range map[string]string{
		"AA:BB:CC:00:00:01": "aa:bb:cc:00:00:01",
		"aa-bb-cc-00-00-01": "aa:bb:cc:00:00:01",
		"aa:bb:cc:00:00":    "",
		"":                  "",
	} {
		if got := NormMAC(in); got != want {
			t.Errorf("NormMAC(%q) = %q, want %q", in, got, want)
		}
	}
	if NormIP(" 10.0.0.1 ") != "10.0.0.1" || NormIP("nope") != "" {
		t.Fatal("NormIP")
	}
}
