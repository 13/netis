package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"netis/internal/netdetect"
	"netis/internal/store"
)

func TestCreateSubnetViaSettings(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
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
	st.SetSetting("onboarded", "1")
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
	st.SetSetting("onboarded", "1")
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
	st.SetSetting("onboarded", "1")
	// Seed a stored password, then load the settings page as admin.
	st.SetSetting("pihole_password", "topsecret")
	rec := authedGet(t, srv, st, "/settings?tab=integrations")
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
	st.SetSetting("onboarded", "1")
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

func TestIntegrationStatusRendered(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.SetSetting("pihole_url", "https://pi.hole")
	st.SetSetting("proxmox_url", "https://pve:8006")
	now := time.Now().UTC().Format(time.RFC3339)
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "pihole", OK: true, LastRun: now, Detail: "48 leases, 2 new"})
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "proxmox", OK: false, LastRun: now, Detail: "auth failed"})

	body := authedGet(t, srv, st, "/settings?tab=integrations").Body.String()
	for _, want := range []string{"Pi-hole", "connected", "48 leases, 2 new", "failing", "not configured"} {
		if !strings.Contains(body, want) {
			t.Errorf("integrations tab missing %q", want)
		}
	}
}

func TestWelcomeIntegrationsStillRenders(t *testing.T) {
	srv, st := testServer(t)
	body := authedGet(t, srv, st, "/welcome/integrations").Body.String()
	for _, want := range []string{`name="proxmox_url"`, `name="wg_ssh_addr"`, `name="pihole_url"`, "<legend>"} {
		if !strings.Contains(body, want) {
			t.Errorf("welcome integrations missing %q", want)
		}
	}
}

func TestSettingsTabsShowOneSection(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	// Users tab shows the users section, not the subnet "Add subnet" form.
	rec := authedGet(t, srv, st, "/settings?tab=users")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Add user") {
		t.Fatalf("users tab: code=%d", rec.Code)
	}
	if strings.Contains(body, "Add subnet") {
		t.Fatal("users tab must not render the subnet create form")
	}
	// Unknown tab falls back to subnets.
	rec = authedGet(t, srv, st, "/settings?tab=bogus")
	if !strings.Contains(rec.Body.String(), "Add subnet") {
		t.Fatal("unknown tab should fall back to subnets")
	}
}

func TestSettingsTabsRender(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	subnets := authedGet(t, srv, st, "/settings?tab=subnets").Body.String()
	if !strings.Contains(subnets, "10.0.0.0/24") || !strings.Contains(subnets, "setting-card") {
		t.Error("subnets tab should render the subnet as a card")
	}
	if !strings.Contains(subnets, `hx-post="/subnets/1/scan"`) {
		t.Error("subnets tab should keep the per-subnet scan control")
	}
	users := authedGet(t, srv, st, "/settings?tab=users").Body.String()
	if !strings.Contains(users, `name="username"`) {
		t.Error("users tab missing add-user form")
	}
	gen := authedGet(t, srv, st, "/settings?tab=general").Body.String()
	if !strings.Contains(gen, `name="offline_after"`) {
		t.Error("general tab missing offline_after field")
	}
}

func TestSettingsSubnetsTabShowsDetected(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	// Inject a detected subnet not yet configured.
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{{CIDR: "192.168.7.0/24", Iface: "eth0"}}, nil
	}
	rec := authedGet(t, srv, st, "/settings?tab=subnets")
	if !strings.Contains(rec.Body.String(), "192.168.7.0/24") {
		t.Fatal("detected subnet should appear on the subnets tab")
	}
	// Once configured, it is no longer offered.
	st.CreateSubnet(store.Subnet{CIDR: "192.168.7.0/24", Name: "eth0", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
	rec = authedGet(t, srv, st, "/settings?tab=subnets")
	// The configured subnet shows in the table, but not as a fresh "add" row.
	if strings.Contains(rec.Body.String(), "no new subnets detected") == false {
		t.Fatal("expected 'no new subnets detected' once all detected subnets exist")
	}
}
