package web

import (
	"net/http"

	"netis/internal/store"
)

// writeFailure classifies an error from a store write. A unique-constraint
// failure is the user asking for something that already exists: 409 with
// conflictMsg. A foreign-key failure is a form naming a record that does not
// exist: 400 with missingMsg (when the caller has one; a write that references
// nothing the user chose passes ""). ok is false for anything else, which is a
// server failure.
func writeFailure(err error, conflictMsg, missingMsg string) (status int, msg string, ok bool) {
	switch {
	case store.IsUniqueViolation(err):
		return http.StatusConflict, conflictMsg, true
	case missingMsg != "" && store.IsForeignKeyViolation(err):
		return http.StatusBadRequest, missingMsg, true
	}
	return 0, "", false
}
