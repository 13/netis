package web

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/store"
)

// fakeIdP is an OpenID Connect provider small enough to drive the whole flow:
// discovery, keys, and a token endpoint that checks the PKCE verifier and
// signs ID tokens with a test RSA key. The authorization step is played by
// the test itself, which reads the redirect netis sends and mints a code.
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]fakeGrant
}

// identity is who the fake provider says signed in.
type identity struct {
	sub, username string
	groups        []string
	// nonce overrides the nonce put in the ID token, to impersonate a token
	// minted for some other login.
	nonce string
}

type fakeGrant struct {
	id               identity
	nonce, challenge string
}

const testClientID, testClientSecret = "netis", "s3cret"

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key, codes: map[string]fakeGrant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		b64 := base64.RawURLEncoding.EncodeToString
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// authorize plays the provider's login page: it checks the redirect netis
// sent and returns a code bound to its nonce and PKCE challenge.
func (f *fakeIdP) authorize(t *testing.T, loc string, id identity) (code, state string) {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil || !strings.HasPrefix(loc, f.srv.URL+"/authorize") {
		t.Fatalf("redirect %q is not to the provider", loc)
	}
	q := u.Query()
	if q.Get("client_id") != testClientID || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("nonce") == "" || q.Get("redirect_uri") != "https://netis.test/auth/oidc/callback" ||
		!strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("authorization request missing parameters: %v", q)
	}
	code = "code-" + q.Get("state")[:8]
	f.mu.Lock()
	f.codes[code] = fakeGrant{id: id, nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	f.mu.Unlock()
	return code, q.Get("state")
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	f.mu.Lock()
	g, found := f.codes[r.PostForm.Get("code")]
	delete(f.codes, r.PostForm.Get("code"))
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if id != testClientID || secret != testClientSecret || !found ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	nonce := g.nonce
	if g.id.nonce != "" {
		nonce = g.id.nonce
	}
	claims := map[string]any{
		"iss": f.srv.URL, "sub": g.id.sub, "aud": testClientID,
		"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
		"nonce": nonce, "preferred_username": g.id.username,
	}
	if g.id.groups != nil {
		claims["groups"] = g.id.groups
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": f.sign(claims),
	})
}

func (f *fakeIdP) sign(claims map[string]any) string {
	enc := base64.RawURLEncoding.EncodeToString
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	c, _ := json.Marshal(claims)
	input := enc(h) + "." + enc(c)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return input + "." + enc(sig)
}

