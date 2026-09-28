package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// postLogin submits a login from the peer address given.
func postLogin(srv *Server, remote, username, password string) int {
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code
}

// The limiter used to be checked before the bcrypt compare and charged only
// after it, so a burst of guesses sent together all passed the check while
// none had been counted yet.
func TestLoginParallelBurstIsLimited(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	const n = 20
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = postLogin(srv, "10.9.9.9:1234", "ben", "wrong")
		}()
	}
	wg.Wait()
	tried := 0
	for _, c := range codes {
		switch c {
		case http.StatusUnauthorized:
			tried++
		case http.StatusTooManyRequests:
		default:
			t.Fatalf("unexpected code %d", c)
		}
	}
	if tried > loginIPMax {
		t.Fatalf("%d of %d parallel guesses reached bcrypt, want at most %d", tried, n, loginIPMax)
	}
}

// Rotating source addresses must not buy unlimited guesses at one account.
func TestLoginPerUsernameLimit(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	for i := range loginUserMax {
		if code := postLogin(srv, "10.1.0."+strconv.Itoa(i+1)+":1", "ben", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: code=%d", i, code)
		}
	}
	if code := postLogin(srv, "10.2.0.1:1", "ben", "wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("guess at the same account from a fresh address: code=%d, want 429", code)
	}
	// Another account is untouched by that.
	if code := postLogin(srv, "10.2.0.1:1", "someone-else", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("another account: code=%d, want 401", code)
	}
}

// The per-username bucket is a window, not a lockout: once it passes, the real
// user can sign in again.
func TestUserLimitExpires(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)
	for range 2 {
		if !rl.reserve("u") {
			t.Fatal("reserve under the limit refused")
		}
		rl.done("u", true)
	}
	if rl.reserve("u") {
		t.Fatal("reserve over the limit allowed")
	}
	rl.mu.Lock()
	for i := range rl.buckets["u"].fails {
		rl.buckets["u"].fails[i] = time.Now().Add(-2 * time.Minute)
	}
	rl.mu.Unlock()
	if !rl.reserve("u") {
		t.Fatal("limit did not expire with its window")
	}
}

// A success releases the pending slot without charging a failure.
func TestLimiterSuccessIsNotCharged(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)
	for range 3 {
		if !rl.reserve("k") {
			t.Fatal("successes must not use up the budget")
		}
		rl.done("k", false)
	}
}

// An IPv6 host usually owns a whole /64, so keying on the full address hands
// it 2^64 buckets.
func TestLoginLimitKeysIPv6ByPrefix(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	for i := range loginIPMax {
		remote := "[2001:db8:1:2::" + strconv.Itoa(i+1) + "]:1"
		// Distinct usernames keep the per-user bucket out of it.
		if code := postLogin(srv, remote, "user"+strconv.Itoa(i), "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: code=%d", i, code)
		}
	}
	if code := postLogin(srv, "[2001:db8:1:2:ffff::9]:1", "another", "wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("same /64: code=%d, want 429", code)
	}
	if code := postLogin(srv, "[2001:db8:1:3::1]:1", "another", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("neighbouring /64: code=%d, want 401", code)
	}
}

func TestLimitKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.7":           "203.0.113.7",
		"2001:db8:1:2:3:4:5:6":  "2001:db8:1:2::/64",
		"not-an-ip":             "not-an-ip",
		"@unix-socket-whatever": "@unix-socket-whatever",
	} {
		if got := limitKey(in); got != want {
			t.Errorf("limitKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// The current-password check on a signed-in session is a password oracle too.
func TestPasswordChangeIsRateLimited(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	addUser(t, st, "kim", "old-password", "viewer", "kim-here")
	bad := url.Values{
		"current_password": {"not-it"},
		"new_password":     {"new-password"},
		"confirm_password": {"new-password"},
	}
	for i := range loginUserMax {
		if rec := postAs(t, srv, "kim-here", "/settings/password", bad); rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d: code=%d", i, rec.Code)
		}
	}
	good := url.Values{
		"current_password": {"old-password"},
		"new_password":     {"new-password"},
		"confirm_password": {"new-password"},
	}
	if rec := postAs(t, srv, "kim-here", "/settings/password", good); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong guesses: code=%d, want 429", loginUserMax, rec.Code)
	}
	if !passwordWorks(t, st, "kim", "old-password") {
		t.Error("a limited attempt must not change the password")
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	srv, _ := testServer(t)
	for _, ct := range []string{"application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		body := "username=ben&password=" + strings.Repeat("a", maxBodyBytes+1)
		req := httptest.NewRequest("POST", "/setup", strings.NewReader(body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code=%d, want 413 or 400", ct, rec.Code)
		}
		if n, _ := srv.store.CountUsers(t.Context()); n != 0 {
			t.Fatalf("%s: oversized setup created a user", ct)
		}
	}
	// The plain urlencoded case is the one browsers send; it must say 413.
	body := "username=ben&password=" + strings.Repeat("a", maxBodyBytes+1)
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code=%d, want 413", rec.Code)
	}
}

// bcrypt refuses passwords over 72 bytes; every place a password is set used
// to turn that into a 500.
func TestLongPasswordRejectedWithMessage(t *testing.T) {
	long := strings.Repeat("x", maxPasswordLen+1)

	t.Run("setup", func(t *testing.T) {
		srv, _ := testServer(t)
		form := url.Values{"username": {"ben"}, "password": {long}}
		req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "72") {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("change own", func(t *testing.T) {
		srv, st := testServer(t)
		st.SetSetting(t.Context(), "onboarded", "1")
		addUser(t, st, "kim", "old-password", "viewer", "kim-here")
		rec := postAs(t, srv, "kim-here", "/settings/password", url.Values{
			"current_password": {"old-password"}, "new_password": {long}, "confirm_password": {long},
		})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "72") {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("admin reset", func(t *testing.T) {
		srv, st := testServer(t)
		st.SetSetting(t.Context(), "onboarded", "1")
		addUser(t, st, "root", "admin-password", "admin", "root-here")
		id := addUser(t, st, "kim", "old-password", "viewer", "kim-here")
		rec := postAs(t, srv, "root-here", "/settings/users/"+strconv.FormatInt(id, 10)+"/password",
			url.Values{"new_password": {long}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "72") {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("user create", func(t *testing.T) {
		srv, st := testServer(t)
		st.SetSetting(t.Context(), "onboarded", "1")
		addUser(t, st, "root", "admin-password", "admin", "root-here")
		rec := postAs(t, srv, "root-here", "/settings/users",
			url.Values{"username": {"new"}, "password": {long}, "role": {"viewer"}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "72") {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("login fails normally", func(t *testing.T) {
		srv, st := testServer(t)
		addAdmin(t, st)
		if code := postLogin(srv, "10.3.3.3:1", "ben", long); code != http.StatusUnauthorized {
			t.Fatalf("code=%d, want 401", code)
		}
	})
}
