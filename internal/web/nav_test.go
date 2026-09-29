package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

// The shell has a sidebar and a phone tab bar, both marking the page being
// shown, and no longer the old top nav. A device page belongs to Devices.
func TestNavigationMarksCurrentPage(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	for path, want := range map[string]string{
		"/":                       `href="/" aria-current="page"`,
		"/devices":                `href="/devices" aria-current="page"`,
		"/devices/" + itoa(devID): `href="/devices" aria-current="page"`,
		"/events":                 `href="/events" aria-current="page"`,
		"/settings/network":       `href="/settings/account" aria-current="page"`,
	} {
		body := authedGet(t, srv, st, path).Body.String()
		if n := strings.Count(body, want); n != 2 && !(path == "/settings/network" && n == 1) {
			t.Errorf("%s: %q appears %d times, want in the sidebar and the tab bar", path, want, n)
		}
		for _, part := range []string{`class="sidebar"`, `class="tabbar"`, `class="topbar"`, `action="/logout"`} {
			if !strings.Contains(body, part) {
				t.Errorf("%s: shell missing %s", path, part)
			}
		}
		if strings.Contains(body, `class="top"`) {
			t.Errorf("%s: still renders the old top nav", path)
		}
		if path == "/" && strings.Contains(body, `href="/devices" aria-current`) {
			t.Error("dashboard marks Devices current too")
		}
	}
}

// The command palette's fixed entries follow the role: a viewer is offered
// neither the admin settings pages nor the admin actions.
func TestPaletteEntriesFollowRole(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	admin := authedGet(t, srv, st, "/").Body.String()
	for _, want := range []string{`id="palette"`, `role="combobox"`, `data-action="new-device"`, `data-action="scan-all"`,
		`data-href="/settings/network"`, `id="shortcuts"`, "/static/palette.js"} {
		if !strings.Contains(admin, want) {
			t.Errorf("admin page missing %s", want)
		}
	}
	viewer := viewerGet(t, srv, st, "/")
	for _, leak := range []string{`data-action="new-device"`, `data-action="scan-all"`, `data-href="/settings/network"`,
		`data-href="/settings/users"`} {
		if strings.Contains(viewer, leak) {
			t.Errorf("viewer page offers %s", leak)
		}
	}
	if !strings.Contains(viewer, `data-href="/settings/account"`) {
		t.Error("viewer palette lacks the account page")
	}
}

// /api/search finds devices by name, IP and MAC and subnets by name or CIDR,
// for any signed-in role, and nobody else.
func TestSearchAPI(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	snID, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.20.0.0/24", Name: "iot", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(ctx, store.Device{Name: "kitchen-plug", Kind: "iot", Source: "manual"})
	mac := "aa:bb:cc:00:11:22"
	ifID, _ := st.AddIface(ctx, devID, &mac, nil)
	st.UpsertIPAssignment(ctx, ifID, snID, "10.20.0.7", "dhcp")
	st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})

	search := func(q string) searchResult {
		t.Helper()
		rec := authedGet(t, srv, st, "/api/search?q="+url.QueryEscape(q))
		if rec.Code != 200 {
			t.Fatalf("search %q: code=%d", q, rec.Code)
		}
		var res searchResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, q := range []string{"kitchen", "10.20.0.7", "cc:00:11"} {
		res := search(q)
		if len(res.Devices) != 1 || res.Devices[0].ID != devID || res.Devices[0].IP != "10.20.0.7" || res.Devices[0].MAC != mac {
			t.Errorf("search %q devices = %+v", q, res.Devices)
		}
	}
	if res := search("10.20"); len(res.Subnets) != 1 || res.Subnets[0].Name != "iot" || res.Subnets[0].CIDR != "10.20.0.0/24" {
		t.Errorf("subnet search = %+v", res.Subnets)
	}
	if res := search(""); res.Devices == nil || len(res.Devices) != 0 {
		t.Errorf("empty query devices = %+v, want an empty list", res.Devices)
	}

	if body := viewerGet(t, srv, st, "/api/search?q=nas"); !strings.Contains(body, `"name":"nas"`) {
		t.Errorf("viewer search = %s", body)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/search?q=nas", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous search: code=%d, want 401", rec.Code)
	}
}

// Each integration panel saves only its own settings; another integration's
// stored values survive. A panel that is not set up opens its form and has
// no Run now; one that is set up keeps the form closed behind Configure.
func TestIntegrationPanelsSaveSeparately(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	st.SetSetting(ctx, "proxmox_url", "https://pve:8006")
	st.SetSetting(ctx, "proxmox_token_id", "root@pam!netis")
	st.SetSetting(ctx, "proxmox_insecure", "1")

	body := authedGet(t, srv, st, "/settings/integrations").Body.String()
	pve := panelOf(body, "proxmox")
	if !strings.Contains(pve, `hx-post="/settings/integrations/proxmox/run"`) || strings.Contains(pve, `<details class="disclosure" open`) {
		t.Errorf("configured Proxmox panel: want Run now and a closed form:\n%s", pve)
	}
	pihole := panelOf(body, "pihole")
	if strings.Contains(pihole, "/run") || !strings.Contains(pihole, `<details class="disclosure" open`) {
		t.Errorf("unconfigured Pi-hole panel: want no Run now and an open form:\n%s", pihole)
	}

	rec := authedPost(t, srv, st, "/settings/integrations", url.Values{
		"integration": {"pihole"}, "pihole_url": {"https://pi.hole"}, "pihole_password": {"pw"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/integrations#integration-pihole" {
		t.Fatalf("save: code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	for k, want := range map[string]string{
		"pihole_url": "https://pi.hole", "pihole_password": "pw",
		"proxmox_url": "https://pve:8006", "proxmox_token_id": "root@pam!netis", "proxmox_insecure": "1",
	} {
		if v, _ := st.GetSetting(ctx, k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if rec := authedPost(t, srv, st, "/settings/integrations", url.Values{"integration": {"nope"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown integration: code=%d, want 400", rec.Code)
	}
}

// panelOf cuts one integration's panel out of the integrations page.
func panelOf(body, name string) string {
	i := strings.Index(body, `id="integration-`+name+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest, "</section>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// The retention form on the System page saves without the Network page's
// fields and returns to System.
func TestSystemRetentionSavesAlone(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.SetSetting(t.Context(), "offline_after", "4")
	rec := authedPost(t, srv, st, "/settings/general", url.Values{"section": {"system"}, "event_retention_days": {"12"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/system" {
		t.Fatalf("code=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if v, _ := st.GetSetting(t.Context(), "event_retention_days"); v != "12" {
		t.Errorf("event_retention_days = %q", v)
	}
	if v, _ := st.GetSetting(t.Context(), "offline_after"); v != "4" {
		t.Errorf("offline_after = %q, want it left alone", v)
	}
}

// On a phone the page names itself once, in its h1; the top bar carries the
// mark and search, not the page title again.
func TestTopbarDoesNotRepeatThePageTitle(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/events").Body.String()
	start := strings.Index(body, `<header class="topbar">`)
	end := strings.Index(body[start:], `</header>`)
	if start < 0 || end < 0 {
		t.Fatal("no top bar")
	}
	if bar := body[start : start+end]; strings.Contains(bar, "Events") {
		t.Errorf("top bar repeats the page title: %s", bar)
	}
	if !strings.Contains(body, "<h1>Events</h1>") {
		t.Error("page lost its h1")
	}
}
