package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"netis/internal/netdetect"
	"netis/internal/store"
)

// pageGet is authedGet as a browser sends it, asking for HTML.
func pageGet(t *testing.T, srv *Server, st *store.Store, path string) *httptest.ResponseRecorder {
	t.Helper()
	authedGet(t, srv, st, "/") // ensures admin+session exist
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// currentStep is the label of the step the stepper marks current.
func currentStep(body string) string {
	i := strings.Index(body, `aria-current="step"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, `class="lbl">`)
	if j < 0 {
		return ""
	}
	rest = rest[j+len(`class="lbl">`):]
	return rest[:strings.Index(rest, "<")]
}

// The flow runs setup, network, services, done in one sequence, the stepper
// marking each step, and finishing it starts the first scan.
func TestOnboardingFlowSteps(t *testing.T) {
	srv, st, trig := testServerTrig(t)
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{{CIDR: "192.168.5.0/24", Iface: "eth0"}}, nil
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/setup", nil))
	if got := currentStep(rec.Body.String()); got != "Account" {
		t.Errorf("setup marks step %q, want Account", got)
	}
	if !strings.Contains(rec.Body.String(), "At least 8 characters") {
		t.Error("setup should state the password rule outside the placeholder")
	}

	body := authedGet(t, srv, st, "/welcome").Body.String()
	if got := currentStep(body); got != "Network" {
		t.Errorf("/welcome marks step %q, want Network", got)
	}
	for _, want := range []string{"192.168.5.0/24", "The network on eth0 · 254 addresses", `value="192.168.5.0/24|eth0" checked`, "Add another subnet"} {
		if !strings.Contains(body, want) {
			t.Errorf("network step missing %q", want)
		}
	}

	rec = authedPost(t, srv, st, "/welcome/subnets", url.Values{"subnet": {"192.168.5.0/24|eth0"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome/integrations" {
		t.Fatalf("network step: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	body = authedGet(t, srv, st, "/welcome/integrations").Body.String()
	if got := currentStep(body); got != "Services" {
		t.Errorf("services step marks %q", got)
	}
	for _, want := range []string{"Pi-hole", "Names and DHCP leases from your Pi-hole", `class="ob-service"`, "Skip for now"} {
		if !strings.Contains(body, want) {
			t.Errorf("services step missing %q", want)
		}
	}
	// Nothing is stored on a fresh install, so there is nothing to keep.
	if strings.Contains(strings.ToLower(body), "leave blank") {
		t.Error("a fresh install's services step talks about keeping a stored secret")
	}

	rec = authedPost(t, srv, st, "/welcome/integrations", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome/done" {
		t.Fatalf("services step: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if v, _ := st.GetSetting(t.Context(), "onboarded"); v != "1" {
		t.Error("finishing the flow should mark onboarding done")
	}
	subs, _ := st.ListSubnets(t.Context())
	if got := trig.got(); len(got) != 1 || got[0] != subs[0].ID {
		t.Errorf("finishing the flow triggered scans %v, want the new subnet %d", got, subs[0].ID)
	}

	body = authedGet(t, srv, st, "/welcome/done").Body.String()
	if got := currentStep(body); got != "Done" {
		t.Errorf("done step marks %q", got)
	}
	for _, want := range []string{"Scanning your network", `hx-get="/welcome/progress"`, `sse-connect="/events/stream"`, `href="/"`, "0</span> devices found so far"} {
		if !strings.Contains(body, want) {
			t.Errorf("done step missing %q", want)
		}
	}

	// The live count follows the inventory.
	if _, err := st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	frag := authedGet(t, srv, st, "/welcome/progress").Body.String()
	if !strings.Contains(frag, "1</span> device found so far") || strings.Contains(frag, "<html") {
		t.Errorf("progress fragment: %s", frag)
	}
}

// A connected service gets Run now on the last step; before that a stored
// secret is the only reason to talk about keeping one.
func TestOnboardingServicesAfterSave(t *testing.T) {
	srv, st := testServer(t)
	rec := authedPost(t, srv, st, "/welcome/integrations", url.Values{
		"pihole_url": {"https://pi.hole"}, "pihole_password": {"s3cret"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	done := authedGet(t, srv, st, "/welcome/done").Body.String()
	if !strings.Contains(done, `hx-post="/settings/integrations/pihole/run"`) {
		t.Error("the done step should offer Run now for the service just connected")
	}
	if strings.Contains(done, "/settings/integrations/proxmox/run") {
		t.Error("Run now offered for a service that is not set up")
	}
	back := authedGet(t, srv, st, "/welcome/integrations").Body.String()
	if !strings.Contains(back, "Leave blank to keep it") {
		t.Error("with a secret stored, the field should say a blank keeps it")
	}
}

// A bad manual subnet comes back on the page, next to its field, with what
// was typed and picked kept.
func TestOnboardingNetworkInlineError(t *testing.T) {
	srv, st := testServer(t)
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{{CIDR: "192.168.5.0/24", Iface: "eth0"}, {CIDR: "10.1.0.0/24", Iface: "eth1"}}, nil
	}
	rec := authedPost(t, srv, st, "/welcome/subnets", url.Values{
		"subnet": {"10.1.0.0/24|eth1"}, "manual_cidr": {"10.0.0.0/8"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`class="field-error"`, "too large to scan", `value="10.0.0.0/8"`, `aria-invalid="true"`,
		`value="10.1.0.0/24|eth1" checked`, "Your network"} {
		if !strings.Contains(body, want) {
			t.Errorf("error page missing %q", want)
		}
	}
	if strings.Contains(body, `value="192.168.5.0/24|eth0" checked`) {
		t.Error("a subnet the admin unticked came back ticked")
	}
}

// Large detected subnets say what their size costs; a range too large to
// scan cannot be picked.
func TestOnboardingSizeWarnings(t *testing.T) {
	srv, st := testServer(t)
	srv.detect = func() ([]netdetect.Detected, error) {
		return []netdetect.Detected{
			{CIDR: "192.168.20.0/22", Iface: "eth0"},
			{CIDR: "172.16.0.0/16", Iface: "eth1"},
			{CIDR: "10.0.0.0/8", Iface: "eth2"},
		}, nil
	}
	body := authedGet(t, srv, st, "/welcome").Body.String()
	for _, want := range []string{"1,022 addresses", "Larger than most home networks", "65,534 addresses",
		"takes several minutes", "Too large to scan", `value="10.0.0.0/8|eth2" disabled`} {
		if !strings.Contains(body, want) {
			t.Errorf("network step missing %q", want)
		}
	}
}

// Skip for now never dead-ends: the dashboard lists what is missing, each
// linking back into its step, until it is done or dismissed.
func TestSkipShowsFinishSetupPanel(t *testing.T) {
	srv, st := testServer(t)
	if rec := authedPost(t, srv, st, "/welcome/skip", url.Values{}); rec.Header().Get("Location") != "/" {
		t.Fatalf("skip went to %q", rec.Header().Get("Location"))
	}
	dash := authedGet(t, srv, st, "/").Body.String()
	for _, want := range []string{"Finish setup", "Add the networks to scan", `href="/welcome"`,
		"Connect a service", `href="/welcome/integrations"`, `action="/welcome/dismiss"`} {
		if !strings.Contains(dash, want) {
			t.Errorf("finish setup panel missing %q", want)
		}
	}
	if strings.Contains(dash, "Run the first scan") {
		t.Error("with no subnet there is nothing to scan yet")
	}

	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	dash = authedGet(t, srv, st, "/").Body.String()
	if strings.Contains(dash, "Add the networks to scan") || !strings.Contains(dash, "Run the first scan") ||
		!strings.Contains(dash, `hx-post="/scan"`) {
		t.Error("with a subnet and no scan, the panel should ask for the first scan only")
	}

	st.SetIntegrationStatus(t.Context(), store.IntegrationStatus{Name: "scan", OK: true, LastRun: time.Now().UTC().Format(time.RFC3339)})
	st.SetSetting(t.Context(), "pihole_url", "https://pi.hole")
	if dash = authedGet(t, srv, st, "/").Body.String(); strings.Contains(dash, "Finish setup") {
		t.Error("the panel should go once nothing is missing")
	}

	// Dismissing it closes it for good, missing pieces or not.
	st.SetSetting(t.Context(), "pihole_url", "")
	if !strings.Contains(authedGet(t, srv, st, "/").Body.String(), "Finish setup") {
		t.Fatal("the panel should be back while a piece is missing")
	}
	if rec := authedPost(t, srv, st, "/welcome/dismiss", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("dismiss: %d", rec.Code)
	}
	if strings.Contains(authedGet(t, srv, st, "/").Body.String(), "Finish setup") {
		t.Error("a dismissed panel came back")
	}
}

// Finishing the flow normally leaves no nag behind, and a viewer never sees
// the panel.
func TestFinishSetupPanelOnlyWhenNeeded(t *testing.T) {
	srv, st := testServer(t)
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	authedPost(t, srv, st, "/welcome/integrations", url.Values{})
	if strings.Contains(authedGet(t, srv, st, "/").Body.String(), "Finish setup") {
		t.Error("a completed flow should not ask to finish setup")
	}

	srv2, st2 := testServer(t)
	cookie := viewerSession(t, st2) // onboarded, no subnets
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv2.Handler().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "Finish setup") {
		t.Error("a viewer cannot finish setup and should not be asked to")
	}
}

// Settings pages with nothing in them say so and offer the next step.
func TestSettingsEmptyStates(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	cases := map[string][]string{
		"/settings/network":       {"No subnets yet", "Add your first subnet", `action="/settings/subnets"`},
		"/settings/users":         {"Only you so far"},
		"/settings/tokens":        {"No API tokens", `action="/settings/tokens"`},
		"/settings/audit":         {"Nothing recorded yet"},
		"/settings/notifications": {"No notifications set up", "goes offline"},
		"/settings/sessions":      {"only place you are signed in"},
	}
	for path, wants := range cases {
		body := authedGet(t, srv, st, path).Body.String()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
	// The network page's add form is out in the open, not behind a toggle.
	network := authedGet(t, srv, st, "/settings/network").Body.String()
	if i := strings.Index(network, `action="/settings/subnets"`); i < 0 || strings.Contains(network[:i], `<details class="disclosure">`) {
		t.Error("with no subnets the add form should not sit in a disclosure")
	}
	if sessions := authedGet(t, srv, st, "/settings/sessions").Body.String(); strings.Contains(sessions, "revoke-others") {
		t.Error("with one session there is no one else to sign out")
	}
	if n := authedGet(t, srv, st, "/settings/notifications").Body.String(); strings.Contains(n, "keep it") {
		t.Error("with nothing stored the credential fields should not talk about keeping it")
	}
	if a := authedGet(t, srv, st, "/settings/audit?action=user.create").Body.String(); !strings.Contains(a, "No entries match these filters") {
		t.Error("an empty filtered audit log should say the filters matched nothing")
	}
}

// A browser gets an error page in the app shell; the API keeps JSON; an
// htmx request keeps its short text.
func TestErrorPages(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	rec := pageGet(t, srv, st, "/no/such/page")
	body := rec.Body.String()
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("unknown page: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"Page not found", `class="sidebar"`, `href="/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("404 page missing %q", want)
		}
	}
	if rec := pageGet(t, srv, st, "/devices/9999"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Page not found") {
		t.Errorf("missing device: %d", rec.Code)
	}

	// Without asking for HTML (curl, tests) the text stays as it was.
	if rec := authedGet(t, srv, st, "/no/such/page"); strings.Contains(rec.Body.String(), "<html") {
		t.Error("a non-browser request got an HTML page")
	}

	// The API answers JSON, for a missing thing and a missing route alike.
	for _, path := range []string{"/api/devices/9999", "/api/no-such-thing"} {
		rec := pageGet(t, srv, st, path)
		var v map[string]string
		if rec.Code != http.StatusNotFound || json.Unmarshal(rec.Body.Bytes(), &v) != nil || v["error"] == "" {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body.String())
		}
	}

	// A viewer opening an admin page gets a page saying why.
	cookieSrv, cst := testServer(t)
	cookie := viewerSession(t, cst)
	req := httptest.NewRequest("GET", "/settings/users", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	cookieSrv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "open this page") {
		t.Errorf("viewer on an admin page: %d", rec.Code)
	}

	// htmx swaps text, not pages.
	req = httptest.NewRequest("GET", "/no/such/page", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("htmx 404: %d", rec.Code)
	}

	// Signed out, a public address that does not exist gets the minimal shell.
	req = httptest.NewRequest("GET", "/static/nope.css", nil)
	req.Header.Set("Accept", "text/html")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "onboard-brand") ||
		strings.Contains(rec.Body.String(), `class="sidebar"`) {
		t.Errorf("signed-out 404: %d", rec.Code)
	}
}

// When the database is down a browser gets a page saying so.
func TestUnavailablePage(t *testing.T) {
	srv, st := testServer(t)
	st.Close()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "x"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "reach its database") {
		t.Errorf("unavailable: %d %s", rec.Code, rec.Body.String())
	}
}

// With the admin in place, /setup sends the browser on instead of refusing.
func TestSetupAfterAdminRedirects(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/setup", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("GET /setup after setup: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
