package web

import (
	_ "embed"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/clock"
	"netis/internal/store"
)

// The visual regression suite (e2e/) runs against this fixture: a fixed data
// set served at a frozen clock, so every page renders identically on every
// run and machine.

//go:embed testdata/e2e_seed.sql
var e2eSeed string

// e2eNow is the fixture's frozen time; the browser clock is frozen at the
// same instant (e2e/fixtures.ts).
var e2eNow = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// e2eSession is the admin session cookie the suite signs in with. The admin
// can also sign in as admin / password123.
const e2eSession = "e2e-admin-session"

// seedE2E loads the fixture data and its admin into st.
func seedE2E(t *testing.T, st *store.Store) {
	t.Helper()
	for _, stmt := range strings.Split(e2eSeed, ";\n") {
		if strings.TrimSpace(stripSQLComments(stmt)) == "" {
			continue
		}
		if _, err := st.DB.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	uid, err := st.CreateUser(t.Context(), "admin", string(h), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(t.Context(), e2eSession, uid, "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func stripSQLComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// TestE2EServe serves the fixture on NETIS_E2E_ADDR until it is interrupted.
// Without that variable it is skipped, so the normal suite never blocks here.
// The e2e suite builds this test binary and runs it as its web server:
//
//	go test -c -o e2e/.bin/netis-e2e ./internal/web
//	TZ=UTC NETIS_E2E_ADDR=127.0.0.1:18600 e2e/.bin/netis-e2e -test.run '^TestE2EServe$' -test.timeout 0
func TestE2EServe(t *testing.T) {
	addr := os.Getenv("NETIS_E2E_ADDR")
	if addr == "" {
		t.Skip("NETIS_E2E_ADDR not set: the visual regression fixture only runs for e2e/")
	}
	defer clock.Freeze(e2eNow)()
	srv, st := testServer(t)
	seedE2E(t, st)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener.Close()
	ts.Listener = ln
	ts.Start()
	defer ts.Close()
	t.Logf("serving the e2e fixture at %s", ts.URL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}

// The fixture data loads against the current schema and every page the e2e
// suite visits renders, so a migration that breaks the seed fails here, in
// the Go suite, and not only in the browser run.
func TestE2ESeedRenders(t *testing.T) {
	defer clock.Freeze(e2eNow)()
	srv, st := testServer(t)
	seedE2E(t, st)
	for _, path := range []string{"/", "/devices", "/devices/5", "/subnets/1", "/events", "/settings/network"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: "netis_session", Value: e2eSession})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: code %d", path, rec.Code)
		}
	}
	// Relative times come from the frozen clock.
	req := httptest.NewRequest("GET", "/events", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: e2eSession})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, ">3m ago<") || !strings.Contains(body, "Yesterday") {
		t.Error("events page does not render relative to the frozen clock")
	}
}
