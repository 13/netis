package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"netis/internal/store"
)

func postForm(srv *Server, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.7:4000"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func auditEntries(t *testing.T, st *store.Store) []store.AuditEntry {
	t.Helper()
	es, _, err := st.ListAudit(t.Context(), store.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestUserRoleChange(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/") // admin "ben" + session
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	eve, _ := st.CreateUser(t.Context(), "eve", "h", "viewer")

	path := func(id int64) string { return "/settings/users/" + itoa(id) + "/role" }

	// The only admin cannot demote themselves.
	rec := authedPost(t, srv, st, path(ben.ID), url.Values{"role": {"viewer"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "cannot demote the last admin") {
		t.Fatalf("demote last admin: code=%d", rec.Code)
	}
	if u, _, _ := st.GetUser(t.Context(), ben.ID); u.Role != "admin" {
		t.Fatalf("last admin demoted: %q", u.Role)
	}

	rec = authedPost(t, srv, st, path(eve), url.Values{"role": {"admin"}})
	wantRedirect(t, rec, "/settings?tab=users")
	if u, _, _ := st.GetUser(t.Context(), eve); u.Role != "admin" {
		t.Fatalf("promote: role %q", u.Role)
	}

	if rec := authedPost(t, srv, st, path(eve), url.Values{"role": {"root"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad role: code=%d", rec.Code)
	}
	if rec := authedPost(t, srv, st, path(eve+100), url.Values{"role": {"viewer"}}); rec.Code != http.StatusNotFound {
		t.Errorf("missing user: code=%d", rec.Code)
	}

	// With eve an admin, ben may step down, and his very next request is a
	// viewer's: the role is read per request, so no session has to go.
	wantRedirect(t, authedPost(t, srv, st, path(ben.ID), url.Values{"role": {"viewer"}}), "/settings?tab=users")
	if rec := authedPost(t, srv, st, path(eve), url.Values{"role": {"viewer"}}); rec.Code != http.StatusForbidden {
		t.Errorf("demoted admin still acting as one: code=%d", rec.Code)
	}

	// Every attempt is in the audit log, refusals included.
	var roles []string
	for _, e := range auditEntries(t, st) {
		if e.Action == "user.role" {
			roles = append(roles, e.Target+" "+e.Detail+" "+itoa(int64(e.Status)))
		}
	}
	want := []string{
		"user " + itoa(eve) + "  403", // the demoted ben's attempt, refused before the handler ran
		"user ben admin -> viewer 303",
		"user eve bad role 400",
		"user eve viewer -> admin 303",
		"user ben admin -> viewer 400",
	}
	// The 404 has no user to name and is recorded under the path's id.
	if len(roles) != 6 || roles[2] != "user "+itoa(eve+100)+"  404" {
		t.Fatalf("role entries = %q", roles)
	}
	roles = append(roles[:2], roles[3:]...)
	for i := range want {
		if roles[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, roles[i], want[i])
		}
	}
}

func TestUsersTabOffersRoleChange(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	body := authedGet(t, srv, st, "/settings?tab=users").Body.String()
	if !strings.Contains(body, `/settings/users/1/role`) {
		t.Error("users tab has no role form")
	}
}

func TestAuditRecordsLogins(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st) // ben / secret

	postForm(srv, "/login", url.Values{"username": {"ben"}, "password": {"wrong-password"}}, nil)
	postForm(srv, "/login", url.Values{"username": {"hunter2hunter2"}, "password": {"x"}}, nil)
	rec := postForm(srv, "/login", url.Values{"username": {"ben"}, "password": {"secret"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login code=%d", rec.Code)
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "netis_session" {
			session = c
		}
	}
	postForm(srv, "/logout", nil, session)

	es := auditEntries(t, st)
	if len(es) != 4 {
		t.Fatalf("entries = %+v", es)
	}
	type row struct {
		user, action, detail string
		status               int
	}
	want := []row{
		{"ben", "logout", "", 303},
		{"ben", "login", "", 303},
		{"", "login", "unknown user", 401},
		{"ben", "login", "wrong password", 401},
	}
	for i, w := range want {
		e := es[i]
		if got := (row{e.Username, e.Action, e.Detail, e.Status}); got != w {
			t.Errorf("entry %d = %+v, want %+v", i, got, w)
		}
		if e.IP != "192.0.2.7" {
			t.Errorf("entry %d ip = %q", i, e.IP)
		}
	}
	if es[0].UserID == nil || es[3].UserID == nil || es[2].UserID != nil {
		t.Errorf("user ids: %v %v %v", es[0].UserID, es[2].UserID, es[3].UserID)
	}
	// Neither a password nor an unknown "username" that may be one is kept.
	for _, e := range es {
		all := e.Username + e.Target + e.Detail
		for _, secret := range []string{"secret", "wrong-password", "hunter2"} {
			if strings.Contains(all, secret) {
				t.Errorf("entry %+v contains %q", e, secret)
			}
		}
	}
}

func TestAuditRecordsChangesAndRefusals(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	rec := authedPost(t, srv, st, "/devices", url.Values{"name": {"nas"}, "kind": {"server"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create device code=%d body=%s", rec.Code, rec.Body.String())
	}
	authedPost(t, srv, st, "/settings/users", url.Values{
		"username": {"carol"}, "password": {"carols-password"}, "role": {"viewer"},
	})
	// Reads are not recorded, nor is a post to a route that does not exist.
	authedGet(t, srv, st, "/devices")
	authedPost(t, srv, st, "/no/such/route", nil)

	viewer := viewerSessionFor(t, st)
	postForm(srv, "/settings/general", url.Values{"offline_after": {"5"}}, viewer)

	es := auditEntries(t, st)
	if len(es) != 3 {
		t.Fatalf("entries = %+v", es)
	}
	if e := es[0]; e.Username != "vic" || e.Action != "settings.save" || e.Status != http.StatusForbidden {
		t.Errorf("viewer refusal = %+v", e)
	}
	if e := es[1]; e.Username != "ben" || e.Action != "user.create" || e.Target != "user carol" || e.Detail != "role viewer" {
		t.Errorf("user create = %+v", e)
	}
	if strings.Contains(es[1].Detail+es[1].Target, "carols-password") {
		t.Error("password recorded")
	}
	if e := es[2]; e.Action != "device.create" || e.Target != "device nas" || e.Status != http.StatusSeeOther {
		t.Errorf("device create = %+v", e)
	}
}

// A write through the API with a bearer token is attributed to the token's
// owner and marked as coming through a token; the token itself is not kept.
func TestAuditAttributesBearerWritesToTokenOwner(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	tokenFor(t, st, ben.ID, "netis_bentoken", "")

	rec := bearer(t, srv, "POST", "/api/devices", "netis_bentoken", `{"name":"printer","kind":"other"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: code=%d body=%s", rec.Code, rec.Body.String())
	}
	es := auditEntries(t, st)
	if len(es) != 1 {
		t.Fatalf("entries = %+v", es)
	}
	e := es[0]
	if e.Action != "api.device.create" || e.Username != "ben" || e.UserID == nil || *e.UserID != ben.ID ||
		e.Target != "device printer" || e.Detail != "via API token" || e.Status != http.StatusCreated {
		t.Errorf("entry = %+v", e)
	}
	if strings.Contains(e.Target+e.Detail, "netis_bentoken") {
		t.Error("token recorded")
	}
}

// A route with no friendly name is still recorded, under its pattern, and its
// target comes from the pattern's wildcard.
func TestAuditFallsBackToPattern(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	srv.mux.HandleFunc("POST /widgets/{id}/poke", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	authedPost(t, srv, st, "/widgets/42/poke", nil)
	es := auditEntries(t, st)
	if len(es) != 1 || es[0].Action != "POST /widgets/{id}/poke" || es[0].Target != "widget 42" || es[0].Status != 204 {
		t.Fatalf("entries = %+v", es)
	}
}

// Every registered mutating route has a friendly action name, so the table
// does not silently fall behind the routes.
func TestEveryMutatingRouteHasAnAuditAction(t *testing.T) {
	for _, r := range registeredRoutes(t) {
		key := r.method + " " + r.path
		if isMutating(r.method) && auditActions[key] == "" {
			t.Errorf("%s has no entry in auditActions", key)
		}
	}
}

func TestAuditTab(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/")
	for i := range auditPageSize + 5 {
		st.AddAudit(t.Context(), store.AuditEntry{Username: "ben", Action: "device.update", Target: "device " + itoa(int64(i)), Status: 303})
	}
	st.AddAudit(t.Context(), store.AuditEntry{Action: "login", Detail: "unknown user", Status: 401})

	body := authedGet(t, srv, st, "/settings?tab=audit").Body.String()
	if !strings.Contains(body, "Audit log") || !strings.Contains(body, "unknown user") {
		t.Fatal("audit tab missing entries")
	}
	if !strings.Contains(body, "before=") {
		t.Error("no link to older entries")
	}
	if strings.Contains(body, ">device 0<") {
		t.Error("oldest entry on the first page")
	}

	body = authedGet(t, srv, st, "/settings?tab=audit&action=login").Body.String()
	if !strings.Contains(body, "unknown user") || strings.Contains(body, "device.update</td>") {
		t.Error("action filter not applied")
	}

	// A viewer neither sees the tab nor gets its contents by asking.
	req := httptest.NewRequest("GET", "/settings?tab=audit", nil)
	req.AddCookie(viewerSessionFor(t, st))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if b := rec.Body.String(); strings.Contains(b, "Audit log") || strings.Contains(b, "unknown user") ||
		strings.Contains(b, "tab=audit") {
		t.Error("viewer can see the audit log")
	}
}

// viewerSessionFor adds a viewer and session to a store that already has its
// admin (viewerSession creates the admin too).
func viewerSessionFor(t *testing.T, st *store.Store) *http.Cookie {
	t.Helper()
	id, err := st.CreateUser(t.Context(), "vic", "h", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(t.Context(), "victok", id, "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "netis_session", Value: "victok"}
}

func TestGeneralSavesAuditRetention(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	rec := authedPost(t, srv, st, "/settings/general", url.Values{"offline_after": {"3"}, "audit_retention_days": {"90"}})
	wantRedirect(t, rec, "/settings?tab=general")
	if v, _ := st.GetSetting(t.Context(), "audit_retention_days"); v != "90" {
		t.Errorf("audit_retention_days = %q", v)
	}
	if rec := authedPost(t, srv, st, "/settings/general", url.Values{"offline_after": {"3"}, "audit_retention_days": {"-1"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("negative retention: code=%d", rec.Code)
	}
}
