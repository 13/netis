package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestLoginRecordsSessionProvenance(t *testing.T) {
	srv := newTrustingServer(t, "192.0.2.0/24")
	st := srv.store
	addAdmin(t, st)

	form := url.Values{"username": {"ben"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (test)")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login code=%d", rec.Code)
	}

	u, _, _ := st.GetUserByName(t.Context(), "ben")
	list, err := st.ListSessionsForUser(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("sessions=%+v", list)
	}
	sess := list[0]
	if sess.CreatedAt == nil || *sess.CreatedAt == "" {
		t.Error("created_at not recorded")
	}
	// The forwarded client, not the proxy: this server trusts 192.0.2.0/24.
	if sess.IP == nil || *sess.IP != "10.9.9.9" {
		t.Errorf("ip=%v, want the forwarded client address", sess.IP)
	}
	if sess.UserAgent == nil || *sess.UserAgent != "Mozilla/5.0 (test)" {
		t.Errorf("user_agent=%v", sess.UserAgent)
	}
}

// A user agent is whatever the client sends, and it is rendered into a page.
func TestLoginTruncatesAbsurdUserAgent(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", strings.Repeat("A", 5000))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	u, _, _ := st.GetUserByName(t.Context(), "ben")
	list, _ := st.ListSessionsForUser(t.Context(), u.ID)
	if len(list) != 1 || list[0].UserAgent == nil {
		t.Fatalf("sessions=%+v", list)
	}
	if len(*list[0].UserAgent) != maxUserAgentLen {
		t.Fatalf("stored user agent length=%d, want %d", len(*list[0].UserAgent), maxUserAgentLen)
	}
}

func TestSettingsListsOwnSessionsOnly(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addUser(t, st, "kim", "kim-password", "viewer", "kim-token")
	// authedGet creates admin "ben" with session "testtok".
	body := authedGet(t, srv, st, "/settings?tab=users").Body.String()

	if !strings.Contains(body, "this browser") {
		t.Error("the session making the request should be marked")
	}
	if strings.Contains(body, "testtok") || strings.Contains(body, "kim-token") {
		t.Error("session tokens must never be rendered into the page")
	}
	if !strings.Contains(body, store.SessionID("testtok")) {
		t.Error("the caller's own session should be listed by its digest id")
	}
	if strings.Contains(body, store.SessionID("kim-token")) {
		t.Error("another user's session must not appear")
	}
	if !strings.Contains(body, `action="/settings/sessions/revoke-others"`) {
		t.Error("the sign-out-others control should be offered")
	}
}

func TestRevokeOneSession(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/") // admin "ben" + session "testtok"
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	st.CreateSession(t.Context(), "other-browser", ben.ID,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339))

	rec := authedPost(t, srv, st, "/settings/sessions/"+store.SessionID("other-browser")+"/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok, _ := st.GetSession(t.Context(), "other-browser"); ok {
		t.Error("the named session should be revoked")
	}
	if _, ok, _ := st.GetSession(t.Context(), "testtok"); !ok {
		t.Error("the caller's own session should survive")
	}
	if rec := authedPost(t, srv, st, "/settings/sessions/deadbeefdeadbeef/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: code=%d, want 404", rec.Code)
	}
}

// Revoking the session you are using is a legitimate choice, and it has to log
// you out rather than leave the browser holding a cookie for a dead session.
func TestRevokingCurrentSessionLogsOut(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/")
	rec := authedPost(t, srv, st, "/settings/sessions/"+store.SessionID("testtok")+"/delete", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "netis_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the session cookie should be cleared")
	}
	if _, ok, _ := st.GetSession(t.Context(), "testtok"); ok {
		t.Error("the session should be gone")
	}
}

func TestRevokeOtherSessions(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/")
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	st.CreateSession(t.Context(), "phone", ben.ID, expires)
	st.CreateSession(t.Context(), "laptop", ben.ID, expires)
	addUser(t, st, "kim", "kim-password", "viewer", "kim-token")

	rec := authedPost(t, srv, st, "/settings/sessions/revoke-others", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", rec.Code)
	}
	for _, tok := range []string{"phone", "laptop"} {
		if _, ok, _ := st.GetSession(t.Context(), tok); ok {
			t.Errorf("session %s should be revoked", tok)
		}
	}
	if _, ok, _ := st.GetSession(t.Context(), "testtok"); !ok {
		t.Error("the caller's own session should survive")
	}
	if _, ok, _ := st.GetSession(t.Context(), "kim-token"); !ok {
		t.Error("another user's session must not be touched")
	}
}

// Session management is per-account, so a viewer manages their own — and only
// their own.
func TestViewerManagesOwnSessions(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	kimID := addUser(t, st, "kim", "kim-password", "viewer", "kim-here")
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	st.CreateSession(t.Context(), "kim-phone", kimID, expires)
	benID := func() int64 {
		u, _, _ := st.GetUserByName(t.Context(), "ben")
		return u.ID
	}()
	st.CreateSession(t.Context(), "ben-token", benID, expires)

	if rec := postAs(t, srv, "kim-here", "/settings/sessions/"+store.SessionID("kim-phone")+"/delete",
		url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("viewer revoking own session: code=%d", rec.Code)
	}
	if _, ok, _ := st.GetSession(t.Context(), "kim-phone"); ok {
		t.Error("kim's own session should be revoked")
	}
	// The admin's session id is not kim's to revoke; it matches nothing she owns.
	if rec := postAs(t, srv, "kim-here", "/settings/sessions/"+store.SessionID("ben-token")+"/delete",
		url.Values{}); rec.Code != http.StatusNotFound {
		t.Fatalf("viewer revoking another user's session: code=%d, want 404", rec.Code)
	}
	if _, ok, _ := st.GetSession(t.Context(), "ben-token"); !ok {
		t.Error("the admin's session must survive")
	}
}
