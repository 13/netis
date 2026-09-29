package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// viewerGet fetches path as a viewer-role user, creating the admin and the
// viewer on first use.
func viewerGet(t *testing.T, srv *Server, st *store.Store, path string) string {
	t.Helper()
	if _, ok, _ := st.GetUserByName(t.Context(), "ben"); !ok {
		addAdmin(t, st)
	}
	u, ok, _ := st.GetUserByName(t.Context(), "eve")
	if !ok {
		id, err := st.CreateUser(t.Context(), "eve", "h", "viewer")
		if err != nil {
			t.Fatal(err)
		}
		u.ID = id
		if err := st.CreateSession(t.Context(), "viewertok", id, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as viewer: code=%d", path, rec.Code)
	}
	return rec.Body.String()
}

// integrationConfig is a set of integration settings whose values must never
// reach a viewer.
var integrationConfig = map[string]string{
	"proxmox_url":        "https://pve.internal:8006",
	"proxmox_token_id":   "root@pam!netis",
	"wg_ssh_addr":        "10.9.9.1:22",
	"wg_ssh_user":        "wgadmin",
	"wg_ssh_key_path":    "/root/.ssh/id_netis",
	"wg_ssh_known_hosts": "/root/.ssh/known_hosts_netis",
	"pihole_url":         "https://pihole.internal",
}

func seedSettingsForViewer(t *testing.T, st *store.Store) {
	t.Helper()
	st.SetSetting(t.Context(), "onboarded", "1")
	for k, v := range integrationConfig {
		if err := st.SetSetting(t.Context(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CreateUser(t.Context(), "carol", "h", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetIntegrationStatus(t.Context(), store.IntegrationStatus{
		Name: "proxmox", LastRun: time.Now().UTC().Format(time.RFC3339), Detail: "connection failed",
	}); err != nil {
		t.Fatal(err)
	}
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
}

// The settings page used to render the full admin UI to viewers: every
// integration's address, user and key path, every username, and forms and
// buttons that only answered 403 when used.
func TestViewerSettingsHideAdminConfig(t *testing.T) {
	srv, st := testServer(t)
	seedSettingsForViewer(t, st)

	forbidden := []string{
		"carol", "reset password", "Run now", "Add user", "Add subnet", "Detected subnets",
		`action="/settings/users"`, `/settings/users/`, `action="/settings/integrations"`,
		`/settings/integrations/`, `action="/settings/subnets`, `/settings/general`,
		`hx-post="/subnets/`, `name="offline_after"`,
	}
	for _, v := range integrationConfig {
		forbidden = append(forbidden, v)
	}
	// Nor does any page a viewer can open link into the Admin area.
	forbidden = append(forbidden, "/settings/network", "/settings/integrations", "/settings/notifications",
		`"/settings/users"`, "/settings/audit", "/settings/system", "Manage subnets", "Scan all subnets")
	for _, path := range []string{"/settings/account", "/settings/sessions", "/settings/tokens", "/", "/subnets", "/devices"} {
		body := viewerGet(t, srv, st, path)
		for _, f := range forbidden {
			if strings.Contains(body, f) {
				t.Errorf("%s shows a viewer %q", path, f)
			}
		}
	}
	// And asking for an admin page directly is refused.
	for _, path := range []string{"/settings/network", "/settings/integrations", "/settings/notifications",
		"/settings/users", "/settings/audit", "/settings/system"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer GET %s: code=%d, want 403", path, rec.Code)
		}
	}

	account := viewerGet(t, srv, st, "/settings/account")
	if !strings.Contains(account, `action="/settings/password"`) {
		t.Error("viewer account page missing the password form")
	}
	eve, _, _ := st.GetUserByName(t.Context(), "eve")
	st.CreateSession(t.Context(), "eve-phone", eve.ID, "2099-01-01T00:00:00Z")
	if sessions := viewerGet(t, srv, st, "/settings/sessions"); !strings.Contains(sessions, "/settings/sessions/revoke-others") {
		t.Error("viewer sessions page missing sign out other sessions")
	}
}

func TestAdminSettingsStillShowConfig(t *testing.T) {
	srv, st := testServer(t)
	seedSettingsForViewer(t, st)

	integrations := authedGet(t, srv, st, "/settings/integrations").Body.String()
	for _, v := range integrationConfig {
		if !strings.Contains(integrations, v) {
			t.Errorf("admin integrations tab missing %q", v)
		}
	}
	for _, want := range []string{"Run now", `action="/settings/integrations"`} {
		if !strings.Contains(integrations, want) {
			t.Errorf("admin integrations tab missing %q", want)
		}
	}
	users := authedGet(t, srv, st, "/settings/users").Body.String()
	for _, want := range []string{"carol", "Reset password", "Add user"} {
		if !strings.Contains(users, want) {
			t.Errorf("admin users tab missing %q", want)
		}
	}
	if network := authedGet(t, srv, st, "/settings/network").Body.String(); !strings.Contains(network, `action="/settings/general"`) {
		t.Error("admin network page missing the scanning form")
	}
}

// Every button that posts to an admin-only route answered a viewer with 403.
// Viewers no longer get the buttons.
func TestViewerPagesHideAdminActions(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, snID := seedInventory(t, st)
	unreviewed, err := st.CreateDevice(t.Context(), store.Device{Name: "unknown-1", Kind: "other", Source: "scan"})
	if err != nil {
		t.Fatal(err)
	}
	st.AddLink(t.Context(), devID, "ui", "https://gw.lan")
	st.SetCustomField(t.Context(), devID, "rack", "a1")

	forbidden := []string{
		`hx-post="/scan"`, "/scan\"", "/wol", "/portscan", "/delete", "/edit", "/approve",
		"/links", "/fields", "/ip/kind", "/devices/new", fmt.Sprintf(`hx-post="/subnets/%d/cell"`, snID),
		"Danger zone",
	}
	pages := []string{
		"/", "/dashboard/widgets", "/devices", "/subnets",
		fmt.Sprintf("/devices/%d", devID), fmt.Sprintf("/devices/%d", unreviewed),
		fmt.Sprintf("/subnets/%d", snID), fmt.Sprintf("/subnets/%d/grid", snID),
		fmt.Sprintf("/subnets/%d/cell?ip=10.0.0.1", snID),
	}
	for _, p := range pages {
		body := viewerGet(t, srv, st, p)
		for _, f := range forbidden {
			if strings.Contains(body, f) {
				t.Errorf("%s shows a viewer %q", p, f)
			}
		}
	}

	// The same pages still carry the actions for an admin.
	dev := authedGet(t, srv, st, fmt.Sprintf("/devices/%d", devID)).Body.String()
	for _, want := range []string{"/wol", "/portscan", "/delete", "/edit", "/links", "/fields", "/ip/kind"} {
		if !strings.Contains(dev, want) {
			t.Errorf("admin device page missing %q", want)
		}
	}
	grid := authedGet(t, srv, st, fmt.Sprintf("/subnets/%d", snID)).Body.String()
	for _, want := range []string{fmt.Sprintf("/subnets/%d/scan", snID), "/devices/new"} {
		if !strings.Contains(grid, want) {
			t.Errorf("admin grid page missing %q", want)
		}
	}
	if list := authedGet(t, srv, st, "/devices").Body.String(); !strings.Contains(list, "/approve") {
		t.Error("admin device list missing Approve")
	}
}
