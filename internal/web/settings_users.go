package web

import (
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	role := r.FormValue("role")
	note := auditNote(r)
	note.Target, note.Detail = "user "+username, "role "+role
	if username == "" || len(password) < minPasswordLen {
		s.settingsError(w, r, "users", http.StatusBadRequest, "enter a username and a password of at least "+strconv.Itoa(minPasswordLen)+" characters")
		return
	}
	if len(password) > maxPasswordLen {
		s.settingsError(w, r, "users", http.StatusBadRequest, passwordTooLongMsg)
		return
	}
	if role != "admin" && role != "viewer" {
		s.settingsError(w, r, "users", http.StatusBadRequest, "choose a role: Admin or Viewer")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.CreateUser(r.Context(), username, string(hash), role); err != nil {
		s.settingsWriteError(w, r, "users", err, "a user named "+username+" already exists")
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusSeeOther)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u, found, err := s.store.GetUser(r.Context(), id); err == nil && found {
		auditNote(r).Target = "user " + u.Username
	}
	// Deleting yourself would sign you out mid-click and, for the last
	// admin with SSO off, lock everyone out; another admin has to do it.
	if me, ok := userFrom(r); ok && me.ID == id {
		s.settingsError(w, r, "users", http.StatusBadRequest, "you cannot delete your own account; sign in as another admin to delete it")
		return
	}
	// DeleteUserGuarded performs the existence check, admin count, and
	// delete atomically in a single SQL statement so two concurrent
	// deletes of two different admins can't both succeed and leave zero
	// admins (a TOCTOU race a separate ListUsers-then-DeleteUser sequence
	// would be vulnerable to).
	deleted, err := s.store.DeleteUserGuarded(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !deleted {
		// Distinguish "no such user" (404) from "refused: last admin"
		// (400) for a useful error response.
		users, err := s.store.ListUsers(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		exists := false
		for _, u := range users {
			if u.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
		s.settingsError(w, r, "users", http.StatusBadRequest, "this is the only admin; make another user an admin first, then delete this one")
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusSeeOther)
}

// handleUserRole promotes a viewer or demotes an admin. Sessions are left
// alone: the role is read from the database on every request, so the change
// applies to the user's very next one.
func (s *Server) handleUserRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u, found, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	role := r.FormValue("role")
	note := auditNote(r)
	note.Target, note.Detail = "user "+u.Username, u.Role+" -> "+role
	if role != "admin" && role != "viewer" {
		note.Detail = "choose a role: Admin or Viewer"
		s.settingsError(w, r, "users", http.StatusBadRequest, "choose a role: Admin or Viewer")
		return
	}
	// Guarded in one statement, like delete: two admins demoting each other
	// at once cannot leave nobody able to administer netis.
	changed, err := s.store.SetUserRoleGuarded(r.Context(), id, role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !changed {
		s.settingsError(w, r, "users", http.StatusBadRequest, "this is the only admin; make another user an admin first, then change this role")
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusSeeOther)
}
