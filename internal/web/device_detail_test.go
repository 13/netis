package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// The header offers Approve only for an unreviewed device, Wake on LAN only
// with a MAC and Scan ports only with an IP; Delete sits in the More menu,
// styled as danger. A viewer gets none of it.
func TestDeviceDetailActions(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	snID, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	bare, _ := st.CreateDevice(ctx, store.Device{Name: "bare", Kind: "other", Source: "manual"})
	found, _ := st.CreateDevice(ctx, store.Device{Name: "found", Kind: "other", Source: "scan"})
	mac := "3c:61:05:aa:bb:01"
	ifID, _ := st.AddIface(ctx, found, &mac, nil)
	st.AssignIP(ctx, ifID, snID, "10.0.0.9", "dhcp")

	page := authedGet(t, srv, st, "/devices/"+itoa(bare)).Body.String()
	for _, absent := range []string{"Wake on LAN", "Scan ports", "/approve"} {
		if strings.Contains(page, absent) {
			t.Errorf("reviewed device without MAC or IP offers %q", absent)
		}
	}
	for _, want := range []string{`class="more dd-more"`, `class="menu-item danger"`, "Delete device", "Turn on offline alerts", `class="btn btn-primary"`} {
		if !strings.Contains(page, want) {
			t.Errorf("device page missing %q", want)
		}
	}

	page = authedGet(t, srv, st, "/devices/"+itoa(found)).Body.String()
	for _, want := range []string{"Wake on LAN", "Scan ports", `action="/devices/` + itoa(found) + `/approve"`, `name="next" value="/devices/` + itoa(found) + `"`, ">new<"} {
		if !strings.Contains(page, want) {
			t.Errorf("unreviewed device with MAC and IP: page missing %q", want)
		}
	}
	// Approve is the primary action there, so Edit steps back.
	if i, j := strings.Index(page, "Approve"), strings.Index(page, `/devices/`+itoa(found)+`/edit`); i < 0 || j < 0 || i > j {
		t.Errorf("Approve should come before Edit: approve@%d edit@%d", i, j)
	}

	// Approving from the device page stays on it and says so.
	rec := authedPost(t, srv, st, "/devices/"+itoa(found)+"/approve", url.Values{"next": {"/devices/" + itoa(found)}})
	wantRedirect(t, rec, "/devices/"+itoa(found))
	if !strings.Contains(rec.Header().Get("Set-Cookie"), toastCookie+"=Device%20approved") {
		t.Errorf("approve leaves no toast: %q", rec.Header().Get("Set-Cookie"))
	}
	if d, _ := st.GetDevice(ctx, found); !d.Reviewed {
		t.Error("device not approved")
	}
	// From the list it still goes back to the list.
	wantRedirect(t, authedPost(t, srv, st, "/devices/"+itoa(found)+"/approve", url.Values{}), "/devices")

	viewer := viewerGet(t, srv, st, "/devices/"+itoa(found))
	for _, absent := range []string{"dd-more", "Delete device", "Wake on LAN", "dd-add"} {
		if strings.Contains(viewer, absent) {
			t.Errorf("viewer sees %q", absent)
		}
	}
}

// The availability bar draws one bar per day from the history and says the
// figure in words for a screen reader.
func TestDeviceDetailAvailabilityBar(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	devID, _ := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	ifID, _ := st.AddIface(ctx, devID, nil, nil)

	page := authedGet(t, srv, st, "/devices/"+itoa(devID)).Body.String()
	if !strings.Contains(page, "No availability recorded in the last 30 days yet.") {
		t.Error("bar without history does not say so")
	}

	hour := time.Now().UTC().Truncate(time.Hour).Format(time.RFC3339)
	st.RecordAvailability(ctx, ifID, true, hour)
	st.RecordAvailability(ctx, ifID, false, hour)
	page = authedGet(t, srv, st, "/devices/"+itoa(devID)).Body.String()
	if n := strings.Count(page, `<rect class="track"`); n != 30 {
		t.Errorf("bar has %d day tracks, want 30", n)
	}
	if n := strings.Count(page, `<rect class="fill"`); n != 1 {
		t.Errorf("bar fills %d days, want the one with data", n)
	}
	if !strings.Contains(page, `role="img" aria-label="50.0% online over the last 30 days.`) {
		t.Error("bar has no accessible summary of the figure")
	}
}

// Overview lists parent and children as links with their status, and each
// IP links to its subnet.
func TestDeviceDetailRelatedAndSubnetLinks(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	snID, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	sw, _ := st.CreateDevice(ctx, store.Device{Name: "core-switch", Kind: "switch", Source: "manual"})
	nas, _ := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual", ParentDeviceID: &sw})
	vm, _ := st.CreateDevice(ctx, store.Device{Name: "media-vm", Kind: "vm", Source: "manual", ParentDeviceID: &nas})
	ifID, _ := st.AddIface(ctx, nas, nil, nil)
	st.AssignIP(ctx, ifID, snID, "10.0.0.5", "static")

	page := authedGet(t, srv, st, "/devices/"+itoa(nas)).Body.String()
	for _, want := range []string{
		`href="/devices/` + itoa(sw) + `">core-switch</a>`,
		`href="/devices/` + itoa(vm) + `">media-vm</a>`,
		`href="/subnets/` + itoa(snID) + `">lab</a>`,
		"Server", // the kind in words
	} {
		if !strings.Contains(page, want) {
			t.Errorf("device page missing %q", want)
		}
	}
}

