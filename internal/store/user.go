package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO user (username,password_hash,role) VALUES (?,?,?)`,
		username, passwordHash, role)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByName(ctx context.Context, username string) (User, bool, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,username,password_hash,role FROM user WHERE username=?`, username).
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
	res, err := s.DB.ExecContext(ctx, `INSERT INTO user (username,password_hash,role)
		SELECT ?,?,'admin' WHERE NOT EXISTS (SELECT 1 FROM user)`,
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

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM user`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,username,password_hash,role FROM user ORDER BY username`)
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
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user WHERE id=?`, id)
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
	res, err := s.DB.ExecContext(ctx, `DELETE FROM user WHERE id=? AND
		(role<>'admin' OR (SELECT COUNT(*) FROM user WHERE role='admin') > 1)`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, expiresAt string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO session (token,user_id,expires_at) VALUES (?,?,?)`,
		token, userID, expiresAt)
	return err
}

func (s *Store) GetSession(ctx context.Context, token string) (User, bool, error) {
	var u User
	now := time.Now().UTC().Format(time.RFC3339)
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.username,u.password_hash,u.role FROM session s
		JOIN user u ON u.id=s.user_id WHERE s.token=? AND s.expires_at>?`, token, now).
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
	_, err := s.DB.ExecContext(ctx, `DELETE FROM session WHERE token=?`, token)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM setting WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO setting (key,value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
