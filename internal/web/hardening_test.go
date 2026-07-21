package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSecurityHeadersSet(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	for h, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := rec.Header().Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q, want default-src 'self'", csp)
	}
}

func TestCrossSitePOSTBlocked(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"secret"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST code=%d, want 403", rec.Code)
	}
	// same-origin still works
	req2 := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Sec-Fetch-Site", "same-origin")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("same-origin POST code=%d, want 303", rec2.Code)
	}
}

func TestSessionCookieSecureBehindTLSProxy(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	form := url.Values{"username": {"ben"}, "password": {"secret"}}

	login := func(forwardedProto string) *http.Cookie {
		t.Helper()
		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if forwardedProto != "" {
			req.Header.Set("X-Forwarded-Proto", forwardedProto)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("login code=%d", rec.Code)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == "netis_session" {
				return c
			}
		}
		t.Fatal("no session cookie")
		return nil
	}

	if c := login(""); c.Secure {
		t.Error("plain-http cookie should not be Secure")
	}
	if c := login("https"); !c.Secure {
		t.Error("cookie behind TLS proxy should be Secure")
	}
}

func TestRateLimiterPrunesStaleIPs(t *testing.T) {
	rl := newRateLimiter()
	rl.fail("10.0.0.1")
	if !rl.allow("10.0.0.1") {
		t.Fatal("one failure should still allow")
	}
	// age out the entry, then allow must delete the key entirely
	rl.mu.Lock()
	rl.attempts["10.0.0.1"] = nil
	rl.mu.Unlock()
	rl.allow("10.0.0.1")
	rl.mu.Lock()
	_, exists := rl.attempts["10.0.0.1"]
	rl.mu.Unlock()
	if exists {
		t.Error("stale IP entry not pruned")
	}
}
