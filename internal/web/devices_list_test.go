package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// listFixture is a small fleet covering every filter: two subnets, online and
// offline devices, an unreviewed discovery on a randomized MAC, a tagged
// device, a Proxmox guest its integration no longer lists, and one address
// two devices claim.
type listFixture struct {
	lan, iot int64
}

func seedDeviceList(t *testing.T, st *store.Store) listFixture {
	t.Helper()
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	lan, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Name: "LAN", Kind: "lan", ScanIntervalSec: 120})
	iot, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.20.0/24", Name: "IoT", Kind: "lan", ScanIntervalSec: 120})
	mk := func(d store.Device, mac string, subnet int64, ip string, online bool) int64 {
		id, err := st.CreateDevice(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		var m *string
		if mac != "" {
			m = &mac
		}
		f, _ := st.AddIface(ctx, id, m, nil)
		if ip != "" {
			if _, err := st.AssignIP(ctx, f, subnet, ip, "dhcp"); err != nil {
				t.Fatal(err)
			}
		}
		if online {
			st.MarkSeen(ctx, f, 1, time.Now())
		}
		return id
	}
	mk(store.Device{Name: "nas", Kind: "server", Source: "manual"}, "00:11:22:33:44:01", lan, "10.0.0.5", true)
	laptop := mk(store.Device{Name: "laptop", Kind: "computer", Source: "manual"}, "00:11:22:33:44:02", lan, "10.0.0.50", false)
	st.SetDeviceTags(ctx, laptop, []string{"family"})
	mk(store.Device{Name: "plug", Kind: "iot", Source: "manual"}, "00:11:22:33:44:03", iot, "10.0.20.7", true)
	mk(store.Device{Name: "stranger", Kind: "other", Source: "scan"}, "da:a1:19:00:00:01", lan, "10.0.0.61", true)
	mk(store.Device{Name: "phone", Kind: "phone", Source: "manual"}, "00:11:22:33:44:05", lan, "10.0.0.61", false)
	guest := mk(store.Device{Name: "k3s", Kind: "vm", Source: "proxmox"}, "", lan, "", false)
	if _, err := st.DB.Exec(`UPDATE device SET upstream_missing_since=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), guest); err != nil {
		t.Fatal(err)
	}
	return listFixture{lan: lan, iot: iot}
}

// rowNames lists the device names in the table body, in order.
func rowNames(body string) []string {
	re := regexp.MustCompile(`<div class="dev-name"><a href="/devices/\d+">([^<]+)</a>`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestDeviceListFilterParams(t *testing.T) {
	srv, st := testServer(t)
	fx := seedDeviceList(t, st)
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"nas", "laptop", "phone", "stranger", "plug", "k3s"}},
		{"status=offline", []string{"laptop", "phone", "k3s"}},
		{"status=online", []string{"nas", "stranger", "plug"}},
		{"kind=iot", []string{"plug"}},
		{"subnet=" + itoa(fx.iot), []string{"plug"}},
		{"tag=family", []string{"laptop"}},
		{"new=1", []string{"stranger"}},
		{"private=1", []string{"stranger"}},
		{"missing=1", []string{"k3s"}},
		{"status=offline&subnet=" + itoa(fx.lan), []string{"laptop", "phone"}},
		{"q=pho&status=offline", []string{"phone"}},
		// Values the page does not know are ignored rather than matching nothing.
		{"kind=toaster&status=sideways&subnet=x", []string{"nas", "laptop", "phone", "stranger", "plug", "k3s"}},
	}
	for _, c := range cases {
		body := authedGet(t, srv, st, "/devices?"+c.query).Body.String()
		if got := rowNames(body); strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("?%s: rows %v, want %v", c.query, got, c.want)
		}
	}

	// The count says how many match out of how many exist.
	body := authedGet(t, srv, st, "/devices?status=offline").Body.String()
	if !strings.Contains(body, "<strong>3</strong> of 6 devices") {
		t.Error("filtered count missing")
	}
	if !strings.Contains(body, `class="btn btn-quiet btn-sm dl-clear" href="/devices"`) {
		t.Error("filtered list offers no way to clear the filters")
	}
	// The form shows the filter it applied.
	if !strings.Contains(body, `<option value="offline" selected>`) {
		t.Error("status select does not show the filter")
	}
	if body := authedGet(t, srv, st, "/devices").Body.String(); !strings.Contains(body, "6 devices") ||
		strings.Contains(body, "dl-clear") {
		t.Error("unfiltered list should say 6 devices and offer no clear")
	}
}

// A plain GET form sends its blank fields too; the page drops them from the
// URL so it says only what is filtered.
func TestDeviceListDropsEmptyParams(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	rec := authedGet(t, srv, st, "/devices?q=&status=offline&kind=&subnet=&tag=")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/devices?status=offline" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	rec = authedGet(t, srv, st, "/devices?q=")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/devices" {
		t.Fatalf("all blank: code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

// The filter bar asks htmx for the results alone and gets no page shell.
func TestDeviceListResultsFragment(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	authedGet(t, srv, st, "/")
	req := httptest.NewRequest("GET", "/devices?kind=iot", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "dev-results")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "<html") || !strings.HasPrefix(strings.TrimSpace(body), `<div id="dev-results"`) {
		t.Fatalf("fragment is not the results alone: %.200s", body)
	}
	if got := rowNames(body); len(got) != 1 || got[0] != "plug" {
		t.Errorf("fragment rows %v", got)
	}
}

// The table shows five columns by default; MAC, lease, function and tags are
// there for the column picker but hidden until chosen.
func TestDeviceListColumnDefaults(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, col := range []string{"col-dev", "col-ip", "col-status", "col-kind", "col-seen"} {
		if !regexp.MustCompile(`<th class="` + col + `"`).MatchString(body) {
			t.Errorf("default column %s missing or optional", col)
		}
	}
	for _, col := range []string{"col-mac", "col-lease", "col-func", "col-tags"} {
		if !strings.Contains(body, `<th class="`+col+` opt"`) {
			t.Errorf("column %s should be optional", col)
		}
		if !strings.Contains(body, `data-col="`+strings.TrimPrefix(col, "col-")+`"`) {
			t.Errorf("column picker lacks %s", col)
		}
	}
	css := authedGet(t, srv, st, "/static/pages/devices.css").Body.String()
	if !strings.Contains(css, ".devtable .opt") {
		t.Error("devices.css does not hide optional columns by default")
	}
	if !strings.Contains(body, `href="/static/pages/devices.css"`) {
		t.Error("devices page does not load its stylesheet")
	}
}

// Tags are on by default: the page carries the show-tags class and a
// checked Tags box before any script runs, and the script keeps that default
// until someone picks columns.
func TestDeviceListShowsTagsByDefault(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, `id="devices" class="devices show-tags"`) {
		t.Error("devices page lacks the default show-tags class")
	}
	if !strings.Contains(body, `data-col="tags" checked`) {
		t.Error("column picker does not check Tags by default")
	}
	js := authedGet(t, srv, st, "/static/devices.js").Body.String()
	if !strings.Contains(js, "DEFAULT_COLS = ['tags']") {
		t.Error("devices.js lacks the default columns")
	}
}

// Tags show as coloured chips that link to the filtered list, and a tag
// filter in effect shows as a chip with a clear link next to the select
// that keeps the list's other query parameters.
func TestDeviceListTagChips(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	id, _ := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	st.SetDeviceTags(ctx, id, []string{"nas"})
	if _, err := st.DB.Exec(`UPDATE tag SET color=? WHERE name=?`, "teal", "nas"); err != nil {
		t.Fatal(err)
	}

	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, `class="tag tag-teal"`) {
		t.Error("tags column does not show the tag in its stored colour")
	}
	if !strings.Contains(body, `href="/devices?tag=nas"`) {
		t.Error("tag chip does not link to the filtered list")
	}

	body = authedGet(t, srv, st, "/devices?tag=nas&q=na&new=1").Body.String()
	re := regexp.MustCompile(`<a class="[^"]*" href="([^"]*)" aria-label="Clear tag filter"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("filtered list missing the clear-tag link: %s", body)
	}
	href := strings.ReplaceAll(m[1], "&amp;", "&")
	if strings.Contains(href, "tag=nas") {
		t.Errorf("clear-tag link still carries the tag filter: %s", href)
	}
	if !strings.Contains(href, "q=na") || !strings.Contains(href, "new=1") {
		t.Errorf("clear-tag link drops the other filters: %s", href)
	}
}

// htmxGet performs an htmx GET against the results target, the same way the
// filter bar's own hx-get does.
func htmxGet(t *testing.T, srv *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "dev-results")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

// activeTagOOBSection returns the body from the dl-active-tag out-of-band
// element up to (and including) the clear link, or up to a reasonable bound
// when there is no clear link (no tag filter in effect).
func activeTagOOBSection(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `id="dl-active-tag"`)
	if i < 0 {
		t.Fatalf("no dl-active-tag element in body: %.300s", body)
	}
	rest := body[i:]
	if j := strings.Index(rest, "</a>"); j >= 0 {
		return rest[:j+len("</a>")]
	}
	end := len(rest)
	if end > 300 {
		end = 300
	}
	return rest[:end]
}

