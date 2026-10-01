package leases

import (
	"testing"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

// ApplyPools gives each subnet the largest range that lies wholly inside it,
// skips ranges that fit no subnet or are malformed, and leaves a pool the
// user set alone.
func TestApplyPools(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		lan, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
		if err != nil {
			t.Fatal(err)
		}
		iot, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.1.0/24", Kind: "lan", ScanIntervalSec: 120})
		if err != nil {
			t.Fatal(err)
		}
		mine, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.2.0/24", Kind: "lan", ScanIntervalSec: 120,
			DHCPStart: "10.0.2.10", DHCPEnd: "10.0.2.20", DHCPPoolSource: "user"})
		if err != nil {
			t.Fatal(err)
		}
		subnets, err := st.ListSubnets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n, err := ApplyPools(ctx, st, "opnsense", subnets, []Range{
			{Start: "10.0.0.10", End: "10.0.0.19"},   // smaller, loses
			{Start: "10.0.0.100", End: "10.0.0.199"}, // largest in lan
			{Start: "10.0.1.250", End: "10.0.2.5"},   // straddles two subnets
			{Start: "10.0.1.50", End: "10.0.1.40"},   // backwards
			{Start: "bogus", End: "10.0.1.60"},
			{Start: "192.168.9.1", End: "192.168.9.9"}, // no subnet
			{Start: "10.0.2.100", End: "10.0.2.200"},   // user owns this pool
		})
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("changed %d subnets, want 1", n)
		}
		want := map[int64][3]string{
			lan:  {"10.0.0.100", "10.0.0.199", "opnsense"},
			iot:  {"", "", ""},
			mine: {"10.0.2.10", "10.0.2.20", "user"},
		}
		for id, w := range want {
			sn, err := st.GetSubnet(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got := [3]string{sn.DHCPStart, sn.DHCPEnd, sn.DHCPPoolSource}; got != w {
				t.Errorf("subnet %s pool = %v, want %v", sn.CIDR, got, w)
			}
		}
	})
}

// ParseRange reads the shapes DHCP servers write a pool in: "a-b" (spaces
// allowed) and a CIDR block, which stands for its first to last address.
func TestParseRange(t *testing.T) {
	for in, want := range map[string]Range{
		"10.0.0.100-10.0.0.199":    {Start: "10.0.0.100", End: "10.0.0.199"},
		" 10.0.0.100 - 10.0.0.199": {Start: "10.0.0.100", End: "10.0.0.199"},
		"10.0.0.128/26":            {Start: "10.0.0.128", End: "10.0.0.191"},
		"10.0.0.130/26":            {Start: "10.0.0.128", End: "10.0.0.191"},
	} {
		got, ok := ParseRange(in)
		if !ok || got != want {
			t.Errorf("ParseRange(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "10.0.0.1", "10.0.0.1-", "x-y", "10.0.0.9-10.0.0.1", "10.0.0.1-fd00::1"} {
		if got, ok := ParseRange(in); ok {
			t.Errorf("ParseRange(%q) = %v, want not ok", in, got)
		}
	}
}
