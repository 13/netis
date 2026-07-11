package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const resourcesJSON = `{"data":[
 {"vmid":100,"name":"nas-vm","node":"pve1","status":"running","type":"qemu"},
 {"vmid":101,"name":"pihole","node":"pve1","status":"stopped","type":"lxc"}
]}`

const qemuConfigJSON = `{"data":{"net0":"virtio=BC:24:11:AA:00:01,bridge=vmbr0","cores":4}}`
const lxcConfigJSON = `{"data":{"net0":"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:AA:00:02,ip=dhcp"}}`

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=root@pam!netis=s3cret" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(resourcesJSON))
	})
	mux.HandleFunc("/api2/json/nodes/pve1/qemu/100/config", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(qemuConfigJSON))
	})
	mux.HandleFunc("/api2/json/nodes/pve1/lxc/101/config", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(lxcConfigJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestListGuests(t *testing.T) {
	srv := fixtureServer(t)
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	guests, err := c.ListGuests(context.Background())
	if err != nil || len(guests) != 2 {
		t.Fatalf("guests=%+v err=%v", guests, err)
	}
	if guests[0].VMID != 100 || guests[0].Type != "qemu" || guests[0].Status != "running" {
		t.Fatalf("guest0=%+v", guests[0])
	}
}

func TestGuestMACs(t *testing.T) {
	srv := fixtureServer(t)
	c := NewClient(srv.URL, "root@pam!netis", "s3cret", false)
	macs, err := c.GuestMACs(context.Background(), "pve1", 100, "qemu")
	if err != nil || len(macs) != 1 || macs[0] != "bc:24:11:aa:00:01" {
		t.Fatalf("macs=%v err=%v", macs, err)
	}
	macs, err = c.GuestMACs(context.Background(), "pve1", 101, "lxc")
	if err != nil || len(macs) != 1 || macs[0] != "bc:24:11:aa:00:02" {
		t.Fatalf("lxc macs=%v err=%v", macs, err)
	}
}
