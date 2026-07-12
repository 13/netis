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
	st.SetSetting("onboarded", "1")
	st.CreateSubnet(store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120})

	body := authedGet(t, srv, st, "/subnets").Body.String()
	for _, want := range []string{"lan", "10.0.0.0/24", `href="/subnets/1"`, `href="/subnets"`} {
		if !strings.Contains(body, want) {
			t.Errorf("subnets index missing %q", want)
		}
	}
}

func TestSubnetsIndexEmptyState(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	body := authedGet(t, srv, st, "/subnets").Body.String()
	if !strings.Contains(body, "No subnets yet") || !strings.Contains(body, "/settings?tab=subnets") {
		t.Errorf("empty state missing: %s", body)
	}
}

func TestSubnetsIndexViewerOK(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("GET", "/subnets", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("viewer GET /subnets = %d, want 200", rec.Code)
	}
}
