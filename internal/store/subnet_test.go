package store

import (
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
