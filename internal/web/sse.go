package web

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// sseKeepalive is how often a comment frame is sent on an idle stream. Proxies
// commonly drop a connection after 60s of silence, and a quiet network can
// easily produce no events for far longer than that. Each tick also re-checks
// the session, so it bounds how long a revoked session keeps its stream. A
// variable only so tests can shorten it.
var sseKeepalive = 25 * time.Second

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// nginx buffers proxied responses by default, which holds each event until
	// the buffer fills and makes the live grid look simply broken. This header
	// is how a response opts out; other proxies ignore it.
	w.Header().Set("X-Accel-Buffering", "no")

	ch, cancel := s.broker.Subscribe()
	defer cancel()

	// Flush the headers immediately so the client's EventSource opens rather
	// than waiting for the first event, which may be a long way off.
	flusher.Flush()

	keepalive := time.NewTicker(sseKeepalive)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			// The session was checked when the stream opened, but a stream
			// can outlive it by days: signing out elsewhere, a password
			// change or deleting the user all revoke it, and none of them
			// would otherwise reach a connection already open. A lookup that
			// fails closes the stream too; the browser reconnects through
			// the normal auth check.
			if !s.sseSessionValid(r) {
				return
			}
			// A comment frame: ignored by EventSource, enough to keep the
			// connection from being reaped as idle.
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case m, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, m.Topic, m.Data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// sseSessionValid reports whether the session cookie on r still names a live,
// unexpired session.
func (s *Server) sseSessionValid(r *http.Request) bool {
	c, err := r.Cookie("netis_session")
	if err != nil {
		return false
	}
	_, ok, err := s.store.GetSession(r.Context(), c.Value)
	return err == nil && ok
}

// writeSSEEvent writes one event frame. A line break inside a data: line would
// end the field there, and a blank line would end the whole frame, so data that
// spans lines (a hostname off the network can) goes out as one data: line per
// line. EventSource joins them back together with "\n". A CR, alone or before
// an LF, is a line break to EventSource as well.
func writeSSEEvent(w io.Writer, topic, data string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "event: %s\n", topic)
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(&b, "data: %s\n", line)
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}
