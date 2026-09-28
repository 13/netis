package store

import (
	"context"
	"testing"
)

// A port scan result replaces what was recorded: closed ports go, ports still
// open keep their first sighting.
func TestConformanceReplaceOpenPorts(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		devID, err := s.CreateDevice(ctx, Device{Name: "box", Kind: "server", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		ifID, err := s.AddIface(ctx, devID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceOpenPorts(ctx, ifID, "tcp",
			[]OpenPort{{Port: 22, ServiceGuess: "ssh"}, {Port: 80, ServiceGuess: "http"}}, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if err := s.ReplaceOpenPorts(ctx, ifID, "tcp",
			[]OpenPort{{Port: 22, ServiceGuess: "ssh"}, {Port: 443, ServiceGuess: "https"}}, "2026-01-02T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		got, err := s.ListOpenPorts(ctx, ifID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Port != 22 || got[1].Port != 443 {
			t.Fatalf("ports = %+v, want 22 and 443", got)
		}
		if got[0].FirstSeen != "2026-01-01T00:00:00Z" || got[0].LastSeen != "2026-01-02T00:00:00Z" {
			t.Errorf("port 22 = %+v, want first seen kept and last seen updated", got[0])
		}

		// Nothing open any more clears the list.
		if err := s.ReplaceOpenPorts(ctx, ifID, "tcp", nil, "2026-01-03T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.ListOpenPorts(ctx, ifID); len(got) != 0 {
			t.Fatalf("ports after empty scan = %+v, want none", got)
		}
	})
}

// A manual device, its interface and its IP are created together or not at
// all.
func TestConformanceCreateDeviceWithIfaceIsAtomic(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		snID, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.6.0.0/24", Kind: "lan", ScanIntervalSec: 60})
		if err != nil {
			t.Fatal(err)
		}
		mac := "ee:00:00:00:00:09"
		devID, err := s.CreateDeviceWithIface(ctx, Device{Name: "nas", Kind: "server", Source: "manual"},
			&mac, snID, "10.6.0.9", "static")
		if err != nil {
			t.Fatal(err)
		}
		iface, ok, err := s.FindIfaceByIP(ctx, snID, "10.6.0.9")
		if err != nil || !ok || iface.DeviceID != devID || iface.MAC == nil || *iface.MAC != mac {
			t.Fatalf("iface = %+v ok=%v err=%v", iface, ok, err)
		}

		// No MAC and no IP: just the device.
		bare, err := s.CreateDeviceWithIface(ctx, Device{Name: "bare", Kind: "other", Source: "manual"}, nil, 0, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if ifs, _ := s.ListIfaces(ctx, bare); len(ifs) != 0 {
			t.Errorf("bare device ifaces = %+v, want none", ifs)
		}

		// A duplicate MAC rolls the device back with it.
		before, _ := s.ListDevices(ctx)
		_, err = s.CreateDeviceWithIface(ctx, Device{Name: "dupe", Kind: "other", Source: "manual"},
			&mac, snID, "10.6.0.10", "static")
		if !IsUniqueViolation(err) {
			t.Fatalf("duplicate MAC err = %v, want a unique violation", err)
		}
		if after, _ := s.ListDevices(ctx); len(after) != len(before) {
			t.Errorf("device count went %d -> %d; the failed create left a device", len(before), len(after))
		}
	})
}
