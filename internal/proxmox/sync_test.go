package proxmox

import (
	"context"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

func TestSyncUpsertsGuestsIdempotently(t *testing.T) {
	storetest.EachDialect(t, testSyncUpsertsGuestsIdempotently)
}

func testSyncUpsertsGuestsIdempotently(t *testing.T, st *store.Store) {
	srv := fixtureServer(t)
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	sync := NewSync(st, c, events.NewService(st, events.NewBroker()))

	for i := 0; i < 2; i++ { // idempotent
		if _, err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.ListDevices(t.Context())
	// pve1 node + 2 guests
	if len(rows) != 3 {
		t.Fatalf("want 3 devices, got %+v", rows)
	}
	var node, vm store.DeviceRow
	for _, r := range rows {
		switch r.Name {
		case "pve1":
			node = r
		case "nas-vm":
			vm = r
		}
	}
	if node.Kind != "server" || node.Source != "proxmox" {
		t.Fatalf("node=%+v", node)
	}
	if vm.Kind != "vm" || vm.ParentDeviceID == nil || *vm.ParentDeviceID != node.ID ||
		vm.ProxmoxVMID == nil || *vm.ProxmoxVMID != 100 {
		t.Fatalf("vm=%+v", vm)
	}
	if len(vm.MACs) != 1 || vm.MACs[0] != "bc:24:11:aa:00:01" {
		t.Fatalf("vm macs=%v", vm.MACs)
	}
	cfs, _ := st.ListCustomFields(t.Context(), vm.ID)
	if len(cfs) != 1 || cfs[0].Key != "proxmox_status" || cfs[0].Value != "running" {
		t.Fatalf("cfs=%+v", cfs)
	}
}

func TestProxmoxRunOnceStats(t *testing.T) {
	srv := fixtureServer(t)
	st, _ := store.Open(":memory:")
	defer st.Close()
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	sync := NewSync(st, c, events.NewService(st, events.NewBroker()))
	stats, err := sync.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Guests != 2 || stats.Nodes != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

// syncTwice runs one sync, lets edit change the store, and runs another, the
// way a user edit lands between two minute-apart integration runs.
func syncTwice(t *testing.T, st *store.Store, edit func()) {
	t.Helper()
	srv := fixtureServer(t)
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	sync := NewSync(st, c, events.NewService(st, events.NewBroker()))
	if _, err := sync.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	edit()
	if _, err := sync.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSyncKeepsUserEditsToGuests(t *testing.T) {
	storetest.EachDialect(t, testSyncKeepsUserEditsToGuests)
}

func testSyncKeepsUserEditsToGuests(t *testing.T, st *store.Store) {
	ctx := t.Context()
	var vmID, lxcID, rackID int64
	syncTwice(t, st, func() {
		vmID, _, _ = st.FindProxmoxGuest(ctx, 100)
		lxcID, _, _ = st.FindProxmoxGuest(ctx, 101)
		var err error
		rackID, err = st.CreateDevice(ctx, store.Device{Name: "rack", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		vm, _ := st.GetDevice(ctx, vmID)
		vm.Name, vm.Kind, vm.Notes = "Storage box", "server", "keep me"
		if err := st.UpdateDevice(ctx, vm); err != nil {
			t.Fatal(err)
		}
		lxc, _ := st.GetDevice(ctx, lxcID)
		lxc.ParentDeviceID = &rackID // a parent the user chose outside proxmox
		if err := st.UpdateDevice(ctx, lxc); err != nil {
			t.Fatal(err)
		}
	})

	vm, _ := st.GetDevice(ctx, vmID)
	if vm.Name != "Storage box" || vm.Kind != "server" || vm.Notes != "keep me" {
		t.Fatalf("sync clobbered user edits: %+v", vm)
	}
	lxc, _ := st.GetDevice(ctx, lxcID)
	if lxc.ParentDeviceID == nil || *lxc.ParentDeviceID != rackID {
		t.Fatalf("sync replaced user-set parent: %+v", lxc)
	}
}

func TestSyncTracksGuestNodeParent(t *testing.T) {
	storetest.EachDialect(t, testSyncTracksGuestNodeParent)
}

func testSyncTracksGuestNodeParent(t *testing.T, st *store.Store) {
	ctx := t.Context()
	var vmID, lxcID, nodeID int64
	syncTwice(t, st, func() {
		vmID, _, _ = st.FindProxmoxGuest(ctx, 100)
		lxcID, _, _ = st.FindProxmoxGuest(ctx, 101)
		nodeID, _, _ = st.FindProxmoxNode(ctx, "pve1")
		// a guest whose parent was cleared gets its node back
		vm, _ := st.GetDevice(ctx, vmID)
		vm.ParentDeviceID = nil
		if err := st.UpdateDevice(ctx, vm); err != nil {
			t.Fatal(err)
		}
		// a guest still recorded on another proxmox node has migrated
		other, err := st.CreateDevice(ctx, store.Device{Name: "pve2", Kind: "server", Source: "proxmox"})
		if err != nil {
			t.Fatal(err)
		}
		lxc, _ := st.GetDevice(ctx, lxcID)
		lxc.ParentDeviceID = &other
		if err := st.UpdateDevice(ctx, lxc); err != nil {
			t.Fatal(err)
		}
	})

	for _, id := range []int64{vmID, lxcID} {
		d, _ := st.GetDevice(ctx, id)
		if d.ParentDeviceID == nil || *d.ParentDeviceID != nodeID {
			t.Fatalf("guest %d parent=%v, want node %d", id, d.ParentDeviceID, nodeID)
		}
	}
}

func TestSyncKeepsRenamedNode(t *testing.T) {
	storetest.EachDialect(t, testSyncKeepsRenamedNode)
}

func testSyncKeepsRenamedNode(t *testing.T, st *store.Store) {
	ctx := t.Context()
	var nodeID int64
	syncTwice(t, st, func() {
		nodeID, _, _ = st.FindProxmoxNode(ctx, "pve1")
		n, _ := st.GetDevice(ctx, nodeID)
		n.Name, n.Kind = "Main host", "router"
		if err := st.UpdateDevice(ctx, n); err != nil {
			t.Fatal(err)
		}
	})

	rows, _ := st.ListDevices(ctx)
	if len(rows) != 3 {
		t.Fatalf("renaming the node made the sync create another: %+v", rows)
	}
	n, _ := st.GetDevice(ctx, nodeID)
	if n.Name != "Main host" || n.Kind != "router" {
		t.Fatalf("node=%+v", n)
	}
	vmID, _, _ := st.FindProxmoxGuest(ctx, 100)
	vm, _ := st.GetDevice(ctx, vmID)
	if vm.ParentDeviceID == nil || *vm.ParentDeviceID != nodeID {
		t.Fatalf("vm parent=%v, want renamed node %d", vm.ParentDeviceID, nodeID)
	}
}

// A guest whose MAC was already discovered by the scan (or Pi-hole) takes that
// interface over, with its IPs, instead of leaving two unmerged devices; the
// discovered device goes away when nothing else is on it. A device the user
// has reviewed keeps its interface.
func TestSyncAdoptsDiscoveredGuestIface(t *testing.T) {
	storetest.EachDialect(t, testSyncAdoptsDiscoveredGuestIface)
}

func testSyncAdoptsDiscoveredGuestIface(t *testing.T, st *store.Store) {
	ctx := t.Context()
	snID, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	vmMAC, lxcMAC := "bc:24:11:aa:00:01", "bc:24:11:aa:00:02"
	// nas-vm's MAC: an unreviewed scan device with nothing else on it.
	scanID, _, err := st.CreateDiscoveredDevice(ctx, store.Device{Name: "unknown-1", Kind: "other", Source: "scan"},
		&vmMAC, nil, snID, "10.0.0.5", "dhcp")
	if err != nil {
		t.Fatal(err)
	}
	// pihole lxc's MAC: a scan device the user has reviewed.
	keptID, _, err := st.CreateDiscoveredDevice(ctx, store.Device{Name: "mine", Kind: "other", Source: "scan"},
		&lxcMAC, nil, snID, "10.0.0.6", "dhcp")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDeviceReviewed(ctx, keptID, true); err != nil {
		t.Fatal(err)
	}

	srv := fixtureServer(t)
	sync := NewSync(st, NewClient(srv.URL, "root@pam!netis", "s3cret", false), events.NewService(st, events.NewBroker()))
	if _, err := sync.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	vmID, ok, err := st.FindProxmoxGuest(ctx, 100)
	if err != nil || !ok {
		t.Fatalf("guest 100: ok=%v err=%v", ok, err)
	}
	iface, ok, err := st.FindIfaceByMAC(ctx, vmMAC)
	if err != nil || !ok || iface.DeviceID != vmID {
		t.Fatalf("vm MAC iface = %+v ok=%v err=%v, want it on guest %d", iface, ok, err, vmID)
	}
	if ips, _ := st.ListIPs(ctx, iface.ID); len(ips) != 1 || ips[0].IP != "10.0.0.5" {
		t.Errorf("adopted iface IPs = %+v, want 10.0.0.5", ips)
	}
	if _, err := st.GetDevice(ctx, scanID); err == nil {
		t.Error("the emptied scan device should have been deleted")
	}

	kept, ok, err := st.FindIfaceByMAC(ctx, lxcMAC)
	if err != nil || !ok || kept.DeviceID != keptID {
		t.Fatalf("reviewed device's iface = %+v ok=%v err=%v, want it left on %d", kept, ok, err, keptID)
	}
}

// A discovered device carrying something the user added keeps existing after
// its interface moves to the guest.
func TestSyncAdoptKeepsDeviceWithUserData(t *testing.T) {
	storetest.EachDialect(t, testSyncAdoptKeepsDeviceWithUserData)
}

func testSyncAdoptKeepsDeviceWithUserData(t *testing.T, st *store.Store) {
	ctx := t.Context()
	vmMAC := "bc:24:11:aa:00:01"
	scanID, err := st.CreateDevice(ctx, store.Device{Name: "unknown-1", Kind: "other", Source: "scan"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddIface(ctx, scanID, &vmMAC, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(ctx, scanID, "admin", "http://10.0.0.5"); err != nil {
		t.Fatal(err)
	}
	srv := fixtureServer(t)
	sync := NewSync(st, NewClient(srv.URL, "root@pam!netis", "s3cret", false), events.NewService(st, events.NewBroker()))
	if _, err := sync.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	vmID, _, _ := st.FindProxmoxGuest(ctx, 100)
	if iface, _, _ := st.FindIfaceByMAC(ctx, vmMAC); iface.DeviceID != vmID {
		t.Fatalf("iface on %d, want guest %d", iface.DeviceID, vmID)
	}
	if _, err := st.GetDevice(ctx, scanID); err != nil {
		t.Errorf("device with a user link must survive: %v", err)
	}
}
