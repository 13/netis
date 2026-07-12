package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"netis/internal/netdetect"
)

// Note: authedGet/authedPost and testServer are defined in the package's other
// _test.go files (dashboard_test.go, devices_test.go, auth_test.go).

func TestOnboardingRedirect(t *testing.T) {
	srv, st := testServer(t)
	srv.detect = func() ([]netdetect.Detected, error) { return nil, nil }
	// Un-onboarded authed user hitting "/" is redirected to /welcome.
	rec := authedGet(t, srv, st, "/")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome" {
		t.Fatalf("want redirect to /welcome, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// /welcome itself is reachable (not redirected).
	rec = authedGet(t, srv, st, "/welcome")
	if rec.Code != 200 {
		t.Fatalf("/welcome code=%d", rec.Code)
	}
	// After onboarding, "/" is served normally.
	st.SetSetting("onboarded", "1")
	rec = authedGet(t, srv, st, "/")
	if rec.Code != 200 {
		t.Fatalf("post-onboarding / code=%d", rec.Code)
	}
}

func TestWelcomeShowsDetectedAndCreates(t *testing.T) {
	srv, st := testServer(t)
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{{CIDR: "192.168.5.0/24", Iface: "eth0"}}, nil
	}
	rec := authedGet(t, srv, st, "/welcome")
	if !strings.Contains(rec.Body.String(), "192.168.5.0/24") {
		t.Fatal("welcome should list the detected subnet")
	}
	// Submit the checked subnet.
	rec = authedPost(t, srv, st, "/welcome/subnets", url.Values{"subnet": {"192.168.5.0/24|eth0"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome/integrations" {
		t.Fatalf("subnets post: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	subnets, _ := st.ListSubnets()
	if len(subnets) != 1 || subnets[0].CIDR != "192.168.5.0/24" || subnets[0].Name != "eth0" ||
		!subnets[0].ScanEnabled || subnets[0].ScanIntervalSec != 120 || subnets[0].Kind != "lan" {
		t.Fatalf("subnet not created with defaults: %+v", subnets)
	}
}

func TestWelcomeSkipSetsOnboarded(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/welcome/skip", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("skip: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if v, _ := st.GetSetting("onboarded"); v != "1" {
		t.Fatalf("onboarded not set: %q", v)
	}
}
