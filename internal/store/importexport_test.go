package store

import (
	"reflect"
	"testing"
)

// FillDevice enriches and never clobbers: empty fields fill, set ones stay,
// and name/kind change only on an unreviewed scan guess.
func TestFillDevice(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		guess, _ := s.CreateDevice(ctx, Device{Name: "10.0.0.5", Kind: "other", Source: "scan"})
		reviewed, _ := s.CreateDevice(ctx, Device{Name: "nas", Kind: "server", Source: "scan", Notes: "mine"})
		s.SetDeviceReviewed(ctx, reviewed, true)
		vm, _ := s.CreateDevice(ctx, Device{Name: "vm101", Kind: "vm", Source: "proxmox"})

		fill := DeviceFill{Name: "printer", Kind: "printer", Notes: "from csv", Vendor: "HP", Model: "M404", Function: "print"}
		for _, id := range []int64{guess, reviewed, vm} {
			if err := s.FillDevice(ctx, id, fill); err != nil {
				t.Fatal(err)
			}
		}

		g, _ := s.GetDevice(ctx, guess)
		if g.Name != "printer" || g.Kind != "printer" || g.Notes != "from csv" || g.Vendor != "HP" {
			t.Errorf("scan guess = %+v, want renamed and filled", g)
		}
		if g.Reviewed {
			t.Error("an import marked the device reviewed")
		}
		r, _ := s.GetDevice(ctx, reviewed)
		if r.Name != "nas" || r.Kind != "server" || r.Notes != "mine" || r.Model != "M404" {
			t.Errorf("reviewed device = %+v, want identity and notes kept, model filled", r)
		}
		v, _ := s.GetDevice(ctx, vm)
		if v.Name != "vm101" || v.Kind != "vm" || v.Function != "print" {
			t.Errorf("proxmox device = %+v, want identity kept, function filled", v)
		}
		if ImportMayRename(r) || ImportMayRename(v) || !ImportMayRename(Device{Source: "scan"}) {
			t.Error("ImportMayRename disagrees with FillDevice")
		}
	})
}

func TestAddDeviceTagsKeepsExisting(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		id, _ := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		if err := s.SetDeviceTags(ctx, id, []string{"core"}); err != nil {
			t.Fatal(err)
		}
		if err := s.AddDeviceTags(ctx, id, []string{"lab", " core ", ""}); err != nil {
			t.Fatal(err)
		}
		names, err := s.deviceTagNames(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(names, []string{"core", "lab"}) {
			t.Fatalf("tags = %v", names)
		}
	})
}

func TestListExportIfaces(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		sn, _ := s.CreateSubnet(ctx, Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
		dev, _ := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		bare, _ := s.CreateDevice(ctx, Device{Name: "bare", Kind: "other", Source: "manual"})
		m1, m2 := "aa:bb:cc:00:00:01", "aa:bb:cc:00:00:02"
		if1, _ := s.AddIface(ctx, dev, &m1, nil)
		s.AddIface(ctx, dev, &m2, nil)
		s.AssignIP(ctx, if1, sn, "10.0.0.1", "static")
		s.AssignIP(ctx, if1, sn, "10.0.0.2", "dhcp")

		got, err := s.ListExportIfaces(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ifs := got[dev]
		if len(ifs) != 2 || *ifs[0].MAC != m1 || *ifs[1].MAC != m2 {
			t.Fatalf("ifaces = %+v", ifs)
		}
		want := []ExportIP{{IP: "10.0.0.1", Kind: "static", Subnet: "10.0.0.0/24"}, {IP: "10.0.0.2", Kind: "dhcp", Subnet: "10.0.0.0/24"}}
		if !reflect.DeepEqual(ifs[0].IPs, want) || len(ifs[1].IPs) != 0 {
			t.Fatalf("ips = %+v / %+v", ifs[0].IPs, ifs[1].IPs)
		}
		if _, ok := got[bare]; ok {
			t.Error("a device with no interface got an entry")
		}
	})
}
