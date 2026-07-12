package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestGridStates(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})

	mk := func(name, ip string) int64 {
		devID, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		ifID, _ := st.AddIface(devID, nil, nil)
		st.AssignIP(ifID, snID, ip, "static")
		return ifID
	}
	onlineIf := mk("gw", "10.0.0.1")
	st.MarkSeen(onlineIf, 1, time.Now())
	offlineIf := mk("nas", "10.0.0.2")
	st.MarkSeen(offlineIf, 1, time.Now())
	st.MarkMissed(offlineIf, 1)
	mk("printer", "10.0.0.3") // reserved: assigned, never seen
	// conflict: second iface claims .1
	dup, _ := st.CreateDevice(store.Device{Name: "ghost", Kind: "other", Source: "manual"})
	dupIf, _ := st.AddIface(dup, nil, nil)
	st.AssignIP(dupIf, snID, "10.0.0.1", "static")

	rec := authedGet(t, srv, st, "/subnets/1/grid")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"sq conflict", "sq offline", "sq reserved", "sq free", "sq edge", "static"} {
		if !strings.Contains(body, want) {
			t.Errorf("grid missing %q", want)
		}
	}
	// /29 full range → 8 squares (network + 6 hosts + broadcast)
	if n := strings.Count(body, `class="sq`); n != 8 {
		t.Errorf("squares=%d, want 8", n)
	}
}

func TestCellDetailAndSetKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")

	det := authedGet(t, srv, st, "/subnets/1/cell?ip=10.0.0.1").Body.String()
	for _, want := range []string{"/devices/1", "Set static", "Set DHCP", `class="dialog"`} {
		if !strings.Contains(det, want) {
			t.Errorf("cell detail missing %q", want)
		}
	}

	rec := authedPost(t, srv, st, "/subnets/1/cell", url.Values{"ip": {"10.0.0.1"}, "kind": {"dhcp"}})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "10.0.0.1 → dhcp") {
		t.Fatalf("set kind code=%d body=%s", rec.Code, rec.Body.String())
	}
	occ, _ := st.SubnetOccupancy(snID)
	if occ["10.0.0.1"].Kind != "dhcp" {
		t.Fatalf("kind not updated: %+v", occ["10.0.0.1"])
	}
}

func TestSetKindBadKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	if rec := authedPost(t, srv, st, "/subnets/1/cell", url.Values{"ip": {"10.0.0.1"}, "kind": {"bogus"}}); rec.Code != 400 {
		t.Fatalf("bad kind code=%d, want 400", rec.Code)
	}
}

func TestSetKindRequiresAdmin(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/subnets/1/cell", strings.NewReader("ip=10.0.0.1&kind=dhcp"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer code=%d, want 403", rec.Code)
	}
}
