// Package store is the SQLite-backed persistence layer: user accounts, the
// shared CA/42 settings, the CA directory cache, the 42-intra cache, manual
// badge links and the scan history. It replaces the mobile app's per-device
// DataStore/JSON-file storage with a shared multi-operator store, while
// keeping the same cache semantics (12h intra TTL, CA directory refreshed
// only on demand, manual links cleared on CA refresh, etc).
package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite only allows one writer at a time; a single connection avoids
	// "database is locked" errors under concurrent requests.
	db.SetMaxOpenConns(1)

	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			ca_endpoint TEXT NOT NULL DEFAULT '',
			ca_username TEXT NOT NULL DEFAULT '',
			ca_password TEXT NOT NULL DEFAULT '',
			ft_token_url TEXT NOT NULL DEFAULT 'https://api.intra.42.fr/oauth/token',
			ft_endpoint TEXT NOT NULL DEFAULT 'https://api.intra.42.fr/v2',
			ft_uid TEXT NOT NULL DEFAULT '',
			ft_secret TEXT NOT NULL DEFAULT '',
			closer_id TEXT NOT NULL DEFAULT '',
			campus_id TEXT NOT NULL DEFAULT '41',
			display_detailed_scans INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS ca_directory_entries (
			pk INTEGER PRIMARY KEY,
			full_name TEXT NOT NULL DEFAULT '',
			ft_login TEXT NOT NULL DEFAULT '',
			ft_id TEXT NOT NULL DEFAULT '',
			badges TEXT NOT NULL DEFAULT '[]'
		)`,
		`CREATE TABLE IF NOT EXISTS ca_directory_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			fetched_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS intra_cache (
			cache_key TEXT PRIMARY KEY,
			fetched_at INTEGER NOT NULL DEFAULT 0,
			login TEXT,
			ft_id TEXT,
			photo_url TEXT,
			user_type TEXT,
			coalition_name TEXT,
			coalition_color TEXT,
			coalition_image_url TEXT,
			coalition_id INTEGER,
			coalitions_user_id INTEGER,
			location TEXT,
			level REAL,
			current_projects TEXT NOT NULL DEFAULT '[]',
			profile_error INTEGER NOT NULL DEFAULT 0,
			coalition_error INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS manual_links (
			uid_hex TEXT PRIMARY KEY,
			login TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id TEXT NOT NULL UNIQUE,
			secret_hash TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			permissions TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL DEFAULT 0,
			rate_limit_per_minute INTEGER NOT NULL DEFAULT 0,
			rate_limit_per_hour INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS intra_bulk_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			fetched_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS api_key_usage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			api_key_id INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			uid_hex TEXT NOT NULL DEFAULT '',
			found INTEGER NOT NULL DEFAULT 0,
			login TEXT NOT NULL DEFAULT '',
			coalition_name TEXT NOT NULL DEFAULT '',
			coalition_color TEXT NOT NULL DEFAULT '',
			coalition_image_url TEXT NOT NULL DEFAULT '',
			photo_url TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_usage_key_ts ON api_key_usage(api_key_id, timestamp DESC)`,
		`CREATE TABLE IF NOT EXISTS scan_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			uid_hex TEXT NOT NULL,
			mifare_hex TEXT NOT NULL DEFAULT '',
			wiegand TEXT NOT NULL DEFAULT '',
			login TEXT,
			ft_id TEXT,
			photo_url TEXT,
			error TEXT,
			user_type TEXT,
			coalition_name TEXT,
			coalition_color TEXT,
			coalition_image_url TEXT,
			reason TEXT,
			is_blame INTEGER NOT NULL DEFAULT 0,
			blame_status TEXT NOT NULL DEFAULT 'NOT_HANDLED',
			tig_duration TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scan_history_timestamp ON scan_history(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_scan_history_login ON scan_history(login)`,
	}
	for _, stmt := range stmts {
		if _, err := s.DB.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w (stmt: %s)", err, stmt)
		}
	}

	// Columns added after api_keys/api_key_usage's original CREATE TABLE
	// already shipped: on a database created before this change, the
	// CREATE TABLE IF NOT EXISTS statements above are no-ops, so these
	// columns need an explicit, idempotent ALTER TABLE to actually reach
	// it. New databases already get them from the CREATE TABLE above (this
	// just no-ops there, "duplicate column name").
	additions := []struct{ table, column, def string }{
		{"api_keys", "rate_limit_per_minute", "INTEGER NOT NULL DEFAULT 0"},
		{"api_keys", "rate_limit_per_hour", "INTEGER NOT NULL DEFAULT 0"},
		{"api_key_usage", "coalition_name", "TEXT NOT NULL DEFAULT ''"},
		{"api_key_usage", "coalition_color", "TEXT NOT NULL DEFAULT ''"},
		{"api_key_usage", "photo_url", "TEXT NOT NULL DEFAULT ''"},
		{"api_key_usage", "coalition_image_url", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, a := range additions {
		if err := s.migrateAddColumn(a.table, a.column, a.def); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateAddColumn(table, column, def string) error {
	_, err := s.DB.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, def))
	if err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf("migrate: add column %s.%s: %w", table, column, err)
	}
	return nil
}

func isDuplicateColumnErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}
