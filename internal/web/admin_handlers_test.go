package web

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"netis/internal/scan"
	"netis/internal/store"
)

// Happy paths for the admin handlers the viewer walk only reaches as far as
// the 403.

func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder, to string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != to {
		t.Fatalf("code=%d location=%q body=%q, want 303 to %s",
			rec.Code, rec.Header().Get("Location"), rec.Body.String(), to)
	}
}

func TestDeviceDeleteRemovesDevice(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := seedInventory(t, st)

	wantRedirect(t, authedPost(t, srv, st, fmt.Sprintf("/devices/%d/delete", devID), nil), "/devices")
	if _, err := st.GetDevice(t.Context(), devID); err == nil {
		t.Error("device still present after delete")
	}
	if rec := authedPost(t, srv, st, "/devices/abc/delete", nil); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id: code=%d, want 404", rec.Code)
	}
}

func TestLinkAddAndDelete(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := seedInventory(t, st)
	devPath := fmt.Sprintf("/devices/%d", devID)

	wantRedirect(t, authedPost(t, srv, st, devPath+"/links",
		url.Values{"label": {" ui "}, "url": {" https://gw.lan "}}), devPath)
	links, _ := st.ListLinks(t.Context(), devID)
	if len(links) != 1 || links[0].Label != "ui" || links[0].URL != "https://gw.lan" {
		t.Fatalf("links = %+v, want one trimmed ui link", links)
	}

	// A blank label or URL is ignored rather than stored half-filled.
	authedPost(t, srv, st, devPath+"/links", url.Values{"label": {"docs"}, "url": {"  "}})
	if links, _ := st.ListLinks(t.Context(), devID); len(links) != 1 {
		t.Errorf("blank url stored a link: %+v", links)
	}

	wantRedirect(t, authedPost(t, srv, st, fmt.Sprintf("/links/%d/delete", links[0].ID),
		url.Values{"device_id": {strconv.FormatInt(devID, 10)}}), devPath)
	if links, _ := st.ListLinks(t.Context(), devID); len(links) != 0 {
		t.Errorf("links after delete = %+v", links)
	}
}

func TestFieldSetAndDelete(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := seedInventory(t, st)
	devPath := fmt.Sprintf("/devices/%d", devID)

	wantRedirect(t, authedPost(t, srv, st, devPath+"/fields",
		url.Values{"key": {" rack "}, "value": {"a1"}}), devPath)
	authedPost(t, srv, st, devPath+"/fields", url.Values{"key": {"rack"}, "value": {"b2"}})
	authedPost(t, srv, st, devPath+"/fields", url.Values{"key": {""}, "value": {"orphan"}})
	fields, _ := st.ListCustomFields(t.Context(), devID)
	if len(fields) != 1 || fields[0].Key != "rack" || fields[0].Value != "b2" {
		t.Fatalf("fields = %+v, want rack=b2 only", fields)
	}

	wantRedirect(t, authedPost(t, srv, st, devPath+"/fields/delete", url.Values{"key": {"rack"}}), devPath)
	if fields, _ := st.ListCustomFields(t.Context(), devID); len(fields) != 0 {
		t.Errorf("fields after delete = %+v", fields)
	}
}

func TestSubnetUpdateAndDelete(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	_, snID := seedInventory(t, st)
	other, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.5.0/24", Name: "b", Kind: "lan", ScanIntervalSec: 120})
	path := fmt.Sprintf("/settings/subnets/%d", snID)

	wantRedirect(t, authedPost(t, srv, st, path, url.Values{
		"cidr": {"10.0.0.7/24"}, "name": {"renamed"}, "kind": {"wireguard"},
		"scan_enabled": {"on"}, "scan_interval_sec": {"300"},
	}), "/settings/network")
	sn, err := st.GetSubnet(t.Context(), snID)
	if err != nil {
		t.Fatal(err)
	}
	if sn.CIDR != "10.0.0.0/24" || sn.Name != "renamed" || sn.Kind != "wireguard" ||
		!sn.ScanEnabled || sn.ScanIntervalSec != 300 {
		t.Errorf("updated subnet = %+v", sn)
	}

	for name, tc := range map[string]struct {
		path string
		form url.Values
		code int
	}{
		"unknown id":     {"/settings/subnets/9999", url.Values{"cidr": {"10.1.0.0/24"}, "kind": {"lan"}}, http.StatusNotFound},
		"non-numeric id": {"/settings/subnets/x", url.Values{"cidr": {"10.1.0.0/24"}, "kind": {"lan"}}, http.StatusNotFound},
		"bad cidr":       {path, url.Values{"cidr": {"nope"}, "kind": {"lan"}}, http.StatusBadRequest},
		"bad kind":       {path, url.Values{"cidr": {"10.0.0.0/24"}, "kind": {"vlan"}}, http.StatusBadRequest},
		"duplicate cidr": {path, url.Values{"cidr": {"10.0.5.0/24"}, "kind": {"lan"}}, http.StatusConflict},
	} {
		if rec := authedPost(t, srv, st, tc.path, tc.form); rec.Code != tc.code {
			t.Errorf("%s: code=%d, want %d", name, rec.Code, tc.code)
		}
	}

	wantRedirect(t, authedPost(t, srv, st, fmt.Sprintf("/settings/subnets/%d/delete", other), nil),
		"/settings/network")
	if _, err := st.GetSubnet(t.Context(), other); err == nil {
		t.Error("subnet still present after delete")
	}
	if rec := authedPost(t, srv, st, "/settings/subnets/x/delete", nil); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric delete: code=%d, want 404", rec.Code)
	}
}

