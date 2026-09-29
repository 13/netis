package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

// htmxRequest sends a request the way htmx does, with HX-Request set, as the
// signed-in admin.
func htmxRequest(t *testing.T, srv *Server, st *store.Store, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	authedGet(t, srv, st, "/") // ensures admin+session exist
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// formTag returns the opening <form ...> tag whose action is action, or "".
func formTag(body, action string) string {
	i := strings.Index(body, `action="`+action+`"`)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(body[:i], "<form")
	end := strings.Index(body[i:], ">")
	if start < 0 || end < 0 {
		return ""
	}
	return body[start : i+end+1]
}

// Every form that deletes or signs something out asks before it posts.
func TestDestructiveFormsAskForConfirmation(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	linkID, _ := st.AddLink(t.Context(), devID, "ui", "https://nas.lan")
	st.SetCustomField(t.Context(), devID, "rack", "a1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	userID := addUser(t, st, "carol", "longenough1", "viewer", "caroltok")
	authedGet(t, srv, st, "/")
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	st.CreateSession(t.Context(), "ben-phone", ben.ID, "2099-01-01T00:00:00Z")

	device := authedGet(t, srv, st, "/devices/"+itoa(devID)).Body.String()
	subnets := authedGet(t, srv, st, "/settings/network").Body.String()
	users := authedGet(t, srv, st, "/settings/users").Body.String() + authedGet(t, srv, st, "/settings/sessions").Body.String()
	for _, c := range []struct{ body, action string }{
		{device, "/devices/" + itoa(devID) + "/delete"},
		{device, "/links/" + itoa(linkID) + "/delete"},
		{device, "/devices/" + itoa(devID) + "/fields/delete"},
		{subnets, "/settings/subnets/" + itoa(snID) + "/delete"},
		{users, "/settings/users/" + itoa(userID) + "/delete"},
		{users, "/settings/sessions/revoke-others"},
	} {
		tag := formTag(c.body, c.action)
		if tag == "" {
			t.Errorf("no form posting to %s", c.action)
			continue
		}
		if !strings.Contains(tag, `data-confirm="`) {
			t.Errorf("form posting to %s does not ask first: %s", c.action, tag)
		}
	}
	// Session revoke forms carry an opaque id; check each one on the page.
	if n, m := strings.Count(users, `/settings/sessions/`)-1, strings.Count(users, `data-confirm="Sign out this session?"`); m == 0 || m != n {
		t.Errorf("session revoke forms: %d confirmations for %d forms", m, n)
	}

	js := authedGet(t, srv, st, "/static/dialog.js").Body.String()
	if !strings.Contains(js, "data-confirm") || !strings.Contains(js, "confirm(") {
		t.Error("dialog.js does not act on data-confirm")
	}
}

// With nothing stored, the theme follows the OS preference, on every page
// that bootstraps it, and keeps following it when the OS setting changes.
func TestThemeFollowsOSPreferenceWhenUnset(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	pages := map[string]string{
		"layout": authedGet(t, srv, st, "/").Body.String(),
		"login":  rec.Body.String(),
	}
	for name, body := range pages {
		if !strings.Contains(body, "prefers-color-scheme: light") {
			t.Errorf("%s bootstrap ignores the OS preference", name)
		}
		if strings.Contains(body, "|| 'dark'") {
			t.Errorf("%s bootstrap still forces dark", name)
		}
	}
	js := authedGet(t, srv, st, "/static/theme.js").Body.String()
	if !strings.Contains(js, "matchMedia") || !strings.Contains(js, "'change'") {
		t.Error("theme.js does not follow OS preference changes")
	}
}

// Wide tables scroll inside their own box instead of widening the page, and
// the nav has a narrow-screen layout. (The events log is a list whose rows
// stack on a phone, so it has no table to scroll.)
func TestWideTablesAndNavFitPhones(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	for _, path := range []string{"/settings/users", "/settings/sessions"} {
		body := authedGet(t, srv, st, path).Body.String()
		if !strings.Contains(body, `class="table-wrap"`) {
			t.Errorf("%s: table not wrapped in a scroll container", path)
		}
	}
	// The devices table does not scroll sideways: it fits the page with its
	// default columns, and on phones its rows stack (pages/devices.css).
	if body := authedGet(t, srv, st, "/devices").Body.String(); !strings.Contains(body, `class="devtable"`) {
		t.Error("/devices: no devices table")
	}
	css := authedGet(t, srv, st, "/static/app.css").Body.String()
	for _, want := range []string{".table-wrap", "@media (max-width:640px)", ":focus-visible"} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %q", want)
		}
	}
}

// Dialogs are native <dialog> elements labelled by their heading, and grid
// cells say their address and state in words.
func TestDialogsAndGridCellsAreAccessible(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(t.Context(), devID, nil, nil)
	if _, err := st.AssignIP(t.Context(), ifID, snID, "10.0.0.1", "static"); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/devices/new", "/devices/" + itoa(devID) + "/edit"} {
		body := htmxRequest(t, srv, st, "GET", path, nil).Body.String()
		if !strings.Contains(body, "<dialog") || !strings.Contains(body, `aria-labelledby="dlg-title"`) ||
			!strings.Contains(body, `id="dlg-title"`) {
			t.Errorf("%s: not a labelled native dialog: %s", path, body)
		}
	}

	grid := authedGet(t, srv, st, "/subnets/"+itoa(snID)).Body.String()
	for _, want := range []string{`aria-label="10.0.0.1 not seen yet, static, gw`, `aria-label="10.0.0.2 free`, `aria-label="10.0.0.0 network address`} {
		if !strings.Contains(grid, want) {
			t.Errorf("grid missing %q", want)
		}
	}
	if !strings.Contains(grid, `role="grid"`) || !strings.Contains(grid, `role="gridcell"`) {
		t.Error("the subnet grid should be an ARIA grid of cells")
	}
	// A port's details are a labelled region whose heading is the address.
	panel := htmxRequest(t, srv, st, "GET", "/subnets/"+itoa(snID)+"/cell?ip=10.0.0.1", nil).Body.String()
	if !strings.Contains(panel, `id="cp-title"`) || !strings.Contains(grid, `aria-label="Address details"`) {
		t.Errorf("cell details are not a labelled panel: %s", panel)
	}
}

