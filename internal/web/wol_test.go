package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// fakeWOL records where packets would have gone instead of sending them.
type fakeWOL struct {
	sent []string
	fail map[string]bool
}

func (f *fakeWOL) send(mac, addr string) error {
	if f.fail[addr] {
		return errors.New("unreachable")
	}
	f.sent = append(f.sent, mac+"@"+addr)
	return nil
}

// Wake-on-LAN goes to the directed broadcast of every subnet the interface
// has an address in, then to the limited broadcast, and the toast says where.
func TestWOLSendsToSubnetBroadcasts(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	fake := &fakeWOL{fail: map[string]bool{"192.168.50.255:9": true}}
	srv.wolSend = fake.send

	ctx := t.Context()
	lan, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.20.0/23", Kind: "lan", ScanIntervalSec: 120})
	iot, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "192.168.50.0/24", Kind: "lan", ScanIntervalSec: 120})
	v6, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "fd00::/112", Kind: "lan", ScanIntervalSec: 120})
	dev, _ := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	mac := "aa:bb:cc:00:11:22"
	ifc, _ := st.AddIface(ctx, dev, &mac, nil)
	st.AssignIP(ctx, ifc, lan, "10.0.21.7", "static")
	st.AssignIP(ctx, ifc, iot, "192.168.50.7", "dhcp")
	st.AssignIP(ctx, ifc, v6, "fd00::7", "static")

	path := fmt.Sprintf("/devices/%d/wol", dev)
	rec := htmxRequest(t, srv, st, "POST", path, url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx WOL = %d: %s", rec.Code, rec.Body.String())
	}
	want := []string{mac + "@10.0.21.255:9", mac + "@255.255.255.255:9"}
	if !reflect.DeepEqual(fake.sent, want) {
		t.Errorf("sent %v, want %v", fake.sent, want)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="toast"`) || !strings.Contains(body, "10.0.21.255, 255.255.255.255") ||
		strings.Contains(body, "192.168.50.255") {
		t.Errorf("toast should list the addresses that worked: %s", body)
	}

	// Without htmx the form post still lands back on the device page.
	fake.sent = nil
	rec = authedPost(t, srv, st, path, url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != fmt.Sprintf("/devices/%d", dev) {
		t.Errorf("plain WOL = %d to %q, want a redirect to the device", rec.Code, rec.Header().Get("Location"))
	}
	if len(fake.sent) != 2 {
		t.Errorf("plain WOL sent %v", fake.sent)
	}
}

// A device with a MAC but no IPs still gets the limited broadcast, as before.
func TestWOLWithoutIPsUsesLimitedBroadcast(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	fake := &fakeWOL{}
	srv.wolSend = fake.send
	dev, _ := st.CreateDevice(t.Context(), store.Device{Name: "pc", Kind: "computer", Source: "manual"})
	mac := "aa:bb:cc:00:11:33"
	st.AddIface(t.Context(), dev, &mac, nil)
	htmxRequest(t, srv, st, "POST", fmt.Sprintf("/devices/%d/wol", dev), url.Values{})
	if !reflect.DeepEqual(fake.sent, []string{mac + "@255.255.255.255:9"}) {
		t.Errorf("sent %v", fake.sent)
	}
}

// The WOL button posts through htmx so the result shows as a toast.
func TestWOLButtonUsesToast(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	dev, _ := st.CreateDevice(t.Context(), store.Device{Name: "pc", Kind: "computer", Source: "manual"})
	mac := "aa:bb:cc:00:11:44"
	st.AddIface(t.Context(), dev, &mac, nil)
	body := authedGet(t, srv, st, fmt.Sprintf("/devices/%d", dev)).Body.String()
	tag := formTag(body, fmt.Sprintf("/devices/%d/wol", dev))
	if !strings.Contains(tag, `hx-post="/devices/1/wol"`) || !strings.Contains(tag, `hx-target="#toasts"`) {
		t.Errorf("WOL form = %s, want an htmx post into the toasts", tag)
	}
}

// A guest its integration no longer lists carries a badge on its page and in
// the device list; a listed one does not.
func TestMissingUpstreamBadge(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	v1, v2 := int64(100), int64(101)
	gone, _ := st.CreateDevice(ctx, store.Device{Name: "old-vm", Kind: "vm", Source: "proxmox", ProxmoxVMID: &v1})
	live, _ := st.CreateDevice(ctx, store.Device{Name: "live-vm", Kind: "vm", Source: "proxmox", ProxmoxVMID: &v2})
	if _, _, err := st.ReconcileUpstream(ctx, store.ScopeProxmoxGuests, map[int64]bool{live: true}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if body := authedGet(t, srv, st, fmt.Sprintf("/devices/%d", gone)).Body.String(); !strings.Contains(body, "Missing upstream") ||
		!strings.Contains(body, "Proxmox no longer lists this device") {
		t.Error("missing guest's page has no missing-upstream badge")
	}
	if body := authedGet(t, srv, st, fmt.Sprintf("/devices/%d", live)).Body.String(); strings.Contains(body, "Missing upstream") {
		t.Error("listed guest's page shows the missing-upstream badge")
	}
	list := authedGet(t, srv, st, "/devices").Body.String()
	if n := strings.Count(list, ">Missing upstream<"); n != 2 { // table row and tile
		t.Errorf("device list shows the badge %d times, want 2 (row and tile of the one missing guest)", n)
	}
}
