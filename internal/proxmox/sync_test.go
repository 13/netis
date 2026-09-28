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