// The filter bar's htmx response must keep the active-tag chip and its clear
// link in step with the results it swaps in: it carries the same fragment
// as an out-of-band swap, since only #dev-results itself is swapped in.
func TestDeviceListHTMXKeepsActiveTagInStep(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	id, _ := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	st.SetDeviceTags(ctx, id, []string{"nas"})
	authedGet(t, srv, st, "/") // seeds the session htmxGet reuses

	body := htmxGet(t, srv, "/devices?tag=nas&q=na")
	if !strings.Contains(body, `id="dl-active-tag" hx-swap-oob="true"`) {
		t.Fatalf("htmx response missing the active-tag out-of-band element: %.400s", body)
	}
	section := activeTagOOBSection(t, body)
	if !regexp.MustCompile(`<span class="tag tag-\w+" title="nas">nas</span>`).MatchString(section) {
		t.Errorf("oob element missing the nas chip: %s", section)
	}
	re := regexp.MustCompile(`href="([^"]*)" aria-label="Clear tag filter"`)
	m := re.FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("oob element missing the clear-tag link: %s", section)
	}
	href := strings.ReplaceAll(m[1], "&amp;", "&")
	if strings.Contains(href, "tag=nas") {
		t.Errorf("oob clear-tag link still carries the tag filter: %s", href)
	}
	if !strings.Contains(href, "q=na") {
		t.Errorf("oob clear-tag link drops the other filters: %s", href)
	}

	// With no tag filter (e.g. after choosing "Any tag"), the oob element is
	// still sent, but empty: no stale chip, no clear link.
	body = htmxGet(t, srv, "/devices?q=na")
	section = activeTagOOBSection(t, body)
	if strings.Contains(section, `class="tag `) || strings.Contains(section, "Clear tag filter") {
		t.Errorf("oob element should be empty with no tag filter: %s", section)
	}
}

