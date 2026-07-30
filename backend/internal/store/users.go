package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    int64
}

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("already exists")

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash string, isAdmin bool) (User, error) {
	res, err := s.DB.Exec(
		`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, isAdmin, time.Now().UnixMilli(),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return User{}, ErrDuplicate
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetUserByID(id)
}

func (s *Store) GetUserByUsername(username string) (User, error) {
	return s.scanUser(s.DB.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = ?`, username,
	))
}

func (s *Store) GetUserByID(id int64) (User, error) {
	return s.scanUser(s.DB.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE id = ?`, id,
	))
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	var isAdmin int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	u.IsAdmin = isAdmin != 0
	return u, nil
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.DB.Query(`SELECT id, username, password_hash, is_admin, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var isAdmin int
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.IsAdmin = isAdmin != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) SetUserPassword(id int64, passwordHash string) error {
	_, err := s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

func (s *Store) SetUserAdmin(id int64, isAdmin bool) error {
	_, err := s.DB.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, isAdmin, id)
	return err
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
