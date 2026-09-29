package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	"netis/internal/config"
	"netis/internal/store"
	"netis/internal/web/views"
)

// OpenID Connect login: the authorization code flow with PKCE against the
// provider named by NETIS_OIDC_ISSUER. The flow's state, nonce and PKCE
// verifier ride in a short-lived signed cookie between the redirect out and
// the callback, so nothing about a login in flight is kept server side.

const (
	oidcFlowCookie = "netis_oidc"
	oidcFlowPath   = "/auth/oidc/"
	// oidcFlowTTL is how long a user has at the provider before the flow
	// cookie expires and the callback is refused.
	oidcFlowTTL = 10 * time.Minute
	// oidcHTTPTimeout bounds each call netis makes to the provider:
	// discovery, keys and the code exchange.
	oidcHTTPTimeout = 10 * time.Second
	// maxSSOUsernameLen caps a username taken from the provider's claims.
	maxSSOUsernameLen = 128
)

// oidcAuth holds the SSO configuration and the provider, discovered lazily.
type oidcAuth struct {
	cfg config.OIDC
	// key signs the flow cookie. It is made at startup and never stored: a
	// restart only invalidates logins that were mid-flight.
	key    []byte
	client *http.Client

	mu       sync.Mutex
	provider *oidc.Provider // nil until discovery has succeeded
}

func newOIDCAuth(cfg config.OIDC) *oidcAuth {
	if !cfg.Enabled() {
		return nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return &oidcAuth{cfg: cfg, key: key, client: &http.Client{Timeout: oidcHTTPTimeout}}
}

// oidcPublicPath reports whether path is part of the SSO flow a signed-out
// browser has to reach.
func oidcPublicPath(path string) bool {
	return path == "/auth/oidc/login" || path == config.OIDCCallbackPath
}

// ctx carries the provider HTTP client, which go-oidc and oauth2 both look for.
func (a *oidcAuth) ctx(parent context.Context) context.Context {
	return oidc.ClientContext(parent, a.client)
}

// discover returns the provider, fetching its discovery document the first
// time. A failure is not cached, so a provider that was down when netis
// started is picked up on the next login attempt instead of never.
func (a *oidcAuth) discover(ctx context.Context) (*oidc.Provider, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return a.provider, nil
	}
	// The provider keeps the context's HTTP client for fetching keys later,
	// so it must not be the request's context, which ends with the request.
	p, err := oidc.NewProvider(a.ctx(context.WithoutCancel(ctx)), a.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	a.provider = p
	return p, nil
}

func (a *oidcAuth) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.cfg.ClientID,
		ClientSecret: a.cfg.ClientSecret,
		RedirectURL:  a.cfg.RedirectURL,
		Endpoint:     p.Endpoint(),
		// "groups" is not a standard scope, but Authelia and Pocket ID only
		// put the claim in the token when it is asked for, and providers that
		// do not know it ignore it.
		Scopes: []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}
}

// oidcFlow is what the flow cookie carries from the redirect to the callback.
type oidcFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	// Link is the signed-in user asking to link their account, or 0 for a
	// login. Signing the cookie is what stops a client from pointing it at
	// someone else's account.
	Link    int64 `json:"l,omitempty"`
	Expires int64 `json:"e"`
}

func (a *oidcAuth) sign(payload []byte) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write(payload)
	return m.Sum(nil)
}

func (a *oidcAuth) encodeFlow(f oidcFlow) (string, error) {
	payload, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(a.sign(payload)), nil
}

// decodeFlow returns the flow in the cookie value, if it is signed with this
// process's key and has not expired.
func (a *oidcAuth) decodeFlow(v string) (oidcFlow, bool) {
	var f oidcFlow
	p, sig, ok := strings.Cut(v, ".")
	if !ok {
		return f, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(p)
	if err != nil {
		return f, false
	}
	mac, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(mac, a.sign(payload)) {
		return f, false
	}
	if json.Unmarshal(payload, &f) != nil || time.Now().Unix() > f.Expires {
		return f, false
	}
	return f, true
}

func (s *Server) setFlowCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	// SameSite=Lax, not Strict: the provider's redirect back is a top-level
	// cross-site navigation, which Lax lets the cookie ride along on.
	http.SetCookie(w, &http.Cookie{
		Name: oidcFlowCookie, Value: value, Path: oidcFlowPath, MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureRequest(r),
	})
}

