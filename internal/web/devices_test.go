package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

func TestDialogPersistsVendorModelFunction(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")

	// The dialog exposes the three inputs.
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	for _, want := range []string{`name="vendor"`, `name="model"`, `name="function"`} {
		if !strings.Contains(body, want) {
			t.Errorf("new dialog missing %q", want)
		}
	}

	// Create persists all three.
	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"archera8"}, "kind": {"router"},
		"vendor": {"TP-Link"}, "model": {"ARCHER-A8 v1"}, "function": {"AP Dachboden CH:1,36"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create code=%d", rec.Code)
	}
	d, _ := st.GetDevice(1)
	if d.Vendor != "TP-Link" || d.Model != "ARCHER-A8 v1" || d.Function != "AP Dachboden CH:1,36" {
		t.Fatalf("create persisted %+v", d)
	}

	// Update overwrites all three.
	rec = authedPost(t, srv, st, "/devices/1", url.Values{
		"name": {"archera8"}, "kind": {"router"},
		"vendor": {"TP-Link Corp"}, "model": {"ARCHER-C7 v5"}, "function": {"AP Garten"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update code=%d", rec.Code)
	}
	d2, _ := st.GetDevice(1)
	if d2.Vendor != "TP-Link Corp" || d2.Model != "ARCHER-C7 v5" || d2.Function != "AP Garten" {
		t.Fatalf("update persisted %+v", d2)
	}
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

func TestDetailPageHasEditButtonNoInlineForm(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "nas", Kind: "server", Notes: "shelf", Source: "manual"})
	body := authedGet(t, srv, st, "/devices/1").Body.String()

	if !strings.Contains(body, `hx-get="/devices/1/edit"`) {
		t.Error("detail page should have an Edit button targeting the edit fragment")
	}
	// The old inline edit form had a notes <textarea>; it now lives only in the dialog.
	if strings.Contains(body, "<textarea") {
		t.Error("detail page should no longer contain the inline edit form")
	}
	// The old per-tag add form posted to /devices/1/tags; it is gone.
	if strings.Contains(body, `/devices/1/tags`) {
		t.Error("detail page should no longer contain inline tag forms")
	}
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

func TestApproveDevice(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "unknown-bb", Kind: "other", Source: "scan"})
	// The unreviewed device shows an Approve control.
	if !strings.Contains(authedGet(t, srv, st, "/devices").Body.String(), "/devices/1/approve") {
		t.Fatal("unreviewed device should show an Approve control")
	}
	rec := authedPost(t, srv, st, "/devices/1/approve", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("approve code=%d", rec.Code)
	}
	d, _ := st.GetDevice(devID)
	if !d.Reviewed {
		t.Fatal("approve did not set reviewed")
	}
	// After approval the Approve control is gone.
	if strings.Contains(authedGet(t, srv, st, "/devices").Body.String(), "/devices/1/approve") {
		t.Fatal("approved device should no longer show Approve")
	}
}

func TestApproveRequiresAdmin(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	st.CreateDevice(store.Device{Name: "unknown-cc", Kind: "other", Source: "scan"})
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/devices/1/approve", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer approve code=%d, want 403", rec.Code)
	}
}

func TestGridShowsLowestIP(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	d, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	f, _ := st.AddIface(d, nil, nil)
	// Assign in non-ascending order so the insertion-order index 0 would be wrong.
	st.AssignIP(f, snID, "10.0.0.50", "dhcp")
	st.AssignIP(f, snID, "10.0.0.5", "static")
	body := authedGet(t, srv, st, "/devices").Body.String()
	gridIdx := strings.Index(body, `id="dev-grid"`)
	if gridIdx < 0 {
		t.Fatal("dev-grid not found")
	}
	grid := body[gridIdx:]
	if !strings.Contains(grid, "10.0.0.5") {
		t.Fatalf("grid tile missing lowest IP: %s", grid)
	}
	if strings.Contains(grid, "10.0.0.50") {
		t.Fatalf("grid tile shows insertion-order IP instead of lowest: %s", grid)
	}
}

func TestApproveClearsDashboardUnknown(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "unknown-aa", Kind: "other", Source: "scan"})

	data, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if data.Stats.Unknown != 1 {
		t.Fatalf("Stats.Unknown = %d, want 1", data.Stats.Unknown)
	}
	found := false
	for _, u := range data.Unknowns {
		if u.ID == devID {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown device missing from attention list before approve")
	}

	if err := st.SetDeviceReviewed(devID, true); err != nil {
		t.Fatal(err)
	}
	data2, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if data2.Stats.Unknown != 0 {
		t.Fatalf("Stats.Unknown after approve = %d, want 0", data2.Stats.Unknown)
	}
	for _, u := range data2.Unknowns {
		if u.ID == devID {
			t.Fatal("approved device still in attention list")
		}
	}
}

