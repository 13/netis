package web

import (
	"context"
	"embed"
	"log/slog"
	"net/http"
	"time"

	"netis/internal/events"
	"netis/internal/netdetect"
	"netis/internal/store"
)

//go:embed static
var staticFS embed.FS

type ScanTrigger interface {
	Trigger(subnetID int64)
}

// IntegrationRunner triggers a single on-demand run of a named integration.
type IntegrationRunner interface {
	Run(ctx context.Context, name string) error
}

type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	broker  *events.Broker
	trigger ScanTrigger
	runner  IntegrationRunner
	limiter *rateLimiter
	detect  func() ([]netdetect.Detected, error)
}

func NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger, runner IntegrationRunner) *Server {
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, runner: runner, limiter: newRateLimiter(),
		detect: netdetect.DetectSubnets,
	}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("POST /setup", s.handleSetup)
	s.mux.HandleFunc("GET /events/stream", s.handleSSE)
	s.mux.HandleFunc("GET /welcome", s.handleWelcome)
	s.mux.HandleFunc("POST /welcome/subnets", s.requireAdmin(s.handleWelcomeSubnets))
	s.mux.HandleFunc("GET /welcome/integrations", s.handleWelcomeIntegrationsPage)
	s.mux.HandleFunc("POST /welcome/integrations", s.requireAdmin(s.handleWelcomeIntegrations))
	s.mux.HandleFunc("POST /welcome/skip", s.requireAdmin(s.handleWelcomeSkip))
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /dashboard/widgets", s.handleDashboardWidgets)
	s.mux.HandleFunc("GET /subnets", s.handleSubnetsIndex)
	s.mux.HandleFunc("GET /subnets/{id}", s.handleSubnetPage)
	s.mux.HandleFunc("GET /subnets/{id}/grid", s.handleGridFrag)
	s.mux.HandleFunc("POST /subnets/{id}/scan", s.requireAdmin(s.handleScanNow))
	s.mux.HandleFunc("GET /subnets/{id}/cell", s.handleCellDetail)
	s.mux.HandleFunc("POST /subnets/{id}/cell", s.requireAdmin(s.handleCellKind))
	s.mux.HandleFunc("POST /scan", s.requireAdmin(s.handleScanAll))
	s.mux.HandleFunc("GET /devices", s.handleDeviceList)
	s.mux.HandleFunc("GET /devices/new", s.handleDeviceForm)
	s.mux.HandleFunc("GET /devices/{id}/edit", s.handleDeviceEditForm)
	s.mux.HandleFunc("POST /devices", s.requireAdmin(s.handleDeviceCreate))
	s.mux.HandleFunc("GET /devices/{id}", s.handleDevicePage)
	s.mux.HandleFunc("POST /devices/{id}", s.requireAdmin(s.handleDeviceUpdate))
	s.mux.HandleFunc("POST /devices/{id}/delete", s.requireAdmin(s.handleDeviceDelete))
	s.mux.HandleFunc("POST /devices/{id}/approve", s.requireAdmin(s.handleDeviceApprove))
	s.mux.HandleFunc("POST /devices/{id}/links", s.requireAdmin(s.handleLinkAdd))
	s.mux.HandleFunc("POST /links/{id}/delete", s.requireAdmin(s.handleLinkDelete))
	s.mux.HandleFunc("POST /devices/{id}/fields", s.requireAdmin(s.handleFieldSet))
	s.mux.HandleFunc("POST /devices/{id}/fields/delete", s.requireAdmin(s.handleFieldDelete))
	s.mux.HandleFunc("POST /devices/{id}/wol", s.requireAdmin(s.handleWOL))
	s.mux.HandleFunc("POST /devices/{id}/portscan", s.requireAdmin(s.handlePortScan))
	s.mux.HandleFunc("POST /devices/{id}/ip/kind", s.requireAdmin(s.handleDeviceIPKind))
	s.mux.HandleFunc("GET /events", s.handleEventsPage)
	s.mux.HandleFunc("GET /settings", s.handleSettingsPage)
	s.mux.HandleFunc("POST /settings/subnets", s.requireAdmin(s.handleSubnetCreate))
	s.mux.HandleFunc("POST /settings/subnets/{id}", s.requireAdmin(s.handleSubnetUpdate))
	s.mux.HandleFunc("POST /settings/subnets/{id}/delete", s.requireAdmin(s.handleSubnetDelete))
	s.mux.HandleFunc("POST /settings/integrations", s.requireAdmin(s.handleIntegrationsSave))
	s.mux.HandleFunc("POST /settings/integrations/{name}/run", s.requireAdmin(s.handleIntegrationRun))
	s.mux.HandleFunc("POST /settings/users", s.requireAdmin(s.handleUserCreate))
	s.mux.HandleFunc("POST /settings/users/{id}/delete", s.requireAdmin(s.handleUserDelete))
	s.mux.HandleFunc("POST /settings/general", s.requireAdmin(s.handleGeneralSave))
	return s
}

func (s *Server) Handler() http.Handler {
	cop := http.NewCrossOriginProtection()
	return securityHeaders(cop.Handler(s.requireAuth(s.mux)))
}

// handleHealthz reports whether netis can actually serve: the process being up
// is not enough, since the database may be a server on the far side of a
// network. It is deliberately unauthenticated — orchestrators probe it without
// credentials — so it reveals only reachable/not, never why.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.DB.PingContext(ctx); err != nil {
		slog.Error("healthz: database unreachable", "err", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

// securityHeaders sets baseline browser hardening headers on every response.
// CSP allows inline script/style and eval because the layout bootstraps the
// theme inline and htmx compiles hx-on attributes with Function().
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
