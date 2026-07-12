package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

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
	if _, err := st.CreateUser("ben", string(h), "admin"); err != nil {
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
	u, ok, _ := st.GetUserByName("ben")
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
	n, err := st.CountUsers()
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
