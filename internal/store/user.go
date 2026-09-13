package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	return s.insertReturningID(ctx, `INSERT INTO "user" (username,password_hash,role) VALUES (?,?,?)`,
		username, passwordHash, role)
}

func (s *Store) GetUserByName(ctx context.Context, username string) (User, bool, error) {
	var u User
	err := s.queryRow(ctx, `SELECT id,username,password_hash,role FROM "user" WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

// CreateFirstAdmin inserts an admin user only if the user table is
// currently empty, atomically. It reports whether the row was created.
func (s *Store) CreateFirstAdmin(ctx context.Context, username, passwordHash string) (bool, error) {
	res, err := s.exec(ctx, `INSERT INTO "user" (username,password_hash,role)
		SELECT ?,?,'admin' WHERE NOT EXISTS (SELECT 1 FROM "user")`,
		username, passwordHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// GetUser looks a user up by id, reporting whether one exists.
func (s *Store) GetUser(ctx context.Context, id int64) (User, bool, error) {
	var u User
	err := s.queryRow(ctx, `SELECT id,username,password_hash,role FROM "user" WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

// SetPasswordHash replaces a user's password hash, reporting whether a row was
// updated so a reset aimed at a user who has since been deleted is not
// reported back as a success.
func (s *Store) SetPasswordHash(ctx context.Context, id int64, passwordHash string) (bool, error) {
	res, err := s.exec(ctx, `UPDATE "user" SET password_hash=? WHERE id=?`, passwordHash, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.queryRow(ctx, `SELECT count(*) FROM "user"`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.query(ctx, `SELECT id,username,password_hash,role FROM "user" ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM "user" WHERE id=?`, id)
	return err
}

// DeleteUserGuarded deletes the user with the given id, refusing to do so if
// it would remove the last remaining admin. The existence check, admin
// count, and delete all happen within a single SQL statement so the guard
// is atomic even under concurrent calls (unlike a separate
// ListUsers-then-DeleteUser sequence, which is vulnerable to a TOCTOU race
// where two concurrent admin deletions both pass the "admins > 1" check).
// It reports whether a row was actually deleted.
func (s *Store) DeleteUserGuarded(ctx context.Context, id int64) (deleted bool, err error) {
	res, err := s.exec(ctx, `DELETE FROM "user" WHERE id=? AND
		(role<>'admin' OR (SELECT COUNT(*) FROM "user" WHERE role='admin') > 1)`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// SessionMeta records where a session came from, so its owner can recognise it
// in a list of their sessions and revoke the one they don't know. Every field is
// optional; sessions created before this was recorded simply have none of it.
type SessionMeta struct {
	CreatedAt string
	IP        string
	UserAgent string
}

// Session is one row of a user's session list. The token is deliberately not
// part of it: a page that rendered live session tokens would be handing out the
// credential it is supposed to be managing. ID is a short digest of the token,
// enough to name one session in a revoke request.
type Session struct {
	ID        string
	CreatedAt *string
	ExpiresAt string
	IP        *string
	UserAgent *string
}

// SessionID is the public identifier for a session token: the first bytes of
// its SHA-256, hex encoded. It is derived rather than stored so old sessions
// have one too, and it cannot be used to reconstruct the token.
func SessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt string, meta ...SessionMeta) error {
	var m SessionMeta
	if len(meta) > 0 {
		m = meta[0]
	}
	_, err := s.exec(ctx, `INSERT INTO session (token,user_id,expires_at,created_at,ip,user_agent)
		VALUES (?,?,?,?,?,?)`,
		token, userID, expiresAt, nullable(m.CreatedAt), nullable(m.IP), nullable(m.UserAgent))
	return err
}

// nullable turns an empty string into a SQL NULL, so "not recorded" and
// "recorded as empty" stay distinguishable in the session table.
func nullable(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// ListSessionsForUser returns a user's unexpired sessions, newest first. Rows
// with no created_at (written before it was recorded) sort last.
func (s *Store) ListSessionsForUser(ctx context.Context, userID int64) ([]Session, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.query(ctx, `SELECT token,created_at,expires_at,ip,user_agent FROM session
		WHERE user_id=? AND expires_at>? ORDER BY created_at DESC NULLS LAST, expires_at DESC`,
		userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var token string
		var sess Session
		if err := rows.Scan(&token, &sess.CreatedAt, &sess.ExpiresAt, &sess.IP, &sess.UserAgent); err != nil {
			return nil, err
		}
		sess.ID = SessionID(token)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteSessionByID revokes one of a user's sessions by its public id,
// reporting whether it found one. Scoped to the user so an id guessed or
// borrowed from elsewhere cannot revoke somebody else's session.
// The id is a digest, so the match has to be made in Go. The tokens are read
// out and the cursor closed before the delete runs: SQLite is held to a single
// connection, so a write issued with rows still open waits on a connection the
// caller is holding itself.
func (s *Store) DeleteSessionByID(ctx context.Context, userID int64, id string) (bool, error) {
	tokens, err := s.userSessionTokens(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, token := range tokens {
		if SessionID(token) == id {
			return true, s.DeleteSession(ctx, token)
		}
	}
	return false, nil
}

func (s *Store) userSessionTokens(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.query(ctx, `SELECT token FROM session WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, token string) (User, bool, error) {
	var u User
	now := time.Now().UTC().Format(time.RFC3339)
	err := s.queryRow(ctx, `SELECT u.id,u.username,u.password_hash,u.role FROM session s
		JOIN "user" u ON u.id=s.user_id WHERE s.token=? AND s.expires_at>?`, token, now).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.exec(ctx, `DELETE FROM session WHERE token=?`, token)
	return err
}

// DeleteSessionsForUser revokes every session belonging to a user except the
// one named by keep, which may be empty to revoke all of them.
//
// A password change that left the old sessions alive would not actually lock
// anyone out, so this runs with every change; keep is the session of the person
// making it, so they are not logged out of their own browser.
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID int64, keep string) error {
	_, err := s.exec(ctx, `DELETE FROM session WHERE user_id=? AND token<>?`, userID, keep)
	return err
}

// GetSetting reads a setting, decrypting it when it was stored encrypted. An
// unset key reads as the empty string; a value that cannot be decrypted is an
// error rather than an empty string, so a wrong or missing key looks like the
// misconfiguration it is instead of an integration that forgot its credentials.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	v, err := s.rawSetting(ctx, key)
	if err != nil || !strings.HasPrefix(v, encPrefix) {
		return v, err
	}
	if s.crypter == nil {
		return "", fmt.Errorf("setting %q is encrypted but no secret key is configured (NETIS_SECRET_KEY)", key)
	}
	return s.crypter.open(key, v)
}

// rawSetting reads a setting exactly as stored, encryption and all.
func (s *Store) rawSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.queryRow(ctx, `SELECT value FROM setting WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// SetSetting writes a setting, encrypting it first when the key names a
// credential and a secret key is configured.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if s.crypter != nil && value != "" && isSecretSetting(key) {
		sealed, err := s.crypter.seal(key, value)
		if err != nil {
			return err
		}
		value = sealed
	}
	_, err := s.exec(ctx, `INSERT INTO setting (key,value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
