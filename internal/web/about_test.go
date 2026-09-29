package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/buildinfo"
)

// The footer carries the version on every page and, for an admin, links to
// the System settings page with the build detail.
func TestFooterLinksToAbout(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedGet(t, srv, st, "/devices")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/settings/system"`) {
		t.Error("footer does not link to the System page")
	}
	if !strings.Contains(body, buildinfo.Get().Label()) {
		t.Errorf("footer does not show the version %q", buildinfo.Get().Label())
	}
}

func TestAboutTabReportsBuildAndRuntime(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedGet(t, srv, st, "/settings/system")
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Build", "Version", "Build number", "Commit",
		"Runtime", "Go", "Platform", "Database", "Uptime",
		"Dependencies",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("About tab is missing %q", want)
		}
	}
	// The backend the store actually opened, not a guess.
	if !strings.Contains(body, string(st.Dialect())) {
		t.Errorf("About tab does not report the %q backend", st.Dialect())
	}
	// The page is marked current in the settings navigation.
	if !strings.Contains(body, `href="/settings/system" aria-current="page"`) {
		t.Error("System is not marked current")
	}
}

// Links to the old single settings page keep working: each tab redirects to
// the page that holds it now, the audit filters carry over, and a viewer
// following a link to an admin tab lands on a page they can see. An unknown
// tab, or none, goes to the account page.
func TestOldSettingsLinksRedirect(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	for from, to := range map[string]string{
		"/settings":                        "/settings/account",
		"/settings?tab=nope":               "/settings/account",
		"/settings?tab=subnets":            "/settings/network",
		"/settings?tab=general":            "/settings/network",
		"/settings?tab=integrations":       "/settings/integrations",
		"/settings?tab=users":              "/settings/users",
		"/settings?tab=notifications":      "/settings/notifications",
		"/settings?tab=tokens":             "/settings/tokens",
		"/settings?tab=about":              "/settings/system",
		"/settings?tab=audit&action=login": "/settings/audit?action=login",
	} {
		rec := authedGet(t, srv, st, from)
		if rec.Code != 302 || rec.Header().Get("Location") != to {
			t.Errorf("admin GET %s: code=%d location=%q, want 302 to %s", from, rec.Code, rec.Header().Get("Location"), to)
		}
	}
	vsrv, vst := testServer(t)
	cookie := viewerSession(t, vst)
	for from, to := range map[string]string{
		"/settings?tab=subnets": "/subnets",
		"/settings?tab=users":   "/settings/account",
		"/settings?tab=audit":   "/settings/account",
		"/settings?tab=tokens":  "/settings/tokens",
	} {
		req := httptest.NewRequest("GET", from, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		vsrv.Handler().ServeHTTP(rec, req)
		if rec.Code != 302 || rec.Header().Get("Location") != to {
			t.Errorf("viewer GET %s: code=%d location=%q, want 302 to %s", from, rec.Code, rec.Header().Get("Location"), to)
		}
	}
}

// The About tab is behind the same auth wrapper as the rest of the app; build
// detail must not be readable without a session.
func TestAboutTabRequiresAuth(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/settings/system", nil))
	if rec.Code != 303 && rec.Code != 302 {
		t.Fatalf("anonymous request got %d, want a redirect to login", rec.Code)
	}
}

func TestGeneralTabSavesRetention(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	rec := authedPost(t, srv, st, "/settings/general", url.Values{
		"offline_after":               {"3"},
		"event_retention_days":        {"7"},
		"availability_retention_days": {"0"},
	})
	if rec.Code != 303 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if v, _ := st.GetSetting(t.Context(), "event_retention_days"); v != "7" {
		t.Errorf("event_retention_days = %q, want 7", v)
	}
	// Zero is a valid choice and must be stored, not skipped as blank.
	if v, _ := st.GetSetting(t.Context(), "availability_retention_days"); v != "0" {
		t.Errorf("availability_retention_days = %q, want 0", v)
	}

	// A negative window is rejected rather than silently deleting everything.
	rec = authedPost(t, srv, st, "/settings/general", url.Values{
		"offline_after": {"3"}, "event_retention_days": {"-1"},
	})
	if rec.Code != 400 {
		t.Errorf("negative retention: code=%d, want 400", rec.Code)
	}

	// The saved values come back into the form.
	rec = authedGet(t, srv, st, "/settings/system")
	if !strings.Contains(rec.Body.String(), `name="event_retention_days" min="0" value="7"`) {
		t.Error("System page does not show the saved event retention")
	}
}
