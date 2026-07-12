package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
	"netis/internal/web/views"
)

// authedGet performs a request with a valid admin session cookie.
func authedGet(t *testing.T, srv *Server, st *store.Store, path string) *httptest.ResponseRecorder {
	t.Helper()
	u, ok, _ := st.GetUserByName("ben")
	if !ok {
		addAdmin(t, st)
		u, _, _ = st.GetUserByName("ben")
	}
	st.CreateSession("testtok", u.ID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestDashboardShowsSubnetCounts(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/30", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	ifID, _ := st.AddIface(devID, nil, nil)
	st.AssignIP(ifID, snID, "10.0.0.1", "static")
	st.MarkSeen(ifID, 1, time.Now())

	rec := authedGet(t, srv, st, "/")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	// The redesigned card shows the subnet name/CIDR and an occupancy bar +
	// legend (online/reserved/free counts) rather than occupant device names.
	for _, want := range []string{"lab", "10.0.0.0/30", "online"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestDashboardWidgetsRendersStatusAndAttention(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	// a subnet + an online device + an unknown scan device + a conflict
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	on, _ := st.CreateDevice(store.Device{Name: "gw", Kind: "other", Source: "manual"})
	onIf, _ := st.AddIface(on, nil, nil)
	st.AssignIP(onIf, snID, "10.0.0.1", "static")
	st.MarkSeen(onIf, 1, time.Now())
	unk, _ := st.CreateDevice(store.Device{Name: "unknown-aa:bb:cc:00:00:09", Kind: "other", Source: "scan"})
	unkIf, _ := st.AddIface(unk, nil, nil)
	st.AssignIP(unkIf, snID, "10.0.0.2", "dhcp")
	// conflict: a second device claims .1
	ghost, _ := st.CreateDevice(store.Device{Name: "ghost", Kind: "other", Source: "manual"})
	ghostIf, _ := st.AddIface(ghost, nil, nil)
	st.AssignIP(ghostIf, snID, "10.0.0.1", "static")
	// an integration status row
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "pihole", LastRun: time.Now().UTC().Format(time.RFC3339), OK: true, Detail: "48 leases, 2 new", ItemCount: 48})

	rec := authedGet(t, srv, st, "/dashboard/widgets")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"pihole", "48 leases, 2 new", "unknown-aa:bb:cc:00:00:09", "10.0.0.1"} {
		if !strings.Contains(body, want) {
			t.Errorf("widgets missing %q", want)
		}
	}
}

func TestDashboardPageHasFragmentContainer(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedGet(t, srv, st, "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `hx-get="/dashboard/widgets"`) {
		t.Fatalf("dashboard page missing fragment container (code=%d)", rec.Code)
	}
}

func TestSubnetCardOccupancyCounts(t *testing.T) {
	srv, st := testServer(t)
	snID, _ := st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/29", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	mk := func(name, ip string) int64 {
		devID, _ := st.CreateDevice(store.Device{Name: name, Kind: "other", Source: "manual"})
		ifID, _ := st.AddIface(devID, nil, nil)
		st.AssignIP(ifID, snID, ip, "static")
		return ifID
	}
	onIf := mk("on", "10.0.0.1")
	st.MarkSeen(onIf, 1, time.Now()) // online
	offIf := mk("off", "10.0.0.2")
	st.MarkSeen(offIf, 1, time.Now())
	st.MarkMissed(offIf, 1) // seen then offline
	mk("res", "10.0.0.3")   // assigned, never seen → reserved

	data, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	var row views.DashRow
	for _, r := range data.Rows {
		if r.Subnet.ID == snID {
			row = r
		}
	}
	if row.Online != 1 || row.Reserved != 1 || row.Offline != 1 {
		t.Fatalf("counts: online=%d reserved=%d offline=%d (want 1/1/1)", row.Online, row.Reserved, row.Offline)
	}
	// /29 has 6 host IPs; 3 used → 3 free; Hosts=6.
	if row.Hosts != 6 || row.Free != 3 || row.Used != 3 {
		t.Fatalf("hosts=%d used=%d free=%d (want 6/3/3)", row.Hosts, row.Used, row.Free)
	}
}

func TestBarPct(t *testing.T) {
	if got := views.BarPct(1, 4); got != "25%" {
		t.Errorf("BarPct(1,4)=%q want 25%%", got)
	}
	if got := views.BarPct(3, 0); got != "0%" {
		t.Errorf("BarPct(3,0)=%q want 0%%", got)
	}
}

func TestLayoutHasThemeToggleAndBootstrap(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	rec := authedGet(t, srv, st, "/")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	for _, want := range []string{
		`id="theme-toggle"`, // the toggle control
		`data-theme`,        // the no-flash bootstrap sets it
		`/static/theme.js`,  // toggle + active-nav script
		`class="brand"`,     // restyled nav
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard layout missing %q", want)
		}
	}
}
