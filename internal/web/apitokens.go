package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netis/internal/web/views"
)

// apiTokenPrefix marks a netis API token, so a leaked one is recognisable to
// secret scanners and to whoever finds it in a shell history.
const apiTokenPrefix = "netis_"

// Failed bearer lookups per client address. Generous for a script with a
// typo, useless for guessing a 256-bit value — the point is that a client
// hammering the API with junk tokens cannot make each attempt a database
// query for free.
const (
	apiTokenFailMax    = 20
	apiTokenFailWindow = time.Minute
)

// maxTokenNameLen caps a token's label; it goes into a page.
const maxTokenNameLen = 100

// newAPIToken returns a fresh token: the prefix and 32 random bytes,
// base64url without padding.
func newAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return apiTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// apiBearer returns the bearer token on an /api/ request, if it carries one.
// Only /api/ takes personal tokens; /metrics has its own scrape token.
func apiBearer(r *http.Request) (string, bool) {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return "", false
	}
	tok := tokenFromRequest(r)
	return tok, tok != ""
}

// serveBearer authenticates an /api/ request by its bearer token alone. There
// is deliberately no fallback to the session cookie: a request that names a
// token is answered as that token, so a bad one is a 401 however signed-in the
// browser around it happens to be. Failed lookups count against the caller's
// address; a valid token is never charged.
func (s *Server) serveBearer(w http.ResponseWriter, r *http.Request, next http.Handler, token string) {
	key := "bearer:" + limitKey(s.clientIP(r))
	if !s.tokenLimiter.reserve(key) {
		s.apiError(w, r, http.StatusTooManyRequests, "too many failed token attempts, wait a minute")
		return
	}
	u, ok, err := s.store.GetUserByAPIToken(r.Context(), token, time.Now())
	s.tokenLimiter.done(key, err == nil && !ok)
	if err != nil {
		s.unavailable(w, r, err)
		return
	}
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="netis"`)
		s.apiError(w, r, http.StatusUnauthorized, "invalid or expired API token")
		return
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
}

// handleTokenCreate issues an API token to the signed-in user. It is
// self-service, like changing your own password: the token can do no more than
// its owner. The plaintext is rendered once, on the page this answers with,
// and is not recoverable afterwards.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.settingsError(w, r, "tokens", http.StatusBadRequest, "give the token a name")
		return
	}
	if len(name) > maxTokenNameLen {
		s.settingsError(w, r, "tokens", http.StatusBadRequest, "token name is too long")
		return
	}
	now := time.Now().UTC()
	expires := ""
	if v := strings.TrimSpace(r.FormValue("expires_days")); v != "" {
		days, err := strconv.Atoi(v)
		if err != nil || days < 0 || days > 3650 {
			s.settingsError(w, r, "tokens", http.StatusBadRequest, "expiry must be 0 (never) to 3650 days")
			return
		}
		if days > 0 {
			expires = now.AddDate(0, 0, days).Format(time.RFC3339)
		}
	}
	token, err := newAPIToken()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.CreateAPIToken(r.Context(), u.ID, name, token, expires, now); err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.settingsData(r, "tokens")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d.NewToken = token
	// The page holds a live credential; keep it out of every cache.
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, views.SettingsPage(u.Username, d))
}

// handleTokenRevoke deletes an API token. Anyone may revoke their own; an
// admin may revoke anyone's. For everybody else the delete is scoped to their
// own user id, so another user's token id is simply not found.
func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	owner := u.ID
	if u.Role == "admin" {
		owner = 0
	}
	deleted, err := s.store.DeleteAPIToken(r.Context(), owner, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !deleted {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/settings?tab=tokens", http.StatusSeeOther)
}
