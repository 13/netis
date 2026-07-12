package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
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
	for _, want := range []string{"lab", "10.0.0.0/30", "gw"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestDashboardWidgetsRendersStatusAndAttention(t *testing.T) {
	srv, st := testServer(t)
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
	rec := authedGet(t, srv, st, "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `hx-get="/dashboard/widgets"`) {
		t.Fatalf("dashboard page missing fragment container (code=%d)", rec.Code)
	}
}
