package web

import (
	"fmt"
	"net/http"
	"time"
)

// sseKeepalive is how often a comment frame is sent on an idle stream. Proxies
// commonly drop a connection after 60s of silence, and a quiet network can
// easily produce no events for far longer than that.
const sseKeepalive = 25 * time.Second

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
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Topic, m.Data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
