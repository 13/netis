package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

// post issues a state-changing request with a valid session and the given
// Origin header ("" means the header is absent).
func postWithOrigin(t *testing.T, srv *Server, st *store.Store, origin, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	addSessionCookie(t, st, req)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCrossOriginPostIsRejected(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	form := url.Values{"cidr": {"192.168.9.0/24"}, "name": {"x"}, "kind": {"lan"},
		"scan_interval_sec": {"120"}}

	// httptest requests are sent to host "example.com".
	rec := postWithOrigin(t, srv, st, "https://evil.example.net", "/settings/subnets", form)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST: code=%d, want 403", rec.Code)
	}
	if subnets, _ := st.ListSubnets(t.Context()); len(subnets) != 0 {
		t.Errorf("cross-origin POST still wrote: %+v", subnets)
	}

	// A sibling subdomain is same-site to a browser, so SameSite=Lax would let
	// it through; the host comparison is what stops it.
	rec = postWithOrigin(t, srv, st, "https://other.example.com", "/settings/subnets", form)
	if rec.Code != http.StatusForbidden {
		t.Errorf("sibling-subdomain POST: code=%d, want 403", rec.Code)
	}
}

func TestSameOriginPostIsAllowed(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	form := url.Values{"cidr": {"192.168.9.0/24"}, "name": {"x"}, "kind": {"lan"},
		"scan_interval_sec": {"120"}}

	rec := postWithOrigin(t, srv, st, "http://example.com", "/settings/subnets", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("same-origin POST: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if subnets, _ := st.ListSubnets(t.Context()); len(subnets) != 1 {
		t.Errorf("same-origin POST did not write: %+v", subnets)
	}
}

// Non-browser clients omit Origin, and browsers always send it on the
// cross-origin requests this defends against, so an absent header is allowed.
func TestPostWithoutOriginIsAllowed(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := postWithOrigin(t, srv, st, "", "/settings/subnets", url.Values{
		"cidr": {"192.168.9.0/24"}, "name": {"x"}, "kind": {"lan"},
		"scan_interval_sec": {"120"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Reads are never blocked by the origin check.
func TestCrossOriginGetIsAllowed(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	req := httptest.NewRequest("GET", "/devices", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	addSessionCookie(t, st, req)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("cross-origin GET: code=%d, want 200", rec.Code)
	}
}