// An address two devices claim in the same subnet is flagged on both rows.
func TestDeviceListFlagsDuplicateIPs(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	body := authedGet(t, srv, st, "/devices").Body.String()
	if n := strings.Count(body, `class="status is-conflict"`); n != 2 {
		t.Errorf("conflict chips = %d, want 2 (both holders of 10.0.0.61)", n)
	}
	// Still flagged when the other holder is filtered out.
	body = authedGet(t, srv, st, "/devices?q=phone").Body.String()
	if !strings.Contains(body, `class="status is-conflict"`) {
		t.Error("conflict not flagged once the other holder is filtered out")
	}
}

func TestDeviceListEmptyStates(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{"No devices yet", `href="/settings/network"`, `hx-get="/devices/new"`} {
		if !strings.Contains(body, want) {
			t.Errorf("empty inventory missing %q", want)
		}
	}
	if strings.Contains(body, `id="dev-filters"`) {
		t.Error("empty inventory shows filters with nothing to filter")
	}
	if v := viewerGet(t, srv, st, "/devices"); strings.Contains(v, `href="/settings/network"`) {
		t.Error("viewer is offered admin actions in the empty state")
	}

	seedDeviceList(t, st)
	body = authedGet(t, srv, st, "/devices?q=nothing-like-this").Body.String()
	if !strings.Contains(body, "No devices match") || !strings.Contains(body, ">Clear filters</a>") {
		t.Error("no-match state missing or without a way out")
	}
}

