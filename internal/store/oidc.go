package store

import (
	"context"
	"database/sql"
	"errors"
)

// GetUserByOIDC finds the user linked to an identity at an OpenID Connect
// provider, by issuer and subject.
func (s *Store) GetUserByOIDC(ctx context.Context, issuer, subject string) (User, bool, error) {
	var u User
	err := s.queryRow(ctx, `SELECT id,username,password_hash,role FROM "user"
		WHERE oidc_issuer=? AND oidc_subject=?`, issuer, subject).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

// CreateOIDCUser inserts a user already linked to a provider identity. A clash
// on the username or the identity is a unique violation (IsUniqueViolation).
func (s *Store) CreateOIDCUser(ctx context.Context, username, passwordHash, role, issuer, subject string) (int64, error) {
	return s.insertReturningID(ctx, `INSERT INTO "user" (username,password_hash,role,oidc_issuer,oidc_subject)
		VALUES (?,?,?,?,?)`, username, passwordHash, role, issuer, subject)
}

// LinkOIDC attaches a provider identity to an existing user, reporting whether
// the user still exists. An identity already linked to someone else is a
// unique violation.
func (s *Store) LinkOIDC(ctx context.Context, userID int64, issuer, subject string) (bool, error) {
	res, err := s.exec(ctx, `UPDATE "user" SET oidc_issuer=?, oidc_subject=? WHERE id=?`, issuer, subject, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// OIDCLinked reports whether a user is linked to a provider identity.
func (s *Store) OIDCLinked(ctx context.Context, userID int64) (bool, error) {
	var n int
	err := s.queryRow(ctx, `SELECT count(*) FROM "user" WHERE id=? AND oidc_subject IS NOT NULL`, userID).Scan(&n)
	return n > 0, err
}

// SetSSORole sets the role an SSO login derived from the provider's groups.
func (s *Store) SetSSORole(ctx context.Context, userID int64, role string) error {
	_, err := s.exec(ctx, `UPDATE "user" SET role=? WHERE id=?`, role, userID)
	return err
}
