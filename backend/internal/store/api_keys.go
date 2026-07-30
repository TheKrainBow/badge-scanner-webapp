package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// APIKey is a scoped credential for external callers (the webapp frontend
// itself, the C badge-lookup client, etc). Permissions is a comma-separated
// list of scope strings — currently just "full" (everything the old static
// X-Client-Id/Secret pair used to grant) and "lookup" (POST /api/lookup
// only). See internal/auth's RequireAPIKeyPermission for enforcement.
// RateLimitPerMinute/RateLimitPerHour (0 = unlimited) cap how many
// /api/lookup calls this key may make in a trailing window — see
// CountAPIKeyUsageSince and internal/api's performLookup.
type APIKey struct {
	ID                 int64
	ClientID           string
	SecretHash         string
	Name               string
	Permissions        string
	CreatedAt          int64
	LastUsedAt         int64
	RateLimitPerMinute int
	RateLimitPerHour   int
}

// Scopes splits Permissions into its individual scope strings.
func (k APIKey) Scopes() []string {
	var out []string
	for _, p := range strings.Split(k.Permissions, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

const apiKeyColumns = `id, client_id, secret_hash, name, permissions, created_at, last_used_at, rate_limit_per_minute, rate_limit_per_hour`

func (s *Store) CountAPIKeys() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM api_keys`).Scan(&n)
	return n, err
}

func (s *Store) CreateAPIKey(clientID, secretHash, name, permissions string, rateLimitPerMinute, rateLimitPerHour int) (APIKey, error) {
	res, err := s.DB.Exec(
		`INSERT INTO api_keys (client_id, secret_hash, name, permissions, created_at, last_used_at, rate_limit_per_minute, rate_limit_per_hour)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		clientID, secretHash, name, permissions, time.Now().UnixMilli(), rateLimitPerMinute, rateLimitPerHour,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return APIKey{}, ErrDuplicate
		}
		return APIKey{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetAPIKeyByID(id)
}

func (s *Store) GetAPIKeyByID(id int64) (APIKey, error) {
	return s.scanAPIKey(s.DB.QueryRow(`SELECT `+apiKeyColumns+` FROM api_keys WHERE id = ?`, id))
}

func (s *Store) GetAPIKeyByClientID(clientID string) (APIKey, error) {
	return s.scanAPIKey(s.DB.QueryRow(`SELECT `+apiKeyColumns+` FROM api_keys WHERE client_id = ?`, clientID))
}

func (s *Store) scanAPIKey(row *sql.Row) (APIKey, error) {
	var k APIKey
	if err := row.Scan(&k.ID, &k.ClientID, &k.SecretHash, &k.Name, &k.Permissions, &k.CreatedAt, &k.LastUsedAt,
		&k.RateLimitPerMinute, &k.RateLimitPerHour); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APIKey{}, ErrNotFound
		}
		return APIKey{}, err
	}
	return k, nil
}

