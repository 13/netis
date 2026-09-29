package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netis/internal/store"
)

func TestSubnetsIndexListsSubnets(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	body := authedGet(t, srv, st, "/subnets").Body.String()
	for _, want := range []string{"lan", "10.0.0.0/24", `href="/subnets/1"`, `href="/subnets"`} {
		if !strings.Contains(body, want) {
			t.Errorf("subnets index missing %q", want)
		}
	}
}

func TestSubnetsIndexEmptyState(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/subnets").Body.String()
	if !strings.Contains(body, "No subnets yet") || !strings.Contains(body, `href="/settings/network"`) {
		t.Errorf("empty state missing: %s", body)
	}
}

func TestSubnetsIndexViewerOK(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	uID, _ := st.CreateUser(t.Context(), "eve", "h", "viewer")
	st.CreateSession(t.Context(), "viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("GET", "/subnets", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("viewer GET /subnets = %d, want 200", rec.Code)
	}
}

// Each subnet on the index shows a miniature of its panel (a usage bar when
// larger than a /24), its counts and its scan control; Scan is an admin's.
func TestSubnetsIndexMiniPanelsAndScan(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	lan, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.4.0.0/22", Name: "big", Kind: "lan", ScanIntervalSec: 120})
	holdIP(t, st, lan, "gw", "10.0.0.1", "static", nil)

	body := authedGet(t, srv, st, "/subnets").Body.String()
	if n := strings.Count(body, "<rect class="); n < 256 {
		t.Errorf("mini panel rects = %d, want at least 256 for the /24", n)
	}
	for _, want := range []string{`class="mini-panel"`, `class="usage"`, "<b>1</b> not seen yet", "<b>253</b> free", `hx-post="/subnets/1/scan"`, `hx-post="/subnets/2/scan"`, "Scan all", `id="scan-state-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	viewer := viewerGet(t, srv, st, "/subnets")
	for _, f := range []string{"/scan\"", "Scan all", "Manage subnets"} {
		if strings.Contains(viewer, f) {
			t.Errorf("viewer index shows %q", f)
		}
	}
}

// The empty index offers an admin the one next step.
func TestSubnetsIndexEmptyOffersAdd(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/subnets").Body.String()
	if !strings.Contains(body, "Add a subnet") {
		t.Error("admin empty state should offer Add a subnet")
	}
	if strings.Contains(viewerGet(t, srv, st, "/subnets"), "Add a subnet") {
		t.Error("viewer empty state should not offer Add a subnet")
	}
}
