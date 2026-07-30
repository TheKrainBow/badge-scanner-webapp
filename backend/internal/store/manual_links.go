package store

import (
	"database/sql"
	"errors"
	"strings"
)

// ManualLink lookups mirror mobile/app/.../data/ManualLinkStore.kt: badges
// the CA doesn't recognise, manually pointed at a student's login. Cleared
// whenever the CA directory is refetched (see ClearManualLinks), so they
// never mask a proper CA record that later appears.
func (s *Store) GetManualLink(uidHex string) (string, bool, error) {
	var login string
	err := s.DB.QueryRow(`SELECT login FROM manual_links WHERE uid_hex = ?`, strings.ToUpper(uidHex)).Scan(&login)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return login, true, nil
}

func (s *Store) PutManualLink(uidHex, login string) error {
	_, err := s.DB.Exec(`INSERT INTO manual_links (uid_hex, login) VALUES (?, ?)
		ON CONFLICT(uid_hex) DO UPDATE SET login = excluded.login`,
		strings.ToUpper(uidHex), strings.ToLower(strings.TrimSpace(login)))
	return err
}

func (s *Store) ClearManualLinks() error {
	_, err := s.DB.Exec(`DELETE FROM manual_links`)
	return err
}
