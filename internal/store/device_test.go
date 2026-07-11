package store

import "testing"

func strp(s string) *string { return &s }

func TestDeviceIfaceIP(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, err := s.CreateDevice(Device{Name: "nas", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	ifID, err := s.AddIface(devID, strp("aa:bb:cc:dd:ee:ff"), strp("nas.local"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignIP(ifID, snID, "10.0.0.5", "static"); err != nil {
		t.Fatal(err)
	}

	iface, ok, err := s.FindIfaceByMAC("aa:bb:cc:dd:ee:ff")
	if err != nil || !ok || iface.DeviceID != devID {
		t.Fatalf("byMAC: %+v ok=%v err=%v", iface, ok, err)
	}
	iface, ok, err = s.FindIfaceByIP(snID, "10.0.0.5")
	if err != nil || !ok || iface.ID != ifID {
		t.Fatalf("byIP: %+v ok=%v err=%v", iface, ok, err)
	}
	if _, ok, _ = s.FindIfaceByIP(snID, "10.0.0.99"); ok {
		t.Fatal("unexpected hit")
	}

	rows, err := s.ListDevices()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %+v err=%v", rows, err)
	}
	if rows[0].Name != "nas" || len(rows[0].IPs) != 1 || rows[0].IPs[0] != "10.0.0.5" {
		t.Fatalf("row: %+v", rows[0])
	}

	if err := s.DeleteDevice(devID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FindIfaceByMAC("aa:bb:cc:dd:ee:ff"); ok {
		t.Fatal("iface should cascade-delete")
	}
}

func TestRemoveIfaceIPsKeepsStatic(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, err := s.CreateDevice(Device{Name: "nas", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	ifID, err := s.AddIface(devID, strp("aa:bb:cc:dd:ee:ff"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignIP(ifID, snID, "10.0.0.5", "static"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignIP(ifID, snID, "10.0.0.6", "dhcp"); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveIfaceIPsInSubnetExcept(ifID, snID, "10.0.0.7"); err != nil {
		t.Fatal(err)
	}

	ips, err := s.ListIPs(ifID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ip := range ips {
		got = append(got, ip.IP)
	}
	if len(got) != 1 || got[0] != "10.0.0.5" {
		t.Fatalf("ips after cleanup = %+v, want only static 10.0.0.5 kept", got)
	}
}

func TestUpsertIPAssignment(t *testing.T) {
	s := openTest(t)
	snID, _ := s.CreateSubnet(Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "pihole"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:01"), nil)

	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.5", "dhcp"); err != nil {
		t.Fatal(err)
	}
	// Re-run: no duplicate row.
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.5", "dhcp"); err != nil {
		t.Fatal(err)
	}
	ips, _ := s.ListIPs(ifID)
	if len(ips) != 1 {
		t.Fatalf("want 1 assignment, got %d: %+v", len(ips), ips)
	}
	// Upgrade dhcp -> static.
	if err := s.UpsertIPAssignment(ifID, snID, "10.0.0.5", "static"); err != nil {
		t.Fatal(err)
	}
	ips, _ = s.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "static" {
		t.Fatalf("want single static assignment, got %+v", ips)
	}
}

func TestSetIfaceHostnameIfEmpty(t *testing.T) {
	s := openTest(t)
	devID, _ := s.CreateDevice(Device{Name: "d", Kind: "other", Source: "pihole"})
	ifID, _ := s.AddIface(devID, strp("aa:bb:cc:00:00:02"), nil)

	if err := s.SetIfaceHostnameIfEmpty(ifID, "nas.lan"); err != nil {
		t.Fatal(err)
	}
	ifaces, _ := s.ListIfaces(devID)
	if ifaces[0].Hostname == nil || *ifaces[0].Hostname != "nas.lan" {
		t.Fatalf("hostname not set: %+v", ifaces[0])
	}
	// Must not overwrite an existing hostname.
	if err := s.SetIfaceHostnameIfEmpty(ifID, "other.lan"); err != nil {
		t.Fatal(err)
	}
	ifaces, _ = s.ListIfaces(devID)
	if *ifaces[0].Hostname != "nas.lan" {
		t.Fatalf("hostname was overwritten: %q", *ifaces[0].Hostname)
	}
}
