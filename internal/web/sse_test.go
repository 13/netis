package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// addSessionCookie gives req a valid admin session, the way authedGet does for
// the one-shot request helpers.
func addSessionCookie(t *testing.T, st *store.Store, req *http.Request) {
	t.Helper()
	u, ok, _ := st.GetUserByName(t.Context(), "ben")
	if !ok {
		addAdmin(t, st)
		u, _, _ = st.GetUserByName(t.Context(), "ben")
	}
	if err := st.CreateSession(t.Context(), "ssetok", u.ID,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: "ssetok"})
}

func TestSSEHeadersDisableProxyBuffering(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/events/stream", nil).WithContext(ctx)
	addSessionCookie(t, st, req)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	h := rec.Header()
	if got := h.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := h.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q", got)
	}
	// Without this nginx buffers the stream and the live grid never updates.
	if got := h.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
}

// An event published while a client is connected reaches it.
func TestSSEDeliversEvents(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/events/stream", nil).WithContext(ctx)
	addSessionCookie(t, st, req)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	srv.broker.Publish("grid:1", "refresh")
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: grid:1") || !strings.Contains(body, "data: refresh") {
		t.Errorf("stream body = %q", body)
	}
}

// The keepalive frame is a comment, which EventSource ignores; it exists only
// so an idle connection is not reaped by a proxy.
func TestSSEKeepaliveIsAComment(t *testing.T) {
	if !strings.HasPrefix(": keepalive\n\n", ":") {
		t.Fatal("keepalive frame must start with a colon to be a comment")
	}
	if sseKeepalive >= 60*time.Second {
		t.Errorf("sseKeepalive = %v, too slow to beat a 60s proxy idle timeout", sseKeepalive)
	}
}
