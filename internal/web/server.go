package web

import (
	"embed"
	"net/http"

	"netis/internal/events"
	"netis/internal/netdetect"
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
	detect  func() ([]netdetect.Detected, error)
}

func NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger) *Server {
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, limiter: newRateLimiter(),
		detect: netdetect.DetectSubnets,
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
	s.mux.HandleFunc("GET /welcome", s.handleWelcome)
	s.mux.HandleFunc("POST /welcome/subnets", s.requireAdmin(s.handleWelcomeSubnets))
	s.mux.HandleFunc("GET /welcome/integrations", s.handleWelcomeIntegrationsPage)
	s.mux.HandleFunc("POST /welcome/integrations", s.requireAdmin(s.handleWelcomeIntegrations))
	s.mux.HandleFunc("POST /welcome/skip", s.requireAdmin(s.handleWelcomeSkip))
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /dashboard/widgets", s.handleDashboardWidgets)
	s.mux.HandleFunc("GET /subnets/{id}", s.handleSubnetPage)
	s.mux.HandleFunc("GET /subnets/{id}/grid", s.handleGridFrag)
	s.mux.HandleFunc("POST /subnets/{id}/scan", s.requireAdmin(s.handleScanNow))
	s.mux.HandleFunc("GET /devices", s.handleDeviceList)
	s.mux.HandleFunc("GET /devices/new", s.handleDeviceForm)
	s.mux.HandleFunc("POST /devices", s.requireAdmin(s.handleDeviceCreate))
	s.mux.HandleFunc("GET /devices/{id}", s.handleDevicePage)
	s.mux.HandleFunc("POST /devices/{id}", s.requireAdmin(s.handleDeviceUpdate))
	s.mux.HandleFunc("POST /devices/{id}/delete", s.requireAdmin(s.handleDeviceDelete))
	s.mux.HandleFunc("POST /devices/{id}/links", s.requireAdmin(s.handleLinkAdd))
	s.mux.HandleFunc("POST /links/{id}/delete", s.requireAdmin(s.handleLinkDelete))
	s.mux.HandleFunc("POST /devices/{id}/tags", s.requireAdmin(s.handleTagAdd))
	s.mux.HandleFunc("POST /devices/{id}/tags/{tagID}/delete", s.requireAdmin(s.handleTagRemove))
	s.mux.HandleFunc("POST /devices/{id}/fields", s.requireAdmin(s.handleFieldSet))
	s.mux.HandleFunc("POST /devices/{id}/fields/delete", s.requireAdmin(s.handleFieldDelete))
	s.mux.HandleFunc("POST /devices/{id}/wol", s.requireAdmin(s.handleWOL))
	s.mux.HandleFunc("POST /devices/{id}/portscan", s.requireAdmin(s.handlePortScan))
	s.mux.HandleFunc("GET /events", s.handleEventsPage)
	s.mux.HandleFunc("GET /settings", s.handleSettingsPage)
	s.mux.HandleFunc("POST /settings/subnets", s.requireAdmin(s.handleSubnetCreate))
	s.mux.HandleFunc("POST /settings/subnets/{id}", s.requireAdmin(s.handleSubnetUpdate))
	s.mux.HandleFunc("POST /settings/subnets/{id}/delete", s.requireAdmin(s.handleSubnetDelete))
	s.mux.HandleFunc("POST /settings/integrations", s.requireAdmin(s.handleIntegrationsSave))
	s.mux.HandleFunc("POST /settings/users", s.requireAdmin(s.handleUserCreate))
	s.mux.HandleFunc("POST /settings/users/{id}/delete", s.requireAdmin(s.handleUserDelete))
	s.mux.HandleFunc("POST /settings/general", s.requireAdmin(s.handleGeneralSave))
	return s
}

func (s *Server) Handler() http.Handler { return s.requireAuth(s.mux) }