// oidcTestServer is a netis server with SSO pointed at a fake provider, the
// local admin "ben" already set up and onboarding done.
func oidcTestServer(t *testing.T, tweak func(*config.OIDC)) (*Server, *store.Store, *fakeIdP) {
	t.Helper()
	idp := newFakeIdP(t)
	cfg := config.OIDC{
		Issuer: idp.srv.URL, ClientID: testClientID, ClientSecret: testClientSecret,
		RedirectURL: "https://netis.test/auth/oidc/callback",
		AdminGroup:  "netis-admins", GroupsClaim: "groups", AutoCreate: true,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	return NewServer(st, events.NewBroker(), nil, nil, Options{OIDC: cfg}), st, idp
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func serve(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// startFlow requests start (a GET of the login route, or a POST of the link
// route) and returns the flow cookie and the provider redirect.
func startFlow(t *testing.T, srv *Server, req *http.Request) (*http.Cookie, string) {
	t.Helper()
	rec := serve(srv, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("start: code=%d body=%s", rec.Code, rec.Body)
	}
	flow := cookieNamed(rec, oidcFlowCookie)
	if flow == nil {
		t.Fatal("start set no flow cookie")
	}
	return flow, rec.Header().Get("Location")
}

func callback(srv *Server, code, state string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/auth/oidc/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return serve(srv, req)
}

// ssoLogin runs the whole login as id and returns the callback's response.
func ssoLogin(t *testing.T, srv *Server, idp *fakeIdP, id identity) *httptest.ResponseRecorder {
	t.Helper()
	flow, loc := startFlow(t, srv, httptest.NewRequest("GET", "/auth/oidc/login", nil))
	code, state := idp.authorize(t, loc, id)
	return callback(srv, code, state, flow)
}

func TestOIDCLoginFlowCreatesSession(t *testing.T) {
	srv, st, idp := oidcTestServer(t, nil)

	// The login page offers SSO next to the password form.
	page := serve(srv, httptest.NewRequest("GET", "/login", nil)).Body.String()
	if !strings.Contains(page, `href="/auth/oidc/login"`) || !strings.Contains(page, `action="/login"`) {
		t.Fatalf("login page lacks the SSO button or the password form:\n%s", page)
	}

	flow, loc := startFlow(t, srv, httptest.NewRequest("GET", "/auth/oidc/login", nil))
	if !flow.HttpOnly || flow.SameSite != http.SameSiteLaxMode || flow.Path != "/auth/oidc/" || flow.MaxAge <= 0 {
		t.Errorf("flow cookie attributes: %+v", flow)
	}
	code, state := idp.authorize(t, loc, identity{sub: "u-1", username: "alice"})
	rec := callback(srv, code, state, flow)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback: code=%d loc=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if c := cookieNamed(rec, oidcFlowCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("flow cookie not cleared: %+v", c)
	}
	sess := cookieNamed(rec, "netis_session")
	if sess == nil || !sess.HttpOnly || sess.SameSite != http.SameSiteLaxMode || sess.Path != "/" {
		t.Fatalf("session cookie: %+v", sess)
	}

	u, ok, err := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-1")
	if err != nil || !ok || u.Username != "alice" || u.Role != "viewer" {
		t.Fatalf("created user = %+v ok=%v err=%v", u, ok, err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(sess)
	if rec := serve(srv, req); rec.Code != http.StatusOK {
		t.Fatalf("GET / with the SSO session: code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	// The code was single use at the provider and the flow cookie at netis:
	// replaying the callback gets nothing.
	if rec := callback(srv, code, state, flow); rec.Code == http.StatusSeeOther {
		t.Errorf("replayed callback signed in again")
	}

	// Nobody knows an SSO user's password, so password login fails for them.
	form := url.Values{"username": {"alice"}, "password": {""}}
	preq := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := serve(srv, preq); rec.Code != http.StatusUnauthorized {
		t.Errorf("password login as SSO user: code=%d", rec.Code)
	}
}

func TestOIDCCallbackRejectsBadState(t *testing.T) {
	srv, st, idp := oidcTestServer(t, nil)
	flow, loc := startFlow(t, srv, httptest.NewRequest("GET", "/auth/oidc/login", nil))
	code, state := idp.authorize(t, loc, identity{sub: "u-1", username: "alice"})

	tampered := *flow
	tampered.Value = "x" + flow.Value[1:]
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"wrong state":     callback(srv, code, state+"x", flow),
		"no state":        callback(srv, code, "", flow),
		"no flow cookie":  callback(srv, code, state),
		"tampered cookie": callback(srv, code, state, &tampered),
	} {
		if rec.Code != http.StatusBadRequest || cookieNamed(rec, "netis_session") != nil {
			t.Errorf("%s: code=%d, session=%v; want 400 and no session", name, rec.Code, cookieNamed(rec, "netis_session"))
		}
	}
	if _, ok, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-1"); ok {
		t.Error("a rejected callback created a user")
	}
}

func TestOIDCCallbackRejectsWrongNonce(t *testing.T) {
	srv, st, idp := oidcTestServer(t, nil)
	rec := ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice", nonce: "someone-elses"})
	if rec.Code != http.StatusUnauthorized || cookieNamed(rec, "netis_session") != nil {
		t.Fatalf("code=%d; want 401 and no session", rec.Code)
	}
	if _, ok, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-1"); ok {
		t.Error("a token with the wrong nonce created a user")
	}
}

func TestOIDCRoleFollowsGroups(t *testing.T) {
	srv, st, idp := oidcTestServer(t, nil)
	role := func() string {
		u, _, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-1")
		return u.Role
	}
	if rec := ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice", groups: []string{"family", "netis-admins"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("admin login: code=%d body=%s", rec.Code, rec.Body)
	}
	if r := role(); r != "admin" {
		t.Fatalf("member of the admin group got role %q", r)
	}
	// Out of the group, the next login demotes.
	if rec := ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice", groups: []string{"family"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("relogin: code=%d", rec.Code)
	}
	if r := role(); r != "viewer" {
		t.Fatalf("after leaving the admin group role = %q, want viewer", r)
	}

	// No groups claim at all is no group.
	ssoLogin(t, srv, idp, identity{sub: "u-2", username: "bob"})
	if u, _, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-2"); u.Role != "viewer" {
		t.Fatalf("user without groups got role %q", u.Role)
	}
}

// Without an admin group SSO leaves roles to the admins: a promotion made in
// netis survives the next login.
func TestOIDCWithoutAdminGroupLeavesRoles(t *testing.T) {
	srv, st, idp := oidcTestServer(t, func(c *config.OIDC) { c.AdminGroup = "" })
	ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice", groups: []string{"netis-admins"}})
	u, _, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-1")
	if u.Role != "viewer" {
		t.Fatalf("new user role = %q, want viewer", u.Role)
	}
	st.SetSSORole(t.Context(), u.ID, "admin")
	ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice"})
	if u, _, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-1"); u.Role != "admin" {
		t.Fatalf("role reset to %q by a login with no admin group configured", u.Role)
	}
}