func TestApproveNonexistentDevice404(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/devices/999/approve", url.Values{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("approve nonexistent device code=%d, want 404", rec.Code)
	}
}

func TestDeviceListShowsStoredIcon(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "console", Kind: "other", Icon: "🎮", Source: "manual"})
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "🎮") {
		t.Fatal("device list should render the stored icon")
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

func TestDeviceNewDialogFragment(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	for _, want := range []string{`class="dialog"`, "ic-swatch", `name="parent_device_id"`, `name="tags"`, `name="mac"`} {
		if !strings.Contains(body, want) {
			t.Errorf("new dialog fragment missing %q", want)
		}
	}
	if strings.Contains(body, "<nav") {
		t.Error("new dialog should be a fragment, not a full page with <nav>")
	}
}

func TestDeviceEditDialogPrefilled(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	parentID, _ := st.CreateDevice(store.Device{Name: "core-switch", Kind: "switch", Source: "manual"})
	pid := parentID
	devID, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Notes: "shelf", ParentDeviceID: &pid, Source: "manual"})
	st.SetDeviceTags(devID, []string{"storage"})

	body := authedGet(t, srv, st, "/devices/2/edit").Body.String()
	if !strings.Contains(body, `value="nas"`) {
		t.Error("edit dialog missing prefilled name")
	}
	if !strings.Contains(body, `value="storage"`) {
		t.Error("edit dialog missing prefilled tags")
	}
	// Parent device (id 1) must be the pre-selected option. Kind-select
	// "selected" options carry string values (e.g. "server"), so match the
	// numeric parent value specifically.
	if !strings.Contains(body, "core-switch") || !strings.Contains(body, `value="1" selected`) {
		t.Error("edit dialog should pre-select the parent device")
	}
	// The edited device must not appear as a selectable parent of itself.
	if strings.Contains(body, "nas — ") || strings.Contains(body, ">nas<") {
		t.Error("edit dialog should exclude the device itself from parent options")
	}
	_ = devID
}

func TestDeviceEditDialogBadID404(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	if rec := authedGet(t, srv, st, "/devices/999/edit"); rec.Code != http.StatusNotFound {
		t.Fatalf("edit unknown device = %d, want 404", rec.Code)
	}
}

