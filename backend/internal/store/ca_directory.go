package store

import (
	"encoding/json"
	"strconv"
	"strings"

	"badgescanner/backend/internal/logins"
)

// CADirEntry mirrors mobile/app/.../api/CaApi.kt's CaDirEntry.
type CADirEntry struct {
	PK       int64   `json:"pk"`
	FullName string  `json:"fullName"`
	FTLogin  string  `json:"ftLogin"`
	FTId     string  `json:"ftId"`
	Badges   []int64 `json:"badges"`
}

// DisplayLogin mirrors CaDirEntry.displayLogin: ft_login, else a piscine
// name login.
func (e CADirEntry) DisplayLogin() string {
	if e.FTLogin != "" {
		return e.FTLogin
	}
	return logins.PiscineLoginFromName(e.FullName)
}

// IntraKey mirrors CaDirEntry.intraKey: ft_id when present, else the display login.
func (e CADirEntry) IntraKey() string {
	if e.FTId != "" {
		return e.FTId
	}
	return e.DisplayLogin()
}

func (e CADirEntry) IsResolvable() bool {
	return e.FTLogin != "" || e.FTId != "" || e.DisplayLogin() != ""
}

// IsListable mirrors CaDirEntry.isListable: resolvable, not a "3b3…"
// workstation/system account, not "[BH]" tagged.
func (e CADirEntry) IsListable() bool {
	if !e.IsResolvable() {
		return false
	}
	if strings.Contains(strings.ToLower(e.FullName), "[bh]") {
		return false
	}
	const prefix = "3b3"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.FullName)), prefix) {
		return false
	}
	if strings.HasPrefix(strings.ToLower(e.FTLogin), prefix) {
		return false
	}
	if strings.HasPrefix(strings.ToLower(e.DisplayLogin()), prefix) {
		return false
	}
	return true
}

func (s *Store) CADirectoryFetchedAt() (int64, error) {
	var fetchedAt int64
	err := s.DB.QueryRow(`SELECT fetched_at FROM ca_directory_meta WHERE id = 1`).Scan(&fetchedAt)
	if err != nil {
		return 0, nil // no rows yet -> 0
	}
	return fetchedAt, nil
}

func (s *Store) ListCADirectory() ([]CADirEntry, error) {
	rows, err := s.DB.Query(`SELECT pk, full_name, ft_login, ft_id, badges FROM ca_directory_entries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CADirEntry
	for rows.Next() {
		var e CADirEntry
		var badgesJSON string
		if err := rows.Scan(&e.PK, &e.FullName, &e.FTLogin, &e.FTId, &badgesJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(badgesJSON), &e.Badges)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReplaceCADirectory replaces the whole cached directory — the equivalent
// of CaDirectory.refresh's "kept" write, filtered to IsListable entries by
// the caller before this is invoked.
func (s *Store) ReplaceCADirectory(entries []CADirEntry, fetchedAt int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM ca_directory_entries`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO ca_directory_entries (pk, full_name, ft_login, ft_id, badges) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		badgesJSON, _ := json.Marshal(e.Badges)
		if _, err := stmt.Exec(e.PK, e.FullName, e.FTLogin, e.FTId, string(badgesJSON)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO ca_directory_meta (id, fetched_at) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET fetched_at = excluded.fetched_at`, fetchedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveCADirectoryEntry(pk int64) error {
	_, err := s.DB.Exec(`DELETE FROM ca_directory_entries WHERE pk = ?`, pk)
	return err
}

// FindByBadge is a cache-only lookup, mirroring CaDirectory.findByBadge:
// tries each candidate badge id, in order, against every entry's badges.
func (s *Store) FindByBadge(candidates []string) (CADirEntry, int64, bool, error) {
	entries, err := s.ListCADirectory()
	if err != nil {
		return CADirEntry{}, 0, false, err
	}
	for _, c := range candidates {
		n, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			continue
		}
		for _, e := range entries {
			for _, b := range e.Badges {
				if b == n {
					return e, n, true, nil
				}
			}
		}
	}
	return CADirEntry{}, 0, false, nil
}