func TestOIDCAutoCreateOffRejectsUnknown(t *testing.T) {
	srv, st, idp := oidcTestServer(t, func(c *config.OIDC) { c.AutoCreate = false })
	rec := ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice"})
	if rec.Code != http.StatusForbidden || cookieNamed(rec, "netis_session") != nil {
		t.Fatalf("code=%d; want 403 and no session", rec.Code)
	}
	if n, _ := st.CountUsers(t.Context()); n != 1 {
		t.Errorf("users = %d, want only the local admin", n)
	}
}

// An SSO identity whose preferred_username matches a local account must not
// get that account: the provider may let users pick their own username.
func TestOIDCDoesNotTakeOverLocalAccountByName(t *testing.T) {
	srv, st, idp := oidcTestServer(t, nil)
	rec := ssoLogin(t, srv, idp, identity{sub: "attacker", username: "ben", groups: []string{"netis-admins"}})
	if rec.Code != http.StatusForbidden || cookieNamed(rec, "netis_session") != nil {
		t.Fatalf("code=%d; want 403 and no session", rec.Code)
	}
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	if linked, _ := st.OIDCLinked(t.Context(), ben.ID); linked {
		t.Error("local account was linked to the SSO identity by name")
	}
}

func TestOIDCLinkExistingAccount(t *testing.T) {
	srv, st, idp := oidcTestServer(t, func(c *config.OIDC) { c.AutoCreate = false })
	// "eve", signed in with a password.
	eveID, err := st.CreateUser(t.Context(), "eve", "h", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(t.Context(), "evetok", eveID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "netis_session", Value: "evetok"}

	// The users tab offers the link.
	req := httptest.NewRequest("GET", "/settings?tab=users", nil)
	req.AddCookie(cookie)
	if body := serve(srv, req).Body.String(); !strings.Contains(body, `action="/settings/sso/link"`) {
		t.Fatal("users tab lacks the SSO link form")
	}

	req = httptest.NewRequest("POST", "/settings/sso/link", nil)
	req.AddCookie(cookie)
	flow, loc := startFlow(t, srv, req)
	code, state := idp.authorize(t, loc, identity{sub: "u-eve", username: "eve.sso"})

	// The callback only links for the browser still signed in as eve.
	if rec := callback(srv, code, state, flow); rec.Code != http.StatusForbidden {
		t.Fatalf("link callback without the session: code=%d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/settings/sso/link", nil)
	req.AddCookie(cookie)
	flow, loc = startFlow(t, srv, req)
	code, state = idp.authorize(t, loc, identity{sub: "u-eve", username: "eve.sso"})
	rec := callback(srv, code, state, flow, cookie)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings?tab=users" {
		t.Fatalf("link callback: code=%d loc=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if u, ok, _ := st.GetUserByOIDC(t.Context(), idp.srv.URL, "u-eve"); !ok || u.ID != eveID {
		t.Fatalf("identity linked to %+v ok=%v, want eve", u, ok)
	}

	// With auto-create off, the linked identity now signs in as eve.
	rec = ssoLogin(t, srv, idp, identity{sub: "u-eve", username: "eve.sso"})
	if rec.Code != http.StatusSeeOther || cookieNamed(rec, "netis_session") == nil {
		t.Fatalf("SSO login after linking: code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestOIDCDisablePasswordKeepsBreakGlass(t *testing.T) {
	srv, st, idp := oidcTestServer(t, func(c *config.OIDC) { c.DisablePassword = true })
	page := serve(srv, httptest.NewRequest("GET", "/login", nil)).Body.String()
	if strings.Contains(page, `action="/login"`) || !strings.Contains(page, "/login?local=1") {
		t.Fatalf("login page should hide the password form behind a local link:\n%s", page)
	}
	if page := serve(srv, httptest.NewRequest("GET", "/login?local=1", nil)).Body.String(); !strings.Contains(page, `action="/login"`) {
		t.Fatal("?local=1 does not show the password form")
	}

	h, _ := bcrypt.GenerateFromPassword([]byte("viewerpass"), bcryptCost)
	if _, err := st.CreateUser(t.Context(), "eve", string(h), "viewer"); err != nil {
		t.Fatal(err)
	}
	login := func(user, pass string) int {
		form := url.Values{"username": {user}, "password": {pass}}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return serve(srv, req).Code
	}
	if code := login("ben", "secret"); code != http.StatusSeeOther {
		t.Errorf("local admin (break-glass) password login: code=%d, want 303", code)
	}
	if code := login("eve", "viewerpass"); code != http.StatusUnauthorized {
		t.Errorf("viewer password login with passwords disabled: code=%d, want 401", code)
	}
	// An admin who signs in through SSO is not a break-glass account.
	ben, _, _ := st.GetUserByName(t.Context(), "ben")
	st.LinkOIDC(t.Context(), ben.ID, idp.srv.URL, "u-ben")
	if code := login("ben", "secret"); code != http.StatusUnauthorized {
		t.Errorf("SSO-linked admin password login: code=%d, want 401", code)
	}
}

func TestOIDCRoutesWhenOffOrBeforeSetup(t *testing.T) {
	srv, _ := testServer(t)
	if rec := serve(srv, httptest.NewRequest("GET", "/auth/oidc/login", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("SSO off: /auth/oidc/login code=%d, want 404", rec.Code)
	}
	if page := serve(srv, httptest.NewRequest("GET", "/login", nil)).Body.String(); strings.Contains(page, "/auth/oidc/login") {
		t.Error("SSO off, but the login page offers it")
	}

	// Before setup there is no local admin yet, and SSO waits for one.
	idp := newFakeIdP(t)
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv = NewServer(st, events.NewBroker(), nil, nil, Options{OIDC: config.OIDC{
		Issuer: idp.srv.URL, ClientID: testClientID, RedirectURL: "https://netis.test/auth/oidc/callback"}})
	rec := serve(srv, httptest.NewRequest("GET", "/auth/oidc/login", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/setup" {
		t.Errorf("before setup: code=%d loc=%q, want redirect to /setup", rec.Code, rec.Header().Get("Location"))
	}
}

func TestClaimStrings(t *testing.T) {
	if got := claimStrings("admins"); len(got) != 1 || got[0] != "admins" {
		t.Errorf("string claim = %v", got)
	}
	if got := claimStrings([]any{"a", 7, "b"}); len(got) != 2 || got[1] != "b" {
		t.Errorf("array claim = %v", got)
	}
	if got := claimStrings(nil); got != nil {
		t.Errorf("missing claim = %v", got)
	}
}

// SSO sign-ins land in the audit log like password logins, though the
// callback is a GET the audit middleware does not record.
func TestOIDCLoginsAreAudited(t *testing.T) {
	srv, st, idp := oidcTestServer(t, nil)
	ssoLogin(t, srv, idp, identity{sub: "u-1", username: "alice"})
	callback(srv, "code", "forged")
	ssoLogin(t, srv, idp, identity{sub: "u-2", username: "ben"})

	entries, _, err := st.ListAudit(t.Context(), store.AuditFilter{Action: "sso.login"})
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		user, detail string
		status       int
	}
	var got []row
	for _, e := range entries {
		got = append(got, row{e.Username, e.Detail, e.Status})
	}
	want := []row{ // newest first
		{"", "username taken by a local account: ben", http.StatusForbidden},
		{"", "bad state", http.StatusBadRequest},
		{"alice", "", http.StatusSeeOther},
	}
	if len(got) != len(want) {
		t.Fatalf("audit entries = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The email names a new user only when the provider has verified it; an
// unverified address could be anyone's.
func TestSSOUsernameUsesOnlyVerifiedEmail(t *testing.T) {
	parse := func(raw string) oidcClaims {
		var c oidcClaims
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	cases := []struct{ claims, want string }{
		{`{"preferred_username":"alice","email":"a@x","email_verified":true}`, "alice"},
		{`{"email":"a@x","email_verified":true}`, "a@x"},
		{`{"email":"a@x","email_verified":"true"}`, "a@x"},
		{`{"email":"a@x","email_verified":false}`, "sub-1"},
		{`{"email":"a@x"}`, "sub-1"},
	}
	for _, tc := range cases {
		if got := ssoUsername(parse(tc.claims), "sub-1"); got != tc.want {
			t.Errorf("%s: username %q, want %q", tc.claims, got, tc.want)
		}
	}
}
