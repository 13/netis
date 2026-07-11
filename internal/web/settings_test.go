package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCreateSubnetViaSettings(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/settings/subnets", url.Values{
		"cidr": {"192.168.1.0/24"}, "name": {"main"}, "kind": {"lan"},
		"scan_interval_sec": {"120"}, "scan_enabled": {"on"},
	})
	if rec.Code != 303 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	subnets, _ := st.ListSubnets()
	if len(subnets) != 1 || subnets[0].Name != "main" || !subnets[0].ScanEnabled {
		t.Fatalf("subnets=%+v", subnets)
	}
	// invalid CIDR rejected
	rec = authedPost(t, srv, st, "/settings/subnets", url.Values{
		"cidr": {"not-a-cidr"}, "name": {"x"}, "kind": {"lan"}, "scan_interval_sec": {"120"},
	})
	if rec.Code != 400 {
		t.Fatalf("bad cidr code=%d", rec.Code)
	}
}

func TestViewerCannotPostSettings(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/settings/general",
		nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestCannotDeleteLastAdmin(t *testing.T) {
	srv, st := testServer(t)
	rec := authedGet(t, srv, st, "/") // creates admin "ben" id=1
	_ = rec
	del := authedPost(t, srv, st, "/settings/users/1/delete", url.Values{})
	if del.Code != 400 {
		t.Fatalf("code=%d", del.Code)
	}
	if n, _ := st.CountUsers(); n != 1 {
		t.Fatal("admin must survive")
	}
}
