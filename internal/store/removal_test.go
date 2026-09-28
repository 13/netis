package store

import "testing"

// The delete and list helpers behind the device page's remove buttons, run on
// both dialects: each must remove exactly the row it names and nothing else.
func TestRemovalHelpers(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		devID, err := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}

		// UntagDevice detaches one tag and leaves the other.
		keep, _ := s.CreateTag(ctx, "keep", "#000000")
		drop, _ := s.CreateTag(ctx, "drop", "#000000")
		s.TagDevice(ctx, devID, keep)
		s.TagDevice(ctx, devID, drop)
		if err := s.UntagDevice(ctx, devID, drop); err != nil {
			t.Fatal(err)
		}
		if names, _ := s.deviceTagNames(ctx, devID); len(names) != 1 || names[0] != "keep" {
			t.Errorf("tags after untag = %v, want [keep]", names)
		}

		// DeleteCustomField removes one key.
		s.SetCustomField(ctx, devID, "rack", "a1")
		s.SetCustomField(ctx, devID, "row", "3")
		if err := s.DeleteCustomField(ctx, devID, "rack"); err != nil {
			t.Fatal(err)
		}
		if cfs, _ := s.ListCustomFields(ctx, devID); len(cfs) != 1 || cfs[0].Key != "row" {
			t.Errorf("fields after delete = %+v, want only row", cfs)
		}

		// DeleteLink removes one link by id.
		l1, _ := s.AddLink(ctx, devID, "ui", "https://a")
		s.AddLink(ctx, devID, "docs", "https://b")
		if err := s.DeleteLink(ctx, l1); err != nil {
			t.Fatal(err)
		}
		if links, _ := s.ListLinks(ctx, devID); len(links) != 1 || links[0].Label != "docs" {
			t.Errorf("links after delete = %+v, want only docs", links)
		}

		// DeleteUser removes the user, and with it their sessions.
		uid, err := s.CreateUser(ctx, "gone", "h", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		s.CreateSession(ctx, "gonetok", uid, "2099-01-01T00:00:00Z")
		if err := s.DeleteUser(ctx, uid); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := s.GetUser(ctx, uid); ok {
			t.Error("user still present after DeleteUser")
		}
		if _, ok, _ := s.GetSession(ctx, "gonetok"); ok {
			t.Error("a deleted user's session still resolves")
		}
	})
}

func TestListChildrenAndSubnetIfaceIPs(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		parent, _ := s.CreateDevice(ctx, Device{Name: "host", Kind: "server", Source: "manual"})
		s.CreateDevice(ctx, Device{Name: "vm-b", Kind: "vm", Source: "manual", ParentDeviceID: &parent})
		s.CreateDevice(ctx, Device{Name: "vm-a", Kind: "vm", Source: "manual", ParentDeviceID: &parent})
		s.CreateDevice(ctx, Device{Name: "stranger", Kind: "vm", Source: "manual"})

		kids, err := s.ListChildren(ctx, parent)
		if err != nil {
			t.Fatal(err)
		}
		if len(kids) != 2 || kids[0].Name != "vm-a" || kids[1].Name != "vm-b" {
			t.Errorf("children = %+v, want vm-a, vm-b by name", kids)
		}
		if none, err := s.ListChildren(ctx, kids[0].ID); err != nil || len(none) != 0 {
			t.Errorf("childless device: children=%v err=%v", none, err)
		}

		sn1, _ := s.CreateSubnet(ctx, Subnet{CIDR: "10.0.0.0/24", Name: "a", Kind: "lan", ScanIntervalSec: 120})
		sn2, _ := s.CreateSubnet(ctx, Subnet{CIDR: "10.0.1.0/24", Name: "b", Kind: "lan", ScanIntervalSec: 120})
		withMAC, _ := s.AddIface(ctx, parent, strp("aa:bb:cc:dd:ee:01"), strp("host.lan"))
		noMAC, _ := s.AddIface(ctx, parent, nil, nil)
		s.AssignIP(ctx, withMAC, sn1, "10.0.0.5", "static")
		s.AssignIP(ctx, noMAC, sn1, "10.0.0.6", "dhcp")
		s.AssignIP(ctx, noMAC, sn2, "10.0.1.6", "dhcp")

		rows, err := s.ListSubnetIfaceIPs(ctx, sn1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("subnet 1 rows = %+v, want 2", rows)
		}
		got := map[string]SubnetIfaceIP{}
		for _, r := range rows {
			got[r.IP] = r
		}
		if r := got["10.0.0.5"]; r.IfaceID != withMAC || r.DeviceID != parent || r.MAC == nil || *r.MAC != "aa:bb:cc:dd:ee:01" ||
			r.Hostname == nil || *r.Hostname != "host.lan" {
			t.Errorf("10.0.0.5 row = %+v", r)
		}
		if r := got["10.0.0.6"]; r.IfaceID != noMAC || r.MAC != nil || r.Hostname != nil {
			t.Errorf("10.0.0.6 row = %+v, want iface %d with no MAC", r, noMAC)
		}
	})
}
