package web

import (
	"net/http"

	"netis/internal/store"
)

type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	limiter *rateLimiter
}

func NewServer(st *store.Store) *Server {
	s := &Server{mux: http.NewServeMux(), store: st, limiter: newRateLimiter()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("POST /setup", s.handleSetup)
	return s
}

func (s *Server) Handler() http.Handler { return s.requireAuth(s.mux) }
