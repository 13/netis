package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"netis/internal/store"
)

// sessionSeq makes each session token unique, so a test may authenticate more
// than one request.
var sessionSeq atomic.Int64

// addSessionCookie gives req a valid admin session, the way authedGet does for
// the one-shot request helpers.
func addSessionCookie(t *testing.T, st *store.Store, req *http.Request) {
	t.Helper()
	u, ok, _ := st.GetUserByName(t.Context(), "ben")
	if !ok {
		addAdmin(t, st)
		u, _, _ = st.GetUserByName(t.Context(), "ben")
	}
	token := "testsess-" + strconv.FormatInt(sessionSeq.Add(1), 10)
	if err := st.CreateSession(t.Context(), token, u.ID,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "netis_session", Value: token})
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

// Event data can carry text from the network — a DHCP or PTR hostname can
// hold a line break — and a bare newline inside one data: line would end the
// frame early and turn the rest into a malformed field. Each line of the data
// goes out as its own data: line, which EventSource joins back with "\n".
func TestSSEMultilineDataIsFramed(t *testing.T) {
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
	srv.broker.Publish("events", "host\nevent: forged\r\ndata: x\rtail")
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	want := "event: events\ndata: host\ndata: event: forged\ndata: data: x\ndata: tail\n\n"
	if body := rec.Body.String(); !strings.Contains(body, want) {
		t.Errorf("stream body = %q, want it to contain %q", body, want)
	}
}

// A stream is authenticated when it opens, but it can stay open for days.
// Revoking the session behind it — signing out elsewhere, a password change,
// deleting the user — has to end it too, or the revoked browser keeps
// receiving live updates.
func TestSSEEndsWhenSessionRevoked(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	old := sseKeepalive
	sseKeepalive = 20 * time.Millisecond
	t.Cleanup(func() { sseKeepalive = old })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/events/stream", nil).WithContext(ctx)
	addSessionCookie(t, st, req)
	c, err := req.Cookie("netis_session")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("stream ended while its session was still valid")
	default:
	}
	if err := st.DeleteSession(t.Context(), c.Value); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open after its session was revoked")
	}
}
