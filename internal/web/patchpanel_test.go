package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/scan"
	"netis/internal/store"
)

// holdIP gives a new device an interface holding ip in snID.
func holdIP(t *testing.T, st *store.Store, snID int64, name, ip, kind string, mac *string) int64 {
	t.Helper()
	dev, err := st.CreateDevice(t.Context(), store.Device{Name: name, Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := st.AddIface(t.Context(), dev, mac, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIP(t.Context(), f, snID, ip, kind); err != nil {
		t.Fatal(err)
	}
	return f
}

// The patch panel is rows of 16 ports numbered along the left edge and 0-15
// across the top: one panel for a /24, one short row for a /28, and a stack
// of four panels for a /22 whose rows carry the last two octets.
func TestPatchPanelRowsAndNumbering(t *testing.T) {
	for _, tc := range []struct {
		cidr        string
		rows, ports int
		labels      []string
		sections    int
	}{
		{"10.0.0.0/24", 16, 256, []string{">.0<", ">.16<", ">.240<"}, 0},
		{"10.0.0.16/28", 1, 16, []string{">.16<"}, 0},
		{"10.0.4.0/22", 64, 1024, []string{">4.0<", ">4.16<", ">5.0<", ">7.240<"}, 4},
	} {
		t.Run(tc.cidr, func(t *testing.T) {
			srv, st := testServer(t)
			st.SetSetting(t.Context(), "onboarded", "1")
			snID, err := st.CreateSubnet(t.Context(), store.Subnet{CIDR: tc.cidr, Name: "lab", Kind: "lan", ScanIntervalSec: 120})
			if err != nil {
				t.Fatal(err)
			}
			body := authedGet(t, srv, st, fmt.Sprintf("/subnets/%d/grid", snID)).Body.String()
			if n := strings.Count(body, `role="row"`); n != tc.rows {
				t.Errorf("rows = %d, want %d", n, tc.rows)
			}
			if n := strings.Count(body, `class="port `); n != tc.ports {
				t.Errorf("ports = %d, want %d", n, tc.ports)
			}
			if !strings.Contains(body, "cols-16") || !strings.Contains(body, "<span>0</span>") || !strings.Contains(body, "<span>15</span>") {
				t.Error("column numbers 0-15 missing")
			}
			for _, l := range tc.labels {
				if !strings.Contains(body, `class="patch-off mono" aria-hidden="true"`+l) {
					t.Errorf("row label %s missing", l)
				}
			}
			if n := strings.Count(body, "patch-sec-label"); n != tc.sections {
				t.Errorf("panel labels = %d, want %d", n, tc.sections)
			}
			// Ports carry no htmx attributes or handlers of their own: one
			// delegated listener serves them all, however many there are.
			if strings.Contains(body, `<button type="submit" name="ip" value="10.0.4.1" class="port free" hx-`) ||
				strings.Count(body, "hx-") > 10 {
				t.Errorf("ports carry per-port htmx attributes (%d hx- in fragment)", strings.Count(body, "hx-"))
			}
		})
	}
}

// Next free IP names the first free host address, and says so when there
// is none.
func TestNextFreeIP(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/30", Name: "tiny", Kind: "lan", ScanIntervalSec: 120})
	holdIP(t, st, snID, "gw", "10.0.0.1", "static", nil)

	body := authedGet(t, srv, st, fmt.Sprintf("/subnets/%d", snID)).Body.String()
	if !strings.Contains(body, `data-next-free="10.0.0.2"`) || !strings.Contains(body, `href="/subnets/1?ip=10.0.0.2"`) {
		t.Fatalf("next free should be 10.0.0.2: %s", body)
	}
	holdIP(t, st, snID, "nas", "10.0.0.2", "dhcp", nil)
	body = authedGet(t, srv, st, fmt.Sprintf("/subnets/%d", snID)).Body.String()
	if strings.Contains(body, "data-next-free") || !strings.Contains(body, "No free IP") {
		t.Fatal("a full subnet should say there is no free IP")
	}
}

// The details panel shows the address, its state, the device with its MAC
// and last-seen time, and every device when two claim the address.
func TestCellPanelContent(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	mac := "aa:bb:cc:00:00:05"
	f := holdIP(t, st, snID, "nas", "10.0.0.5", "dhcp", &mac)
	st.MarkSeen(t.Context(), f, 1, time.Now().Add(-4*time.Minute))
	holdIP(t, st, snID, "gw", "10.0.0.9", "static", nil)
	holdIP(t, st, snID, "ghost", "10.0.0.9", "static", nil)

	nas := htmxRequest(t, srv, st, "GET", fmt.Sprintf("/subnets/%d/cell?ip=10.0.0.5", snID), nil).Body.String()
	for _, want := range []string{">10.0.0.5<", "status is-online", ">Online<", "nas", mac, "Last seen", "<time datetime=", "chip dhcp", "Open device"} {
		if !strings.Contains(nas, want) {
			t.Errorf("panel for an online device missing %q", want)
		}
	}
	conflict := htmxRequest(t, srv, st, "GET", fmt.Sprintf("/subnets/%d/cell?ip=10.0.0.9", snID), nil).Body.String()
	for _, want := range []string{"IP conflict", "2 devices claim this address", ">gw<", ">ghost<", "Never seen by a scan"} {
		if !strings.Contains(conflict, want) {
			t.Errorf("conflict panel missing %q", want)
		}
	}
	// An address outside the subnet has no panel.
	if rec := htmxRequest(t, srv, st, "GET", fmt.Sprintf("/subnets/%d/cell?ip=10.9.9.9", snID), nil); rec.Code != http.StatusNotFound {
		t.Errorf("foreign address code = %d, want 404", rec.Code)
	}
}

// Without JavaScript a port is a submit button: the page it loads, and a
// shared ?ip= link, open that port's details and mark it selected.
func TestCellOpensWithoutJavaScript(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	holdIP(t, st, snID, "gw", "10.0.0.1", "static", nil)

	for _, path := range []string{fmt.Sprintf("/subnets/%d?ip=10.0.0.1", snID), fmt.Sprintf("/subnets/%d/cell?ip=10.0.0.1", snID)} {
		body := authedGet(t, srv, st, path).Body.String()
		if !strings.Contains(body, `<form class="patch" method="get" action="/subnets/1"`) {
			t.Errorf("%s: ports are not in a plain GET form", path)
		}
		if !strings.Contains(body, `data-open`) || !strings.Contains(body, `id="cp-title"`) {
			t.Errorf("%s: details not open", path)
		}
		if !strings.Contains(body, `<span role="gridcell" aria-selected="true"><button type="submit" name="ip" value="10.0.0.1"`) {
			t.Errorf("%s: open port not marked selected", path)
		}
	}
	plain := authedGet(t, srv, st, fmt.Sprintf("/subnets/%d", snID)).Body.String()
	if strings.Contains(plain, `aria-selected="true"`) || !strings.Contains(plain, "Pick a port") {
		t.Error("a page without ?ip= should open no details")
	}
}

// Admin actions on the subnet page and in the details panel are left out
// for a viewer: Scan now, Manage subnet, the lease switch, Edit and New
// device here.
func TestSubnetPageRoleGating(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	holdIP(t, st, snID, "gw", "10.0.0.1", "static", nil)
	page := fmt.Sprintf("/subnets/%d", snID)
	adminOnly := []string{"Scan now", "Manage subnet", "/settings/network", "New device here", `hx-post="/subnets/1/cell"`, "/edit"}

	admin := authedGet(t, srv, st, page).Body.String() +
		htmxRequest(t, srv, st, "GET", page+"/cell?ip=10.0.0.1", nil).Body.String() +
		htmxRequest(t, srv, st, "GET", page+"/cell?ip=10.0.0.2", nil).Body.String()
	for _, want := range adminOnly {
		if !strings.Contains(admin, want) {
			t.Errorf("admin missing %q", want)
		}
	}
	viewer := viewerGet(t, srv, st, page) + viewerGet(t, srv, st, page+"?ip=10.0.0.1") + viewerGet(t, srv, st, page+"?ip=10.0.0.2")
	for _, f := range adminOnly {
		if strings.Contains(viewer, f) {
			t.Errorf("viewer sees %q", f)
		}
	}
	if !strings.Contains(viewer, "Open device") || !strings.Contains(viewer, "No device holds this address") {
		t.Error("viewer should still read the details")
	}
}

// statusTrigger is a ScanTrigger that also reports scan status, as the
// scheduler does.
type statusTrigger struct {
	recordingTrigger
	status scan.Status
}

func (s *statusTrigger) Status(int64) scan.Status { return s.status }

// The scan control follows the scheduler: disabled while a scan runs, the
// last scan's time once one has finished. Scan now on a subnet page swaps
// the control in place; on another page it only toasts.
func TestScanControlState(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	trig := &statusTrigger{}
	srv := NewServer(st, events.NewBroker(), trig, nil)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 300})
	page := fmt.Sprintf("/subnets/%d", snID)

	if body := authedGet(t, srv, st, page).Body.String(); !strings.Contains(body, "Scans every 5m") || !strings.Contains(body, "Scan now") {
		t.Errorf("idle control should show the schedule and Scan now")
	}
	trig.status = scan.Status{Running: true}
	if body := authedGet(t, srv, st, page).Body.String(); !strings.Contains(body, `disabled aria-busy="true"`) || strings.Contains(body, `hx-post="/subnets/1/scan"`) {
		t.Errorf("running control should be a disabled Scanning button")
	}
	trig.status = scan.Status{LastDone: time.Now().Add(-2 * time.Minute), LastOK: true}
	if body := authedGet(t, srv, st, page).Body.String(); !strings.Contains(body, "Last scan") || !strings.Contains(body, "2m ago") {
		t.Errorf("finished control should show when the last scan ran")
	}

	scanFrom := func(current string) string {
		req := httptest.NewRequest("POST", page+"/scan", strings.NewReader(url.Values{}.Encode()))
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", current)
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if body := scanFrom("http://netis.lan" + page); !strings.Contains(body, `id="scan-state-1" class="scan-ctl" hx-swap-oob="true"`) {
		t.Errorf("scan from the subnet page should swap the control: %s", body)
	}
	if body := scanFrom("http://netis.lan/"); strings.Contains(body, "hx-swap-oob") {
		t.Errorf("scan from the dashboard should only toast: %s", body)
	}
}

// The live refresh swaps the ports and brings the counts, next free
// address and scan control along out of band.
func TestGridRefreshUpdatesHeader(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	holdIP(t, st, snID, "gw", "10.0.0.1", "static", nil)
	body := authedGet(t, srv, st, fmt.Sprintf("/subnets/%d/grid", snID)).Body.String()
	for _, want := range []string{`id="sn-stats" hx-swap-oob="true"`, `id="next-free" class="next-free" hx-swap-oob="true"`, `id="scan-state-1" class="scan-ctl" hx-swap-oob="true"`, "<b>5</b> free", "<b>1</b> not seen yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("refresh missing %q", want)
		}
	}
}