func (s *Store) ListAPIKeys() ([]APIKey, error) {
	rows, err := s.DB.Query(`SELECT ` + apiKeyColumns + ` FROM api_keys ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.ClientID, &k.SecretHash, &k.Name, &k.Permissions, &k.CreatedAt, &k.LastUsedAt,
			&k.RateLimitPerMinute, &k.RateLimitPerHour); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

func (s *Store) TouchAPIKeyLastUsed(id int64) error {
	_, err := s.DB.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	return err
}

// UpdateAPIKey backs the API key edit page: name, permissions, and rate
// limits are all editable after creation (client id/secret are not — a key
// with different credentials is a different key, mirroring how account
// passwords are reset via a dedicated action rather than editing in place).
func (s *Store) UpdateAPIKey(id int64, name, permissions string, rateLimitPerMinute, rateLimitPerHour int) error {
	_, err := s.DB.Exec(
		`UPDATE api_keys SET name = ?, permissions = ?, rate_limit_per_minute = ?, rate_limit_per_hour = ? WHERE id = ?`,
		name, permissions, rateLimitPerMinute, rateLimitPerHour, id,
	)
	return err
}

// APIKeyUsage is one recorded /api/lookup call for a given key — see
// RecordAPIKeyUsage. Not a general request audit log (that would mostly
// capture the frontend's own "full"-scope key's routine page-load traffic,
// which isn't useful "usage" to show an operator); lookups are the one
// action a restricted "lookup"-scope key can take at all. CoalitionName/
// Color are a snapshot at lookup time (same philosophy as scan_history's
// coalition fields), not a live join — coalition assignment can change
// later without rewriting history.
type APIKeyUsage struct {
	ID                int64
	APIKeyID          int64
	Timestamp         int64
	UIDHex            string
	Found             bool
	Login             string
	CoalitionName     string
	CoalitionColor    string
	CoalitionImageURL string
	PhotoURL          string
}

// RecordAPIKeyUsage returns the new row's id — callers that echo it back to
// the caller (performLookup does, for live taps) let a client key its local
// per-badge state (notes, "to handle") consistently whether it learned
// about that tap live or later via ListAPIKeyUsage/ListAllAPIKeyUsage.
func (s *Store) RecordAPIKeyUsage(apiKeyID int64, uidHex string, found bool, login, coalitionName, coalitionColor, coalitionImageURL, photoURL string) (int64, error) {
	res, err := s.DB.Exec(
		`INSERT INTO api_key_usage (api_key_id, timestamp, uid_hex, found, login, coalition_name, coalition_color, coalition_image_url, photo_url)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		apiKeyID, time.Now().UnixMilli(), uidHex, found, login, coalitionName, coalitionColor, coalitionImageURL, photoURL,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CountAPIKeyUsageSince powers rate-limit enforcement: how many lookups has
// this key made since sinceMillis (a trailing 1-minute or 1-hour window).
func (s *Store) CountAPIKeyUsageSince(apiKeyID int64, sinceMillis int64) (int, error) {
	var n int
	err := s.DB.QueryRow(
		`SELECT COUNT(*) FROM api_key_usage WHERE api_key_id = ? AND timestamp >= ?`, apiKeyID, sinceMillis,
	).Scan(&n)
	return n, err
}

func (s *Store) ListAPIKeyUsage(apiKeyID int64, limit int) ([]APIKeyUsage, error) {
	rows, err := s.DB.Query(
		`SELECT id, api_key_id, timestamp, uid_hex, found, login, coalition_name, coalition_color, coalition_image_url, photo_url FROM api_key_usage
			WHERE api_key_id = ? ORDER BY timestamp DESC LIMIT ?`, apiKeyID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeyUsage
	for rows.Next() {
		var u APIKeyUsage
		var found int
		if err := rows.Scan(&u.ID, &u.APIKeyID, &u.Timestamp, &u.UIDHex, &found, &u.Login, &u.CoalitionName, &u.CoalitionColor, &u.CoalitionImageURL, &u.PhotoURL); err != nil {
			return nil, err
		}
		u.Found = found != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// APIKeyUsageEntry is one row of the cross-key lookup history (the History
// page): a usage record plus the name of the key that made it.
type APIKeyUsageEntry struct {
	APIKeyUsage
	APIKeyName string
}

// ListAllAPIKeyUsage backs the History page: every lookup across every key,
// newest first.
func (s *Store) ListAllAPIKeyUsage(limit int) ([]APIKeyUsageEntry, error) {
	rows, err := s.DB.Query(
		`SELECT u.id, u.api_key_id, u.timestamp, u.uid_hex, u.found, u.login, u.coalition_name, u.coalition_color, u.coalition_image_url, u.photo_url, k.name
			FROM api_key_usage u JOIN api_keys k ON k.id = u.api_key_id
			ORDER BY u.timestamp DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeyUsageEntry
	for rows.Next() {
		var e APIKeyUsageEntry
		var found int
		if err := rows.Scan(&e.ID, &e.APIKeyID, &e.Timestamp, &e.UIDHex, &found, &e.Login, &e.CoalitionName, &e.CoalitionColor, &e.CoalitionImageURL, &e.PhotoURL, &e.APIKeyName); err != nil {
			return nil, err
		}
		e.Found = found != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteAPIKeyUsage removes all usage rows for a key — called when the key
// itself is deleted, so orphaned usage history doesn't linger.
func (s *Store) DeleteAPIKeyUsage(apiKeyID int64) error {
	_, err := s.DB.Exec(`DELETE FROM api_key_usage WHERE api_key_id = ?`, apiKeyID)
	return err
}
