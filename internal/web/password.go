package web

import (
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// minPasswordLen is the shortest password netis accepts, everywhere a password
// is set: initial setup, adding a user, changing your own, and an admin reset.
// Setup asked for 8 and the add-user form asked for 6, which meant the weakest
// account on the system was whichever one an admin created later.
const minPasswordLen = 8

// handlePasswordChange changes the signed-in user's own password. It is not
// admin-gated: a viewer must be able to rotate their own credential without
// asking an admin to delete and recreate the account, which was the only way
// to change a password at all.
func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// The current password is required even though the session already proves
	// who this is: it stops a stolen or borrowed session from locking the real
	// owner out of their own account.
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.FormValue("current_password"))) != nil {
		http.Error(w, "current password is wrong", http.StatusForbidden)
		return
	}
	next, ok := s.validNewPassword(w, r)
	if !ok {
		return
	}
	if !s.setPassword(w, r, u.ID, next) {
		return
	}
	http.Redirect(w, r, "/settings?tab=users", http.StatusSeeOther)
}

// handleUserPasswordReset sets another user's password without knowing the old
// one, for the admin whose job it is to hand out a new one.
func (s *Server) handleUserPasswordReset(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, found, err := s.store.GetUser(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	} else if !found {
		http.NotFound(w, r)
		return
	}
	next, ok := s.validNewPassword(w, r)
	if !ok {
		return
	}
	if !s.setPassword(w, r, id, next) {
		return
	}
	http.Redirect(w, r, "/settings?tab=users", http.StatusSeeOther)
}

// validNewPassword reads and checks the new-password fields, writing a 400 and
// returning ok=false when they don't hold up.
func (s *Server) validNewPassword(w http.ResponseWriter, r *http.Request) (string, bool) {
	next := r.FormValue("new_password")
	if confirm := r.FormValue("confirm_password"); confirm != "" && confirm != next {
		http.Error(w, "new passwords do not match", http.StatusBadRequest)
		return "", false
	}
	if len(next) < minPasswordLen {
		http.Error(w, "password must be at least "+strconv.Itoa(minPasswordLen)+" characters",
			http.StatusBadRequest)
		return "", false
	}
	return next, true
}

// setPassword hashes and stores a new password for userID and revokes that
// user's other sessions, reporting whether it got that far.
//
// Revoking is the point of changing a password: a session that outlived the
// credential it was created with would leave a leaked password still usable
// for up to 30 days. The session making the request is kept, so a user
// changing their own password is not logged out of the browser they're in;
// when an admin resets someone else's, that user has no session to keep and
// all of theirs go.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request, userID int64, password string) bool {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.fail(w, r, err)
		return false
	}
	updated, err := s.store.SetPasswordHash(r.Context(), userID, string(hash))
	if err != nil {
		s.fail(w, r, err)
		return false
	}
	if !updated {
		http.NotFound(w, r)
		return false
	}
	keep := ""
	if c, err := r.Cookie("netis_session"); err == nil {
		keep = c.Value
	}
	if err := s.store.DeleteSessionsForUser(r.Context(), userID, keep); err != nil {
		s.fail(w, r, err)
		return false
	}
	return true
}
