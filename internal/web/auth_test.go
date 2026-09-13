package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/store"
)

func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewServer(st, events.NewBroker(), nil, nil), st
}

func addAdmin(t *testing.T, st *store.Store) {
	t.Helper()
	h, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if _, err := st.CreateUser(t.Context(), "ben", string(h), "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestRedirectToSetupWhenNoUsers(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 303 || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSetupCreatesAdminOnce(t *testing.T) {
	srv, st := testServer(t)
	form := url.Values{"username": {"ben"}, "password": {"secret123"}}
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 303 {
		t.Fatalf("code=%d", rec.Code)
	}
	u, ok, _ := st.GetUserByName(t.Context(), "ben")
	if !ok || u.Role != "admin" {
		t.Fatalf("user=%+v", u)
	}
	// second setup attempt rejected
	req2 := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != 403 {
		t.Fatalf("second setup code=%d", rec2.Code)
	}
}

func TestSetupRaceCreatesOneAdmin(t *testing.T) {
	srv, st := testServer(t)
	usernames := []string{"alice", "bob"}
	var wg sync.WaitGroup
	for _, name := range usernames {
		wg.Add(1)
		go func(username string) {
			defer wg.Done()
			form := url.Values{"username": {username}, "password": {"secret123"}}
			req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
		}(name)
	}
	wg.Wait()
	n, err := st.CountUsers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CountUsers()=%d, want 1", n)
	}
}

func TestLoginSetsSessionCookie(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 303 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "netis_session" {
			sess = c
		}
	}
	if sess == nil || sess.Value == "" || !sess.HttpOnly {
		t.Fatalf("cookie=%+v", sess)
	}
	// authed request passes middleware
	req2 := httptest.NewRequest("GET", "/healthz", nil)
	req2.AddCookie(sess)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("authed healthz=%d", rec2.Code)
	}
}

func TestSetupOnboardingChrome(t *testing.T) {
	srv, _ := testServer(t) // no users → /setup renders
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/setup", nil))
	if rec.Code != 200 {
		t.Fatalf("setup code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="username"`, `name="password"`, `class="stepper"`, "onboard-brand", "Integrations"} {
		if !strings.Contains(body, want) {
			t.Errorf("setup page missing %q", want)
		}
	}
}

func TestLoginBrand(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st) // users exist → /login renders
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	body := rec.Body.String()
	for _, want := range []string{`name="username"`, "onboard-brand"} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"wrong"}}
	var last int
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "10.9.9.9:1234"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != 429 {
		t.Fatalf("6th attempt code=%d, want 429", last)
	}
}

// newTrustingServer builds a server that trusts one proxy network.
func newTrustingServer(t *testing.T, cidrs string) *Server {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	prefixes, err := config.ParseTrustedProxies(cidrs)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(st, events.NewBroker(), nil, nil, Options{TrustedProxies: prefixes})
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "203.0.113.7:4444"
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	if got := srv.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want the peer address", got)
	}
}

func TestClientIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	srv := newTrustingServer(t, "10.0.0.0/8")
	cases := []struct {
		name, xff, want string
	}{
		{"single hop", "203.0.113.7", "203.0.113.7"},
		{"client through two proxies", "203.0.113.7, 10.0.0.2", "203.0.113.7"},
		{"spoofed prefix is not reached", "1.2.3.4, 203.0.113.7, 10.0.0.2", "203.0.113.7"},
		{"all trusted falls back to peer", "10.0.0.2, 10.0.0.3", "10.0.0.1"},
		{"absent falls back to peer", "", "10.0.0.1"},
		{"garbage falls back to peer", "not-an-ip", "10.0.0.1"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "10.0.0.1:5555"
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := srv.clientIP(req); got != c.want {
			t.Errorf("%s: clientIP = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSecureRequestOnlyTrustsForwardedProtoFromProxy(t *testing.T) {
	plain, _ := testServer(t)
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "203.0.113.7:4444"
	req.Header.Set("X-Forwarded-Proto", "https")
	if plain.secureRequest(req) {
		t.Error("X-Forwarded-Proto from an untrusted peer must not mark the request secure")
	}

	srv := newTrustingServer(t, "10.0.0.0/8")
	req2 := httptest.NewRequest("POST", "/login", nil)
	req2.RemoteAddr = "10.0.0.1:5555"
	req2.Header.Set("X-Forwarded-Proto", "https")
	if !srv.secureRequest(req2) {
		t.Error("X-Forwarded-Proto from a trusted proxy must mark the request secure")
	}
}

// Behind a reverse proxy every login arrives from the proxy's address. Without
// forwarded-header support one attacker exhausts the single bucket and locks
// every other user out; with it, each real client gets its own.
func TestLoginRateLimitIsPerForwardedClient(t *testing.T) {
	srv := newTrustingServer(t, "10.0.0.0/8")
	addAdmin(t, srv.store)
	post := func(xff string) int {
		form := url.Values{"username": {"ben"}, "password": {"wrong"}}
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", xff)
		req.RemoteAddr = "10.0.0.1:5555"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 5; i++ {
		if code := post("203.0.113.7"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: code=%d", i, code)
		}
	}
	if code := post("203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt from the same client: code=%d, want 429", code)
	}
	if code := post("203.0.113.8"); code != http.StatusUnauthorized {
		t.Fatalf("first attempt from another client: code=%d, want 401", code)
	}
}
