package web

import (
	"net/http/httptest"
	"testing"
)

func TestHealthzOKWhenDatabaseIsReachable(t *testing.T) {
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

// The process outliving its database is a real state once the backend is a
// server on the network, and a health check blind to it is worse than none.
func TestHealthzFailsWhenDatabaseIsGone(t *testing.T) {
	srv, st := testServer(t)
	st.Close() // the database is now unreachable, the process is still up
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 {
		t.Fatalf("code=%d, want 503; body=%q", rec.Code, rec.Body.String())
	}
}
