package web

import (
	"context"
	"embed"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
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

// Options carries deployment settings that are not stored in the database
// because they describe the environment netis runs in, not the network it
// tracks.
type Options struct {
	// TrustedProxies lists the networks whose X-Forwarded-For and
	// X-Forwarded-Proto headers netis believes. Empty means the headers are
	// ignored and every request is attributed to its direct peer.
	TrustedProxies []netip.Prefix
	// MetricsToken is the bearer token a scraper may present to read /metrics
	// without a session. Empty means /metrics needs a logged-in session, which
	// no scraper has.
	MetricsToken string
}

type Server struct {
	mux     *http.ServeMux
	store   *store.Store
	broker  *events.Broker
	trigger ScanTrigger
	runner  IntegrationRunner
	limiter *rateLimiter
	detect  func() ([]netdetect.Detected, error)

	trustedProxies []netip.Prefix
	metricsToken   string
}

func NewServer(st *store.Store, broker *events.Broker, trigger ScanTrigger, runner IntegrationRunner, opts ...Options) *Server {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	s := &Server{
		mux: http.NewServeMux(), store: st, broker: broker,
		trigger: trigger, runner: runner, limiter: newRateLimiter(),
		detect:         netdetect.DetectSubnets,
		trustedProxies: o.TrustedProxies,
		metricsToken:   o.MetricsToken,
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
	// Read-only JSON for scripts, and Prometheus metrics. Both authenticate with
	// the session cookie; /metrics also takes a scrape token.
	s.mux.HandleFunc("GET /api/devices", s.handleAPIDevices)
	s.mux.HandleFunc("GET /api/devices/{id}", s.handleAPIDevice)
	s.mux.HandleFunc("GET /api/subnets", s.handleAPISubnets)
	s.mux.HandleFunc("GET /api/events", s.handleAPIEvents)
	s.mux.HandleFunc("GET /api/status", s.handleAPIStatus)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /settings", s.handleSettingsPage)
	s.mux.HandleFunc("POST /settings/subnets", s.requireAdmin(s.handleSubnetCreate))
	s.mux.HandleFunc("POST /settings/subnets/{id}", s.requireAdmin(s.handleSubnetUpdate))
	s.mux.HandleFunc("POST /settings/subnets/{id}/delete", s.requireAdmin(s.handleSubnetDelete))
	s.mux.HandleFunc("POST /settings/integrations", s.requireAdmin(s.handleIntegrationsSave))
	s.mux.HandleFunc("POST /settings/integrations/{name}/run", s.requireAdmin(s.handleIntegrationRun))
	s.mux.HandleFunc("POST /settings/users", s.requireAdmin(s.handleUserCreate))
	s.mux.HandleFunc("POST /settings/users/{id}/delete", s.requireAdmin(s.handleUserDelete))
	// Changing your own password needs no role: every account must be able to
	// rotate its own credential. Resetting someone else's is admin-only.
	s.mux.HandleFunc("POST /settings/password", s.handlePasswordChange)
	// Session management is likewise per-account, not admin business.
	s.mux.HandleFunc("POST /settings/sessions/{id}/delete", s.handleSessionRevoke)
	s.mux.HandleFunc("POST /settings/sessions/revoke-others", s.handleSessionRevokeOthers)
	s.mux.HandleFunc("POST /settings/users/{id}/password", s.requireAdmin(s.handleUserPasswordReset))
	s.mux.HandleFunc("POST /settings/general", s.requireAdmin(s.handleGeneralSave))
	return s
}

func (s *Server) Handler() http.Handler {
	cop := http.NewCrossOriginProtection()
	return securityHeaders(checkOrigin(cop.Handler(s.requireAuth(s.mux))))
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

// checkOrigin rejects a state-changing request whose Origin names a different
// host than the one it was sent to.
//
// The session cookie is SameSite=Lax, which already stops a cross-site form
// POST from carrying it, so this is defence in depth rather than the only
// guard. What Lax does not cover is a sibling origin — anything on the same
// registrable domain counts as same-site to a browser, so a compromised
// service on another subdomain can still post here with the cookie attached.
// Comparing hosts closes that, and costs one header read.
//
// A request with no Origin at all is allowed through: non-browser clients
// (curl, scripts) omit it, and browsers always send it on cross-origin
// state-changing requests, which is the case being defended against.
func checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "null" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				slog.Warn("rejected cross-origin request",
					"origin", origin, "host", r.Host, "path", r.URL.Path)
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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

// fail reports an unexpected server-side error: the real one goes to the log
// with the request that produced it, and the client gets a fixed message.
//
// Handlers used to pass err.Error() straight to http.Error. A driver error
// carries the SQL it was running along with constraint and schema names, and
// every page is reachable by a viewer-role user, so that put database internals
// in front of anyone with an account.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
