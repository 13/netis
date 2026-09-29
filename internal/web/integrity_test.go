package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

func deviceCount(t *testing.T, st *store.Store) int {
	t.Helper()
	rows, err := st.ListDevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// The create form's MAC and IP are validated, and a device is created with its
// interface and IP or not at all.
func TestDeviceCreateValidatesMACAndIP(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, err := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "192.168.1.0/24", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	sn := itoa(snID)

	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"nas"}, "kind": {"server"}, "mac": {"AA-BB-CC-DD-EE-01"},
		"ip": {"192.168.1.10"}, "subnet_id": {sn},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid create = %d %s", rec.Code, rec.Body.String())
	}
	iface, ok, err := st.FindIfaceByIP(t.Context(), snID, "192.168.1.10")
	if err != nil || !ok || iface.MAC == nil || *iface.MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("iface = %+v ok=%v err=%v", iface, ok, err)
	}

	cases := []struct {
		name string
		form url.Values
		code int
		msg  string
	}{
		{"non-hex MAC of the right length", url.Values{"mac": {"zz:zz:zz:zz:zz:zz"}}, 400, "invalid MAC"},
		{"short MAC", url.Values{"mac": {"aa:bb:cc"}}, 400, "invalid MAC"},
		{"bad IP", url.Values{"ip": {"192.168.1.999"}, "subnet_id": {sn}}, 400, "invalid IP"},
		{"IP outside the subnet", url.Values{"ip": {"10.0.0.5"}, "subnet_id": {sn}}, 400, "not in subnet"},
		{"IP without a subnet", url.Values{"ip": {"192.168.1.11"}}, 400, "subnet"},
		{"unknown subnet", url.Values{"ip": {"192.168.1.11"}, "subnet_id": {"999"}}, 400, "unknown subnet"},
		{"duplicate MAC", url.Values{"mac": {"aa:bb:cc:dd:ee:01"}}, 409, "MAC"},
		{"missing parent", url.Values{"parent_device_id": {"999"}}, 400, "parent"},
	}
	for _, c := range cases {
		before := deviceCount(t, st)
		c.form.Set("name", "x")
		c.form.Set("kind", "other")
		rec := authedPost(t, srv, st, "/devices", c.form)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.msg) {
			t.Errorf("%s: %d %q, want %d containing %q", c.name, rec.Code, rec.Body.String(), c.code, c.msg)
		}
		if after := deviceCount(t, st); after != before {
			t.Errorf("%s: device count went %d -> %d", c.name, before, after)
		}
	}
}

func TestDeviceUpdateMissingParent400(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	rec := authedPost(t, srv, st, "/devices/1", url.Values{
		"name": {"nas"}, "kind": {"server"}, "parent_device_id": {"999"},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "parent") {
		t.Fatalf("update with missing parent = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDuplicateSubnetAndUserAre409(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	subnet := url.Values{"cidr": {"10.1.0.0/24"}, "kind": {"lan"}, "scan_interval_sec": {"120"}}
	if rec := authedPost(t, srv, st, "/settings/subnets", subnet); rec.Code != http.StatusSeeOther {
		t.Fatalf("first subnet = %d %s", rec.Code, rec.Body.String())
	}
	if rec := authedPost(t, srv, st, "/settings/subnets", subnet); rec.Code != http.StatusConflict {
		t.Errorf("duplicate subnet = %d %q, want 409", rec.Code, rec.Body.String())
	}
	other, err := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.2.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	if rec := authedPost(t, srv, st, "/settings/subnets/"+itoa(other), subnet); rec.Code != http.StatusConflict {
		t.Errorf("update onto an existing CIDR = %d %q, want 409", rec.Code, rec.Body.String())
	}

	user := url.Values{"username": {"carol"}, "password": {"longenough1"}, "role": {"viewer"}}
	if rec := authedPost(t, srv, st, "/settings/users", user); rec.Code != http.StatusSeeOther {
		t.Fatalf("first user = %d %s", rec.Code, rec.Body.String())
	}
	if rec := authedPost(t, srv, st, "/settings/users", user); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "carol") {
		t.Errorf("duplicate user = %d %q, want 409 naming carol", rec.Code, rec.Body.String())
	}
}

// The wizard applies the settings form's subnet checks and reports a bad
// entry instead of silently dropping it; a subnet that already exists is fine.
func TestWelcomeSubnetsValidatesAndReports(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/welcome/subnets", url.Values{
		"subnet": {"192.168.5.0/24|eth0"}, "manual_cidr": {"10.0.0.0/8"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-wide manual CIDR = %d %q, want 400", rec.Code, rec.Body.String())
	}
	if subs, _ := st.ListSubnets(t.Context()); len(subs) != 0 {
		t.Fatalf("a rejected wizard page created %+v", subs)
	}
	rec = authedPost(t, srv, st, "/welcome/subnets", url.Values{"manual_cidr": {"not-a-cidr"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "is not a subnet") {
		t.Fatalf("garbage CIDR = %d %q", rec.Code, rec.Body.String())
	}
	for i := 0; i < 2; i++ { // resubmitting is harmless
		rec = authedPost(t, srv, st, "/welcome/subnets", url.Values{"subnet": {"192.168.5.0/24|eth0"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("submit %d = %d %q", i, rec.Code, rec.Body.String())
		}
	}
	if subs, _ := st.ListSubnets(t.Context()); len(subs) != 1 || subs[0].Name != "eth0" {
		t.Fatalf("subnets = %+v", subs)
	}
}

// A port scan replaces the recorded ports, so one that has closed disappears.
func TestPortScanReplacesRecordedPorts(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "127.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "lo", Kind: "server", Source: "manual"})
	ifID, _ := st.AddIface(t.Context(), devID, nil, nil)
	st.AssignIP(t.Context(), ifID, snID, "127.0.0.1", "static")
	// Port 1 is not among the probed ports and nothing listens there.
	if err := st.UpsertOpenPort(t.Context(), ifID, 1, "tcp", "", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	rec := authedPost(t, srv, st, "/devices/"+itoa(devID)+"/portscan", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("portscan = %d %q", rec.Code, rec.Body.String())
	}
	ports, err := st.ListOpenPorts(t.Context(), ifID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ports {
		if p.Port == 1 {
			t.Fatalf("stale port 1 survived the scan: %+v", ports)
		}
	}
}
