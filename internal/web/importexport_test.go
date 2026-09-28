package web

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"netis/internal/store"
)

func TestExportJSON(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	devID, _ := seedInventory(t, st)
	st.CreateDevice(t.Context(), store.Device{Name: "bare", Kind: "other", Source: "manual"})

	rec := authedGet(t, srv, st, "/api/export/devices.json")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("code=%d disposition=%q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	var out struct{ Devices []exportDevice }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Devices) != 2 {
		t.Fatalf("devices = %+v", out.Devices)
	}
	var gw, bare exportDevice
	for _, d := range out.Devices {
		if d.ID == devID {
			gw = d
		} else {
			bare = d
		}
	}
	if gw.Name != "gw" || gw.Vendor != "MikroTik" || !gw.Online || gw.LastSeen == nil ||
		!reflect.DeepEqual(gw.Tags, []string{"core"}) || len(gw.Ifaces) != 1 ||
		*gw.Ifaces[0].MAC != "bc:24:11:00:00:01" ||
		!reflect.DeepEqual(gw.Ifaces[0].IPs, []exportIP{{IP: "10.0.0.1", Kind: "static", Subnet: "10.0.0.0/24"}}) {
		t.Fatalf("gw = %+v", gw)
	}
	// Empty lists, not null, for a device with nothing attached.
	if !strings.Contains(rec.Body.String(), `"ifaces":[]`) || bare.Tags == nil {
		t.Errorf("bare device: %s", rec.Body.String())
	}
}

func TestExportCSV(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	seedInventory(t, st)
	st.CreateDevice(t.Context(), store.Device{Name: "=HYPERLINK(\"http://x\")", Kind: "other", Source: "manual", Notes: "a, b"})

	// A viewer may export.
	cookie := viewerSession(t, st)
	req := httptest.NewRequest("GET", "/api/export/devices.csv", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	recs, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recs[0], csvHeader) || len(recs) != 3 {
		t.Fatalf("csv = %q", recs)
	}
	byName := map[string][]string{}
	for _, r := range recs[1:] {
		byName[r[1]] = r
	}
	gw := byName["gw"]
	if gw == nil || gw[4] != "bc:24:11:00:00:01" || gw[5] != "10.0.0.1" || gw[6] != "core" || gw[11] != "true" {
		t.Fatalf("gw row = %q", gw)
	}
	// A formula-looking name is defused for spreadsheets.
	if byName[`'=HYPERLINK("http://x")`] == nil {
		t.Errorf("formula cell not escaped: %q", recs)
	}
}

// importPost uploads a CSV file to the import route as the admin, optionally
// committing.
func importPost(t *testing.T, srv *Server, st *store.Store, data string, commit bool) *httptest.ResponseRecorder {
	t.Helper()
	authedGet(t, srv, st, "/")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "devices.csv")
	fw.Write([]byte(data))
	if commit {
		mw.WriteField("commit", "1")
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/devices/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestImportDryRunThenCommit(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})

	mk := func(d store.Device, mac string) int64 {
		id, err := st.CreateDevice(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		st.AddIface(ctx, id, &mac, nil)
		return id
	}
	guess := mk(store.Device{Name: "10.0.0.7", Kind: "other", Source: "scan"}, "aa:00:00:00:00:01")
	vm := mk(store.Device{Name: "vm101", Kind: "vm", Source: "proxmox", Notes: "set by user"}, "aa:00:00:00:00:02")
	same := mk(store.Device{Name: "nas", Kind: "server", Source: "manual", Vendor: "Synology"}, "aa:00:00:00:00:03")
	st.SetDeviceTags(ctx, same, []string{"core"})

	data := "MAC,Name,Kind,IP,Tags,Notes,Vendor\n" +
		"AA-00-00-00-00-01,printer,printer,,office,,HP\n" + // rename the scan guess, fill vendor
		"aa:00:00:00:00:02,renamed,server,,lab,from csv,Dell\n" + // keep identity and notes; fill vendor, add tag
		"aa:00:00:00:00:03,nas,server,,core,,Synology\n" + // nothing new
		"aa:00:00:00:00:04,new-box,,10.0.0.50,x;y,hello,\n" + // create
		"aa:00:00:00:00:05,,other,,,,\n" + // create without a name
		",nomac,other,,,,\n" +
		"zz,badmac,other,,,,\n" +
		"aa:00:00:00:00:06,badkind,toaster,,,,\n" +
		"aa:00:00:00:00:07,far,other,192.168.1.1,,,\n" + // IP in no subnet
		"aa-00-00-00-00-04,dup,other,,,,\n" // same MAC twice

	before := deviceCount(t, st)
	rec := importPost(t, srv, st, data, false)
	if rec.Code != 200 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Dry run", "<strong>1</strong> to create", "<strong>2</strong> to update",
		"<strong>1</strong> unchanged", "<strong>6</strong> with errors",
		"no MAC address", "invalid MAC", "unknown kind", "no configured subnet contains 192.168.1.1",
		"new device needs a name", "MAC also on line", `name="commit"`} {
		if !strings.Contains(body, want) {
			t.Errorf("preview lacks %q", want)
		}
	}
	if n := deviceCount(t, st); n != before {
		t.Fatalf("dry run wrote: %d -> %d devices", before, n)
	}
	if d, _ := st.GetDevice(ctx, guess); d.Name != "10.0.0.7" {
		t.Fatal("dry run renamed a device")
	}

	rec = importPost(t, srv, st, data, true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Imported") {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	if n := deviceCount(t, st); n != before+1 {
		t.Fatalf("devices %d -> %d, want one created", before, n)
	}
	g, _ := st.GetDevice(ctx, guess)
	if g.Name != "printer" || g.Kind != "printer" || g.Vendor != "HP" {
		t.Errorf("scan guess = %+v", g)
	}
	v, _ := st.GetDevice(ctx, vm)
	if v.Name != "vm101" || v.Kind != "vm" || v.Notes != "set by user" || v.Vendor != "Dell" || v.Source != "proxmox" {
		t.Errorf("integration device clobbered: %+v", v)
	}
	if tags, _ := st.DeviceTags(ctx, vm); len(tags) != 1 || tags[0].Name != "lab" {
		t.Errorf("vm tags = %+v", tags)
	}
	ifc, ok, _ := st.FindIfaceByMAC(ctx, "aa:00:00:00:00:04")
	if !ok {
		t.Fatal("new device not created")
	}
	nd, _ := st.GetDevice(ctx, ifc.DeviceID)
	if nd.Name != "new-box" || nd.Kind != "other" || nd.Source != "manual" || nd.Notes != "hello" {
		t.Errorf("created %+v", nd)
	}
	if ips, _ := st.ListIPs(ctx, ifc.ID); len(ips) != 1 || ips[0].IP != "10.0.0.50" || ips[0].Kind != "static" {
		t.Errorf("created IPs = %+v", ips)
	}
	if tags, _ := st.DeviceTags(ctx, ifc.DeviceID); len(tags) != 2 {
		t.Errorf("created tags = %+v", tags)
	}
}

