package proxmox

import (
	"context"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

func TestSyncUpsertsGuestsIdempotently(t *testing.T) {
	srv := fixtureServer(t)
	st, _ := store.Open(":memory:")
	defer st.Close()
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	sync := NewSync(st, c, events.NewService(st, events.NewBroker()))

	for i := 0; i < 2; i++ { // idempotent
		if _, err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.ListDevices()
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
	cfs, _ := st.ListCustomFields(vm.ID)
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
