package store

import (
	"net/netip"
	"strings"
	"testing"
)

func TestSubnetCRUD(t *testing.T) {
	s := openTest(t)
	id, err := s.CreateSubnet(t.Context(), Subnet{CIDR: "192.168.1.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	sn, err := s.GetSubnet(t.Context(), id)
	if err != nil || sn.CIDR != "192.168.1.0/24" || !sn.ScanEnabled {
		t.Fatalf("get: %+v err=%v", sn, err)
	}
	sn.Name = "main"
	if err := s.UpdateSubnet(t.Context(), sn); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSubnets(t.Context())
	if err != nil || len(list) != 1 || list[0].Name != "main" {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	if err := s.DeleteSubnet(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.ListSubnets(t.Context()); len(list) != 0 {
		t.Fatalf("want empty, got %+v", list)
	}
}

// Subnets list by network address, numerically, IPv4 before IPv6.
func TestListSubnetsSortsByAddress(t *testing.T) {
	s := openTest(t)
	for _, cidr := range []string{"fd00::/64", "192.168.1.0/24", "10.10.10.0/24", "10.6.0.0/24", "10.6.0.0/16"} {
		if _, err := s.CreateSubnet(t.Context(), Subnet{CIDR: cidr, Name: cidr, Kind: "lan", ScanIntervalSec: 120}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListSubnets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, sn := range list {
		got = append(got, sn.CIDR)
	}
	want := []string{"10.6.0.0/16", "10.6.0.0/24", "10.10.10.0/24", "192.168.1.0/24", "fd00::/64"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// A subnet's DHCP pool is stored and read back, and InDHCPPool answers for
// the bounds themselves and for addresses either side of them.
func TestSubnetDHCPPool(t *testing.T) {
	s := openTest(t)
	id, err := s.CreateSubnet(t.Context(), Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120,
		DHCPStart: "10.0.0.100", DHCPEnd: "10.0.0.199"})
	if err != nil {
		t.Fatal(err)
	}
	sn, err := s.GetSubnet(t.Context(), id)
	if err != nil || sn.DHCPStart != "10.0.0.100" || sn.DHCPEnd != "10.0.0.199" {
		t.Fatalf("get: %+v err=%v", sn, err)
	}
	for ip, want := range map[string]bool{"10.0.0.99": false, "10.0.0.100": true, "10.0.0.150": true, "10.0.0.199": true, "10.0.0.200": false} {
		if got := sn.InDHCPPool(netip.MustParseAddr(ip)); got != want {
			t.Errorf("InDHCPPool(%s) = %v, want %v", ip, got, want)
		}
	}
	sn.DHCPStart, sn.DHCPEnd = "", ""
	if err := s.UpdateSubnet(t.Context(), sn); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSubnets(t.Context())
	if err != nil || len(list) != 1 || list[0].DHCPStart != "" || list[0].InDHCPPool(netip.MustParseAddr("10.0.0.150")) {
		t.Fatalf("after clearing the pool: %+v err=%v", list, err)
	}
}

// An integration sets a subnet's pool only while nobody else owns it: not
// over a pool the user typed, nor over another integration's. It keeps its
// own pool in step, and reports whether anything changed.
func TestSetDHCPPoolFrom(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		id, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
		if err != nil {
			t.Fatal(err)
		}
		set := func(start, end, source string) bool {
			t.Helper()
			changed, err := s.SetDHCPPoolFrom(ctx, id, start, end, source)
			if err != nil {
				t.Fatal(err)
			}
			return changed
		}
		pool := func() Subnet {
			t.Helper()
			sn, err := s.GetSubnet(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			return sn
		}

		if !set("10.0.0.100", "10.0.0.199", "opnsense") {
			t.Fatal("an unowned pool was not set")
		}
		if sn := pool(); sn.DHCPStart != "10.0.0.100" || sn.DHCPEnd != "10.0.0.199" || sn.DHCPPoolSource != "opnsense" {
			t.Fatalf("after first set: %+v", sn)
		}
		if set("10.0.0.100", "10.0.0.199", "opnsense") {
			t.Error("the same pool again reported a change")
		}
		if !set("10.0.0.50", "10.0.0.99", "opnsense") {
			t.Error("the owner could not move its pool")
		}
		if set("10.0.0.1", "10.0.0.9", "pihole") {
			t.Error("another integration took over the pool")
		}

		sn := pool()
		sn.DHCPStart, sn.DHCPEnd, sn.DHCPPoolSource = "10.0.0.200", "10.0.0.250", "user"
		if err := s.UpdateSubnet(ctx, sn); err != nil {
			t.Fatal(err)
		}
		if set("10.0.0.50", "10.0.0.99", "opnsense") {
			t.Error("an integration overwrote the user's pool")
		}
		if sn := pool(); sn.DHCPStart != "10.0.0.200" || sn.DHCPPoolSource != "user" {
			t.Fatalf("user pool changed: %+v", sn)
		}

		list, err := s.ListSubnets(ctx)
		if err != nil || len(list) != 1 || list[0].DHCPPoolSource != "user" {
			t.Fatalf("list: %+v err=%v", list, err)
		}
	})
}