// startOIDC sends the browser to the provider, remembering the flow in the
// signed cookie. link is the user linking their account, or 0 for a login.
func (s *Server) startOIDC(w http.ResponseWriter, r *http.Request, link int64) {
	a := s.oidc
	p, err := a.discover(r.Context())
	if err != nil {
		slog.Error("oidc discovery failed", "issuer", a.cfg.Issuer, "err", err)
		s.loginError(w, r, http.StatusBadGateway, "the sign-in provider is unreachable; try again later")
		return
	}
	state, err1 := newToken()
	nonce, err2 := newToken()
	if err := errors.Join(err1, err2); err != nil {
		s.fail(w, r, err)
		return
	}
	f := oidcFlow{State: state, Nonce: nonce, Verifier: oauth2.GenerateVerifier(), Link: link,
		Expires: time.Now().Add(oidcFlowTTL).Unix()}
	v, err := a.encodeFlow(f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setFlowCookie(w, r, v, int(oidcFlowTTL/time.Second))
	http.Redirect(w, r, a.oauth(p).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(f.Verifier)),
		http.StatusSeeOther)
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	// The first admin is always a local account made at setup: it is the way
	// back in when the provider is down.
	if n, err := s.store.CountUsers(r.Context()); err != nil {
		s.unavailable(w, r, err)
		return
	} else if n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.startOIDC(w, r, 0)
}

// handleOIDCLink starts the flow for the signed-in user to attach an SSO
// identity to their own account. They prove both identities, so nothing has
// to be matched by name.
func (s *Server) handleOIDCLink(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	s.startOIDC(w, r, u.ID)
}

// oidcClaims are the ID token claims netis reads besides sub and nonce.
type oidcClaims struct {
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	// EmailVerified gates the email as a username: an address the provider
	// has not verified is whatever the user typed at sign-up.
	EmailVerified claimBool `json:"email_verified"`
}

// claimBool reads a boolean claim that some providers send as the string
// "true" instead of a JSON boolean. Anything else is false.
type claimBool bool