func TestDeviceBulkActions(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	ctx := t.Context()
	id := func(name string) string {
		rows, _ := st.ListDevices(ctx)
		for _, r := range rows {
			if r.Name == name {
				return itoa(r.ID)
			}
		}
		t.Fatalf("no device %s", name)
		return ""
	}
	nas, stranger, plug := id("nas"), id("stranger"), id("plug")

	// The admin list has the selection boxes and the actions.
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{`id="bulk"`, `form="bulk"`, `formaction="/devices/bulk/approve"`,
		`formaction="/devices/bulk/tag"`, `formaction="/devices/bulk/delete"`} {
		if !strings.Contains(body, want) {
			t.Errorf("admin list missing %q", want)
		}
	}

	ret := "status=online"
	rec := authedPost(t, srv, st, "/devices/bulk/approve", url.Values{"id": {stranger, nas, "999", "x"}, "return": {ret}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/devices?status=online" {
		t.Fatalf("approve: code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if d, _ := st.GetDevice(ctx, mustID(t, stranger)); !d.Reviewed {
		t.Error("approve left the device unreviewed")
	}

	rec = authedPost(t, srv, st, "/devices/bulk/tag", url.Values{"id": {nas, plug}, "tag": {"rack, core"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("tag: code=%d", rec.Code)
	}
	for _, dev := range []string{nas, plug} {
		tags, _ := st.DeviceTags(ctx, mustID(t, dev))
		if len(tags) != 2 {
			t.Errorf("device %s tags = %+v, want rack and core", dev, tags)
		}
	}
	if rec := authedPost(t, srv, st, "/devices/bulk/tag", url.Values{"id": {nas}, "tag": {" "}}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank tag: code=%d, want 400", rec.Code)
	}

	// Delete asks first when the browser has not.
	rec = authedPost(t, srv, st, "/devices/bulk/delete", url.Values{"id": {nas, plug}, "return": {ret}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Delete 2 devices?") {
		t.Fatalf("unconfirmed delete: code=%d", rec.Code)
	}
	if _, err := st.GetDevice(ctx, mustID(t, nas)); err != nil {
		t.Fatal("unconfirmed delete removed the device")
	}
	rec = authedPost(t, srv, st, "/devices/bulk/delete", url.Values{"id": {nas, plug}, "confirm": {"1"}, "return": {ret}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/devices?status=online" {
		t.Fatalf("delete: code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := st.GetDevice(ctx, mustID(t, plug)); err == nil {
		t.Error("confirmed delete kept the device")
	}

	// A return value cannot send the browser off the devices list.
	rec = authedPost(t, srv, st, "/devices/bulk/approve", url.Values{"id": {stranger}, "return": {"//evil.example/x"}})
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/devices") {
		t.Errorf("return escaped the list: %q", loc)
	}

	// The audit log names what was done to which devices.
	entries, _, _ := st.ListAudit(ctx, store.AuditFilter{})
	var actions []string
	for _, e := range entries {
		actions = append(actions, e.Action+" "+e.Target)
	}
	joined := strings.Join(actions, "\n")
	for _, want := range []string{"device.bulk_approve devices stranger, nas", "device.bulk_tag devices nas, plug", "device.bulk_delete devices nas, plug"} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit log lacks %q in:\n%s", want, joined)
		}
	}
}

// A viewer sees no selection or bulk actions, and posting them anyway changes
// nothing (the route walk checks the 403 for every admin route).
func TestDeviceBulkActionsAdminOnly(t *testing.T) {
	srv, st := testServer(t)
	seedDeviceList(t, st)
	body := viewerGet(t, srv, st, "/devices")
	for _, leak := range []string{`id="bulk"`, `form="bulk"`, "/devices/bulk/"} {
		if strings.Contains(body, leak) {
			t.Errorf("viewer list contains %q", leak)
		}
	}
	req := httptest.NewRequest("POST", "/devices/bulk/delete", strings.NewReader("id=1&confirm=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer bulk delete: code=%d, want 403", rec.Code)
	}
	if _, err := st.GetDevice(t.Context(), 1); err != nil {
		t.Error("viewer bulk delete removed a device")
	}
}

// A row's Approve comes back to the list as it was filtered, and on the
// subnet page to the subnet.
func TestDeviceListApproveReturnsToFilter(t *testing.T) {
	srv, st := testServer(t)
	fx := seedDeviceList(t, st)
	body := authedGet(t, srv, st, "/devices?new=1").Body.String()
	if !strings.Contains(body, `<input type="hidden" name="next" value="/devices?new=1">`) {
		t.Error("row Approve does not carry the filtered list as next")
	}
	sub := "/subnets/" + itoa(fx.lan)
	if body := authedGet(t, srv, st, sub).Body.String(); !strings.Contains(body, `name="next" value="`+sub+`"`) {
		t.Error("subnet page Approve does not come back to the subnet")
	}
}

func mustID(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// On a phone a stacked device row is one tap target for the device: the
// name link stretches over the row (unclipped by the ellipsis box), with the
// checkbox and Approve stacked above it.
func TestStackedDeviceRowIsOneTapTarget(t *testing.T) {
	srv, st := testServer(t)
	css := authedGet(t, srv, st, "/static/pages/devices.css").Body.String()
	for _, want := range []string{
		".devtable tr { position:relative; }",
		".devtable.devtable .dev-name a { position:static; }",
		".devtable.devtable .dev-name a::after { content:\"\"; position:absolute; inset:0;",
		".devtable td.col-sel, .devtable td.col-act { position:relative; z-index:1; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("devices.css missing %q", want)
		}
	}
}