// The back link returns to the page of ours the user came from, and to the
// device list from anywhere else.
func TestDeviceDetailBackLink(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	authedGet(t, srv, st, "/")
	get := func(referer string) string {
		req := httptest.NewRequest("GET", "/devices/"+itoa(devID), nil)
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Body.String()
	}
	for _, c := range []struct{ referer, href, label string }{
		{"", "/devices", "Devices"},
		{"http://example.com/devices?q=nas&sort=name", "/devices?q=nas&amp;sort=name", "Devices"},
		{"http://example.com/subnets/" + itoa(snID), "/subnets/" + itoa(snID), "lab"},
		{"http://example.com/", "/", "Dashboard"},
		{"http://evil.example/subnets/1", "/devices", "Devices"},
		{"http://example.com/devices/" + itoa(devID), "/devices", "Devices"},
	} {
		page := get(c.referer)
		i := strings.Index(page, `class="dd-back"`)
		if i < 0 {
			t.Fatalf("no back link")
		}
		tag := page[i : i+strings.Index(page[i:], "</a>")]
		if !strings.Contains(tag, `href="`+c.href+`"`) || !strings.HasSuffix(strings.TrimSpace(tag), c.label) {
			t.Errorf("referer %q: back link %q, want %s to %s", c.referer, tag, c.label, c.href)
		}
	}
}

// The form asks for what matters first: name, kind, then MAC, IP and
// subnet; the rest waits under More details. Opened for an address, it
// says which one and picks its subnet.
func TestDeviceFormOrderAndPrefill(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/16", Name: "wide", Kind: "lan", ScanIntervalSec: 120})
	narrow, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.20.0/24", Name: "iot", Kind: "lan", ScanIntervalSec: 120})

	body := htmxRequest(t, srv, st, "GET", "/devices/new", nil).Body.String()
	last := -1
	for _, name := range []string{`name="name"`, `name="kind"`, `name="icon"`, `name="mac"`, `name="ip"`, `name="subnet_id"`,
		`class="df-more"`, `name="vendor"`, `name="model"`, `name="function"`, `name="parent_device_id"`, `name="notes"`, `name="tags"`} {
		i := strings.Index(body, name)
		if i < 0 {
			t.Fatalf("form missing %s", name)
		}
		if i < last {
			t.Errorf("%s comes before the field above it", name)
		}
		last = i
	}
	if strings.Contains(body, `<details class="df-more" open`) {
		t.Error("More details starts open on an empty form")
	}

	body = htmxRequest(t, srv, st, "GET", "/devices/new?ip=10.0.20.7", nil).Body.String()
	for _, want := range []string{">New device at 10.0.20.7<", `class="df-prefill"`, `value="` + itoa(narrow) + `" selected`} {
		if !strings.Contains(body, want) {
			t.Errorf("form for a free address missing %q", want)
		}
	}

	// Editing a device that has details opens them, so nothing is hidden.
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "tv", Kind: "iot", Source: "manual", Vendor: "LG"})
	body = htmxRequest(t, srv, st, "GET", "/devices/"+itoa(devID)+"/edit", nil).Body.String()
	if !strings.Contains(body, `<details class="df-more" open`) {
		t.Error("edit form hides the vendor it holds")
	}
}

func TestSubnetForPicksMostSpecific(t *testing.T) {
	subnets := []store.Subnet{{ID: 1, CIDR: "10.0.0.0/16"}, {ID: 2, CIDR: "10.0.20.0/24"}, {ID: 3, CIDR: "bogus"}}
	for ip, want := range map[string]int64{"10.0.20.7": 2, "10.0.1.1": 1, "192.168.1.1": 0, "": 0, "nope": 0} {
		if got := subnetFor(subnets, ip); got != want {
			t.Errorf("subnetFor(%q) = %d, want %d", ip, got, want)
		}
	}
}

// Actions that redirect leave a toast that repeats what was done.
func TestDeviceActionsLeaveToasts(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	path := "/devices/" + itoa(devID)
	for _, c := range []struct {
		path string
		form url.Values
		want string
	}{
		{path + "/alert", url.Values{"alert_offline": {"1"}}, "Offline%20alerts%20turned%20on"},
		{path + "/links", url.Values{"label": {"ui"}, "url": {"https://nas.lan"}}, "Link%20added"},
		{path + "/fields", url.Values{"key": {"rack"}, "value": {"a1"}}, "Field%20saved"},
		{path, url.Values{"name": {"nas"}, "kind": {"server"}}, "Changes%20saved"},
	} {
		rec := authedPost(t, srv, st, c.path, c.form)
		if got := rec.Header().Get("Set-Cookie"); !strings.Contains(got, toastCookie+"="+c.want) {
			t.Errorf("POST %s: Set-Cookie %q, want toast %s", c.path, got, c.want)
		}
	}
	js := authedGet(t, srv, st, "/static/toasts.js").Body.String()
	if !strings.Contains(js, "netis_toast") || !strings.Contains(js, "textContent") {
		t.Error("toasts.js does not show the flashed message as text")
	}
}
