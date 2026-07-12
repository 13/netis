package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

func TestSubnetPageDevicesList(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snA, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "A", Kind: "lan", ScanIntervalSec: 120})
	snB, _ := st.CreateSubnet(store.Subnet{CIDR: "10.1.0.0/24", Name: "B", Kind: "lan", ScanIntervalSec: 120})

	din, _ := st.CreateDevice(store.Device{Name: "insub", Kind: "server", Source: "manual"})
	fin, _ := st.AddIface(din, nil, nil)
	st.AssignIP(fin, snA, "10.0.0.5", "static")

	dout, _ := st.CreateDevice(store.Device{Name: "outsub", Kind: "server", Source: "manual"})
	fout, _ := st.AddIface(dout, nil, nil)
	st.AssignIP(fout, snB, "10.1.0.5", "static")

	body := authedGet(t, srv, st, "/subnets/1").Body.String()
	for _, want := range []string{"Devices in this subnet", "insub", `hx-get="/devices/new?subnet=1"`, "Devices without an IP here"} {
		if !strings.Contains(body, want) {
			t.Errorf("subnet page missing %q", want)
		}
	}
	if strings.Contains(body, "outsub") {
		t.Error("subnet page should not list a device from another subnet")
	}
	_ = snA
	_ = snB
}

func TestSubnetDeviceListSortable(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "A", Kind: "lan", ScanIntervalSec: 120})
	for _, x := range []struct {
		name, ip string
	}{{"beta", "10.0.0.9"}, {"alpha", "10.0.0.3"}} {
		d, _ := st.CreateDevice(store.Device{Name: x.name, Kind: "server", Source: "manual"})
		f, _ := st.AddIface(d, nil, nil)
		st.AssignIP(f, snID, x.ip, "static")
	}
	base := "/subnets/" + strconv.FormatInt(snID, 10)

	// The table is the sortable component: sort-header links target this subnet.
	body := authedGet(t, srv, st, base).Body.String()
	if !strings.Contains(body, `href="`+base+`?`) {
		t.Fatalf("subnet device list is not sortable (no %s sort links): %q", base, body)
	}

	// Sorting by name orders alpha before beta.
	sorted := authedGet(t, srv, st, base+"?sort=name&dir=asc").Body.String()
	ia, ib := strings.Index(sorted, "alpha"), strings.Index(sorted, "beta")
	if ia < 0 || ib < 0 || ia > ib {
		t.Fatalf("name sort failed: alpha=%d beta=%d", ia, ib)
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
