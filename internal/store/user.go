package store

import (
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

func (s *Store) CreateUser(username, passwordHash, role string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO user (username,password_hash,role) VALUES (?,?,?)`,
		username, passwordHash, role)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByName(username string) (User, bool, error) {
	var u User
	err := s.DB.QueryRow(`SELECT id,username,password_hash,role FROM user WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, false, nil
		}
		return u, false, err
	}
	return u, true, nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT count(*) FROM user`).Scan(&n)
	return n, err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.DB.Query(`SELECT id,username,password_hash,role FROM user ORDER BY username`)
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

func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM user WHERE id=?`, id)
	return err
}

func (s *Store) CreateSession(token string, userID int64, expiresAt string) error {
	_, err := s.DB.Exec(`INSERT INTO session (token,user_id,expires_at) VALUES (?,?,?)`,
		token, userID, expiresAt)
	return err
}

func (s *Store) GetSession(token string) (User, bool, error) {
	var u User
	now := time.Now().UTC().Format(time.RFC3339)
	err := s.DB.QueryRow(`SELECT u.id,u.username,u.password_hash,u.role FROM session s
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

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM session WHERE token=?`, token)
	return err
}

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM setting WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO setting (key,value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
