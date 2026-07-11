package web

import (
	"embed"
	"net/http"

	"netis/internal/events"
	"netis/internal/store"
)

//go:embed static
var staticFS embed.FS

type ScanTrigger interface {
	Trigger(subnetID int64)
}

type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	broker  *events.Broker
	trigger ScanTrigger
	limiter *rateLimiter
}

func NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger) *Server {
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, limiter: newRateLimiter(),
	}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	s.mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("POST /setup", s.handleSetup)
	s.mux.HandleFunc("GET /events/stream", s.handleSSE)
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /subnets/{id}", s.handleSubnetPage)
	s.mux.HandleFunc("GET /subnets/{id}/grid", s.handleGridFrag)
	s.mux.HandleFunc("POST /subnets/{id}/scan", s.requireAdmin(s.handleScanNow))
	return s
}

func (s *Server) Handler() http.Handler { return s.requireAuth(s.mux) }
