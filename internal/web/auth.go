package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netis/internal/store"
	"netis/internal/web/views"
)

type ctxKey int

const userKey ctxKey = 0

func userFrom(r *http.Request) (store.User, bool) {
	u, ok := r.Context().Value(userKey).(store.User)
	return u, ok
}

// Login limits. The per-address bucket slows one client down; the
// per-username bucket stops a client that rotates addresses from guessing at
// one account indefinitely. The per-username window is deliberately bounded
// rather than a lockout: an attacker can keep an account throttled only for as
// long as they keep guessing, and the owner gets back in once the window
// passes.
const (
	loginIPMax      = 5
	loginIPWindow   = time.Minute
	loginUserMax    = 10
	loginUserWindow = 15 * time.Minute
)

// maxUsernameKeyLen caps how much of a submitted username goes into a limiter
// key, so an unauthenticated client cannot park large strings in the map.
const maxUsernameKeyLen = 128

// rateLimiter counts failed attempts per key over a sliding window.
//
// An attempt is reserved before the expensive check and settled after it.
// Checking first and recording the failure only once bcrypt had answered let
// a burst of guesses sent together all pass the check before any of them was
// counted; counting in-flight attempts against the limit closes that.
type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*limitBucket
}

type limitBucket struct {
	fails   []time.Time
	pending int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, buckets: make(map[string]*limitBucket)}
}

// reserve claims an attempt for key, reporting false when the key has used up
// its failures (counting attempts still in flight) for the window. Every true
// must be followed by exactly one done.
func (rl *rateLimiter) reserve(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	// Bound the map: a key that is never tried again is never visited by the
	// per-key pruning below, so sweep stale keys once the map grows large.
	if len(rl.buckets) > 1024 {
		for k, b := range rl.buckets {
			if b.prune(now.Add(-rl.window)); len(b.fails) == 0 && b.pending == 0 {
				delete(rl.buckets, k)
			}
		}
	}
	b := rl.buckets[key]
	if b == nil {
		b = &limitBucket{}
		rl.buckets[key] = b
	}
	b.prune(now.Add(-rl.window))
	if len(b.fails)+b.pending >= rl.limit {
		return false
	}
	b.pending++
	return true
}

// done settles an attempt reserve granted, charging it as a failure or not.
func (rl *rateLimiter) done(key string, failed bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b := rl.buckets[key]
	if b == nil {
		return
	}
	b.pending--
	if failed {
		b.fails = append(b.fails, time.Now())
	}
	if len(b.fails) == 0 && b.pending <= 0 {
		delete(rl.buckets, key)
	}
}

func (b *limitBucket) prune(cutoff time.Time) {
	kept := b.fails[:0]
	for _, t := range b.fails {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.fails = kept
}

// limitKey is the rate-limit bucket for a client address. An IPv6 client is
// keyed by its /64: a single host is routinely handed a whole /64, and keying
// on the full address would give it 2^64 fresh buckets to rotate through.
func limitKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() || addr.Is4In6() {
		return ip
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

// secureRequest reports whether the request arrived over TLS, directly or via
// a reverse proxy, so session cookies can carry the Secure flag when it works.
//
// X-Forwarded-Proto counts only when the peer is a trusted proxy. Believing it
// from anyone let a plain-HTTP client mark its own session cookie Secure, after
// which the browser would refuse to send the cookie back and the user could
// never stay logged in.
func (s *Server) secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer, _ := peerAddr(r)
	return s.trustsProxy(peer) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// peerAddr returns the address of the host the connection actually came from,
// plus its string form. The string is returned separately because RemoteAddr
// is not guaranteed to be an IP (a unix socket, say), and the caller still
// needs something to key a rate-limit bucket on.
func peerAddr(r *http.Request) (netip.Addr, string) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, host
	}
	addr = addr.Unmap().WithZone("")
	return addr, addr.String()
}

// trustsProxy reports whether addr is one of the configured reverse proxies.
func (s *Server) trustsProxy(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	for _, p := range s.trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// clientIP returns the address a request should be attributed to: the peer
// itself, or — when the peer is a configured reverse proxy — the rightmost
// X-Forwarded-For entry that is not itself a trusted proxy.
//
// Walking from the right is what makes the header safe to use: a client can
// prepend anything it likes to X-Forwarded-For, but each hop appends the
// address it actually saw, so the last untrusted entry is the furthest-left
// address netis can vouch for. With no trusted proxies configured the header
// is ignored entirely.
//
// This matters for the login rate limiter. Keyed on the peer address, every
// login behind a reverse proxy shares one bucket, so five wrong passwords from
// anywhere lock out every user for a minute.
func (s *Server) clientIP(r *http.Request) string {
	peer, peerStr := peerAddr(r)
	if !s.trustsProxy(peer) {
		return peerStr
	}
	fields := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(fields) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(fields[i]))
		if err != nil {
			continue
		}
		addr = addr.Unmap().WithZone("")
		if s.trustsProxy(addr) {
			continue
		}
		return addr.String()
	}
	return peerStr
}

