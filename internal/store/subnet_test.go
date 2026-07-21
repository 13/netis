package store

import "testing"

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
