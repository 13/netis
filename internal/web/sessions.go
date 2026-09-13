package web

import (
	"net/http"

	"netis/internal/store"
)

// handleSessionRevoke revokes one of the signed-in user's own sessions. It is
// not admin-gated and is scoped to the caller's own user id: the id in the path
// is a digest of a session token, and one belonging to somebody else does not
// match anything this user owns.
func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deleted, err := s.store.DeleteSessionByID(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !deleted {
		http.NotFound(w, r)
		return
	}
	// Revoking the session in use is a deliberate option — "this browser is the
	// one I want gone" — so follow it with the logout the user is now in.
	if c, cerr := r.Cookie("netis_session"); cerr == nil && store.SessionID(c.Value) == r.PathValue("id") {
		clearSessionCookie(w, s.secureRequest(r))
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?tab=users", http.StatusSeeOther)
}

// handleSessionRevokeOthers signs the user out everywhere except here, the
// thing you want after losing a laptop, without changing the password.
func (s *Server) handleSessionRevokeOthers(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	keep := ""
	if c, err := r.Cookie("netis_session"); err == nil {
		keep = c.Value
	}
	if err := s.store.DeleteSessionsForUser(r.Context(), u.ID, keep); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings?tab=users", http.StatusSeeOther)
}
