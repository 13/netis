package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// bearer sends a request with an API token and an optional JSON body.
func bearer(t *testing.T, srv *Server, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// tokenFor creates an API token for a user.
func tokenFor(t *testing.T, st *store.Store, userID int64, token, expires string) {
	t.Helper()
	if _, err := st.CreateAPIToken(t.Context(), userID, "t", token, expires, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func wantJSONError(t *testing.T, rec *httptest.ResponseRecorder, code int, contains string) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("code=%d body=%s, want %d", rec.Code, rec.Body.String(), code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type=%q, want JSON", ct)
	}
	var e struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || !strings.Contains(e.Error, contains) {
		t.Errorf("error body=%s, want it to mention %q", rec.Body.String(), contains)
	}
}

func TestBearerTokenAuthenticatesAPI(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	seedInventory(t, st)
	viewer := addUser(t, st, "eve", "password1", "viewer", "evesess")
	tokenFor(t, st, viewer, "netis_eve", "")
	tokenFor(t, st, viewer, "netis_old", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))

	rec := bearer(t, srv, "GET", "/api/devices", "netis_eve", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"gw"`) {
		t.Fatalf("viewer token read: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// The use is recorded.
	toks, _ := st.ListAPITokens(t.Context(), viewer)
	used := false
	for _, tok := range toks {
		used = used || tok.LastUsedAt != nil
	}
	if !used {
		t.Error("last_used_at not recorded")
	}

	for name, tok := range map[string]string{"unknown": "netis_nope", "expired": "netis_old"} {
		rec := bearer(t, srv, "GET", "/api/devices", tok, "")
		wantJSONError(t, rec, http.StatusUnauthorized, "invalid or expired")
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: no WWW-Authenticate header", name)
		}
	}

	// Tokens are for the API only: a page still wants a session.
	rec = bearer(t, srv, "GET", "/devices", "netis_eve", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("token on a page: code=%d loc=%q, want redirect to login", rec.Code, rec.Header().Get("Location"))
	}
}

// A request that names a token is judged by the token alone; a valid browser
// session alongside a bad token does not rescue it.
func TestBadBearerDoesNotFallBackToCookie(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/") // admin session "testtok"
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer netis_wrong")
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	wantJSONError(t, rec, http.StatusUnauthorized, "invalid")
}

// Bearer requests carry no ambient credential, so the cross-origin checks
// that guard cookie requests do not apply to them; a cookie request with the
// same headers is still refused.
func TestBearerExemptFromOriginChecks(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	authedGet(t, srv, st, "/")
	admin, _, _ := st.GetUserByName(t.Context(), "ben")
	tokenFor(t, st, admin.ID, "netis_admin", "")

	send := func(auth func(*http.Request)) int {
		req := httptest.NewRequest("POST", "/api/devices", strings.NewReader(`{"name":"x","kind":"other"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example.net")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		auth(req)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := send(func(r *http.Request) { r.Header.Set("Authorization", "Bearer netis_admin") }); code != http.StatusCreated {
		t.Errorf("bearer cross-origin create: code=%d, want 201", code)
	}
	if code := send(func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "netis_session", Value: "testtok"}) }); code != http.StatusForbidden {
		t.Errorf("cookie cross-origin create: code=%d, want 403", code)
	}
}

// Failed token lookups are rate limited per address; the limit answers before
// the database is asked.
func TestBearerFailuresRateLimited(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	for i := range apiTokenFailMax {
		if rec := bearer(t, srv, "GET", "/api/status", "netis_guess", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: code=%d, want 401", i+1, rec.Code)
		}
	}
	wantJSONError(t, bearer(t, srv, "GET", "/api/status", "netis_guess", ""), http.StatusTooManyRequests, "too many")
}

func TestTokenFormat(t *testing.T) {
	a, err := newAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newAPIToken()
	if a == b || !regexp.MustCompile(`^netis_[A-Za-z0-9_-]{43}$`).MatchString(a) {
		t.Fatalf("tokens %q %q", a, b)
	}
}

