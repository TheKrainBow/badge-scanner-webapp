package service

import (
	"errors"
	"time"

	"badgescanner/backend/internal/store"
)

// RefreshCADirectory is the server-side port of CaDirectory.refresh: a full
// (slow) refetch of the CA user listing, kept only for IsListable entries,
// and — same rule as the Kotlin doc comment — clears every manual badge
// link, since a fresh directory supersedes hand-made links.
func (s *Service) RefreshCADirectory() (int, error) {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return 0, err
	}
	onPage := func(fetched, total int) {
		if s.Events != nil {
			s.Events.RefreshProgress("ca", fetched, total, "")
		}
	}
	fresh, err := s.CA.FetchAllUsers(s.caConfig(settings), 30, onPage)
	if err != nil {
		if s.Events != nil {
			s.Events.RefreshError("ca", err.Error())
		}
		return 0, err
	}

	var filtered []store.CADirEntry
	for _, e := range fresh {
		if e.IsListable() {
			filtered = append(filtered, e)
		}
	}

	if err := s.Store.ReplaceCADirectory(filtered, time.Now().UnixMilli()); err != nil {
		return 0, err
	}
	if err := s.Store.ClearManualLinks(); err != nil {
		return 0, err
	}
	if s.Events != nil {
		s.Events.RefreshComplete("ca", len(filtered))
	}
	return len(filtered), nil
}

type CADirectoryInfo struct {
	UserCount int   `json:"userCount"`
	FetchedAt int64 `json:"fetchedAt"`
}

func (s *Service) CADirectoryInfo() (CADirectoryInfo, error) {
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return CADirectoryInfo{}, err
	}
	fetchedAt, err := s.Store.CADirectoryFetchedAt()
	if err != nil {
		return CADirectoryInfo{}, err
	}
	return CADirectoryInfo{UserCount: len(entries), FetchedAt: fetchedAt}, nil
}

// RefreshAllIntraUsers bulk-refreshes the 42 profile for every login
// currently known via the CA directory cache — the bulk counterpart to
// RefreshUserProfile (users.go), reusing its per-entry core
// (refreshProfileForEntry) so the two never drift apart. Not transactional
// across users, matching the existing per-user refresh semantics: a
// failure on one login is recorded via that entry's profile_error flag
// (same as RefreshUserProfile already does) and doesn't abort the rest.
func (s *Service) RefreshAllIntraUsers() (int, error) {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return 0, err
	}
	if !settings.FTConfigured() {
		err := errors.New("42 API credentials are not configured (see Admin settings)")
		if s.Events != nil {
			s.Events.RefreshError("intra", err.Error())
		}
		return 0, err
	}
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return 0, err
	}
	cfg := s.intraConfig(settings)
	count := 0
	for i, e := range entries {
		if err := s.refreshProfileForEntry(cfg, e); err == nil {
			count++
		}
		if s.Events != nil {
			s.Events.RefreshProgress("intra", i+1, len(entries), e.DisplayLogin())
		}
	}
	if err := s.Store.ReplaceIntraBulkMeta(time.Now().UnixMilli()); err != nil {
		return count, err
	}
	if s.Events != nil {
		s.Events.RefreshComplete("intra", count)
	}
	return count, nil
}

// RefreshAllCoalitions bulk-refreshes just the coalition fields for every
// known login — the bulk counterpart to RefreshUserCoalition (users.go),
// same tolerate-per-user-failure semantics as RefreshAllIntraUsers above.
func (s *Service) RefreshAllCoalitions() (int, error) {
	settings, err := s.Store.GetSettings()
	if err != nil {
		return 0, err
	}
	if !settings.FTConfigured() {
		err := errors.New("42 API credentials are not configured (see Admin settings)")
		if s.Events != nil {
			s.Events.RefreshError("coalitions", err.Error())
		}
		return 0, err
	}
	entries, err := s.Store.ListCADirectory()
	if err != nil {
		return 0, err
	}
	cfg := s.intraConfig(settings)
	count := 0
	for i, e := range entries {
		if err := s.refreshCoalitionForEntry(cfg, e); err == nil {
			count++
		}
		if s.Events != nil {
			s.Events.RefreshProgress("coalitions", i+1, len(entries), e.DisplayLogin())
		}
	}
	if s.Events != nil {
		s.Events.RefreshComplete("coalitions", count)
	}
	return count, nil
}

// IntraBulkInfo reports the size of the intra cache and when it was last
// bulk-refreshed — parity with CADirectoryInfo for the admin dashboard's
// "42 users" section.
func (s *Service) IntraBulkInfo() (CADirectoryInfo, error) {
	all, err := s.Store.AllIntra()
	if err != nil {
		return CADirectoryInfo{}, err
	}
	fetchedAt, err := s.Store.IntraBulkFetchedAt()
	if err != nil {
		return CADirectoryInfo{}, err
	}
	return CADirectoryInfo{UserCount: len(all), FetchedAt: fetchedAt}, nil
}