// A device form that fails validation comes back as the form, with the
// message and what was typed, keeping the status code: a full page for a
// plain post, the dialog alone for htmx.
func TestDeviceFormErrorsRenderInline(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	form := url.Values{"name": {"printer"}, "kind": {"printer"}, "mac": {"zz:zz"}, "vendor": {"HP"}}

	rec := authedPost(t, srv, st, "/devices", form)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("plain post code=%d", rec.Code)
	}
	for _, want := range []string{"<nav", `class="form-error"`, "Invalid MAC address", `value="printer"`, `value="HP"`, `value="zz:zz"`} {
		if !strings.Contains(body, want) {
			t.Errorf("plain post error page missing %q", want)
		}
	}

	rec = htmxRequest(t, srv, st, "POST", "/devices", form)
	body = rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("htmx post code=%d", rec.Code)
	}
	if strings.Contains(body, "<nav") || !strings.Contains(body, "<dialog") || !strings.Contains(body, "Invalid MAC address") {
		t.Errorf("htmx error should be the dialog with the message: %s", body)
	}

	// Success under htmx navigates the whole page rather than swapping the
	// device page into the modal.
	rec = htmxRequest(t, srv, st, "POST", "/devices", url.Values{"name": {"ok"}, "kind": {"other"}})
	if loc := rec.Header().Get("HX-Redirect"); !strings.HasPrefix(loc, "/devices/") {
		t.Errorf("htmx create: HX-Redirect=%q code=%d", loc, rec.Code)
	}

	// A duplicate MAC on create is a 409 shown in the form.
	authedPost(t, srv, st, "/devices", url.Values{"name": {"a"}, "kind": {"other"}, "mac": {"aa:bb:cc:dd:ee:01"}})
	rec = authedPost(t, srv, st, "/devices", url.Values{"name": {"b"}, "kind": {"other"}, "mac": {"aa:bb:cc:dd:ee:01"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `class="form-error"`) {
		t.Errorf("duplicate MAC = %d, want 409 with inline error", rec.Code)
	}

	js := authedGet(t, srv, st, "/static/dialog.js").Body.String()
	if !strings.Contains(js, "htmx:beforeSwap") {
		t.Error("dialog.js does not let htmx swap a form error into the modal")
	}
}

// Settings conflicts come back as the settings page with the message.
func TestSettingsErrorsRenderInline(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	subnet := url.Values{"cidr": {"10.1.0.0/24"}, "name": {"lan"}, "kind": {"lan"}, "scan_interval_sec": {"120"}}
	authedPost(t, srv, st, "/settings/subnets", subnet)
	rec := authedPost(t, srv, st, "/settings/subnets", subnet)
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate subnet code=%d", rec.Code)
	}
	for _, want := range []string{"<nav", `class="form-error"`, "already exists", `href="/settings/network" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("duplicate subnet page missing %q", want)
		}
	}

	user := url.Values{"username": {"carol"}, "password": {"longenough1"}, "role": {"viewer"}}
	authedPost(t, srv, st, "/settings/users", user)
	rec = authedPost(t, srv, st, "/settings/users", user)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `class="form-error"`) ||
		!strings.Contains(rec.Body.String(), `href="/settings/users" aria-current="page"`) {
		t.Errorf("duplicate user = %d, want 409 on the users tab with the message", rec.Code)
	}

	rec = authedPost(t, srv, st, "/settings/general", url.Values{"offline_after": {"99"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `class="form-error"`) {
		t.Errorf("bad general setting = %d, want 400 with inline error", rec.Code)
	}
}

// The device search works as a plain GET form, and New/Edit are links to the
// form pages, which render as full pages without htmx.
func TestDeviceControlsWorkWithoutJS(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})

	list := authedGet(t, srv, st, "/devices?sort=name&dir=desc").Body.String()
	search := formTag(list, "/devices")
	if search == "" || !strings.Contains(search, `method="get"`) {
		t.Fatalf("search is not inside a GET form: %q", search)
	}
	if !strings.Contains(list, `<input type="hidden" name="sort" value="name">`) {
		t.Error("search form drops the current sort")
	}
	if !strings.Contains(list, `href="/devices/new"`) {
		t.Error("New device is not a link")
	}
	page := authedGet(t, srv, st, "/devices/"+itoa(devID)).Body.String()
	if !strings.Contains(page, `href="/devices/`+itoa(devID)+`/edit"`) {
		t.Error("Edit is not a link")
	}

	for _, path := range []string{"/devices/new", "/devices/" + itoa(devID) + "/edit"} {
		body := authedGet(t, srv, st, path).Body.String()
		if !strings.Contains(body, "<nav") || !strings.Contains(body, `name="name"`) {
			t.Errorf("%s without htmx is not a full page with the form", path)
		}
	}
}
