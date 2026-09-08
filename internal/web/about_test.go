package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/buildinfo"
)

// The footer carries the version on every page and links to the About tab.
func TestFooterLinksToAbout(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedGet(t, srv, st, "/devices")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/settings?tab=about"`) {
		t.Error("footer does not link to the About tab")
	}
	if !strings.Contains(body, buildinfo.Get().Label()) {
		t.Errorf("footer does not show the version %q", buildinfo.Get().Label())
	}
}

func TestAboutTabReportsBuildAndRuntime(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedGet(t, srv, st, "/settings?tab=about")
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
	// The tab is selected in the tab bar.
	if !strings.Contains(body, `class="tab active" href="/settings?tab=about"`) {
		t.Error("About tab is not marked active")
	}
}

// An unknown tab still falls back to subnets rather than rendering nothing.
func TestUnknownSettingsTabFallsBack(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedGet(t, srv, st, "/settings?tab=nope")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `class="tab active" href="/settings?tab=subnets"`) {
		t.Error("unknown tab did not fall back to subnets")
	}
}

// The About tab is behind the same auth wrapper as the rest of the app; build
// detail must not be readable without a session.
func TestAboutTabRequiresAuth(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/settings?tab=about", nil))
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
	rec = authedGet(t, srv, st, "/settings?tab=general")
	if !strings.Contains(rec.Body.String(), `name="event_retention_days" min="0" value="7"`) {
		t.Error("General tab does not show the saved event retention")
	}
}
