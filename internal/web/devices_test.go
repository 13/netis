package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

func authedPost(t *testing.T, srv *Server, st *store.Store, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec0 := authedGet(t, srv, st, "/") // ensures admin+session exist
	_ = rec0
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestWOLAndPortScanUnknownDevice404(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	for _, path := range []string{"/devices/999/wol", "/devices/999/portscan"} {
		rec := authedPost(t, srv, st, path, url.Values{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s on unknown device = %d, want 404", path, rec.Code)
		}
	}
}

func TestWOLDeviceWithoutMAC400(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "nomac", Kind: "other", Source: "manual"})
	st.AddIface(devID, nil, nil) // iface but no MAC
	rec := authedPost(t, srv, st, "/devices/1/wol", url.Values{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("WOL on device without MAC = %d, want 400", rec.Code)
	}
	_ = devID
}

func TestCreateAndShowDevice(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"office-switch"}, "kind": {"switch"}, "notes": {"rack top"},
	})
	if rec.Code != 303 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	rows, _ := st.ListDevices()
	if len(rows) != 1 || rows[0].Kind != "switch" {
		t.Fatalf("rows=%+v", rows)
	}
	page := authedGet(t, srv, st, rec.Header().Get("Location"))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "office-switch") {
		t.Fatalf("detail code=%d", page.Code)
	}
}

func TestDeviceListFilter(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "alpha", Kind: "computer", Source: "manual"})
	st.CreateDevice(store.Device{Name: "beta", Kind: "phone", Source: "manual"})
	rec := authedGet(t, srv, st, "/devices?q=alp")
	body := rec.Body.String()
	if !strings.Contains(body, "alpha") || strings.Contains(body, "beta") {
		t.Fatalf("filter failed: %s", body)
	}
}

func TestDeviceListDefaultSortIPNumeric(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	mk := func(name, ip string) {
		d, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		f, _ := st.AddIface(d, nil, nil)
		st.AssignIP(f, snID, ip, "dhcp")
	}
	mk("c", "10.0.0.100")
	mk("a", "10.0.0.2")
	mk("b", "10.0.0.10")
	body := authedGet(t, srv, st, "/devices").Body.String()
	// Numeric IP order: .2 before .10 before .100 (string sort would flip .10/.100/.2).
	i2, i10, i100 := strings.Index(body, "10.0.0.2<"), strings.Index(body, "10.0.0.10<"), strings.Index(body, "10.0.0.100<")
	if !(i2 >= 0 && i10 > i2 && i100 > i10) {
		t.Fatalf("IP order wrong: .2@%d .10@%d .100@%d", i2, i10, i100)
	}
}

func TestDeviceListLeaseChipsAndGrid(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	d, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	f, _ := st.AddIface(d, nil, nil)
	st.AssignIP(f, snID, "10.0.0.5", "static")
	st.AssignIP(f, snID, "10.0.0.6", "dhcp")
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{"chip static", "chip", `id="dev-grid"`, `id="dev-list"`, `class="seg"`} {
		if !strings.Contains(body, want) {
			t.Errorf("device list missing %q", want)
		}
	}
}

func TestDeviceListNewMarker(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "unknown-aa", Kind: "other", Source: "scan"})
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "new") {
		t.Fatal("unreviewed scan device should show a 'new' marker")
	}
}

func TestDeleteDeviceRequiresAdmin(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "x", Kind: "other", Source: "manual"})
	// viewer session
	uID, _ := st.CreateUser("eve", "hash", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/devices/1/delete", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("viewer delete code=%d", rec.Code)
	}
	if _, err := st.GetDevice(devID); err != nil {
		t.Fatal("device must still exist")
	}
}
