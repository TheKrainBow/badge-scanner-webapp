package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// IntraTTL is the freshness window for cached intra lookups — 12h, exactly
// like IntraCache.kt's TTL_MS.
const IntraTTL = 12 * time.Hour

// IntraInfo mirrors mobile/app/.../data/IntraCache.kt's IntraInfo.
type IntraInfo struct {
	FetchedAt         int64    `json:"fetchedAt"`
	Login             *string  `json:"login,omitempty"`
	FTId              *string  `json:"ftId,omitempty"`
	PhotoURL          *string  `json:"photoUrl,omitempty"`
	UserType          *string  `json:"userType,omitempty"`
	CoalitionName     *string  `json:"coalitionName,omitempty"`
	CoalitionColor    *string  `json:"coalitionColor,omitempty"`
	CoalitionImageURL *string  `json:"coalitionImageUrl,omitempty"`
	CoalitionID       *int     `json:"coalitionId,omitempty"`
	CoalitionsUserID  *int     `json:"coalitionsUserId,omitempty"`
	Location          *string  `json:"location,omitempty"`
	Level             *float64 `json:"level,omitempty"`
	CurrentProjects   []string `json:"currentProjects"`
	ProfileError      bool     `json:"profileError"`
	CoalitionError    bool     `json:"coalitionError"`
}

func (s *Store) PeekIntra(key string) (IntraInfo, bool, error) {
	key = strings.ToLower(key)
	var i IntraInfo
	var login, ftID, photoURL, userType, coName, coColor, coImage, location sql.NullString
	var coID, coUserID sql.NullInt64
	var level sql.NullFloat64
	var projectsJSON string
	var profileErr, coErr int

	err := s.DB.QueryRow(`SELECT fetched_at, login, ft_id, photo_url, user_type, coalition_name,
		coalition_color, coalition_image_url, coalition_id, coalitions_user_id, location, level,
		current_projects, profile_error, coalition_error FROM intra_cache WHERE cache_key = ?`, key).
		Scan(&i.FetchedAt, &login, &ftID, &photoURL, &userType, &coName, &coColor, &coImage,
			&coID, &coUserID, &location, &level, &projectsJSON, &profileErr, &coErr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IntraInfo{}, false, nil
		}
		return IntraInfo{}, false, err
	}
	i.Login = nullToPtr(login)
	i.FTId = nullToPtr(ftID)
	i.PhotoURL = nullToPtr(photoURL)
	i.UserType = nullToPtr(userType)
	i.CoalitionName = nullToPtr(coName)
	i.CoalitionColor = nullToPtr(coColor)
	i.CoalitionImageURL = nullToPtr(coImage)
	i.Location = nullToPtr(location)
	if coID.Valid {
		v := int(coID.Int64)
		i.CoalitionID = &v
	}
	if coUserID.Valid {
		v := int(coUserID.Int64)
		i.CoalitionsUserID = &v
	}
	if level.Valid {
		v := level.Float64
		i.Level = &v
	}
	_ = json.Unmarshal([]byte(projectsJSON), &i.CurrentProjects)
	i.ProfileError = profileErr != 0
	i.CoalitionError = coErr != 0
	return i, true, nil
}

// GetFreshIntra returns cached info only if within IntraTTL, else (false).
func (s *Store) GetFreshIntra(key string) (IntraInfo, bool, error) {
	info, ok, err := s.PeekIntra(key)
	if err != nil || !ok {
		return IntraInfo{}, false, err
	}
	age := time.Since(time.UnixMilli(info.FetchedAt))
	if age > IntraTTL {
		return IntraInfo{}, false, nil
	}
	return info, true, nil
}

func (s *Store) PutIntra(key string, i IntraInfo) error {
	key = strings.ToLower(key)
	projectsJSON, _ := json.Marshal(i.CurrentProjects)
	_, err := s.DB.Exec(`INSERT INTO intra_cache (cache_key, fetched_at, login, ft_id, photo_url, user_type,
		coalition_name, coalition_color, coalition_image_url, coalition_id, coalitions_user_id, location,
		level, current_projects, profile_error, coalition_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cache_key) DO UPDATE SET fetched_at=excluded.fetched_at, login=excluded.login,
			ft_id=excluded.ft_id, photo_url=excluded.photo_url, user_type=excluded.user_type,
			coalition_name=excluded.coalition_name, coalition_color=excluded.coalition_color,
			coalition_image_url=excluded.coalition_image_url, coalition_id=excluded.coalition_id,
			coalitions_user_id=excluded.coalitions_user_id, location=excluded.location, level=excluded.level,
			current_projects=excluded.current_projects, profile_error=excluded.profile_error,
			coalition_error=excluded.coalition_error`,
		key, i.FetchedAt, ptrToNull(i.Login), ptrToNull(i.FTId), ptrToNull(i.PhotoURL), ptrToNull(i.UserType),
		ptrToNull(i.CoalitionName), ptrToNull(i.CoalitionColor), ptrToNull(i.CoalitionImageURL),
		intPtrToNull(i.CoalitionID), intPtrToNull(i.CoalitionsUserID), ptrToNull(i.Location),
		floatPtrToNull(i.Level), string(projectsJSON), i.ProfileError, i.CoalitionError,
	)
	return err
}

// AllIntra returns the whole cache keyed by lowercased key, for user-list aggregation.
func (s *Store) AllIntra() (map[string]IntraInfo, error) {
	rows, err := s.DB.Query(`SELECT cache_key FROM intra_cache`)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	out := map[string]IntraInfo{}
	for _, k := range keys {
		info, ok, err := s.PeekIntra(k)
		if err != nil {
			return nil, err
		}
		if ok {
			out[k] = info
		}
	}
	return out, nil
}

// ReplaceIntraBulkMeta upserts the last-bulk-refresh timestamp — parity
// with ca_directory_meta/ReplaceCADirectory (ca_directory.go), so the
// "Refetch 42 users" admin section can show a last-fetched time the same
// way the CA directory section does.
func (s *Store) ReplaceIntraBulkMeta(fetchedAt int64) error {
	_, err := s.DB.Exec(`INSERT INTO intra_bulk_meta (id, fetched_at) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET fetched_at = excluded.fetched_at`, fetchedAt)
	return err
}

func (s *Store) IntraBulkFetchedAt() (int64, error) {
	var fetchedAt int64
	err := s.DB.QueryRow(`SELECT fetched_at FROM intra_bulk_meta WHERE id = 1`).Scan(&fetchedAt)
	if err != nil {
		return 0, nil // no rows yet -> 0
	}
	return fetchedAt, nil
}

func nullToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func ptrToNull(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func intPtrToNull(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

func floatPtrToNull(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}