// Any role issues its own token from settings; it is shown once, works on the
// API with the owner's role, and the page is not cached.
func TestCreateTokenFromSettings(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	addUser(t, st, "eve", "password1", "viewer", "evesess")

	rec := postAs(t, srv, "evesess", "/settings/tokens", url.Values{"name": {"grafana"}, "expires_days": {"30"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the page showing a new token may be cached")
	}
	tok := regexp.MustCompile(`netis_[A-Za-z0-9_-]{43}`).FindString(rec.Body.String())
	if tok == "" {
		t.Fatal("new token not shown")
	}
	// It sits in a selectable read-only field with a Copy button, which the
	// script reveals, pointed at it.
	if !strings.Contains(rec.Body.String(), `id="new-token" readonly value="`+tok+`"`) ||
		!strings.Contains(rec.Body.String(), `data-copy="new-token" hidden>Copy</button>`) {
		t.Error("new token lacks its field or Copy button")
	}
	if js := getAs(t, srv, "evesess", "/static/dialog.js"); !strings.Contains(js, "[data-copy]") {
		t.Error("dialog.js does not handle Copy buttons")
	}
	if rec := bearer(t, srv, "GET", "/api/status", tok, ""); rec.Code != http.StatusOK {
		t.Fatalf("new token rejected: %d", rec.Code)
	}
	// It acts as a viewer: no writes.
	if rec := bearer(t, srv, "DELETE", "/api/devices/1", tok, ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer token delete: code=%d, want 403", rec.Code)
	}
	eve, _, _ := st.GetUserByName(t.Context(), "eve")
	toks, _ := st.ListAPITokens(t.Context(), eve.ID)
	if len(toks) != 1 || toks[0].Name != "grafana" || toks[0].ExpiresAt == nil {
		t.Fatalf("tokens = %+v", toks)
	}
	// A reload of the tab shows the token's name but never the token.
	body := getAs(t, srv, "evesess", "/settings?tab=tokens")
	if !strings.Contains(body, "grafana") || strings.Contains(body, tok) {
		t.Errorf("token list: has name=%v has token=%v", strings.Contains(body, "grafana"), strings.Contains(body, tok))
	}

	for _, bad := range []url.Values{{"name": {""}}, {"name": {"x"}, "expires_days": {"-1"}}, {"name": {strings.Repeat("n", 101)}}} {
		if rec := postAs(t, srv, "evesess", "/settings/tokens", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("create %v: code=%d, want 400", bad, rec.Code)
		}
	}
}

func getAs(t *testing.T, srv *Server, session, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: session})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: code=%d", path, rec.Code)
	}
	return rec.Body.String()
}

// A user revokes their own tokens only; an admin can revoke anyone's and sees
// them all, with their owners.
func TestTokenRevokeScopedToOwner(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	admin := addUser(t, st, "ben", "password1", "admin", "bensess")
	eve := addUser(t, st, "eve", "password1", "viewer", "evesess")
	adminTok, _ := st.CreateAPIToken(t.Context(), admin, "ben-script", "netis_ben", "", time.Now())
	eveTok, _ := st.CreateAPIToken(t.Context(), eve, "eve-script", "netis_eve", "", time.Now())

	if body := getAs(t, srv, "evesess", "/settings?tab=tokens"); strings.Contains(body, "ben-script") || !strings.Contains(body, "eve-script") {
		t.Error("a viewer's token tab shows someone else's token, or not their own")
	}
	if body := getAs(t, srv, "bensess", "/settings?tab=tokens"); !strings.Contains(body, "ben-script") || !strings.Contains(body, "eve-script") {
		t.Error("the admin's token tab does not list every token")
	}

	if rec := postAs(t, srv, "evesess", "/settings/tokens/"+itoa(adminTok)+"/delete", nil); rec.Code != http.StatusNotFound {
		t.Errorf("viewer revoking the admin's token: code=%d, want 404", rec.Code)
	}
	if rec := bearer(t, srv, "GET", "/api/status", "netis_ben", ""); rec.Code != http.StatusOK {
		t.Fatal("the admin's token was revoked by a viewer")
	}
	wantRedirect(t, postAs(t, srv, "bensess", "/settings/tokens/"+itoa(eveTok)+"/delete", nil), "/settings?tab=tokens")
	if rec := bearer(t, srv, "GET", "/api/status", "netis_eve", ""); rec.Code != http.StatusUnauthorized {
		t.Error("revoked token still works")
	}
	wantRedirect(t, postAs(t, srv, "bensess", "/settings/tokens/"+itoa(adminTok)+"/delete", nil), "/settings?tab=tokens")
}

// Deleting a user revokes their tokens.
func TestDeletedUsersTokensStopWorking(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addUser(t, st, "ben", "password1", "admin", "bensess")
	eve := addUser(t, st, "eve", "password1", "viewer", "evesess")
	tokenFor(t, st, eve, "netis_eve", "")
	wantRedirect(t, postAs(t, srv, "bensess", "/settings/users/"+itoa(eve)+"/delete", nil), "/settings?tab=users")
	if rec := bearer(t, srv, "GET", "/api/status", "netis_eve", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("deleted user's token: code=%d, want 401", rec.Code)
	}
}
