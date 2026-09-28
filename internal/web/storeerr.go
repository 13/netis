package web

import (
	"net/http"

	"netis/internal/store"
)

// failWrite answers an error from a store write. A unique-constraint failure
// is the user asking for something that already exists and gets a 409 with
// conflictMsg; a foreign-key failure is a form naming a record that does not
// exist and gets a 400 with missingMsg (when the caller has one; a write that
// references nothing the user chose passes ""). Anything else is a server
// failure.
func (s *Server) failWrite(w http.ResponseWriter, r *http.Request, err error, conflictMsg, missingMsg string) {
	switch {
	case store.IsUniqueViolation(err):
		http.Error(w, conflictMsg, http.StatusConflict)
	case missingMsg != "" && store.IsForeignKeyViolation(err):
		http.Error(w, missingMsg, http.StatusBadRequest)
	default:
		s.fail(w, r, err)
	}
}