// maxUserAgentLen caps the stored user agent. Nothing legitimate is near this
// long, and the header is whatever the client chose to send.
const maxUserAgentLen = 200

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// dummyHash is a precomputed bcrypt hash compared against when a login
// username is unknown, so unknown-user requests cost roughly the same as
// known-user requests and don't leak timing information.
var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("netis-dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}()

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/login" || r.URL.Path == "/setup" ||
			strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		// A scrape token stands in for a session, on /metrics only: it is the
		// one endpoint a machine with no browser has to reach.
		if r.URL.Path == "/metrics" && s.metricsToken != "" {
			if got := tokenFromRequest(r); got != "" &&
				subtle.ConstantTimeCompare([]byte(got), []byte(s.metricsToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		if c, err := r.Cookie("netis_session"); err == nil {
			if u, ok, _ := s.store.GetSession(r.Context(), c.Value); ok {
				// The onboarding wizard is a browser flow; bouncing a script or
				// a scraper into it would answer a data request with a page.
				if !onboardingAllowed(r.URL.Path) && !forMachines(r.URL.Path) {
					if v, _ := s.store.GetSetting(r.Context(), "onboarded"); v != "1" {
						http.Redirect(w, r, "/welcome", http.StatusSeeOther)
						return
					}
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
		// A redirect to a login page is no use to a caller that wanted JSON.
		if forMachines(r.URL.Path) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if n, _ := s.store.CountUsers(r.Context()); n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// forMachines reports whether a path serves scripts and scrapers rather than
// browsers, and so should answer with a status code instead of a redirect.
func forMachines(path string) bool {
	return path == "/metrics" || strings.HasPrefix(path, "/api/")
}

// onboardingAllowed reports whether a path is reachable before onboarding is
// complete (so the wizard, logout, and the SSE stream the layout always opens
// don't get caught by the redirect).
func onboardingAllowed(path string) bool {
	return path == "/welcome" || strings.HasPrefix(path, "/welcome/") ||
		path == "/logout" || path == "/events/stream"
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFrom(r)
		if !ok || u.Role != "admin" {
			http.Error(w, "admin only", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, views.LoginPage(""))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	username, password := r.FormValue("username"), r.FormValue("password")
	// Both buckets are reserved before bcrypt runs and settled once it has
	// answered, so parallel guesses count against the limit while in flight.
	ipKey, userKey := limitKey(s.clientIP(r)), "login:"+truncate(username, maxUsernameKeyLen)
	if !s.limiter.reserve(ipKey) {
		http.Error(w, "too many attempts, wait a minute", http.StatusTooManyRequests)
		return
	}
	if !s.userLimiter.reserve(userKey) {
		s.limiter.done(ipKey, false)
		http.Error(w, "too many attempts for this account, try again later", http.StatusTooManyRequests)
		return
	}
	failed := true
	defer func() {
		s.limiter.done(ipKey, failed)
		s.userLimiter.done(userKey, failed)
	}()
	u, ok, err := s.store.GetUserByName(r.Context(), username)
	var match bool
	// bcrypt ignores everything past 72 bytes, so a longer password can
	// never be the one that was set; it just fails like any wrong password.
	if err == nil && ok && len(password) <= maxPasswordLen {
		match = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
	} else {
		// Run a bcrypt compare against a dummy hash even when the user is
		// unknown, so this path costs about the same as the known-user
		// path and doesn't leak username validity via timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	}
	if err == nil && ok && match {
		failed = false
		token, terr := newToken()
		if terr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		expires := time.Now().UTC().Add(30 * 24 * time.Hour)
		// Where the session came from, so its owner can tell their own sessions
		// apart in Settings > Users and revoke one they don't recognise. The
		// user agent is capped: it is attacker-supplied and goes in a page.
		meta := store.SessionMeta{
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			IP:        s.clientIP(r),
			UserAgent: truncate(r.UserAgent(), maxUserAgentLen),
		}
		if serr := s.store.CreateSession(r.Context(), token, u.ID, expires.Format(time.RFC3339), meta); serr != nil {
			slog.Error("create session", "err", serr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "netis_session", Value: token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires,
			Secure: s.secureRequest(r),
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	s.render(w, r, views.LoginPage("wrong username or password"))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("netis_session"); err == nil {
		s.store.DeleteSession(r.Context(), c.Value)
	}
	clearSessionCookie(w, s.secureRequest(r))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// clearSessionCookie expires the session cookie. Shared with session revocation
// so the two paths cannot drift apart on the cookie's attributes.
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: "netis_session", Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure,
	})
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(r.Context()); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	s.render(w, r, views.SetupPage(""))
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(r.Context()); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	if username == "" || len(password) < minPasswordLen {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, r, views.SetupPage("username required, password min "+strconv.Itoa(minPasswordLen)+" chars"))
		return
	}
	if len(password) > maxPasswordLen {
		w.WriteHeader(http.StatusBadRequest)
		s.render(w, r, views.SetupPage(passwordTooLongMsg))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	created, err := s.store.CreateFirstAdmin(r.Context(), username, string(hash))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !created {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
