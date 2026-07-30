package store

// AppSettings mirrors mobile/app/.../data/Settings.kt's AppSettings — now a
// single shared row instead of per-device DataStore.
type AppSettings struct {
	CAEndpoint           string `json:"caEndpoint"`
	CAUsername           string `json:"caUsername"`
	CAPassword           string `json:"caPassword"`
	FTTokenURL           string `json:"ftTokenUrl"`
	FTEndpoint           string `json:"ftEndpoint"`
	FTUid                string `json:"ftUid"`
	FTSecret             string `json:"ftSecret"`
	CloserID             string `json:"closerId"`
	CampusID             string `json:"campusId"`
	DisplayDetailedScans bool   `json:"displayDetailedScans"`
}

func (a AppSettings) CAConfigured() bool {
	return a.CAEndpoint != "" && a.CAUsername != "" && a.CAPassword != ""
}

func (a AppSettings) FTConfigured() bool {
	return a.FTUid != "" && a.FTSecret != ""
}

func (s *Store) GetSettings() (AppSettings, error) {
	// Ensure the singleton row exists.
	if _, err := s.DB.Exec(`INSERT OR IGNORE INTO app_settings (id) VALUES (1)`); err != nil {
		return AppSettings{}, err
	}
	var a AppSettings
	var displayDetailed int
	err := s.DB.QueryRow(`SELECT ca_endpoint, ca_username, ca_password, ft_token_url, ft_endpoint,
		ft_uid, ft_secret, closer_id, campus_id, display_detailed_scans FROM app_settings WHERE id = 1`).
		Scan(&a.CAEndpoint, &a.CAUsername, &a.CAPassword, &a.FTTokenURL, &a.FTEndpoint,
			&a.FTUid, &a.FTSecret, &a.CloserID, &a.CampusID, &displayDetailed)
	a.DisplayDetailedScans = displayDetailed != 0
	return a, err
}

func (s *Store) SaveSettings(a AppSettings) error {
	_, err := s.DB.Exec(`INSERT INTO app_settings (id, ca_endpoint, ca_username, ca_password, ft_token_url,
		ft_endpoint, ft_uid, ft_secret, closer_id, campus_id, display_detailed_scans)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET ca_endpoint=excluded.ca_endpoint, ca_username=excluded.ca_username,
			ca_password=excluded.ca_password, ft_token_url=excluded.ft_token_url, ft_endpoint=excluded.ft_endpoint,
			ft_uid=excluded.ft_uid, ft_secret=excluded.ft_secret, closer_id=excluded.closer_id,
			campus_id=excluded.campus_id, display_detailed_scans=excluded.display_detailed_scans`,
		a.CAEndpoint, a.CAUsername, a.CAPassword, a.FTTokenURL, a.FTEndpoint,
		a.FTUid, a.FTSecret, a.CloserID, a.CampusID, a.DisplayDetailedScans,
	)
	return err
}