func (b *claimBool) UnmarshalJSON(data []byte) error {
	*b = claimBool(string(data) == "true" || string(data) == `"true"`)
	return nil
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	a := s.oidc
	if a == nil {
		http.NotFound(w, r)
		return
	}
	// The callback is a GET, which the audit middleware leaves alone, but a
	// sign-in is what the audit log is for; it is recorded here instead, as
	// a password login would be.
	sw := &statusRecorder{ResponseWriter: w}
	w = sw
	note := &auditRecord{}
	r = r.WithContext(context.WithValue(r.Context(), auditKey{}, note))
	action := "sso.login"
	defer func() { s.recordAudit(r, action, sw.status, note) }()

	// Failed callbacks count against the same per-address budget as failed
	// passwords, so the endpoint is no cheaper to hammer than /login.
	ipKey := limitKey(s.clientIP(r))
	if !s.limiter.reserve(ipKey) {
		note.Detail = "rate limited"
		http.Error(w, "too many attempts, wait a minute", http.StatusTooManyRequests)
		return
	}
	failed := true
	defer func() { s.limiter.done(ipKey, failed) }()

	// The flow cookie is single use whatever happens next.
	s.setFlowCookie(w, r, "", -1)
	var f oidcFlow
	c, err := r.Cookie(oidcFlowCookie)
	if err == nil {
		f, err = oidcFlowOrErr(a.decodeFlow(c.Value))
	}
	q := r.URL.Query()
	if err != nil || q.Get("state") == "" ||
		subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.State)) != 1 {
		slog.Warn("oidc callback with missing or mismatched state", "ip", s.clientIP(r))
		note.Detail = "bad state"
		s.loginError(w, r, http.StatusBadRequest, "sign-in expired or was started in another browser; try again")
		return
	}
	if f.Link != 0 {
		action = "sso.link"
	}
	if e := q.Get("error"); e != "" {
		note.Detail = "refused by provider"
		slog.Warn("oidc provider refused sign-in", "error", truncate(e, 100),
			"description", truncate(q.Get("error_description"), 200))
		s.loginError(w, r, http.StatusUnauthorized, "the sign-in provider refused the sign-in")
		return
	}

	p, err := a.discover(r.Context())
	if err != nil {
		slog.Error("oidc discovery failed", "issuer", a.cfg.Issuer, "err", err)
		note.Detail = "provider unreachable"
		s.loginError(w, r, http.StatusBadGateway, "the sign-in provider is unreachable; try again later")
		return
	}
	ctx := a.ctx(r.Context())
	tok, err := a.oauth(p).Exchange(ctx, q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		slog.Warn("oidc code exchange failed", "err", err)
		note.Detail = "code exchange failed"
		s.loginError(w, r, http.StatusUnauthorized, "sign-in failed; try again, or sign in with your password")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	// Verify checks the signature against the provider's keys, the issuer,
	// that the audience is this client, and expiry.
	idt, err := p.Verifier(&oidc.Config{ClientID: a.cfg.ClientID}).Verify(ctx, raw)
	if err != nil {
		slog.Warn("oidc id token rejected", "err", err)
		note.Detail = "id token rejected"
		s.loginError(w, r, http.StatusUnauthorized, "sign-in failed; try again, or sign in with your password")
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(f.Nonce)) != 1 {
		slog.Warn("oidc id token nonce mismatch", "sub", idt.Subject)
		note.Detail = "nonce mismatch"
		s.loginError(w, r, http.StatusUnauthorized, "sign-in failed; try again, or sign in with your password")
		return
	}
	var claims oidcClaims
	var all map[string]any
	if err := errors.Join(idt.Claims(&claims), idt.Claims(&all)); err != nil {
		slog.Warn("oidc id token claims unreadable", "err", err)
		note.Detail = "claims unreadable"
		s.loginError(w, r, http.StatusUnauthorized, "sign-in failed; try again, or sign in with your password")
		return
	}
	groups := claimStrings(all[a.cfg.GroupsClaim])

	if f.Link != 0 {
		if s.linkOIDC(w, r, f.Link, idt.Issuer, idt.Subject) {
			failed = false
		}
		return
	}

	u, out, err := s.oidcUser(r.Context(), idt.Issuer, idt.Subject, claims, groups)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !out.allowed {
		slog.Warn("oidc login refused", "sub", idt.Subject, "reason", out.reason)
		note.Detail = out.reason
		s.loginError(w, r, http.StatusForbidden, out.message)
		return
	}
	note.UserID, note.Username = &u.ID, u.Username
	if err := s.startSession(w, r, u.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	failed = false
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func oidcFlowOrErr(f oidcFlow, ok bool) (oidcFlow, error) {
	if !ok {
		return f, errors.New("invalid or expired flow cookie")
	}
	return f, nil
}

// oidcOutcome says whether an SSO login may proceed, and if not, why: reason
// for the log, message for the person signing in.
type oidcOutcome struct {
	allowed         bool
	reason, message string
}

// oidcUser maps a verified identity to the local user it signs in as,
// creating one when that is allowed, and applies the role the provider's
// groups give it.
//
// Only the issuer and subject pair is matched. An unknown identity is never
// attached to an existing account by username: users can often choose their
// own preferred_username at the provider, and matching on it would let one
// call themselves "admin" and walk into that account.
func (s *Server) oidcUser(ctx context.Context, issuer, subject string, c oidcClaims, groups []string) (store.User, oidcOutcome, error) {
	cfg := s.oidc.cfg
	role, managed := "viewer", cfg.AdminGroup != ""
	if managed && slices.Contains(groups, cfg.AdminGroup) {
		role = "admin"
	}
	u, found, err := s.store.GetUserByOIDC(ctx, issuer, subject)
	if err != nil {
		return u, oidcOutcome{}, err
	}
	if found {
		// The role follows the group on every login, so taking someone out
		// of the admin group demotes them the next time they sign in.
		if managed && u.Role != role {
			if err := s.store.SetSSORole(ctx, u.ID, role); err != nil {
				return u, oidcOutcome{}, err
			}
			slog.Info("sso role updated from groups", "user", u.Username, "role", role)
			u.Role = role
		}
		return u, oidcOutcome{allowed: true}, nil
	}
	if !cfg.AutoCreate {
		return u, oidcOutcome{reason: "unknown identity, auto-create off",
			message: "no netis account is linked to this SSO account; sign in with your password and link it under Settings, Profile, or ask an admin"}, nil
	}
	name := ssoUsername(c, subject)
	// SSO accounts get a password nobody knows, so password login fails for
	// them exactly as a wrong password does, timing included, until an admin
	// sets a real one.
	secret, err := newToken()
	if err != nil {
		return u, oidcOutcome{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return u, oidcOutcome{}, err
	}
	id, err := s.store.CreateOIDCUser(ctx, name, string(hash), role, issuer, subject)
	if store.IsUniqueViolation(err) {
		return u, oidcOutcome{reason: "username taken by a local account: " + name,
			message: "a netis account named " + name + " already exists; sign in to it with your password and link SSO under Settings, Profile"}, nil
	}
	if err != nil {
		return u, oidcOutcome{}, err
	}
	slog.Info("sso user created", "user", name, "role", role)
	return store.User{ID: id, Username: name, PasswordHash: string(hash), Role: role}, oidcOutcome{allowed: true}, nil
}

// linkOIDC attaches the identity to the user who started the link, provided
// this browser is still signed in as them. It reports success.
func (s *Server) linkOIDC(w http.ResponseWriter, r *http.Request, userID int64, issuer, subject string) bool {
	var u store.User
	ok := false
	if c, err := r.Cookie("netis_session"); err == nil {
		var err error
		if u, ok, err = s.store.GetSession(r.Context(), c.Value); err != nil {
			s.unavailable(w, r, err)
			return false
		}
	}
	note := auditNote(r)
	if ok {
		note.UserID, note.Username = &u.ID, u.Username
	}
	if !ok || u.ID != userID {
		note.Detail = "not signed in as the linking user"
		s.loginError(w, r, http.StatusForbidden, "sign in again to link your SSO account")
		return false
	}
	r = r.WithContext(context.WithValue(r.Context(), userKey, u))
	linked, err := s.store.LinkOIDC(r.Context(), u.ID, issuer, subject)
	if store.IsUniqueViolation(err) {
		note.Detail = "identity linked to another user"
		s.settingsError(w, r, "account", http.StatusConflict, "that SSO account is already linked to another netis user")
		return false
	}
	if err != nil {
		s.fail(w, r, err)
		return false
	}
	if !linked {
		note.Detail = "user gone"
		s.loginError(w, r, http.StatusForbidden, "sign in again to link your SSO account")
		return false
	}
	slog.Info("sso identity linked", "user", u.Username)
	http.Redirect(w, r, "/settings/account", http.StatusSeeOther)
	return true
}

// recordAudit writes the audit entry for an SSO callback, which as a GET is
// not recorded by the audit middleware.
func (s *Server) recordAudit(r *http.Request, action string, status int, note *auditRecord) {
	if status == 0 {
		status = http.StatusOK
	}
	e := store.AuditEntry{
		At: time.Now(), Action: action, Detail: note.Detail, IP: s.clientIP(r), Status: status,
		UserID: note.UserID, Username: note.Username,
	}
	if err := s.store.AddAudit(context.WithoutCancel(r.Context()), e); err != nil {
		slog.Error("audit: record entry", "action", e.Action, "err", err)
	}
}

// ssoUsername picks the name for a user created from an SSO login:
// preferred_username, then a verified email, then the subject. An unverified
// email is skipped: anyone can sign up at the provider with an address that is
// not theirs, and would otherwise squat the username it maps to.
func ssoUsername(c oidcClaims, subject string) string {
	name := strings.TrimSpace(c.PreferredUsername)
	if name == "" && c.EmailVerified {
		name = strings.TrimSpace(c.Email)
	}
	if name == "" {
		name = subject
	}
	return strings.ToValidUTF8(truncate(name, maxSSOUsernameLen), "")
}

// claimStrings reads a groups claim, which providers send as an array of
// strings or, with one group, sometimes as a bare string.
func claimStrings(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, g := range v {
			if s, ok := g.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// loginOptions says which ways in the login page offers for r.
func (s *Server) loginOptions(r *http.Request) views.LoginOptions {
	if s.oidc == nil {
		return views.LoginOptions{}
	}
	return views.LoginOptions{
		SSO:          true,
		HidePassword: s.oidc.cfg.DisablePassword && r.URL.Query().Get("local") == "",
	}
}

// loginError answers a failed SSO attempt with the login page and msg.
func (s *Server) loginError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, views.LoginPage(msg, s.loginOptions(r)))
}

// passwordLoginAllowed reports whether u may sign in with a password. With
// NETIS_OIDC_DISABLE_PASSWORD only local admins, accounts with no SSO link,
// may: the break-glass for a provider that is down, and nothing more.
func (s *Server) passwordLoginAllowed(ctx context.Context, u store.User) bool {
	if s.oidc == nil || !s.oidc.cfg.DisablePassword {
		return true
	}
	if u.Role != "admin" {
		return false
	}
	linked, err := s.store.OIDCLinked(ctx, u.ID)
	if err != nil {
		slog.Error("checking sso link for password login", "err", err)
		return false
	}
	return !linked
}
