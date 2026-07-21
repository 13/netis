package store

import (
	"testing"
	"time"
)

func TestSetIPKindAndOccupancyKind(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(t.Context(), Subnet{CIDR: "10.0.0.0/30", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(t.Context(), Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := s.AddIface(t.Context(), devID, nil, nil)
	if _, err := s.AssignIP(t.Context(), ifID, snID, "10.0.0.1", "static"); err != nil {
		t.Fatal(err)
	}

	occ, _ := s.SubnetOccupancy(t.Context(), snID)
	if occ["10.0.0.1"].Kind != "static" {
		t.Fatalf("kind=%q, want static", occ["10.0.0.1"].Kind)
	}
	if err := s.SetIPKind(t.Context(), snID, "10.0.0.1", "dhcp"); err != nil {
		t.Fatal(err)
	}
	occ2, _ := s.SubnetOccupancy(t.Context(), snID)
	if occ2["10.0.0.1"].Kind != "dhcp" {
		t.Fatalf("kind after SetIPKind=%q, want dhcp", occ2["10.0.0.1"].Kind)
	}
}

func TestSubnetOccupancy(t *testing.T) {
	s := openTest(t)
	snID, err := s.CreateSubnet(t.Context(), Subnet{CIDR: "10.0.0.0/30", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	devID, err := s.CreateDevice(t.Context(), Device{Name: "gw", Kind: "other", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	ifID, err := s.AddIface(t.Context(), devID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignIP(t.Context(), ifID, snID, "10.0.0.1", "static"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkSeen(t.Context(), ifID, 1, time.Now()); err != nil {
		t.Fatal(err)
	}

	occ, err := s.SubnetOccupancy(t.Context(), snID)
	if err != nil {
		t.Fatal(err)
	}
	o, ok := occ["10.0.0.1"]
	if !ok {
		t.Fatalf("missing occupant for 10.0.0.1: %+v", occ)
	}
	if o.DeviceID != devID || o.DeviceName != "gw" || !o.Online || !o.EverSeen || o.Count != 1 {
		t.Fatalf("occupant=%+v", o)
	}

	// Second iface claiming the same IP -> conflict, Count=2.
	devID2, _ := s.CreateDevice(t.Context(), Device{Name: "dup", Kind: "other", Source: "manual"})
	ifID2, _ := s.AddIface(t.Context(), devID2, nil, nil)
	if _, err := s.AssignIP(t.Context(), ifID2, snID, "10.0.0.1", "static"); err != nil {
		t.Fatal(err)
	}
	occ2, err := s.SubnetOccupancy(t.Context(), snID)
	if err != nil {
		t.Fatal(err)
	}
	if occ2["10.0.0.1"].Count != 2 {
		t.Fatalf("expected conflict count=2, got %+v", occ2["10.0.0.1"])
	}
}
