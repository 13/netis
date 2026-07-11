package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	rl.attempts[ip] = kept
	return len(kept) < 5
}

func (rl *rateLimiter) fail(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/login" || r.URL.Path == "/setup" ||
			strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie("netis_session"); err == nil {
			if u, ok, _ := s.store.GetSession(c.Value); ok {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
				return
			}
		}
		if n, _ := s.store.CountUsers(); n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
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
	u, ok, err := s.store.GetUserByName(username)
	if err == nil && ok &&
		bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil {
		token := newToken()
		expires := time.Now().UTC().Add(30 * 24 * time.Hour)
		s.store.CreateSession(token, u.ID, expires.Format(time.RFC3339))
		http.SetCookie(w, &http.Cookie{
			Name: "netis_session", Value: token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: expires,
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
		s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "netis_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	views.SetupPage("").Render(r.Context(), w)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.store.CountUsers(); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	if username == "" || len(password) < 6 {
		w.WriteHeader(http.StatusBadRequest)
		views.SetupPage("username required, password min 6 chars").Render(r.Context(), w)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateUser(username, string(hash), "admin"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