// An export fed straight back in changes nothing.
func TestExportImportRoundTrip(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	seedInventory(t, st)
	export := authedGet(t, srv, st, "/api/export/devices.csv").Body.String()
	rec := importPost(t, srv, st, export, false)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<strong>1</strong> unchanged") ||
		!strings.Contains(rec.Body.String(), "<strong>0</strong> with errors") {
		t.Fatalf("round trip preview: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportRefusesUnusableFiles(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	for name, data := range map[string]string{
		"empty":   "",
		"no mac":  "name,kind\nx,other\n",
		"not csv": "mac,name\n\"unterminated,x\n",
	} {
		rec := importPost(t, srv, st, data, false)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "form-error") {
			t.Errorf("%s: code=%d", name, rec.Code)
		}
	}
	// The preview's hidden field posts the text back without a file.
	authedGet(t, srv, st, "/")
	rec := authedPost(t, srv, st, "/devices/import", url.Values{"csv": {"mac,name\naa:00:00:00:00:09,x\n"}, "commit": {"1"}})
	if rec.Code != 200 {
		t.Fatalf("commit from hidden field: %d", rec.Code)
	}
	if _, ok, _ := st.FindIfaceByMAC(t.Context(), "aa:00:00:00:00:09"); !ok {
		t.Error("commit from hidden field created nothing")
	}
}

// The import route alone takes bodies over the global 1 MB cap, up to 5 MB.
func TestImportBodyLimit(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	row := "aa:00:00:00:00:01,name,other,,,," + strings.Repeat("n", 90) + "\n"
	big := "mac,name,kind,ip,tags,vendor,notes\n" + strings.Repeat(row, (2<<20)/len(row))
	if rec := importPost(t, srv, st, big, false); rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("2 MB import refused with 413")
	}
	huge := strings.Repeat(row, (6<<20)/len(row))
	if rec := importPost(t, srv, st, huge, false); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("6 MB import: code=%d, want 413", rec.Code)
	}
	// Everything else keeps the 1 MB cap.
	rec := authedPost(t, srv, st, "/devices", url.Values{"notes": {strings.Repeat("n", 2<<20)}})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("2 MB device form: code=%d, want 413", rec.Code)
	}
}

func TestDeviceListExportImportButtons(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	admin := authedGet(t, srv, st, "/devices").Body.String()
	for _, want := range []string{`href="/api/export/devices.csv"`, `href="/api/export/devices.json"`, `href="/devices/import"`} {
		if !strings.Contains(admin, want) {
			t.Errorf("admin device list lacks %s", want)
		}
	}
	viewer := viewerGet(t, srv, st, "/devices")
	if !strings.Contains(viewer, `href="/api/export/devices.csv"`) || strings.Contains(viewer, `href="/devices/import"`) {
		t.Error("viewer device list: export missing or import offered")
	}
	page := viewerGet(t, srv, st, "/devices/import")
	if strings.Contains(page, `type="file"`) || !strings.Contains(page, "needs the admin role") {
		t.Error("viewer import page offers the upload form")
	}
}