func TestCreateDeviceWithIconParentTags(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	parentID, _ := st.CreateDevice(store.Device{Name: "rack", Kind: "switch", Source: "manual"})

	rec := authedPost(t, srv, st, "/devices", url.Values{
		"name": {"nas"}, "kind": {"server"}, "icon": {"🗄️"},
		"parent_device_id": {strconv.FormatInt(parentID, 10)},
		"tags":             {"storage, media"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	// The new device is id 2 (parent is id 1).
	d, err := st.GetDevice(2)
	if err != nil {
		t.Fatal(err)
	}
	if d.Icon != "🗄️" {
		t.Errorf("icon=%q, want 🗄️", d.Icon)
	}
	if d.ParentDeviceID == nil || *d.ParentDeviceID != parentID {
		t.Errorf("parent=%v, want %d", d.ParentDeviceID, parentID)
	}
	tags, _ := st.DeviceTags(2)
	if len(tags) != 2 {
		t.Fatalf("tags=%v, want 2", tags)
	}
}

func TestUpdateDeviceSyncsTags(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	devID, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	st.SetDeviceTags(devID, []string{"a", "b"})

	rec := authedPost(t, srv, st, "/devices/1", url.Values{
		"name": {"nas"}, "kind": {"server"}, "tags": {"a"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update code=%d", rec.Code)
	}
	tags, _ := st.DeviceTags(devID)
	if len(tags) != 1 || tags[0].Name != "a" {
		t.Fatalf("after update tags=%v, want [a]", tags)
	}
}

func TestLayoutHasModalContainer(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{`id="modal"`, "/static/dialog.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("layout missing %q", want)
		}
	}
}

func TestRouterModemKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")

	// The dialog offers the new kinds.
	body := authedGet(t, srv, st, "/devices/new").Body.String()
	for _, want := range []string{`<option value="router">`, `<option value="modem">`} {
		if !strings.Contains(body, want) {
			t.Errorf("new dialog missing %q", want)
		}
	}

	// A router device can be created (handler accepts the kind, store persists it).
	rec := authedPost(t, srv, st, "/devices", url.Values{"name": {"ap"}, "kind": {"router"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create router code=%d body=%s", rec.Code, rec.Body.String())
	}
	d, _ := st.GetDevice(1)
	if d.Kind != "router" {
		t.Fatalf("kind=%q, want router", d.Kind)
	}
}

func TestListShowsAndFiltersNewFields(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateDevice(store.Device{Name: "pv", Kind: "iot", Source: "manual",
		Vendor: "Espressif Inc.", Model: "Shelly Plus Plug", Function: "PV Powermeter"})
	st.CreateDevice(store.Device{Name: "printer0", Kind: "printer", Source: "manual",
		Vendor: "Acme"})

	body := authedGet(t, srv, st, "/devices").Body.String()
	// Function value and the "vendor · model" subline render.
	if !strings.Contains(body, "PV Powermeter") {
		t.Error("list missing function value")
	}
	if !strings.Contains(body, "Espressif Inc. · Shelly Plus Plug") {
		t.Error("list missing vendor · model subline")
	}

	// ?q matches the vendor field.
	filtered := authedGet(t, srv, st, "/devices?q=espressif").Body.String()
	if !strings.Contains(filtered, "pv") || strings.Contains(filtered, "printer0") {
		t.Error("?q=espressif should match by vendor and exclude the Acme device")
	}
}

func TestDeviceRowRegression(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})
	d, _ := st.CreateDevice(store.Device{Name: "nas", Kind: "server", Source: "manual"})
	f, _ := st.AddIface(d, nil, nil)
	st.AssignIP(f, snID, "10.0.0.5", "static")
	body := authedGet(t, srv, st, "/devices").Body.String()
	if !strings.Contains(body, "nas") || !strings.Contains(body, "chip static") {
		t.Fatal("devices list should still render device rows after deviceRow extraction")
	}
}

func TestNewDeviceSubnetPreselect(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	body := authedGet(t, srv, st, "/devices/new?subnet=1").Body.String()
	if !strings.Contains(body, `value="1" selected`) {
		t.Fatalf("new device dialog should preselect subnet 1: %s", body)
	}
}

func TestNavDevicesBeforeSubnets(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/devices").Body.String()
	di := strings.Index(body, `href="/devices"`)
	si := strings.Index(body, `href="/subnets"`)
	if di < 0 || si < 0 {
		t.Fatalf("nav links missing: devices@%d subnets@%d", di, si)
	}
	if di > si {
		t.Fatalf("Devices nav link should come before Subnets: devices@%d subnets@%d", di, si)
	}
}

func TestDeviceIPKindToggle(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")

	// Toggle static -> dhcp.
	rec := authedPost(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/ip/kind", url.Values{
		"subnet_id": {strconv.FormatInt(snID, 10)}, "ip": {"10.0.0.1"}, "kind": {"dhcp"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle code=%d", rec.Code)
	}
	body := rec.Body.String()
	// The re-rendered control shows the new kind and offers the reverse next step.
	// Note: HTML attributes are encoded, so "kind":"static" appears as &#34;kind&#34;:&#34;static&#34;
	if !strings.Contains(body, "dhcp") || !strings.Contains(body, `&#34;kind&#34;:&#34;static&#34;`) {
		t.Fatalf("fragment did not reflect flip: %q", body)
	}
	ips, _ := st.ListIPs(ifID)
	if len(ips) != 1 || ips[0].Kind != "dhcp" {
		t.Fatalf("kind not persisted: %+v", ips)
	}

	// Toggle back dhcp -> static.
	authedPost(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/ip/kind", url.Values{
		"subnet_id": {strconv.FormatInt(snID, 10)}, "ip": {"10.0.0.1"}, "kind": {"static"},
	})
	ips, _ = st.ListIPs(ifID)
	if ips[0].Kind != "static" {
		t.Fatalf("toggle back failed: %+v", ips)
	}
}

func TestDeviceIPKindBadKind(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "router", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	rec := authedPost(t, srv, st, "/devices/"+strconv.FormatInt(devID, 10)+"/ip/kind", url.Values{
		"subnet_id": {strconv.FormatInt(snID, 10)}, "ip": {"10.0.0.1"}, "kind": {"bogus"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind code=%d, want 400", rec.Code)
	}
}
