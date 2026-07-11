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
