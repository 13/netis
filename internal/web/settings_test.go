package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

func TestPiholeSecretNeverEchoedAndBlankKeeps(t *testing.T) {
	srv, st := testServer(t)
	// Seed a stored password, then load the settings page as admin.
	st.SetSetting("pihole_password", "topsecret")
	rec := authedGet(t, srv, st, "/settings")
	if rec.Code != 200 {
		t.Fatalf("settings page code=%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "topsecret") {
		t.Fatal("pihole_password must never be rendered into the settings form")
	}
	// Posting integrations with a blank pihole_password keeps the stored value.
	authedPost(t, srv, st, "/settings/integrations", url.Values{
		"pihole_url":      {"https://pi.hole"},
		"pihole_password": {""},
		"pihole_insecure": {"on"},
	})
	if v, _ := st.GetSetting("pihole_password"); v != "topsecret" {
		t.Fatalf("blank password should keep stored value, got %q", v)
	}
	if v, _ := st.GetSetting("pihole_insecure"); v != "1" {
		t.Fatalf("insecure checkbox should normalize to '1', got %q", v)
	}
	if v, _ := st.GetSetting("pihole_url"); v != "https://pi.hole" {
		t.Fatalf("url not saved: %q", v)
	}
}

// TestConcurrentAdminDeleteKeepsOne guards against a TOCTOU race in the
// last-admin delete check: two concurrent deletes of two different admins
// must not both succeed, which would leave zero admins (a permanent
// lockout, since /setup refuses once any user exists).
func TestConcurrentAdminDeleteKeepsOne(t *testing.T) {
	srv, st := testServer(t)
	authedGet(t, srv, st, "/") // creates admin "ben" (id=1) + session "testtok"
	ben, ok, err := st.GetUserByName("ben")
	if err != nil || !ok {
		t.Fatalf("ben=%+v ok=%v err=%v", ben, ok, err)
	}
	admin2ID, err := st.CreateUser("admin2", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}

	ids := []int64{ben.ID, admin2ID}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/settings/users/"+strconv.FormatInt(id, 10)+"/delete", nil)
			req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
		}(id)
	}
	wg.Wait()

	users, err := st.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	admins := 0
	for _, u := range users {
		if u.Role == "admin" {
			admins++
		}
	}
	if admins < 1 {
		t.Fatalf("expected at least 1 admin remaining, got %d (users=%+v)", admins, users)
	}
}
