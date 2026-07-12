package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
)

type recordingRunner struct {
	mu    sync.Mutex
	names []string
}

func (r *recordingRunner) Run(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, name)
	return nil
}

func (r *recordingRunner) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.names))
	copy(out, r.names)
	return out
}

func testServerRun(t *testing.T) (*Server, *store.Store, *recordingRunner) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	run := &recordingRunner{}
	return NewServer(st, events.NewBroker(), nil, run), st, run
}

func TestIntegrationRunPihole(t *testing.T) {
	srv, st, run := testServerRun(t)
	st.SetSetting("onboarded", "1")
	st.SetIntegrationStatus(store.IntegrationStatus{Name: "pihole", OK: true, Detail: "48 leases, 2 new"})
	rec := authedPost(t, srv, st, "/settings/integrations/pihole/run", url.Values{})
	if rec.Code != 200 {
		t.Fatalf("run code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Pi-hole", "connected", "48 leases, 2 new"} {
		if !strings.Contains(body, want) {
			t.Errorf("run toast missing %q: %s", want, body)
		}
	}
	if got := run.got(); len(got) != 1 || got[0] != "pihole" {
		t.Fatalf("runner names=%v, want [pihole]", got)
	}
}

func TestIntegrationRunBadName(t *testing.T) {
	srv, st, _ := testServerRun(t)
	st.SetSetting("onboarded", "1")
	if rec := authedPost(t, srv, st, "/settings/integrations/bogus/run", url.Values{}); rec.Code != 400 {
		t.Fatalf("bad name code=%d, want 400", rec.Code)
	}
}

func TestIntegrationRunNilRunner(t *testing.T) {
	srv, st := testServer(t) // nil runner
	st.SetSetting("onboarded", "1")
	rec := authedPost(t, srv, st, "/settings/integrations/pihole/run", url.Values{})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "not available") {
		t.Fatalf("nil runner code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationRunRequiresAdmin(t *testing.T) {
	srv, st, _ := testServerRun(t)
	st.SetSetting("onboarded", "1")
	addAdmin(t, st)
	uID, _ := st.CreateUser("eve", "h", "viewer")
	st.CreateSession("viewertok", uID, "2099-01-01T00:00:00Z")
	req := httptest.NewRequest("POST", "/settings/integrations/pihole/run", nil)
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "viewertok"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer code=%d, want 403", rec.Code)
	}
}