func TestUserCreate(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	pw := strings.Repeat("p", minPasswordLen)

	wantRedirect(t, authedPost(t, srv, st, "/settings/users",
		url.Values{"username": {" alice "}, "password": {pw}, "role": {"viewer"}}), "/settings/users")
	u, ok, _ := st.GetUserByName(t.Context(), "alice")
	if !ok || u.Role != "viewer" {
		t.Fatalf("created user = %+v ok=%v", u, ok)
	}
	if !passwordWorks(t, st, "alice", pw) {
		t.Error("stored hash does not match the password")
	}

	for name, tc := range map[string]struct {
		form url.Values
		code int
	}{
		"blank username": {url.Values{"username": {" "}, "password": {pw}, "role": {"viewer"}}, http.StatusBadRequest},
		"short password": {url.Values{"username": {"bob"}, "password": {"x"}, "role": {"viewer"}}, http.StatusBadRequest},
		"long password":  {url.Values{"username": {"bob"}, "password": {strings.Repeat("p", maxPasswordLen+1)}, "role": {"viewer"}}, http.StatusBadRequest},
		"bad role":       {url.Values{"username": {"bob"}, "password": {pw}, "role": {"root"}}, http.StatusBadRequest},
		"duplicate":      {url.Values{"username": {"alice"}, "password": {pw}, "role": {"admin"}}, http.StatusConflict},
	} {
		if rec := authedPost(t, srv, st, "/settings/users", tc.form); rec.Code != tc.code {
			t.Errorf("%s: code=%d, want %d", name, rec.Code, tc.code)
		}
	}
	if _, ok, _ := st.GetUserByName(t.Context(), "bob"); ok {
		t.Error("a rejected form created bob")
	}
}

func TestEventsPageFiltersByType(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.AddEvent(t.Context(), "online", nil, "came-up-detail")
	st.AddEvent(t.Context(), "offline", nil, "went-down-detail")

	all := authedGet(t, srv, st, "/events")
	if all.Code != http.StatusOK {
		t.Fatalf("code=%d", all.Code)
	}
	for _, want := range []string{"came-up-detail", "went-down-detail"} {
		if !strings.Contains(all.Body.String(), want) {
			t.Errorf("unfiltered page missing %q", want)
		}
	}
	only := authedGet(t, srv, st, "/events?type=offline").Body.String()
	if !strings.Contains(only, "went-down-detail") || strings.Contains(only, "came-up-detail") {
		t.Error("type=offline should show only the offline event")
	}
}

func TestLogoutEndsSession(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/") // creates the admin and session testtok

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	wantRedirect(t, rec, "/login")

	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "netis_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("logout did not expire the session cookie")
	}
	if _, ok, _ := st.GetSession(t.Context(), "testtok"); ok {
		t.Error("session still valid after logout")
	}
}

func TestPortScanRecordsOpenPorts(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	// Listen on one of the ports the scan probes, on loopback, so the scan
	// finds something real. Low ports need privileges; take the first high one
	// that is free.
	var ln net.Listener
	var port int
	for _, p := range scan.CommonPorts {
		if p < 1024 {
			continue
		}
		if l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p)); err == nil {
			ln, port = l, p
			break
		}
	}
	if ln == nil {
		t.Skip("no probed port free on loopback")
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "127.0.0.0/24", Name: "lo", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "self", Kind: "server", Source: "manual"})
	ifID, _ := st.AddIface(t.Context(), devID, nil, nil)
	st.AssignIP(t.Context(), ifID, snID, "127.0.0.1", "static")
	devPath := fmt.Sprintf("/devices/%d", devID)

	wantRedirect(t, authedPost(t, srv, st, devPath+"/portscan", nil), devPath)
	ports, _ := st.ListOpenPorts(t.Context(), ifID)
	found := false
	for _, p := range ports {
		if p.Port == port {
			found = true
		}
	}
	if !found {
		t.Errorf("open ports = %+v, want %d among them", ports, port)
	}

	// A device with an interface but no address, or no interface at all, has
	// nothing to scan.
	bare, _ := st.CreateDevice(t.Context(), store.Device{Name: "bare", Kind: "other", Source: "manual"})
	if rec := authedPost(t, srv, st, fmt.Sprintf("/devices/%d/portscan", bare), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no iface: code=%d, want 400", rec.Code)
	}
	st.AddIface(t.Context(), bare, nil, nil)
	if rec := authedPost(t, srv, st, fmt.Sprintf("/devices/%d/portscan", bare), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no ip: code=%d, want 400", rec.Code)
	}
}
