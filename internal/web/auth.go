package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
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

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{attempts: make(map[string][]time.Time)}
}

// allow reports whether ip may attempt a login (max 5 failures/minute).
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	kept := rl.attempts[ip][:0]
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(rl.attempts, ip)
		return true
	}
	rl.attempts[ip] = kept
	return len(kept) < 5
}

func (rl *rateLimiter) fail(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	// Bound the map: an IP that only ever fails is never visited by allow's
	// per-key pruning, so sweep stale keys once the map grows large.
	if len(rl.attempts) > 1024 {
		cutoff := time.Now().Add(-time.Minute)
		for k, ts := range rl.attempts {
			live := false
			for _, t := range ts {
				if t.After(cutoff) {
					live = true
					break
				}
			}
			if !live {
				delete(rl.attempts, k)
			}
		}
	}
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}

// secureRequest reports whether the request arrived over TLS, directly or via
// a reverse proxy, so session cookies can carry the Secure flag when it works.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
		if c, err := r.Cookie("netis_session"); err == nil {
			if u, ok, _ := s.store.GetSession(r.Context(), c.Value); ok {
				if !onboardingAllowed(r.URL.Path) {
					if v, _ := s.store.GetSetting(r.Context(), "onboarded"); v != "1" {
						http.Redirect(w, r, "/welcome", http.StatusSeeOther)
						return
					}
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
		if n, _ := s.store.CountUsers(r.Context()); n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
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
	views.LoginPage("").Render(r.Context(), w)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		http.Error(w, "too many attempts, wait a minute", http.StatusTooManyRequests)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	u, ok, err := s.store.GetUserByName(r.Context(), username)
	var match bool
	if err == nil && ok {
		match = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
	} else {
		// Run a bcrypt compare against a dummy hash even when the user is
		// unknown, so this path costs about the same as the known-user
		// path and doesn't leak username validity via timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	}
	if err == nil && ok && match {
		token, terr := newToken()
		if terr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		expires := time.Now().UTC().Add(30 * 24 * time.Hour)
		if serr := s.store.CreateSession(r.Context(), token, u.ID, expires.Format(time.RFC3339)); serr != nil {
			log.Printf("create session: %v", serr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: "netis_session", Value: token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires,
			Secure: secureRequest(r),
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.limiter.fail(ip)
	w.WriteHeader(http.StatusUnauthorized)
	views.LoginPage("wrong username or password").Render(r.Context(), w)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("netis_session"); err == nil {
		s.store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "netis_session", Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureRequest(r),
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(r.Context()); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	views.SetupPage("").Render(r.Context(), w)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(r.Context()); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	if username == "" || len(password) < 8 {
		w.WriteHeader(http.StatusBadRequest)
		views.SetupPage("username required, password min 8 chars").Render(r.Context(), w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	created, err := s.store.CreateFirstAdmin(r.Context(), username, string(hash))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !created {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
