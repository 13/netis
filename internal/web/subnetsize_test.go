package web

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"netis/internal/scan"
	"netis/internal/store"
)

// An over-wide prefix is a plausible typo, and before the limit existed it
// made the process allocate until it died. It must be refused at the form.
func TestCreateSubnetRejectsOversizedPrefix(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	for _, cidr := range []string{"10.0.0.0/8", "0.0.0.0/0", "2001:db8::/64"} {
		rec := authedPost(t, srv, st, "/settings/subnets", url.Values{
			"cidr": {cidr}, "name": {"too big"}, "kind": {"lan"},
			"scan_interval_sec": {"120"},
		})
		if rec.Code != 400 {
			t.Errorf("%s: code=%d, want 400", cidr, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "or narrower") {
			t.Errorf("%s: message %q does not say what would work", cidr, rec.Body.String())
		}
	}
	if subnets, _ := st.ListSubnets(t.Context()); len(subnets) != 0 {
		t.Errorf("oversized subnets were stored: %+v", subnets)
	}

	// The largest supported subnet is still accepted.
	rec := authedPost(t, srv, st, "/settings/subnets", url.Values{
		"cidr": {"10.0.0.0/16"}, "name": {"big but fine"}, "kind": {"lan"},
		"scan_interval_sec": {"120"},
	})
	if rec.Code != 303 {
		t.Fatalf("/16: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// A subnet stored before the limit existed must make its page report a
// configuration problem, not hang or return a server error.
func TestSubnetPageRefusesOversizedStoredSubnet(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	// Written straight to the store, bypassing the form validation.
	id, err := st.CreateSubnet(t.Context(), store.Subnet{
		CIDR: "10.0.0.0/8", Kind: "lan", ScanEnabled: false, ScanIntervalSec: 120})
	if err != nil {
		t.Fatal(err)
	}
	rec := authedGet(t, srv, st, "/subnets/"+strconv.FormatInt(id, 10))
	if rec.Code != 400 {
		t.Fatalf("code=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "larger than") {
		t.Errorf("body = %q", rec.Body.String())
	}
	// And the grid fragment htmx polls must refuse it the same way.
	rec = authedGet(t, srv, st, "/subnets/"+strconv.FormatInt(id, 10)+"/grid")
	if rec.Code != 400 {
		t.Errorf("grid fragment: code=%d, want 400", rec.Code)
	}
}

func TestMaxSubnetAddressesIsAnIPv4Slash16(t *testing.T) {
	if scan.MaxSubnetAddresses != 65536 {
		t.Errorf("MaxSubnetAddresses = %d, want 65536", scan.MaxSubnetAddresses)
	}
}

// A handler must not hand driver text to the browser: a database error carries
// the SQL it was running plus constraint and schema names, and every page is
// reachable by a viewer-role account.
func TestHandlerErrorsDoNotLeakDriverText(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	// The session has to exist before the database goes away, since the auth
	// middleware reads it on every request.
	seed := httptest.NewRequest("GET", "/", nil)
	addSessionCookie(t, st, seed)
	cookie := seed.Header.Get("Cookie")

	st.Close() // every query from here on fails with a driver error

	for _, path := range []string{"/", "/devices", "/events", "/settings"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		body := rec.Body.String()
		for _, leak := range []string{"sql:", "SQL", "database is closed", "syntax", "SELECT", "iface_status"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s leaked %q in its error body: %q", path, leak, body)
			}
		}
	}
}
