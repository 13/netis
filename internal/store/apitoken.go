package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// APIToken is one row of a token list. Like Session, it never carries the
// token itself: the store only has its digest, and the plaintext is shown
// once, by whoever created it.
type APIToken struct {
	ID         int64
	UserID     int64
	Username   string
	Name       string
	CreatedAt  string
	LastUsedAt *string
	ExpiresAt  *string
	// ReadOnly tokens may only read: GET and HEAD.
	ReadOnly bool
}

// TokenUser is the owner of a presented API token, and whether the token is
// read-only.
type TokenUser struct {
	User
	ReadOnly bool
}

// tokenScope is how a token's read-only flag is stored.
func tokenScope(readOnly bool) string {
	if readOnly {
		return "read"
	}
	return "full"
}

// apiTokenTouchInterval is how stale last_used_at may get before a lookup
// writes it again. A script polling every second should not turn each of its
// reads into a write.
const apiTokenTouchInterval = time.Minute

// CreateAPIToken stores a new token for a user under its digest. expiresAt is
// an RFC3339 timestamp, or "" for a token that does not expire; a readOnly
// token may only read.
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name, token, expiresAt string, readOnly bool, now time.Time) (int64, error) {
	return s.insertReturningID(ctx, `INSERT INTO api_token (user_id,name,token_hash,created_at,expires_at,scope)
		VALUES (?,?,?,?,?,?)`,
		userID, name, hashToken(token), now.UTC().Format(time.RFC3339), nullable(expiresAt), tokenScope(readOnly))
}

// GetUserByAPIToken resolves a presented token to its owner and scope,
// reporting false for an unknown or expired one. It records the use, at most
// once per apiTokenTouchInterval per token.
func (s *Store) GetUserByAPIToken(ctx context.Context, token string, now time.Time) (TokenUser, bool, error) {
	var u TokenUser
	var id int64
	var scope string
	nowS := now.UTC().Format(time.RFC3339)
	err := s.queryRow(ctx, `SELECT t.id,t.scope,u.id,u.username,u.password_hash,u.role FROM api_token t
		JOIN "user" u ON u.id=t.user_id
		WHERE t.token_hash=? AND (t.expires_at IS NULL OR t.expires_at>?)`, hashToken(token), nowS).
		Scan(&id, &scope, &u.ID, &u.Username, &u.PasswordHash, &u.Role)
	u.ReadOnly = scope == "read"
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, false, nil
		}
		return u, false, err
	}
	stale := now.Add(-apiTokenTouchInterval).UTC().Format(time.RFC3339)
	if _, err := s.exec(ctx, `UPDATE api_token SET last_used_at=?
		WHERE id=? AND (last_used_at IS NULL OR last_used_at<=?)`, nowS, id, stale); err != nil {
		return u, false, err
	}
	return u, true, nil
}

// ListAPITokens returns tokens newest first: one user's when userID is
// non-zero, everybody's (for an admin) when it is zero.
func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	q := `SELECT t.id,t.user_id,u.username,t.name,t.created_at,t.last_used_at,t.expires_at,t.scope
		FROM api_token t JOIN "user" u ON u.id=t.user_id`
	var args []any
	if userID != 0 {
		q += ` WHERE t.user_id=?`
		args = append(args, userID)
	}
	q += ` ORDER BY t.created_at DESC, t.id DESC`
	var out []APIToken
	err := s.eachRow(ctx, q, func(rows *sql.Rows) error {
		var t APIToken
		var scope string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Username, &t.Name, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &scope); err != nil {
			return err
		}
		t.ReadOnly = scope == "read"
		out = append(out, t)
		return nil
	}, args...)
	return out, err
}

// DeleteAPIToken revokes a token by id, reporting whether it found one. A
// non-zero userID restricts the delete to that user's tokens, so an id
// belonging to somebody else matches nothing; zero is the admin's any-token
// revoke.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) (bool, error) {
	q := `DELETE FROM api_token WHERE id=?`
	args := []any{id}
	if userID != 0 {
		q += ` AND user_id=?`
		args = append(args, userID)
	}
	res, err := s.exec(ctx, q, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// PruneExpiredAPITokens deletes tokens whose expiry has passed. They are
// already refused at lookup; like expired sessions, there is no reason to keep
// a credential at rest that can no longer be used.
func (s *Store) PruneExpiredAPITokens(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM api_token WHERE expires_at IS NOT NULL AND expires_at <= ?`,
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
